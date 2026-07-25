package review_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	userA = "10000000-0000-4000-8000-000000000001"
	userB = "10000000-0000-4000-8000-000000000002"
	userC = "10000000-0000-4000-8000-000000000003"
)

func TestPostgresEnsureReviewConcurrentAndRestartRecovery(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)

	repository := review.NewPostgresRepository(pool)
	generator := &countingGenerator{delay: 50 * time.Millisecond}
	service := review.NewEnsureService(repository, sourceReader{}, generator)
	command := ensureCommand(userA, "session-concurrent")

	const callerCount = 16
	start := make(chan struct{})
	results := make(chan review.FormalReview, callerCount)
	failures := make(chan error, callerCount)
	var callers sync.WaitGroup
	callers.Add(callerCount)
	for range callerCount {
		go func() {
			defer callers.Done()
			<-start
			result, err := service.EnsureReview(context.Background(), command)
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	callers.Wait()
	close(results)
	close(failures)

	for err := range failures {
		t.Errorf("concurrent EnsureReview: %v", err)
	}
	var reviewID string
	for result := range results {
		if result.Status != review.FormalReviewCompleted {
			t.Errorf("status = %q, want completed", result.Status)
		}
		if len(result.Evidence) != 1 {
			t.Errorf("evidence count = %d, want 1", len(result.Evidence))
		}
		if reviewID == "" {
			reviewID = result.ID
		} else if result.ID != reviewID {
			t.Errorf("review id = %q, want %q", result.ID, reviewID)
		}
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("generator calls = %d, want 1", got)
	}

	restartedRepository := review.NewPostgresRepository(pool)
	restartedService := review.NewEnsureService(
		restartedRepository,
		sourceReader{},
		&countingGenerator{},
	)
	recovered, err := restartedService.EnsureReview(
		context.Background(),
		command,
	)
	if err != nil {
		t.Fatalf("recover completed review after restart: %v", err)
	}
	if recovered.ID != reviewID || len(recovered.Evidence) != 1 {
		t.Fatalf("recovered review = %+v, want id %s with evidence", recovered, reviewID)
	}
}

func TestPostgresEnsureReviewImplementationConflictConcurrent(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)

	repository := review.NewPostgresRepository(pool)
	generator := &countingGenerator{delay: 50 * time.Millisecond}
	service := review.NewEnsureService(repository, sourceReader{}, generator)

	type ensureResult struct {
		review review.FormalReview
		err    error
	}
	const callerCount = 16
	start := make(chan struct{})
	results := make(chan ensureResult, callerCount)
	var callers sync.WaitGroup
	callers.Add(callerCount)
	for caller := range callerCount {
		implementationVersion := "review-v1"
		if caller%2 == 1 {
			implementationVersion = "review-v2"
		}
		go func() {
			defer callers.Done()
			<-start
			command := ensureCommand(userA, "implementation-conflict")
			command.ImplementationVersion = implementationVersion
			formalReview, err := service.EnsureReview(
				context.Background(),
				command,
			)
			results <- ensureResult{review: formalReview, err: err}
		}()
	}
	close(start)
	callers.Wait()
	close(results)

	var winnerVersion string
	var winnerID string
	successes := 0
	conflicts := 0
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			if winnerVersion == "" {
				winnerVersion = result.review.ImplementationVersion
				winnerID = result.review.ID
			} else if result.review.ImplementationVersion != winnerVersion ||
				result.review.ID != winnerID {
				t.Fatalf(
					"successful Ensure returned %+v, want version %s id %s",
					result.review,
					winnerVersion,
					winnerID,
				)
			}
		case errors.Is(result.err, review.ErrReviewImplementationConflict):
			conflicts++
		default:
			t.Fatalf("concurrent implementation Ensure error = %v", result.err)
		}
	}
	if successes == 0 || conflicts == 0 {
		t.Fatalf(
			"successes = %d, conflicts = %d, want both outcomes",
			successes,
			conflicts,
		)
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("generator calls = %d, want 1", got)
	}
	assertSessionReviewCount(
		t,
		pool,
		userA,
		"implementation-conflict",
		1,
	)

	losingVersion := "review-v1"
	if winnerVersion == losingVersion {
		losingVersion = "review-v2"
	}
	losingCommand := ensureCommand(userA, "implementation-conflict")
	losingCommand.ImplementationVersion = losingVersion
	if _, err := service.EnsureReview(
		context.Background(),
		losingCommand,
	); !errors.Is(err, review.ErrReviewImplementationConflict) {
		t.Fatalf("sequential implementation conflict error = %v", err)
	}
}

func TestPostgresReviewFailureRetryPendingAndLostResponseRecovery(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)

	failedCommand := ensureCommand(userA, "session-failed")
	failing := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{failuresRemaining: 1},
	)
	first, err := failing.EnsureReview(context.Background(), failedCommand)
	if !errors.Is(err, review.ErrGenerationFailed) {
		t.Fatalf("first failed EnsureReview error = %v", err)
	}
	if first.ID != "" {
		t.Fatalf("failed EnsureReview unexpectedly returned result: %+v", first)
	}
	persistedFailure, err := repository.EnsurePending(
		context.Background(),
		failedCommand,
	)
	if err != nil {
		t.Fatalf("recover failed Review state: %v", err)
	}
	if persistedFailure.Status != review.FormalReviewFailed ||
		persistedFailure.StableErrorCategory != "provider_timeout" {
		t.Fatalf("persisted failure = %+v", persistedFailure)
	}

	retried, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		&countingGenerator{},
	).EnsureReview(context.Background(), failedCommand)
	if err != nil {
		t.Fatalf("retry failed Review: %v", err)
	}
	if retried.ID != persistedFailure.ID {
		t.Fatalf("retry Review id = %s, want %s", retried.ID, persistedFailure.ID)
	}
	attempts, err := repository.ListAttempts(
		context.Background(),
		failedCommand.Actor,
		retried.ID,
	)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 2 ||
		attempts[0].Status != "failed" ||
		attempts[0].StableErrorCategory != "provider_timeout" ||
		attempts[1].Status != "succeeded" {
		t.Fatalf("attempt recovery = %+v", attempts)
	}

	pendingCommand := ensureCommand(userA, "session-pending")
	pending, err := repository.EnsurePending(
		context.Background(),
		pendingCommand,
	)
	if err != nil {
		t.Fatalf("persist pending Review: %v", err)
	}
	recoveredPending, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		&countingGenerator{},
	).EnsureReview(context.Background(), pendingCommand)
	if err != nil {
		t.Fatalf("recover pending Review after restart: %v", err)
	}
	if recoveredPending.ID != pending.ID ||
		recoveredPending.Status != review.FormalReviewCompleted {
		t.Fatalf("pending recovery = %+v, want id %s completed", recoveredPending, pending.ID)
	}

	stalledCommand := ensureCommand(userA, "session-stalled-generation")
	stalled, err := repository.EnsurePending(
		context.Background(),
		stalledCommand,
	)
	if err != nil {
		t.Fatalf("persist stalled Review: %v", err)
	}
	_, oldStalledJob, claimed, err := repository.ClaimGeneration(
		context.Background(),
		stalledCommand.Actor,
		stalled.ID,
		20*time.Millisecond,
	)
	if err != nil || !claimed {
		t.Fatalf("claim stalled Review: claimed=%v err=%v", claimed, err)
	}
	time.Sleep(40 * time.Millisecond)
	oldStalledJob.LeaseUntil = time.Now().Add(time.Hour)
	if err := repository.FailGeneration(
		context.Background(),
		oldStalledJob,
		"late_worker",
	); !errors.Is(err, review.ErrGenerationClaimLost) {
		t.Fatalf("expired worker failure write error = %v", err)
	}
	if _, err := repository.CompleteGeneration(
		context.Background(),
		oldStalledJob,
		validResult(),
		validEvidence(),
	); !errors.Is(err, review.ErrGenerationClaimLost) {
		t.Fatalf("expired worker completion error = %v", err)
	}
	recoveredStalled, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		&countingGenerator{},
	).EnsureReview(context.Background(), stalledCommand)
	if err != nil {
		t.Fatalf("recover expired generation after restart: %v", err)
	}
	stalledAttempts, err := repository.ListAttempts(
		context.Background(),
		stalledCommand.Actor,
		recoveredStalled.ID,
	)
	if err != nil {
		t.Fatalf("list recovered stalled attempts: %v", err)
	}
	if len(stalledAttempts) != 2 ||
		stalledAttempts[0].StableErrorCategory != "lease_expired" ||
		stalledAttempts[1].Status != "succeeded" {
		t.Fatalf("stalled recovery attempts = %+v", stalledAttempts)
	}

	lostResponseCommand := ensureCommand(userA, "session-lost-response")
	lostResponseReview, err := repository.EnsurePending(
		context.Background(),
		lostResponseCommand,
	)
	if err != nil {
		t.Fatalf("ensure lost-response Review: %v", err)
	}
	_, job, claimed, err := repository.ClaimGeneration(
		context.Background(),
		lostResponseCommand.Actor,
		lostResponseReview.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf("claim lost-response Review: claimed=%v err=%v", claimed, err)
	}
	job.LeaseUntil = time.Unix(0, 0)
	committed, err := repository.CompleteGeneration(
		context.Background(),
		job,
		validResult(),
		validEvidence(),
	)
	if err != nil {
		t.Fatalf("commit before simulated response loss: %v", err)
	}
	// Discard committed as if the database response was lost after commit.
	recoveredLostResponse, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		&countingGenerator{},
	).EnsureReview(context.Background(), lostResponseCommand)
	if err != nil {
		t.Fatalf("recover after response loss: %v", err)
	}
	if recoveredLostResponse.ID != committed.ID {
		t.Fatalf(
			"response-loss recovery id = %s, want %s",
			recoveredLostResponse.ID,
			committed.ID,
		)
	}
}

func TestPostgresReviewTerminalFailureDoesNotCallGeneratorAfterRestart(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	command := ensureCommand(userA, "session-quota-exhausted")
	generator := &terminalCountingGenerator{}

	firstService := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		generator,
	)
	if _, err := firstService.EnsureReview(
		context.Background(),
		command,
	); !errors.Is(err, review.ErrGenerationFailed) {
		t.Fatalf("first quota failure error = %v", err)
	}

	// Rebuild both service and Repository to exercise the persisted restart
	// path. Repeated resumes must surface the same terminal classification
	// without creating a new attempt or spending provider quota.
	restarted := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		generator,
	)
	for attempt := 0; attempt < 3; attempt++ {
		_, err := restarted.EnsureReview(context.Background(), command)
		if !errors.Is(err, review.ErrGenerationFailed) {
			t.Fatalf("resume %d error = %v", attempt+1, err)
		}
		var categorized review.StableGenerationError
		if !errors.As(err, &categorized) ||
			categorized.StableCategory() != "quota_exhausted" {
			t.Fatalf("resume %d lost quota classification: %v", attempt+1, err)
		}
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("generator calls = %d, want 1", got)
	}

	repository := review.NewPostgresRepository(pool)
	persisted, err := repository.EnsurePending(
		context.Background(),
		command,
	)
	if err != nil {
		t.Fatalf("read persisted quota failure: %v", err)
	}
	attempts, err := repository.ListAttempts(
		context.Background(),
		command.Actor,
		persisted.ID,
	)
	if err != nil {
		t.Fatalf("list quota attempts: %v", err)
	}
	if persisted.Status != review.FormalReviewFailed ||
		persisted.StableErrorCategory != "quota_exhausted" ||
		len(attempts) != 1 ||
		attempts[0].StableErrorCategory != "quota_exhausted" {
		t.Fatalf(
			"persisted terminal failure = %+v, attempts = %+v",
			persisted,
			attempts,
		)
	}
}

func TestPostgresReviewOwnerIsolationDeletionAndOldWorkerFence(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA, userB, userC)
	repository := review.NewPostgresRepository(pool)
	service := review.NewEnsureService(repository, sourceReader{}, &countingGenerator{})

	reviewA, err := service.EnsureReview(
		context.Background(),
		ensureCommand(userA, "shared-session-id"),
	)
	if err != nil {
		t.Fatalf("ensure user A: %v", err)
	}
	reviewB, err := service.EnsureReview(
		context.Background(),
		ensureCommand(userB, "shared-session-id"),
	)
	if err != nil {
		t.Fatalf("ensure user B: %v", err)
	}
	if reviewA.ID == reviewB.ID {
		t.Fatal("different owners received the same Review")
	}
	conflicting := ensureCommand(userA, "shared-session-id")
	conflicting.SourceTurnID = "turn-4"
	if _, err := service.EnsureReview(
		context.Background(),
		conflicting,
	); !errors.Is(err, review.ErrReviewSourceConflict) {
		t.Fatalf("conflicting source replay error = %v", err)
	}
	if reviewA.SourceTurnID != "turn-3" ||
		reviewA.SourceTurnVersion != "confirmed-v2" ||
		reviewA.SourceManifestFingerprint != "manifest-sha256:abc" {
		t.Fatalf("Review trigger identity = %+v", reviewA)
	}
	if _, err := repository.Get(
		context.Background(),
		actor(userB),
		reviewA.ID,
	); !errors.Is(err, review.ErrReviewNotFound) {
		t.Fatalf("user B guessed user A Review error = %v", err)
	}
	listB, err := repository.List(context.Background(), actor(userB))
	if err != nil || len(listB) != 1 || listB[0].ID != reviewB.ID {
		t.Fatalf("user B list = %+v, err = %v", listB, err)
	}

	if err := repository.DeleteUserData(context.Background(), review.DeleteUserReviewsCommand{
		UserID:             userB,
		DeletionGeneration: 1,
	}); !errors.Is(err, review.ErrInvalidReview) {
		t.Fatalf("active-account deletion error = %v", err)
	}
	if _, err := repository.Get(
		context.Background(),
		actor(userB),
		reviewB.ID,
	); err != nil {
		t.Fatalf("rejected active deletion changed Review: %v", err)
	}
	setAccountStatus(t, pool, userB, "deleting")
	if err := repository.DeleteUserData(context.Background(), review.DeleteUserReviewsCommand{
		UserID:             userB,
		DeletionGeneration: 2,
	}); err != nil {
		t.Fatalf("delete user B Review data: %v", err)
	}
	if err := repository.DeleteUserData(context.Background(), review.DeleteUserReviewsCommand{
		UserID:             userB,
		DeletionGeneration: 2,
	}); err != nil {
		t.Fatalf("repeat delete user B Review data: %v", err)
	}
	if err := repository.DeleteUserData(context.Background(), review.DeleteUserReviewsCommand{
		UserID:             userB,
		DeletionGeneration: 1,
	}); !errors.Is(err, review.ErrDeletionGenerationStale) {
		t.Fatalf("stale deletion generation error = %v", err)
	}
	// Even an invalid external status rollback cannot reopen Review writes once
	// this module has persisted its deletion fence.
	setAccountStatus(t, pool, userB, "active")
	if _, err := repository.List(
		context.Background(),
		actor(userB),
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("deleted user B list error = %v", err)
	}
	if _, err := service.EnsureReview(
		context.Background(),
		ensureCommand(userB, "post-fence-session"),
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("deletion fence allowed new Review write: %v", err)
	}
	if recoveredA, err := repository.Get(
		context.Background(),
		actor(userA),
		reviewA.ID,
	); err != nil || recoveredA.ID != reviewA.ID {
		t.Fatalf("user B deletion affected user A: %+v, %v", recoveredA, err)
	}

	commandC := ensureCommand(userC, "old-worker-session")
	pendingC, err := repository.EnsurePending(context.Background(), commandC)
	if err != nil {
		t.Fatalf("ensure user C pending: %v", err)
	}
	_, oldJob, claimed, err := repository.ClaimGeneration(
		context.Background(),
		commandC.Actor,
		pendingC.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf("claim old user C job: claimed=%v err=%v", claimed, err)
	}
	setAccountStatus(t, pool, userC, "deleting")
	if err := repository.DeleteUserData(context.Background(), review.DeleteUserReviewsCommand{
		UserID:             userC,
		DeletionGeneration: 1,
	}); err != nil {
		t.Fatalf("delete user C: %v", err)
	}
	if _, err := repository.CompleteGeneration(
		context.Background(),
		oldJob,
		validResult(),
		validEvidence(),
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("old worker completion error = %v", err)
	}
	if _, err := service.EnsureReview(
		context.Background(),
		commandC,
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("deleted user re-ensure error = %v", err)
	}
	assertReviewCount(t, pool, userC, 0)
}

func TestPostgresReviewDeleteWinsAgainstGeneratingWorker(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	entered := make(chan struct{})
	release := make(chan struct{})
	service := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{entered: entered, release: release},
	)

	result := make(chan error, 1)
	go func() {
		_, err := service.EnsureReview(
			context.Background(),
			ensureCommand(userA, "delete-race"),
		)
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("generator did not start")
	}
	setAccountStatus(t, pool, userA, "deleting")
	if err := repository.DeleteUserData(context.Background(), review.DeleteUserReviewsCommand{
		UserID:             userA,
		DeletionGeneration: 1,
	}); err != nil {
		t.Fatalf("delete during generation: %v", err)
	}
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, review.ErrAccountDeleted) {
			t.Fatalf("generating worker after delete error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("generating worker did not stop")
	}
	assertReviewCount(t, pool, userA, 0)
}

func TestPostgresDeletingIdentityUpdateRejectsWaitingReviewWrite(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	service := review.NewEnsureService(repository, sourceReader{}, &countingGenerator{})

	identityTx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin Identity deletion transaction: %v", err)
	}
	defer func() { _ = identityTx.Rollback(context.Background()) }()
	if _, err := identityTx.Exec(context.Background(), `
		SELECT id
		FROM identity_users
		WHERE id = $1
		FOR UPDATE
	`, userA); err != nil {
		t.Fatalf("lock Identity user for deletion: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := service.EnsureReview(
			context.Background(),
			ensureCommand(userA, "identity-delete-wins"),
		)
		result <- err
	}()
	waitForBlockedIdentityShareLock(t, pool)
	if _, err := identityTx.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, userA); err != nil {
		t.Fatalf("mark Identity user deleting: %v", err)
	}
	if err := identityTx.Commit(context.Background()); err != nil {
		t.Fatalf("commit Identity deleting status: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, review.ErrAccountDeleted) {
			t.Fatalf("waiting Review write error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting Review write did not resume")
	}
	assertReviewCount(t, pool, userA, 0)
}

func TestPostgresCompletedReviewAndEvidenceOwnershipConstraints(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA, userB)
	repository := review.NewPostgresRepository(pool)
	pending, err := repository.EnsurePending(
		context.Background(),
		ensureCommand(userA, "constraint-session"),
	)
	if err != nil {
		t.Fatalf("ensure constraint Review: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE reviews
		SET status = 'failed'
		WHERE id = $1
	`, pending.ID); err == nil {
		t.Fatal("failed Review without stable error unexpectedly passed CHECK")
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE reviews
		SET stable_error_category = 'unexpected_error'
		WHERE id = $1
	`, pending.ID); err == nil {
		t.Fatal("pending Review with stable error unexpectedly passed CHECK")
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin evidence constraint transaction: %v", err)
	}
	resultJSON, err := json.Marshal(validResult())
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		UPDATE reviews
		SET status = 'completed', result = $2, completed_at = now()
		WHERE id = $1
	`, pending.ID, resultJSON); err != nil {
		t.Fatalf("stage completed Review without evidence: %v", err)
	}
	if err := tx.Commit(context.Background()); err == nil {
		t.Fatal("completed Review without evidence unexpectedly committed")
	}

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO review_evidence (
			review_id,
			owner_user_id,
			conclusion_key,
			source_type,
			source_id,
			source_version
		)
		VALUES ($1, $2, 'summary', 'turn', 'turn-1', 'v1')
	`, pending.ID, userB); err == nil {
		t.Fatal("cross-owner evidence unexpectedly passed composite foreign key")
	}
}

func TestPostgresCompletedReviewRejectsInvalidResultShapes(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	cases := []struct {
		name   string
		result string
	}{
		{
			name:   "score above range",
			result: `{"overall_score":101,"summary":"Summary","conclusions":[{"key":"summary","category":"clarity","message":"Clear."}]}`,
		},
		{
			name:   "fractional score",
			result: `{"overall_score":82.5,"summary":"Summary","conclusions":[{"key":"summary","category":"clarity","message":"Clear."}]}`,
		},
		{
			name:   "blank summary",
			result: `{"overall_score":82,"summary":" ","conclusions":[{"key":"summary","category":"clarity","message":"Clear."}]}`,
		},
		{
			name:   "missing category",
			result: `{"overall_score":82,"summary":"Summary","conclusions":[{"key":"summary","message":"Clear."}]}`,
		},
		{
			name:   "duplicate conclusion key",
			result: `{"overall_score":82,"summary":"Summary","conclusions":[{"key":"summary","category":"clarity","message":"Clear."},{"key":"summary","category":"structure","message":"Structured."}]}`,
		},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			pending, err := repository.EnsurePending(
				context.Background(),
				ensureCommand(
					userA,
					fmt.Sprintf("invalid-result-%d", index),
				),
			)
			if err != nil {
				t.Fatalf("ensure pending Review: %v", err)
			}
			tx, err := pool.Begin(context.Background())
			if err != nil {
				t.Fatalf("begin invalid result transaction: %v", err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(context.Background(), `
				INSERT INTO review_evidence (
					review_id,
					owner_user_id,
					conclusion_key,
					source_type,
					source_id,
					source_version
				)
				VALUES ($1, $2, 'summary', 'conversation_turn', 'turn-3', 'v1')
			`, pending.ID, userA); err != nil {
				t.Fatalf("insert evidence: %v", err)
			}
			if _, err := tx.Exec(context.Background(), `
				UPDATE reviews
				SET status = 'completed',
				    result = $2::jsonb,
				    completed_at = now()
				WHERE id = $1
			`, pending.ID, test.result); err != nil {
				t.Fatalf("stage invalid completed result: %v", err)
			}
			err = tx.Commit(context.Background())
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) ||
				postgresError.Code != "23514" {
				t.Fatalf("commit error = %v, want PostgreSQL check violation", err)
			}
			recovered, err := repository.Get(
				context.Background(),
				actor(userA),
				pending.ID,
			)
			if err != nil {
				t.Fatalf("recover rejected Review: %v", err)
			}
			if recovered.Status != review.FormalReviewPending ||
				recovered.Result != nil ||
				len(recovered.Evidence) != 0 {
				t.Fatalf("rejected Review changed state: %+v", recovered)
			}
		})
	}
}

func TestReviewMigrationRevertsAndReappliesAfterIdentityPrerequisite(t *testing.T) {
	pool := reviewDatabase(t)
	var evidenceIndexDefinition string
	if err := pool.QueryRow(context.Background(), `
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname = 'review_evidence_source_idx'
	`).Scan(&evidenceIndexDefinition); err != nil {
		t.Fatalf("read Evidence source index: %v", err)
	}
	const ownerFirstIndex = "(owner_user_id, source_type"
	if !strings.Contains(evidenceIndexDefinition, ownerFirstIndex) {
		t.Fatalf(
			"Evidence source index = %q, want owner-first prefix %q",
			evidenceIndexDefinition,
			ownerFirstIndex,
		)
	}
	down, err := migrations.Files.ReadFile(
		"000008_review_formal_reviews.down.sql",
	)
	if err != nil {
		t.Fatalf("read Review down migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(down)); err != nil {
		t.Fatalf("apply Review down migration: %v", err)
	}
	var reviewsTable *string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT to_regclass('reviews')::text`,
	).Scan(&reviewsTable); err != nil {
		t.Fatalf("inspect reverted Review schema: %v", err)
	}
	if reviewsTable != nil {
		t.Fatalf("reviews table remained after down migration: %s", *reviewsTable)
	}

	up, err := migrations.Files.ReadFile(
		"000008_review_formal_reviews.up.sql",
	)
	if err != nil {
		t.Fatalf("read Review up migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(up)); err != nil {
		t.Fatalf("reapply Review migration: %v", err)
	}
}

type sourceReader struct{}

func (sourceReader) ReadReviewSource(
	_ context.Context,
	_ review.Actor,
	practiceSessionID string,
) (review.ReviewSourceSnapshot, error) {
	return review.ReviewSourceSnapshot{
		PracticeSessionID:   practiceSessionID,
		SessionVersion:      "session-v3",
		SourceTurnID:        "turn-3",
		SourceTurnVersion:   "confirmed-v2",
		ManifestFingerprint: "manifest-sha256:abc",
		Sources: []review.SourceObject{{
			SourceType:    review.SourceTypeConversationTurn,
			SourceID:      "turn-3",
			SourceVersion: "confirmed-v2",
			Checksum:      "sha256:abc",
			Snapshot:      json.RawMessage(`{"segment":"minimal evidence"}`),
		}},
	}, nil
}

type countingGenerator struct {
	calls             atomic.Int32
	failuresRemaining int32
	delay             time.Duration
	entered           chan struct{}
	release           chan struct{}
}

func (g *countingGenerator) GenerateReview(
	ctx context.Context,
	_ review.ReviewGenerationInput,
) (review.GeneratedReview, error) {
	g.calls.Add(1)
	if g.entered != nil {
		close(g.entered)
		select {
		case <-ctx.Done():
			return review.GeneratedReview{}, ctx.Err()
		case <-g.release:
		}
	}
	if g.delay > 0 {
		timer := time.NewTimer(g.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return review.GeneratedReview{}, ctx.Err()
		case <-timer.C:
		}
	}
	if atomic.AddInt32(&g.failuresRemaining, -1) >= 0 {
		return review.GeneratedReview{}, categorizedError{}
	}
	return review.GeneratedReview{
		Result: validResult(),
		EvidenceLinks: []review.EvidenceLink{{
			ConclusionKey: "summary",
			SourceType:    review.SourceTypeConversationTurn,
			SourceID:      "turn-3",
			SourceVersion: "confirmed-v2",
		}},
	}, nil
}

type categorizedError struct{}

func (categorizedError) Error() string          { return "temporary provider failure" }
func (categorizedError) StableCategory() string { return "provider_timeout" }

type terminalCountingGenerator struct {
	calls atomic.Int32
}

func (generator *terminalCountingGenerator) GenerateReview(
	context.Context,
	review.ReviewGenerationInput,
) (review.GeneratedReview, error) {
	generator.calls.Add(1)
	return review.GeneratedReview{}, terminalCategorizedError{}
}

type terminalCategorizedError struct{}

func (terminalCategorizedError) Error() string {
	return "free quota exhausted"
}

func (terminalCategorizedError) StableCategory() string {
	return "quota_exhausted"
}

func validResult() review.ReviewResult {
	return review.ReviewResult{
		OverallScore: 82,
		Summary:      "Clear answer with one actionable improvement.",
		Conclusions: []review.ReviewConclusion{{
			Key:        "summary",
			Category:   "clarity",
			Message:    "The response is clear.",
			Suggestion: "Add a concrete outcome.",
		}},
	}
}

func validEvidence() []review.ReviewEvidence {
	return []review.ReviewEvidence{{
		ConclusionKey: "summary",
		SourceType:    review.SourceTypeConversationTurn,
		SourceID:      "turn-3",
		SourceVersion: "confirmed-v2",
		Checksum:      "sha256:abc",
		Snapshot:      json.RawMessage(`{"segment":"minimal evidence"}`),
	}}
}

func actor(userID string) review.Actor {
	return review.Actor{UserID: userID, DeletionGeneration: 0}
}

func ensureCommand(userID, sessionID string) review.EnsureReviewCommand {
	return review.EnsureReviewCommand{
		Actor:                     actor(userID),
		PracticeSessionID:         sessionID,
		ImplementationVersion:     "review-v1",
		SourceTurnID:              "turn-3",
		SourceTurnVersion:         "confirmed-v2",
		SourceManifestFingerprint: "manifest-sha256:abc",
	}
}

var schemaSequence atomic.Uint64

func reviewDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL admin pool: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf(
		"review_%d_%d",
		time.Now().UnixNano(),
		schemaSequence.Add(1),
	)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create Review test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		if _, err := admin.Exec(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+schema+" CASCADE",
		); err != nil {
			t.Errorf("drop Review test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open isolated Review pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `
		CREATE TABLE identity_users (
			id uuid PRIMARY KEY,
			account_status text NOT NULL DEFAULT 'active'
		)
	`); err != nil {
		t.Fatalf("create #79 identity prerequisite fixture: %v", err)
	}
	up, err := migrations.Files.ReadFile(
		"000008_review_formal_reviews.up.sql",
	)
	if err != nil {
		t.Fatalf("read Review migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply Review migration: %v", err)
	}
	return pool
}

func insertUsers(t *testing.T, pool *pgxpool.Pool, userIDs ...string) {
	t.Helper()
	for _, userID := range userIDs {
		if _, err := pool.Exec(
			context.Background(),
			`INSERT INTO identity_users (id) VALUES ($1)`,
			userID,
		); err != nil {
			t.Fatalf("insert identity user %s: %v", userID, err)
		}
	}
}

func assertReviewCount(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM reviews WHERE owner_user_id = $1`,
		userID,
	).Scan(&count); err != nil {
		t.Fatalf("count Review rows: %v", err)
	}
	if count != want {
		t.Fatalf("Review row count = %d, want %d", count, want)
	}
}

func assertSessionReviewCount(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	sessionID string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM reviews
		WHERE owner_user_id = $1 AND practice_session_id = $2
	`, userID, sessionID).Scan(&count); err != nil {
		t.Fatalf("count Session Review rows: %v", err)
	}
	if count != want {
		t.Fatalf("Session Review row count = %d, want %d", count, want)
	}
}

func setAccountStatus(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	status string,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = $2
		WHERE id = $1
	`, userID, status); err != nil {
		t.Fatalf("set account status %q: %v", status, err)
	}
}

func waitForBlockedIdentityShareLock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		err := pool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE query LIKE '%FOR SHARE OF users%'
				  AND wait_event_type = 'Lock'
			)
		`).Scan(&blocked)
		if err != nil {
			t.Fatalf("inspect blocked Review write: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Review write did not block on Identity user row")
}
