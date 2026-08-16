package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	retryUserID     = "51000000-0000-4000-8000-000000000001"
	retryPlanID     = "52000000-0000-4000-8000-000000000001"
	retrySessionID  = "53000000-0000-4000-8000-000000000001"
	retryQuestionID = "54000000-0000-4000-8000-000000000001"
	retryTurnID     = "55000000-0000-4000-8000-000000000001"
	retryLearnerID  = "56000000-0000-4000-8000-000000000001"
	retryCandidate  = "57000000-0000-4000-8000-000000000001"
)

func TestServiceRetryReplayAndFeedbackScopedIdempotency(t *testing.T) {
	pool := reviewTestDatabase(t)
	seedRetryPractice(t, pool)
	evaluationStore, err := evaluationpostgres.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	feedbackItems := seedRetryFeedback(t, evaluationStore)
	practiceRepository, err := practicepostgres.New(
		pool,
		noopCompletionScheduler{},
		noopTurnFeedbackScheduler{},
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(pool, evaluationStore, practiceRepository)
	if err != nil {
		t.Fatal(err)
	}

	type retryResult struct {
		turn     practice.Turn
		replayed bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan retryResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			turn, replayed, err := service.CreateTurn(
				context.Background(), retryUserID, feedbackItems[0].ID, "same-key",
			)
			results <- retryResult{turn: turn, replayed: replayed, err: err}
		}()
	}
	ready.Wait()
	close(start)
	first := <-results
	second := <-results
	for _, result := range []retryResult{first, second} {
		if result.err != nil {
			t.Fatalf("CreateTurn concurrent: %v", result.err)
		}
	}
	if first.turn.ID != second.turn.ID || first.replayed == second.replayed {
		t.Fatalf("same feedback replay = %#v, %#v", first, second)
	}

	other, replayed, err := service.CreateTurn(
		context.Background(), retryUserID, feedbackItems[1].ID, "same-key",
	)
	if err != nil || replayed || other.ID == first.turn.ID {
		t.Fatalf("different feedback same key = %#v, %v, replayed=%v", other, err, replayed)
	}
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*)
		FROM practice_turns WHERE session_id=$1 AND turn_kind='RETRY'`,
		retrySessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("retry turn count = %d, want 2", count)
	}
	rows, err := pool.Query(context.Background(), `SELECT client_request_id
		FROM practice_turns WHERE session_id=$1 AND turn_kind='RETRY'
		ORDER BY turn_id`, retrySessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	requestIDs := make(map[string]struct{}, 2)
	for rows.Next() {
		var requestID string
		if err := rows.Scan(&requestID); err != nil {
			t.Fatal(err)
		}
		if requestID == "same-key" {
			t.Fatal("raw client idempotency key reached Practice persistence")
		}
		requestIDs[requestID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(requestIDs) != 2 {
		t.Fatalf("feedback-scoped request IDs = %#v", requestIDs)
	}
}

func seedRetryFeedback(
	t *testing.T,
	store *evaluationpostgres.Store,
) []evaluation.FeedbackItem {
	t.Helper()
	acoustic := evaluation.AcousticCheckpoint{
		Status: evaluation.AcousticNotAssessed,
		Reason: "ACOUSTIC_ASSESSMENT_NOT_CONFIGURED",
	}
	input, inputHash, err := evaluation.EncodeStrict(evaluation.SpeechInputSnapshot{
		SchemaVersion: evaluation.SpeechInputSchemaVersion,
		Transcript:    "I led the migration and explained the tradeoffs.",
		EvidenceRefID: retryTurnID, QuestionID: retryQuestionID,
		Acoustic: &acoustic,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage, lineageHash, err := evaluation.EncodeStrict(evaluation.ConfigLineage{
		SchemaVersion: evaluation.ConfigLineageSchemaVersion,
		StrategyRef:   "speech-feedback/v1", PipelineVersion: "speech-evaluation/v1",
		PromptVersion: "speech-feedback/v1", ResultSchema: "speech-feedback/v1",
		Provider: "qianwen", Model: "qwen-plus",
	})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := store.Queue(context.Background(), evaluation.QueueCommand{
		UserID: retryUserID, Kind: evaluation.KindPracticeTurnFeedback,
		SourceID: retryTurnID, ContextID: retrySessionID,
		InputSnapshot: input, InputHash: inputHash,
		ConfigLineage: lineage, ConfigHash: lineageHash,
		AvailableAt: time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(context.Background(), evaluation.ClaimLane{
		Kinds:         []evaluation.Kind{evaluation.KindPracticeTurnFeedback},
		LeaseDuration: 30 * time.Second, MaxAttempts: 1,
	})
	if err != nil || claim.ID != record.ID {
		t.Fatalf("ClaimNext = %#v, %v", claim, err)
	}
	result, _, err := evaluation.EncodeStrict(evaluation.SpeechResult{
		SchemaVersion: "speech-feedback/v1", ScoreabilityStatus: "PROVISIONAL",
		Summary: "Feedback is ready.", ReasonCodes: []string{}, Acoustic: acoustic,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := []evaluation.FeedbackItemDraft{
		{
			Category: "CORRECTION", Severity: "MEDIUM",
			Evidence: evaluation.FeedbackEvidence{
				EvidenceRefID: retryTurnID, StartUTF8Byte: 0,
				EndUTF8Byte: 1, OriginalExcerpt: "I",
			},
			Recommendation: "Add a clearer result.",
			Correction:     "I led the migration, reducing deployment time.",
			RepracticeMode: "SAME_QUESTION",
		},
		{
			Category: "RECOMMENDED_EXPRESSION", Severity: "LOW",
			Evidence: evaluation.FeedbackEvidence{
				EvidenceRefID: retryTurnID, StartUTF8Byte: 0,
				EndUTF8Byte: 1, OriginalExcerpt: "I",
			},
			Recommendation: "Make the tradeoff explicit.",
			Correction:     "The main tradeoff was speed versus migration risk.",
			RepracticeMode: "SAME_QUESTION",
		},
	}
	if err := store.CompleteClaim(context.Background(), evaluation.Completion{
		UserID: retryUserID, ID: claim.ID, LeaseToken: claim.LeaseToken,
		Result: result, Items: items,
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.ListFeedbackItems(context.Background(), retryUserID, claim.ID)
	if err != nil || len(stored) != 2 {
		t.Fatalf("ListFeedbackItems = %#v, %v", stored, err)
	}
	return stored
}

func seedRetryPractice(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	fingerprint := make([]byte, 32)
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id,canonical_email) VALUES ($1,$2)`,
		retryUserID, "review-retry@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO practice_plans (
		plan_id,user_id,preparation_snapshot,scene_selection,session_policy,
		practice_objectives,practice_experience,status,version,
		initial_client_request_id,initial_request_fingerprint
	) VALUES ($1,$2,'{}','{}','{}','[{}]','INTERVIEW','ready',1,$3,$4)`,
		retryPlanID, retryUserID, "plan-create-0001", fingerprint); err != nil {
		t.Fatal(err)
	}
	planSnapshot := `{"session_policy":{"retry_allowed":true}}`
	participants := `[{"practice_participant_id":"` + retryLearnerID +
		`","practice_session_id":"` + retrySessionID +
		`","participant_role":"LEARNER","subject_ref":{"namespace":"speakup.user","subject_id":"` +
		retryUserID + `"},"participant_order":1}]`
	if _, err := pool.Exec(ctx, `INSERT INTO practice_sessions (
		session_id,user_id,plan_id,plan_version,practice_experience,
		scene_category,practice_mode,evaluation_policy_ref,status,version,
		effective_turns,plan_snapshot,participants,initial_client_request_id,
		initial_request_fingerprint,started_at,ended_at,end_reason
	) VALUES ($1,$2,$3,1,'INTERVIEW','INTERVIEW_PROFESSIONAL',
		'FULL_SIMULATION','interview.evaluation.v1','completed',2,1,
		$4::jsonb,$5::jsonb,$6,$7,transaction_timestamp(),
		transaction_timestamp(),'COMPLETED')`, retrySessionID, retryUserID,
		retryPlanID, planSnapshot, participants, "session-create-0001", fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO practice_questions (
		question_id,session_id,objective_id,question_type,content,
		speaker_participant_id,addressee_participant_ids,sequence
	) VALUES ($1,$2,'objective-1','PRIMARY','Tell me about a migration.',
		'assistant',ARRAY[$3]::text[],1)`, retryQuestionID, retrySessionID,
		retryLearnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO practice_turns (
		turn_id,session_id,question_id,respondent_participant_id,sequence,
		turn_kind,status,counts_toward_turn_limit,candidate_id,transcript_id,
		evidence_version,transcript,effective_turns_after,session_version_after,
		confirmed_at
	) VALUES ($1,$2,$3,$4,1,'EFFECTIVE','confirmed',true,$5,
		'transcript-1',1,'I led the migration and explained the tradeoffs.',1,2,
		transaction_timestamp())`, retryTurnID, retrySessionID, retryQuestionID,
		retryLearnerID, retryCandidate); err != nil {
		t.Fatal(err)
	}
}

type noopCompletionScheduler struct{}

func (noopCompletionScheduler) ScheduleCompletedSession(
	context.Context,
	pgx.Tx,
	practice.SessionEvidence,
) error {
	return nil
}

type noopTurnFeedbackScheduler struct{}

func (noopTurnFeedbackScheduler) ScheduleConfirmedTurn(
	context.Context,
	pgx.Tx,
	practice.TurnFeedbackEvidence,
) error {
	return nil
}

func reviewTestDatabase(t *testing.T) *pgxpool.Pool {
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
	schema := "review_retry_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
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
