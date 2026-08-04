package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresInterviewShadowConcurrentClaimHasSingleWinner(
	t *testing.T,
) {
	pool, repository, configuration, _ := prepareInterviewShadowRuntime(t)

	const callers = 8
	start := make(chan struct{})
	claims := make(chan InterviewShadowClaim, callers)
	failures := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			claim, acquired, err := repository.ClaimInterviewShadow(
				context.Background(),
				configuration,
			)
			if err != nil {
				failures <- err
				return
			}
			if acquired {
				claims <- claim
			}
		}()
	}
	close(start)
	wait.Wait()
	close(claims)
	close(failures)
	for failure := range failures {
		t.Errorf("concurrent ClaimInterviewShadow: %v", failure)
	}
	var winners []InterviewShadowClaim
	for claim := range claims {
		winners = append(winners, claim)
	}
	if len(winners) != 1 {
		t.Fatalf("claim winners = %d, want 1", len(winners))
	}
	if !winners[0].Valid() || winners[0].AttemptCount != 1 ||
		winners[0].FencingToken != 1 {
		t.Fatalf("winning claim = %#v", winners[0])
	}
	var attempts int
	var runs int
	if err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT attempt_count
			 FROM evaluation_outbox
			 WHERE id = $1),
			(SELECT count(*)
			 FROM evaluation_module_runs
			 WHERE outbox_id = $1)
	`, winners[0].OutboxID).Scan(&attempts, &runs); err != nil {
		t.Fatalf("inspect concurrent claim: %v", err)
	}
	if attempts != 1 || runs != 1 {
		t.Fatalf("attempts=%d runs=%d, want 1/1", attempts, runs)
	}
}

func TestPostgresInterviewShadowLeaseTakeoverReusesLogicalRun(
	t *testing.T,
) {
	pool, repository, configuration, _ := prepareInterviewShadowRuntime(t)
	first := claimInterviewShadow(t, repository, configuration)
	expireInterviewShadowLease(t, pool, first.OutboxID)

	second := claimInterviewShadow(t, repository, configuration)
	if second.ModuleRunID != first.ModuleRunID {
		t.Fatalf(
			"takeover module run = %q, want %q",
			second.ModuleRunID,
			first.ModuleRunID,
		)
	}
	if second.AttemptCount != first.AttemptCount+1 ||
		second.FencingToken != first.FencingToken+1 {
		t.Fatalf("takeover claim = %#v, first = %#v", second, first)
	}
}

func TestPostgresInterviewShadowLeaseStartsFromWallClock(t *testing.T) {
	pool, repository, configuration, evaluation :=
		prepareInterviewShadowRuntime(t)
	configuration.LeaseDuration = 4 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lease blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(ctx) }()
	if _, err := blocker.Exec(ctx, `
		SELECT true
		FROM evaluation_ledgers
		WHERE id = $1
		FOR UPDATE
	`, evaluation.ID); err != nil {
		t.Fatalf("lock lease fixture ledger: %v", err)
	}
	started := make(chan struct{})
	done := make(chan struct {
		claim    InterviewShadowClaim
		acquired bool
		err      error
	}, 1)
	go func() {
		close(started)
		claim, acquired, claimErr := repository.ClaimInterviewShadow(
			ctx,
			configuration,
		)
		done <- struct {
			claim    InterviewShadowClaim
			acquired bool
			err      error
		}{claim, acquired, claimErr}
	}()
	<-started
	time.Sleep(2 * time.Second)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release lease fixture ledger: %v", err)
	}
	outcome := <-done
	if outcome.err != nil || !outcome.acquired {
		t.Fatalf(
			"delayed claim acquired=%t error=%v",
			outcome.acquired,
			outcome.err,
		)
	}
	if remaining := time.Until(outcome.claim.LeaseExpiresAt); remaining <
		3*time.Second {
		t.Fatalf(
			"lease lost transaction wait time: remaining=%s",
			remaining,
		)
	}
}

func TestPostgresInterviewShadowConfigurationDriftTerminatesJob(
	t *testing.T,
) {
	pool, repository, configuration, evaluation :=
		prepareInterviewShadowRuntime(t)
	first := claimInterviewShadow(t, repository, configuration)
	expireInterviewShadowLease(t, pool, first.OutboxID)

	changed := configuration
	changed.FullConfigHash = sha256.Sum256(
		[]byte("interview-shadow-changed-config/v2"),
	)
	claim, acquired, err := repository.ClaimInterviewShadow(
		context.Background(),
		changed,
	)
	if !errors.Is(err, ErrInterviewShadowConfigurationConflict) {
		t.Fatalf("configuration drift error = %v", err)
	}
	if acquired || claim.OutboxID != "" || claim.ModuleRunID != "" {
		t.Fatalf("configuration drift claim acquired=%t value=%#v", acquired, claim)
	}
	assertInterviewShadowTerminalRuntime(
		t,
		pool,
		first.OutboxID,
		"FAILED",
		"FAILED",
		"FAILED",
		interviewShadowConfigurationChangedCode,
	)

	next := reevaluateInterviewShadow(
		t,
		repository,
		evaluation.ID,
		"configuration-drift-next",
	)
	continued := claimInterviewShadow(t, repository, changed)
	if continued.EvaluationRevisionID != next.Revision.ID {
		t.Fatalf(
			"continued revision = %q, want %q",
			continued.EvaluationRevisionID,
			next.Revision.ID,
		)
	}
}

func TestPostgresInterviewShadowReevaluateCancelsPendingRuntime(
	t *testing.T,
) {
	pool, repository, configuration, evaluation :=
		prepareInterviewShadowRuntime(t)
	claim := claimInterviewShadow(t, repository, configuration)

	next := reevaluateInterviewShadow(
		t,
		repository,
		evaluation.ID,
		"cancel-running-runtime",
	)
	assertInterviewShadowTerminalRuntime(
		t,
		pool,
		claim.OutboxID,
		"FAILED",
		"FAILED",
		"SUPERSEDED",
		interviewShadowRevisionSupersededCode,
	)
	replacement := claimInterviewShadow(t, repository, configuration)
	if replacement.EvaluationRevisionID != next.Revision.ID {
		t.Fatalf(
			"replacement revision = %q, want %q",
			replacement.EvaluationRevisionID,
			next.Revision.ID,
		)
	}
}

func TestPostgresInterviewShadowClaimAndReevaluateDoNotDeadlock(
	t *testing.T,
) {
	pool, repository, configuration, evaluation :=
		prepareInterviewShadowRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	type claimOutcome struct {
		claim    InterviewShadowClaim
		acquired bool
		err      error
	}
	claimDone := make(chan claimOutcome, 1)
	reevaluateDone := make(chan struct {
		evaluation Evaluation
		err        error
	}, 1)
	go func() {
		<-start
		claim, acquired, err := repository.ClaimInterviewShadow(
			ctx,
			configuration,
		)
		claimDone <- claimOutcome{claim, acquired, err}
	}()
	go func() {
		<-start
		next, _, err := repository.Reevaluate(
			ctx,
			interviewShadowReevaluationCommand(
				evaluation.ID,
				"claim-race",
			),
		)
		reevaluateDone <- struct {
			evaluation Evaluation
			err        error
		}{next, err}
	}()
	close(start)

	var claimed claimOutcome
	select {
	case claimed = <-claimDone:
	case <-ctx.Done():
		t.Fatal("ClaimInterviewShadow deadlocked with Reevaluate")
	}
	if claimed.err != nil {
		t.Fatalf("concurrent ClaimInterviewShadow: %v", claimed.err)
	}
	var reevaluated struct {
		evaluation Evaluation
		err        error
	}
	select {
	case reevaluated = <-reevaluateDone:
	case <-ctx.Done():
		t.Fatal("Reevaluate deadlocked with ClaimInterviewShadow")
	}
	if reevaluated.err != nil {
		t.Fatalf("concurrent Reevaluate: %v", reevaluated.err)
	}
	if reevaluated.evaluation.Revision.Number != 2 {
		t.Fatalf(
			"replacement revision = %d, want 2",
			reevaluated.evaluation.Revision.Number,
		)
	}
	assertSupersededInterviewShadowRuntime(
		t,
		pool,
		evaluation.Revision.ID,
	)
	assertCurrentInterviewShadowRevisionRunnable(
		t,
		pool,
		reevaluated.evaluation.Revision.ID,
	)
	if claimed.acquired &&
		claimed.claim.EvaluationRevisionID != evaluation.Revision.ID &&
		claimed.claim.EvaluationRevisionID !=
			reevaluated.evaluation.Revision.ID {
		t.Fatalf("claim escaped revision chain: %#v", claimed.claim)
	}
}

func TestPostgresInterviewShadowCompleteAndReevaluateDoNotDeadlock(
	t *testing.T,
) {
	pool, repository, configuration, evaluation :=
		prepareInterviewShadowRuntime(t)
	claim := claimInterviewShadow(t, repository, configuration)
	result := evaluateInterviewShadowClaim(t, claim)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	completeDone := make(chan error, 1)
	reevaluateDone := make(chan struct {
		evaluation Evaluation
		err        error
	}, 1)
	go func() {
		<-start
		completeDone <- repository.CompleteInterviewShadow(
			ctx,
			claim,
			result,
		)
	}()
	go func() {
		<-start
		next, _, err := repository.Reevaluate(
			ctx,
			interviewShadowReevaluationCommand(
				evaluation.ID,
				"complete-race",
			),
		)
		reevaluateDone <- struct {
			evaluation Evaluation
			err        error
		}{next, err}
	}()
	close(start)

	var completeErr error
	select {
	case completeErr = <-completeDone:
	case <-ctx.Done():
		t.Fatal("CompleteInterviewShadow deadlocked with Reevaluate")
	}
	if completeErr != nil &&
		!errors.Is(completeErr, ErrInterviewShadowLeaseLost) {
		t.Fatalf("concurrent CompleteInterviewShadow: %v", completeErr)
	}
	var reevaluated struct {
		evaluation Evaluation
		err        error
	}
	select {
	case reevaluated = <-reevaluateDone:
	case <-ctx.Done():
		t.Fatal("Reevaluate deadlocked with CompleteInterviewShadow")
	}
	if reevaluated.err != nil {
		t.Fatalf("concurrent Reevaluate: %v", reevaluated.err)
	}
	assertCompletedOrCancelledInterviewShadowRuntime(
		t,
		pool,
		claim,
		completeErr,
	)
	assertCurrentInterviewShadowRevisionRunnable(
		t,
		pool,
		reevaluated.evaluation.Revision.ID,
	)
}

func TestPostgresInterviewShadowCompleteIsIdempotent(t *testing.T) {
	pool, repository, configuration, evaluation := prepareInterviewShadowRuntime(t)
	claim := claimInterviewShadow(t, repository, configuration)
	result := evaluateInterviewShadowClaim(t, claim)

	if err := repository.CompleteInterviewShadow(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("CompleteInterviewShadow: %v", err)
	}
	if err := repository.CompleteInterviewShadow(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("idempotent CompleteInterviewShadow: %v", err)
	}
	state, err := repository.GetInterviewShadowState(
		context.Background(),
		testOwnerA,
		evaluation.ID,
		evaluation.Revision.ID,
	)
	if err != nil {
		t.Fatalf("GetInterviewShadowState: %v", err)
	}
	if state.ModuleStatus != InterviewShadowRuntimeReady ||
		state.FullConfigHash != claim.FullConfigHash ||
		state.Result == nil ||
		state.Result.SnapshotID != claim.Snapshot.ID {
		t.Fatalf("ready state = %#v", state)
	}
	var resultCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM evaluation_interview_scene_results
		WHERE evaluation_revision_id = $1
	`, evaluation.Revision.ID).Scan(&resultCount); err != nil {
		t.Fatalf("count Interview Shadow results: %v", err)
	}
	if resultCount != 1 {
		t.Fatalf("result count = %d, want 1", resultCount)
	}
	assertInterviewShadowRevisionNotFinal(
		t,
		pool,
		evaluation.Revision.ID,
	)
}

func TestPostgresInterviewShadowRejectsCrossSnapshotFindingReference(
	t *testing.T,
) {
	pool, repository, configuration, _ := prepareInterviewShadowRuntime(t)
	claim := claimInterviewShadow(t, repository, configuration)
	result := evaluateInterviewShadowClaim(t, claim)
	if len(result.Dimensions) == 0 ||
		len(result.Dimensions[0].Strengths) == 0 ||
		len(result.Dimensions[0].Strengths[0].Evidence) == 0 {
		t.Fatal("fixture has no finding evidence to tamper")
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var providerRequestID any
	if result.Provider != nil {
		providerRequestID = result.Provider.RequestID
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO evaluation_interview_scene_results (
			module_run_id,
			evaluation_id,
			evaluation_revision_id,
			owner_user_id,
			channel,
			strategy_ref,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scene_type,
			snapshot_hash,
			full_config_hash,
			prompt_version,
			provider,
			model,
			provider_request_id,
			fencing_token,
			result_payload
		)
		VALUES (
			$1, $2, $3, $4, 'SCENE', $5, $6, $7, $8,
			'INTERVIEW', $9, $10, $11, $12, $13, $14, $15,
			jsonb_set(
				$16::jsonb,
				'{dimensions,0,strengths,0,evidence,0,evidence_ref_id}',
				'"evidence_from_another_snapshot"'::jsonb
			)
		)
	`, claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		providerRequestID, claim.FencingToken, payload)
	if err == nil {
		t.Fatal("cross-snapshot finding reference was accepted")
	}
	var resultCount int
	if countErr := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM evaluation_interview_scene_results
		WHERE module_run_id = $1
	`, claim.ModuleRunID).Scan(&resultCount); countErr != nil {
		t.Fatal(countErr)
	}
	if resultCount != 0 {
		t.Fatalf("invalid persisted result count = %d", resultCount)
	}
}

func TestPostgresInterviewShadowRejectsExpiredAndStaleFencingToken(
	t *testing.T,
) {
	pool, repository, configuration, _ := prepareInterviewShadowRuntime(t)
	first := claimInterviewShadow(t, repository, configuration)
	result := evaluateInterviewShadowClaim(t, first)
	expireInterviewShadowLease(t, pool, first.OutboxID)

	if err := repository.CompleteInterviewShadow(
		context.Background(),
		first,
		result,
	); !errors.Is(err, ErrInterviewShadowLeaseLost) {
		t.Fatalf("late completion error = %v", err)
	}
	second := claimInterviewShadow(t, repository, configuration)
	if err := repository.CompleteInterviewShadow(
		context.Background(),
		first,
		result,
	); !errors.Is(err, ErrInterviewShadowLeaseLost) {
		t.Fatalf("stale-token completion error = %v", err)
	}
	if err := repository.CompleteInterviewShadow(
		context.Background(),
		second,
		result,
	); err != nil {
		t.Fatalf("takeover completion: %v", err)
	}
}

func TestPostgresInterviewShadowFailureBackoffAndExhaustion(
	t *testing.T,
) {
	pool, repository, configuration, evaluation := prepareInterviewShadowRuntime(t)
	configuration.MaxAttempts = 2
	first := claimInterviewShadow(t, repository, configuration)
	status, err := repository.FailInterviewShadow(
		context.Background(),
		first,
		InterviewShadowFailure{
			Code:      "provider_timeout",
			Retryable: true,
		},
		configuration,
	)
	if err != nil {
		t.Fatalf("retryable FailInterviewShadow: %v", err)
	}
	if status != InterviewShadowRuntimePending {
		t.Fatalf("retryable failure status = %q", status)
	}
	var availableInFuture bool
	var leaseCleared bool
	if err := pool.QueryRow(context.Background(), `
		SELECT
			available_at > transaction_timestamp(),
			lease_expires_at IS NULL
		FROM evaluation_outbox
		WHERE id = $1
	`, first.OutboxID).Scan(
		&availableInFuture,
		&leaseCleared,
	); err != nil {
		t.Fatalf("inspect Interview Shadow backoff: %v", err)
	}
	if !availableInFuture || !leaseCleared {
		t.Fatalf(
			"backoff available_in_future=%t lease_cleared=%t",
			availableInFuture,
			leaseCleared,
		)
	}
	if _, acquired, err := repository.ClaimInterviewShadow(
		context.Background(),
		configuration,
	); err != nil || acquired {
		t.Fatalf("claim during backoff acquired=%t error=%v", acquired, err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE evaluation_outbox
		SET available_at = transaction_timestamp() - interval '1 second'
		WHERE id = $1
	`, first.OutboxID); err != nil {
		t.Fatalf("advance Interview Shadow backoff: %v", err)
	}
	second := claimInterviewShadow(t, repository, configuration)
	status, err = repository.FailInterviewShadow(
		context.Background(),
		second,
		InterviewShadowFailure{
			Code:      "provider_timeout",
			Retryable: true,
		},
		configuration,
	)
	if err != nil {
		t.Fatalf("exhausted FailInterviewShadow: %v", err)
	}
	if status != InterviewShadowRuntimeFailed {
		t.Fatalf("exhausted failure status = %q", status)
	}
	state, err := repository.GetInterviewShadowState(
		context.Background(),
		testOwnerA,
		evaluation.ID,
		evaluation.Revision.ID,
	)
	if err != nil {
		t.Fatalf("read failed Interview Shadow state: %v", err)
	}
	if state.ModuleStatus != InterviewShadowRuntimeFailed ||
		state.Failure == nil ||
		state.Failure.Code != "provider_timeout" {
		t.Fatalf("failed state = %#v", state)
	}
	assertInterviewShadowRevisionNotFinal(
		t,
		pool,
		evaluation.Revision.ID,
	)
}

func TestPostgresInterviewShadowDeletionWinsAgainstLateResult(
	t *testing.T,
) {
	pool, repository, configuration, _ := prepareInterviewShadowRuntime(t)
	claim := claimInterviewShadow(t, repository, configuration)
	result := evaluateInterviewShadowClaim(t, claim)
	ctx := context.Background()

	deletion, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deletion race: %v", err)
	}
	defer func() { _ = deletion.Rollback(ctx) }()
	if _, err := deletion.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("lock deleting owner: %v", err)
	}
	completed := make(chan error, 1)
	go func() {
		completed <- repository.CompleteInterviewShadow(
			context.Background(),
			claim,
			result,
		)
	}()
	select {
	case err := <-completed:
		t.Fatalf("late completion escaped deletion lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := deletion.Exec(ctx, `
		INSERT INTO evaluation_deletion_fences (
			owner_user_id,
			deletion_generation
		)
		VALUES ($1, 1)
		ON CONFLICT (owner_user_id) DO UPDATE
		SET deletion_generation = GREATEST(
			evaluation_deletion_fences.deletion_generation,
			EXCLUDED.deletion_generation
		)
	`, testOwnerA); err != nil {
		t.Fatalf("stage Evaluation deletion fence: %v", err)
	}
	if _, err := deletion.Exec(ctx, `
		DELETE FROM evaluation_module_runs
		WHERE owner_user_id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("delete Evaluation module run in race: %v", err)
	}
	if _, err := deletion.Exec(ctx, `
		DELETE FROM evaluation_ledgers
		WHERE owner_user_id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("delete Evaluation ledger in race: %v", err)
	}
	if _, err := deletion.Exec(ctx, `
		DELETE FROM evaluation_evidence_snapshots
		WHERE owner_user_id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("delete EvidenceSnapshot in race: %v", err)
	}
	if err := deletion.Commit(ctx); err != nil {
		t.Fatalf("commit deletion race: %v", err)
	}
	select {
	case err := <-completed:
		if !errors.Is(err, ErrAccountUnavailable) &&
			!errors.Is(err, ErrInterviewShadowLeaseLost) {
			t.Fatalf("late completion error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late completion did not resume after deletion")
	}
	var results int
	var runs int
	var fence int64
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*)
			 FROM evaluation_interview_scene_results),
			(SELECT count(*)
			 FROM evaluation_module_runs),
			(SELECT deletion_generation
			 FROM evaluation_deletion_fences
			 WHERE owner_user_id = $1)
	`, testOwnerA).Scan(&results, &runs, &fence); err != nil {
		t.Fatalf("inspect deletion-fenced runtime: %v", err)
	}
	if results != 0 || runs != 0 || fence != 1 {
		t.Fatalf(
			"after deletion results=%d runs=%d fence=%d",
			results,
			runs,
			fence,
		)
	}
}

func TestInterviewShadowRuntimeMigrationRoundTrip(t *testing.T) {
	pool := evaluationDatabaseThrough(
		t,
		"000039_evaluation_ielts_speaking_shadow_runtime.up.sql",
	)
	ctx := context.Background()
	ieltsDown, err := migrations.Files.ReadFile(
		"000039_evaluation_ielts_speaking_shadow_runtime.down.sql",
	)
	if err != nil {
		t.Fatalf("read IELTS Speaking Shadow down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(ieltsDown)); err != nil {
		t.Fatalf("apply IELTS Speaking Shadow down migration: %v", err)
	}
	resourceIDsDown, err := migrations.Files.ReadFile(
		"000038_evaluation_practice_resource_ids.down.sql",
	)
	if err != nil {
		t.Fatalf("read Practice resource IDs down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(resourceIDsDown)); err != nil {
		t.Fatalf("apply Practice resource IDs down migration: %v", err)
	}
	down, err := migrations.Files.ReadFile(
		"000037_evaluation_interview_shadow_runtime.down.sql",
	)
	if err != nil {
		t.Fatalf("read Interview Shadow down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply Interview Shadow down migration: %v", err)
	}
	for _, table := range []string{
		"evaluation_module_runs",
		"evaluation_interview_scene_results",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `
			SELECT to_regclass($1) IS NOT NULL
		`, table).Scan(&exists); err != nil {
			t.Fatalf("inspect down-migrated %s: %v", table, err)
		}
		if exists {
			t.Errorf("%s still exists after down migration", table)
		}
	}
	var leaseColumnExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'evaluation_outbox'
			  AND column_name = 'lease_expires_at'
		)
	`).Scan(&leaseColumnExists); err != nil {
		t.Fatalf("inspect down-migrated outbox: %v", err)
	}
	if leaseColumnExists {
		t.Fatal("lease_expires_at still exists after down migration")
	}

	up, err := migrations.Files.ReadFile(
		"000037_evaluation_interview_shadow_runtime.up.sql",
	)
	if err != nil {
		t.Fatalf("read Interview Shadow up migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("reapply Interview Shadow up migration: %v", err)
	}
	for _, table := range []string{
		"evaluation_module_runs",
		"evaluation_interview_scene_results",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `
			SELECT to_regclass($1) IS NOT NULL
		`, table).Scan(&exists); err != nil {
			t.Fatalf("inspect reapplied %s: %v", table, err)
		}
		if !exists {
			t.Errorf("%s missing after migration reapply", table)
		}
	}
	resourceIDsUp, err := migrations.Files.ReadFile(
		"000038_evaluation_practice_resource_ids.up.sql",
	)
	if err != nil {
		t.Fatalf("read Practice resource IDs up migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(resourceIDsUp)); err != nil {
		t.Fatalf("reapply Practice resource IDs up migration: %v", err)
	}
	ieltsUp, err := migrations.Files.ReadFile(
		"000039_evaluation_ielts_speaking_shadow_runtime.up.sql",
	)
	if err != nil {
		t.Fatalf("read IELTS Speaking Shadow up migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(ieltsUp)); err != nil {
		t.Fatalf("reapply IELTS Speaking Shadow up migration: %v", err)
	}
}

func interviewShadowReevaluationRequest(
	clientRequestID string,
) ReevaluateRequest {
	return ReevaluateRequest{
		Channels:         []Channel{ChannelScene},
		SceneStrategyRef: InterviewShadowStrategyRef,
		PipelineVersion:  InterviewShadowPipelineVersion,
		ClientRequestID:  clientRequestID,
	}
}

func interviewShadowReevaluationCommand(
	evaluationID string,
	fingerprintSeed string,
) ReevaluateCommand {
	config, err := normalizeReevaluation(
		interviewShadowReevaluationRequest(fingerprintSeed),
	)
	if err != nil {
		panic(err)
	}
	return ReevaluateCommand{
		OwnerUserID:         testOwnerA,
		EvaluationID:        evaluationID,
		RevisionFingerprint: sha256.Sum256([]byte(fingerprintSeed)),
		Config:              config,
	}
}

func reevaluateInterviewShadow(
	t *testing.T,
	repository *PostgresRepository,
	evaluationID string,
	clientRequestID string,
) Evaluation {
	t.Helper()
	next, replayed, err := repository.Reevaluate(
		context.Background(),
		interviewShadowReevaluationCommand(
			evaluationID,
			clientRequestID,
		),
	)
	if err != nil {
		t.Fatalf("Reevaluate Interview Shadow: %v", err)
	}
	if replayed || next.Revision.Number < 2 {
		t.Fatalf("re-evaluation = %#v, replayed=%t", next, replayed)
	}
	return next
}

func assertInterviewShadowTerminalRuntime(
	t *testing.T,
	pool *pgxpool.Pool,
	outboxID string,
	wantOutboxStatus string,
	wantRunStatus string,
	wantRevisionStatus string,
	wantFailureCode string,
) {
	t.Helper()
	var outboxStatus string
	var runStatus string
	var revisionStatus string
	var outboxFailure string
	var runFailure string
	var leaseCleared bool
	var isFinal bool
	var outboxAttempt int
	var runAttempt int
	var outboxToken int64
	var runToken int64
	if err := pool.QueryRow(context.Background(), `
		SELECT
			outbox.delivery_status,
			run.run_status,
			state.evaluation_status,
			coalesce(outbox.last_failure_code, ''),
			coalesce(run.last_failure_code, ''),
			outbox.lease_expires_at IS NULL,
			state.is_final,
			outbox.attempt_count,
			run.attempt_count,
			outbox.fencing_token,
			run.fencing_token
		FROM evaluation_outbox AS outbox
		JOIN evaluation_module_runs AS run
		  ON run.outbox_id = outbox.id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = outbox.evaluation_id
		 AND state.revision_id = outbox.evaluation_revision_id
		 AND state.owner_user_id = outbox.owner_user_id
		WHERE outbox.id = $1
	`, outboxID).Scan(
		&outboxStatus,
		&runStatus,
		&revisionStatus,
		&outboxFailure,
		&runFailure,
		&leaseCleared,
		&isFinal,
		&outboxAttempt,
		&runAttempt,
		&outboxToken,
		&runToken,
	); err != nil {
		t.Fatalf("inspect terminal Interview Shadow runtime: %v", err)
	}
	if outboxStatus != wantOutboxStatus ||
		runStatus != wantRunStatus ||
		revisionStatus != wantRevisionStatus ||
		outboxFailure != wantFailureCode ||
		runFailure != wantFailureCode ||
		!leaseCleared ||
		isFinal ||
		outboxAttempt != runAttempt ||
		outboxToken != runToken {
		t.Fatalf(
			"terminal runtime outbox=%q run=%q revision=%q "+
				"outbox_failure=%q run_failure=%q "+
				"lease_cleared=%t is_final=%t "+
				"attempts=%d/%d tokens=%d/%d",
			outboxStatus,
			runStatus,
			revisionStatus,
			outboxFailure,
			runFailure,
			leaseCleared,
			isFinal,
			outboxAttempt,
			runAttempt,
			outboxToken,
			runToken,
		)
	}
}

func assertSupersededInterviewShadowRuntime(
	t *testing.T,
	pool *pgxpool.Pool,
	revisionID string,
) {
	t.Helper()
	var outboxStatus string
	var runStatus string
	var revisionStatus string
	var outboxFailure string
	var runFailure string
	var leaseCleared bool
	var isFinal bool
	if err := pool.QueryRow(context.Background(), `
		SELECT
			outbox.delivery_status,
			coalesce(run.run_status, ''),
			state.evaluation_status,
			coalesce(outbox.last_failure_code, ''),
			coalesce(run.last_failure_code, ''),
			outbox.lease_expires_at IS NULL,
			state.is_final
		FROM evaluation_outbox AS outbox
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = outbox.evaluation_id
		 AND state.revision_id = outbox.evaluation_revision_id
		 AND state.owner_user_id = outbox.owner_user_id
		LEFT JOIN evaluation_module_runs AS run
		  ON run.outbox_id = outbox.id
		WHERE outbox.evaluation_revision_id = $1
		  AND outbox.channel = 'SCENE'
	`, revisionID).Scan(
		&outboxStatus,
		&runStatus,
		&revisionStatus,
		&outboxFailure,
		&runFailure,
		&leaseCleared,
		&isFinal,
	); err != nil {
		t.Fatalf("inspect superseded Interview Shadow runtime: %v", err)
	}
	if outboxStatus != "FAILED" ||
		revisionStatus != "SUPERSEDED" ||
		outboxFailure != interviewShadowRevisionSupersededCode ||
		!leaseCleared ||
		isFinal ||
		(runStatus != "" && runStatus != "FAILED") ||
		(runStatus == "FAILED" &&
			runFailure != interviewShadowRevisionSupersededCode) {
		t.Fatalf(
			"superseded runtime outbox=%q run=%q revision=%q "+
				"outbox_failure=%q run_failure=%q "+
				"lease_cleared=%t is_final=%t",
			outboxStatus,
			runStatus,
			revisionStatus,
			outboxFailure,
			runFailure,
			leaseCleared,
			isFinal,
		)
	}
}

func assertCurrentInterviewShadowRevisionRunnable(
	t *testing.T,
	pool *pgxpool.Pool,
	revisionID string,
) {
	t.Helper()
	var outboxStatus string
	var runStatus string
	var revisionStatus string
	if err := pool.QueryRow(context.Background(), `
		SELECT
			outbox.delivery_status,
			coalesce(run.run_status, ''),
			state.evaluation_status
		FROM evaluation_outbox AS outbox
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = outbox.evaluation_id
		 AND state.revision_id = outbox.evaluation_revision_id
		 AND state.owner_user_id = outbox.owner_user_id
		LEFT JOIN evaluation_module_runs AS run
		  ON run.outbox_id = outbox.id
		WHERE outbox.evaluation_revision_id = $1
		  AND outbox.channel = 'SCENE'
	`, revisionID).Scan(
		&outboxStatus,
		&runStatus,
		&revisionStatus,
	); err != nil {
		t.Fatalf("inspect replacement Interview Shadow runtime: %v", err)
	}
	valid := outboxStatus == "PENDING" &&
		((revisionStatus == "QUEUED" && runStatus == "") ||
			(revisionStatus == "RUNNING" && runStatus == "RUNNING"))
	if !valid {
		t.Fatalf(
			"replacement runtime outbox=%q run=%q revision=%q",
			outboxStatus,
			runStatus,
			revisionStatus,
		)
	}
}

func assertCompletedOrCancelledInterviewShadowRuntime(
	t *testing.T,
	pool *pgxpool.Pool,
	claim InterviewShadowClaim,
	completeErr error,
) {
	t.Helper()
	var outboxStatus string
	var runStatus string
	var revisionStatus string
	var outboxFailure string
	var runFailure string
	var resultCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT
			outbox.delivery_status,
			run.run_status,
			state.evaluation_status,
			coalesce(outbox.last_failure_code, ''),
			coalesce(run.last_failure_code, ''),
			(SELECT count(*)
			 FROM evaluation_interview_scene_results AS result
			 WHERE result.module_run_id = run.id)
		FROM evaluation_outbox AS outbox
		JOIN evaluation_module_runs AS run
		  ON run.outbox_id = outbox.id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = outbox.evaluation_id
		 AND state.revision_id = outbox.evaluation_revision_id
		 AND state.owner_user_id = outbox.owner_user_id
		WHERE outbox.id = $1
	`, claim.OutboxID).Scan(
		&outboxStatus,
		&runStatus,
		&revisionStatus,
		&outboxFailure,
		&runFailure,
		&resultCount,
	); err != nil {
		t.Fatalf("inspect completion/re-evaluation race: %v", err)
	}
	if revisionStatus != "SUPERSEDED" {
		t.Fatalf("raced revision status = %q", revisionStatus)
	}
	if completeErr == nil {
		if outboxStatus != "DELIVERED" ||
			runStatus != "READY" ||
			outboxFailure != "" ||
			runFailure != "" ||
			resultCount != 1 {
			t.Fatalf(
				"completed runtime outbox=%q run=%q "+
					"outbox_failure=%q run_failure=%q results=%d",
				outboxStatus,
				runStatus,
				outboxFailure,
				runFailure,
				resultCount,
			)
		}
		return
	}
	if outboxStatus != "FAILED" ||
		runStatus != "FAILED" ||
		outboxFailure != interviewShadowRevisionSupersededCode ||
		runFailure != interviewShadowRevisionSupersededCode ||
		resultCount != 0 {
		t.Fatalf(
			"cancelled runtime outbox=%q run=%q "+
				"outbox_failure=%q run_failure=%q results=%d",
			outboxStatus,
			runStatus,
			outboxFailure,
			runFailure,
			resultCount,
		)
	}
}

func prepareInterviewShadowRuntime(
	t *testing.T,
) (
	*pgxpool.Pool,
	*PostgresRepository,
	InterviewShadowRuntimeConfiguration,
	Evaluation,
) {
	t.Helper()
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	service, request := serviceWithEvidenceSnapshot(
		t,
		pool,
		testOwnerA,
	)
	request.SceneStrategyRef = InterviewShadowStrategyRef
	request.PipelineVersion = InterviewShadowPipelineVersion
	evaluation, replayed, err := service.Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		request,
	)
	if err != nil {
		t.Fatalf("create Interview Shadow Evaluation: %v", err)
	}
	if replayed {
		t.Fatal("fresh Interview Shadow Evaluation was replayed")
	}
	configuration := InterviewShadowRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   5 * time.Second,
		StrategyRef:     InterviewShadowStrategyRef,
		PipelineVersion: InterviewShadowPipelineVersion,
		FullConfigHash: sha256.Sum256(
			[]byte("interview-shadow-integration-config/v1"),
		),
		PromptVersion: InterviewShadowPromptVersion,
		Provider:      "qianwen",
		Model:         "qwen-plus",
	}
	if !configuration.Valid() {
		t.Fatalf("invalid Interview Shadow runtime config: %#v", configuration)
	}
	return pool, NewPostgresRepository(pool), configuration, evaluation
}

func claimInterviewShadow(
	t *testing.T,
	repository *PostgresRepository,
	configuration InterviewShadowRuntimeConfiguration,
) InterviewShadowClaim {
	t.Helper()
	claim, acquired, err := repository.ClaimInterviewShadow(
		context.Background(),
		configuration,
	)
	if err != nil {
		t.Fatalf("ClaimInterviewShadow: %v", err)
	}
	if !acquired || !claim.Valid() {
		t.Fatalf("claim acquired=%t value=%#v", acquired, claim)
	}
	return claim
}

func evaluateInterviewShadowClaim(
	t *testing.T,
	claim InterviewShadowClaim,
) InterviewShadowResult {
	t.Helper()
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), claim.Snapshot)
	if err != nil {
		t.Fatalf("evaluate Interview Shadow claim: %v", err)
	}
	return result
}

func expireInterviewShadowLease(
	t *testing.T,
	pool *pgxpool.Pool,
	outboxID string,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE evaluation_outbox
		SET lease_expires_at =
		    clock_timestamp() - interval '1 second',
		    updated_at = transaction_timestamp()
		WHERE id = $1
	`, outboxID); err != nil {
		t.Fatalf("expire Interview Shadow lease: %v", err)
	}
}

func assertInterviewShadowRevisionNotFinal(
	t *testing.T,
	pool *pgxpool.Pool,
	revisionID string,
) {
	t.Helper()
	var isFinal bool
	if err := pool.QueryRow(context.Background(), `
		SELECT is_final
		FROM evaluation_revision_states
		WHERE revision_id = $1
	`, revisionID).Scan(&isFinal); err != nil {
		t.Fatalf("read Interview Shadow final marker: %v", err)
	}
	if isFinal {
		t.Fatal("Shadow revision was promoted to a formal final result")
	}
}
