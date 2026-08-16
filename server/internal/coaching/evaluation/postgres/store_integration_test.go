package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testUserA    = "10000000-0000-4000-8000-000000000001"
	testUserB    = "10000000-0000-4000-8000-000000000002"
	testSessionA = "20000000-0000-4000-8000-000000000001"
	testSessionB = "20000000-0000-4000-8000-000000000002"
	testTurnA    = "30000000-0000-4000-8000-000000000001"
	testTurnB    = "30000000-0000-4000-8000-000000000002"
	testTurnC    = "30000000-0000-4000-8000-000000000003"
	testQuestion = "40000000-0000-4000-8000-000000000001"
)

func TestEvaluationCoreMigrationLeavesOnlyEvaluationAuthorities(t *testing.T) {
	pool := evaluationTestDatabase(t)
	rows, err := pool.Query(context.Background(), `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = current_schema()
		  AND (table_name LIKE 'evaluation%' OR table_name LIKE 'review%')
		ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list Evaluation and Review tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table names: %v", err)
	}
	want := []string{"evaluation_feedback_items", "evaluations"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("Evaluation/Review authorities = %v, want %v", names, want)
	}
}

func TestStoreQueueClaimCheckpointDeferCompleteAndItems(t *testing.T) {
	pool := evaluationTestDatabase(t)
	insertEvaluationTestUser(t, pool, testUserA, "evaluation-a@example.com")
	store := mustStore(t, pool)
	command := speechQueueCommand(t, testUserA, testTurnA, testSessionA, nil)

	type queueResult struct {
		record   evaluation.Record
		replayed bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan queueResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			record, replayed, err := store.Queue(context.Background(), command)
			results <- queueResult{record: record, replayed: replayed, err: err}
		}()
	}
	ready.Wait()
	close(start)
	first := <-results
	second := <-results
	for _, result := range []queueResult{first, second} {
		if result.err != nil {
			t.Fatalf("Queue: %v", result.err)
		}
	}
	if first.record.ID != second.record.ID || first.replayed == second.replayed {
		t.Fatalf("concurrent queue results = %#v, %#v", first, second)
	}
	changedConfig := command
	changedLineage, changedConfigHash, err := evaluation.EncodeStrict(
		evaluation.ConfigLineage{
			SchemaVersion:   evaluation.ConfigLineageSchemaVersion,
			StrategyRef:     "speech-feedback/v1",
			PipelineVersion: "speech-evaluation/v2",
			PromptVersion:   "speech-feedback/v2",
			ResultSchema:    "speech-feedback/v1",
			Provider:        "qianwen",
			Model:           "qwen-max",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	changedConfig.ConfigLineage = changedLineage
	changedConfig.ConfigHash = changedConfigHash
	changedConfig.InputSnapshot, changedConfig.InputHash, err = evaluation.EncodeStrict(
		evaluation.SpeechInputSnapshot{
			SchemaVersion: evaluation.SpeechInputSchemaVersion,
			Transcript:    "A later caller supplied different source payload.",
			EvidenceRefID: testTurnA,
			QuestionID:    testQuestion,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	replayedRecord, replayed, err := store.Queue(context.Background(), changedConfig)
	if err != nil || !replayed || replayedRecord.InputHash != command.InputHash ||
		replayedRecord.ConfigHash != command.ConfigHash ||
		string(replayedRecord.ConfigLineage) != string(command.ConfigLineage) {
		t.Fatalf("Queue(changed config) = %#v, %v, %v", replayedRecord, replayed, err)
	}
	conflict := command
	conflict.ContextID = testSessionB
	if _, _, err := store.Queue(context.Background(), conflict); !errors.Is(
		err, evaluation.ErrIdempotencyConflict,
	) {
		t.Fatalf("Queue(conflict) = %v", err)
	}

	lane := speechLane(3)
	claim, err := store.ClaimNext(context.Background(), lane)
	if err != nil || claim.AttemptCount != 1 {
		t.Fatalf("ClaimNext = %#v, %v", claim, err)
	}
	pronunciation := 91.0
	checkpointInput, checkpointHash := speechInput(t, testTurnA,
		&evaluation.AcousticCheckpoint{
			Status:          evaluation.AcousticAssessed,
			Pronunciation:   &pronunciation,
			Provider:        "iflytek",
			ProviderSession: "ise-session-1",
		})
	checkpointed, err := store.CheckpointSnapshot(
		context.Background(), evaluation.SnapshotCheckpoint{
			UserID: testUserA, ID: claim.ID, LeaseToken: claim.LeaseToken,
			InputSnapshot: checkpointInput, InputHash: checkpointHash,
		},
	)
	if err != nil || checkpointed.InputHash != checkpointHash {
		t.Fatalf("CheckpointSnapshot = %#v, %v", checkpointed, err)
	}
	if err := store.DeferClaim(context.Background(), evaluation.Deferral{
		UserID: testUserA, ID: claim.ID, LeaseToken: claim.LeaseToken,
		AvailableAt: time.Now().UTC().Add(-time.Second),
	}); err != nil {
		t.Fatalf("DeferClaim: %v", err)
	}
	claim, err = store.ClaimNext(context.Background(), lane)
	if err != nil || claim.AttemptCount != 1 {
		t.Fatalf("ClaimNext after deferral = %#v, %v", claim, err)
	}
	result := speechResult(t, *mustDecodeSpeechInput(t, claim.InputSnapshot).Acoustic)
	item := evaluation.FeedbackItemDraft{
		Category: "CORRECTION", Severity: "MEDIUM",
		Evidence: evaluation.FeedbackEvidence{
			EvidenceRefID: testTurnA, StartUTF8Byte: 0,
			EndUTF8Byte: 5, OriginalExcerpt: "Hello",
		},
		Recommendation: "Use a complete sentence.",
		Correction:     "Hello, I would like to explain.",
		RepracticeMode: "SAME_QUESTION",
	}
	if err := store.CompleteClaim(context.Background(), evaluation.Completion{
		UserID: testUserA, ID: claim.ID, LeaseToken: claim.LeaseToken,
		Result: result, Items: []evaluation.FeedbackItemDraft{item},
	}); err != nil {
		t.Fatalf("CompleteClaim: %v", err)
	}
	if err := store.CompleteClaim(context.Background(), evaluation.Completion{
		UserID: testUserA, ID: claim.ID, LeaseToken: claim.LeaseToken,
		Result: result,
	}); !errors.Is(err, evaluation.ErrClaimLost) {
		t.Fatalf("stale CompleteClaim = %v", err)
	}
	items, err := store.ListFeedbackItems(context.Background(), testUserA, claim.ID)
	if err != nil || len(items) != 1 || items[0].Position != 1 ||
		items[0].Evidence.EvidenceRefID != testTurnA {
		t.Fatalf("ListFeedbackItems = %#v, %v", items, err)
	}
	again, err := store.ListFeedbackItems(context.Background(), testUserA, claim.ID)
	if err != nil || again[0].ID != items[0].ID {
		t.Fatalf("feedback item identity changed: %#v, %v", again, err)
	}
	afterCheckpoint, replayed, err := store.Queue(context.Background(), changedConfig)
	if err != nil || !replayed || afterCheckpoint.ID != claim.ID ||
		afterCheckpoint.InputHash != checkpointHash ||
		afterCheckpoint.ConfigHash != command.ConfigHash ||
		string(afterCheckpoint.ConfigLineage) != string(command.ConfigLineage) {
		t.Fatalf(
			"Queue(changed config after checkpoint) = %#v, %v, %v",
			afterCheckpoint, replayed, err,
		)
	}
}

func TestStoreRetryableFinalAndExpiredClaimsConverge(t *testing.T) {
	pool := evaluationTestDatabase(t)
	insertEvaluationTestUser(t, pool, testUserA, "evaluation-fail@example.com")
	store := mustStore(t, pool)

	retryCommand := speechQueueCommand(t, testUserA, testTurnA, testSessionA, nil)
	if _, _, err := store.Queue(context.Background(), retryCommand); err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(context.Background(), speechLane(3))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FailClaim(context.Background(), evaluation.Failure{
		UserID: testUserA, ID: claim.ID, LeaseToken: claim.LeaseToken,
		Error: evaluation.JobError{
			Code: "PROVIDER_UNAVAILABLE", Retryable: true, Message: "retry",
		},
		RetryAt: time.Now().UTC().Add(-time.Second), MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("FailClaim(retryable): %v", err)
	}
	reclaimed, err := store.ClaimNext(context.Background(), speechLane(3))
	if err != nil || reclaimed.AttemptCount != 2 ||
		reclaimed.LeaseToken == claim.LeaseToken {
		t.Fatalf("reclaimed = %#v, %v", reclaimed, err)
	}
	if err := store.FailClaim(context.Background(), evaluation.Failure{
		UserID: testUserA, ID: reclaimed.ID, LeaseToken: reclaimed.LeaseToken,
		Error: evaluation.JobError{
			Code: "INVALID_PROVIDER_RESULT", Retryable: false, Message: "terminal",
		},
		RetryAt: time.Now().UTC(), MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("FailClaim(final): %v", err)
	}
	failed, err := store.GetRecordBySource(
		context.Background(), testUserA, evaluation.KindPracticeTurnFeedback, testTurnA,
	)
	if err != nil || failed.Status != evaluation.JobFailed ||
		failed.Error == nil || failed.Error.Code != "INVALID_PROVIDER_RESULT" {
		t.Fatalf("failed record = %#v, %v", failed, err)
	}

	crashCommand := speechQueueCommand(t, testUserA, testTurnB, testSessionA, nil)
	crashCommand.AvailableAt = time.Now().UTC().Add(time.Millisecond)
	if _, _, err := store.Queue(context.Background(), crashCommand); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	crashed, err := store.ClaimNext(context.Background(), speechLane(1))
	if err != nil {
		t.Fatal(err)
	}
	expireClaim(t, pool, crashed.ID)
	if _, err := store.ClaimNext(context.Background(), speechLane(1)); !errors.Is(err, evaluation.ErrNotFound) {
		t.Fatalf("ClaimNext(final expired) = %v", err)
	}
	converged, err := store.GetRecordBySource(
		context.Background(), testUserA, evaluation.KindPracticeTurnFeedback, testTurnB,
	)
	if err != nil || converged.Status != evaluation.JobFailed ||
		converged.Error == nil || converged.Error.Code != "LEASE_EXPIRED" {
		t.Fatalf("expired record = %#v, %v", converged, err)
	}
}

func TestStoreSessionAcousticsExposePendingFailedAndAssessed(t *testing.T) {
	pool := evaluationTestDatabase(t)
	insertEvaluationTestUser(t, pool, testUserA, "evaluation-acoustic@example.com")
	store := mustStore(t, pool)

	failedCommand := speechQueueCommand(t, testUserA, testTurnA, testSessionA, nil)
	if _, _, err := store.Queue(context.Background(), failedCommand); err != nil {
		t.Fatal(err)
	}
	failedClaim, err := store.ClaimNext(context.Background(), speechLane(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FailClaim(context.Background(), evaluation.Failure{
		UserID: testUserA, ID: failedClaim.ID, LeaseToken: failedClaim.LeaseToken,
		Error: evaluation.JobError{
			Code: "ACOUSTIC_PROVIDER_FAILED", Retryable: false, Message: "failed",
		},
		RetryAt: time.Now().UTC(), MaxAttempts: 1,
	}); err != nil {
		t.Fatal(err)
	}

	pronunciation := 87.0
	assessed := evaluation.AcousticCheckpoint{
		Status: evaluation.AcousticAssessed, Pronunciation: &pronunciation,
		Provider: "iflytek", ProviderSession: "ise-session-2",
	}
	readyCommand := speechQueueCommand(t, testUserA, testTurnB, testSessionA, &assessed)
	if _, _, err := store.Queue(context.Background(), readyCommand); err != nil {
		t.Fatal(err)
	}
	readyClaim, err := store.ClaimNext(context.Background(), speechLane(1))
	if err != nil {
		t.Fatal(err)
	}
	// The acoustic checkpoint is durable before text feedback completes. A
	// Session Report may consume it while the speech job is still RUNNING.
	if readyClaim.Status != evaluation.JobRunning {
		t.Fatalf("ready acoustic claim status = %s", readyClaim.Status)
	}

	pendingCommand := speechQueueCommand(t, testUserA, testTurnC, testSessionA, nil)
	if _, _, err := store.Queue(context.Background(), pendingCommand); err != nil {
		t.Fatal(err)
	}
	read, err := store.ReadSessionAcoustics(
		context.Background(), testUserA, testSessionA,
		[]string{testTurnA, testTurnB, testTurnC},
	)
	if err != nil || !read.Pending ||
		read.Checkpoints[testTurnA].Reason != "ACOUSTIC_ASSESSMENT_FAILED" ||
		read.Checkpoints[testTurnB].Pronunciation == nil ||
		*read.Checkpoints[testTurnB].Pronunciation != pronunciation {
		t.Fatalf("ReadSessionAcoustics = %#v, %v", read, err)
	}
	if _, err := store.ReadSessionAcoustics(
		context.Background(), testUserA, testSessionA,
		[]string{"30000000-0000-4000-8000-000000000099"},
	); !errors.Is(err, evaluation.ErrAcousticDependencyFailed) {
		t.Fatalf("missing acoustic dependency = %v", err)
	}
}

func TestStoreOwnerIsolationAndDeletionClaimLockOrder(t *testing.T) {
	pool := evaluationTestDatabase(t)
	insertEvaluationTestUser(t, pool, testUserA, "evaluation-owner-a@example.com")
	insertEvaluationTestUser(t, pool, testUserB, "evaluation-owner-b@example.com")
	store := mustStore(t, pool)
	for _, userID := range []string{testUserA, testUserB} {
		if _, _, err := store.Queue(
			context.Background(),
			speechQueueCommand(t, userID, testTurnA, testSessionA, nil),
		); err != nil {
			t.Fatal(err)
		}
	}
	recordA, err := store.GetRecordBySource(
		context.Background(), testUserA, evaluation.KindPracticeTurnFeedback, testTurnA,
	)
	if err != nil {
		t.Fatal(err)
	}
	recordB, err := store.GetRecordBySource(
		context.Background(), testUserB, evaluation.KindPracticeTurnFeedback, testTurnA,
	)
	if err != nil || recordA.ID == recordB.ID || recordA.UserID == recordB.UserID {
		t.Fatalf("owner records = %#v, %#v, %v", recordA, recordB, err)
	}

	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET status='deleting' WHERE id=$1`, testUserB); err != nil {
		t.Fatal(err)
	}
	deletion, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = deletion.Rollback(context.Background()) })
	var status string
	if err := deletion.QueryRow(context.Background(),
		`SELECT status FROM users WHERE id=$1 FOR UPDATE`, testUserA).Scan(&status); err != nil {
		t.Fatal(err)
	}
	claimDone := make(chan error, 1)
	go func() {
		_, err := store.ClaimNext(context.Background(), speechLane(2))
		claimDone <- err
	}()
	select {
	case err := <-claimDone:
		t.Fatalf("claim bypassed deletion user lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := deletion.Exec(context.Background(),
		`UPDATE users SET status='deleting' WHERE id=$1`, testUserA); err != nil {
		t.Fatal(err)
	}
	if err := deletion.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-claimDone:
		if !errors.Is(err, evaluation.ErrAccountUnavailable) {
			t.Fatalf("claim after deletion transition = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claim deadlocked with account deletion")
	}
}

func mustStore(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	store, err := NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func speechLane(maxAttempts int) evaluation.ClaimLane {
	return evaluation.ClaimLane{
		Kinds:         []evaluation.Kind{evaluation.KindPracticeTurnFeedback},
		LeaseDuration: 30 * time.Second, MaxAttempts: maxAttempts,
	}
}

func speechQueueCommand(
	t *testing.T,
	userID string,
	sourceID string,
	contextID string,
	acoustic *evaluation.AcousticCheckpoint,
) evaluation.QueueCommand {
	t.Helper()
	input, inputHash := speechInput(t, sourceID, acoustic)
	lineage, lineageHash, err := evaluation.EncodeStrict(evaluation.ConfigLineage{
		SchemaVersion: evaluation.ConfigLineageSchemaVersion,
		StrategyRef:   "speech-feedback/v1", PipelineVersion: "speech-evaluation/v1",
		PromptVersion: "speech-feedback/v1", ResultSchema: "speech-feedback/v1",
		Provider: "qianwen", Model: "qwen-plus",
	})
	if err != nil {
		t.Fatal(err)
	}
	return evaluation.QueueCommand{
		UserID: userID, Kind: evaluation.KindPracticeTurnFeedback,
		SourceID: sourceID, ContextID: contextID,
		InputSnapshot: input, InputHash: inputHash,
		ConfigLineage: lineage, ConfigHash: lineageHash,
		AvailableAt: time.Now().UTC().Add(-time.Second),
	}
}

func speechInput(
	t *testing.T,
	turnID string,
	acoustic *evaluation.AcousticCheckpoint,
) ([]byte, [32]byte) {
	t.Helper()
	input, hash, err := evaluation.EncodeStrict(evaluation.SpeechInputSnapshot{
		SchemaVersion: evaluation.SpeechInputSchemaVersion,
		Transcript:    "Hello, I would like to explain my answer.",
		EvidenceRefID: turnID, QuestionID: testQuestion, Acoustic: acoustic,
	})
	if err != nil {
		t.Fatal(err)
	}
	return input, hash
}

func speechResult(t *testing.T, acoustic evaluation.AcousticCheckpoint) []byte {
	t.Helper()
	result, _, err := evaluation.EncodeStrict(evaluation.SpeechResult{
		SchemaVersion: "speech-feedback/v1", ScoreabilityStatus: "PROVISIONAL",
		Summary: "Feedback is ready.", ReasonCodes: []string{}, Acoustic: acoustic,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustDecodeSpeechInput(
	t *testing.T,
	value []byte,
) evaluation.SpeechInputSnapshot {
	t.Helper()
	var snapshot evaluation.SpeechInputSnapshot
	if err := evaluation.DecodeStrict(value, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func expireClaim(t *testing.T, pool *pgxpool.Pool, evaluationID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE evaluations
		SET created_at=transaction_timestamp()-interval '10 seconds',
			updated_at=transaction_timestamp()-interval '10 seconds',
			started_at=transaction_timestamp()-interval '10 seconds',
			lease_expires_at=transaction_timestamp()-interval '1 second'
		WHERE id=$1`, evaluationID)
	if err != nil {
		t.Fatalf("expire claim: %v", err)
	}
}

func insertEvaluationTestUser(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	email string,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, canonical_email) VALUES ($1,$2)`, userID, email); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func evaluationTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	admin, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatal("connect to TEST_DATABASE_URL")
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "evaluation_store_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})
	scopedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := scopedURL.Query()
	query.Set("search_path", schema)
	scopedURL.RawQuery = query.Encode()
	runner, err := migration.Open(scopedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if _, err := runner.Up(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	poolConfig, err := pgxpool.ParseConfig(scopedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return pool
}
