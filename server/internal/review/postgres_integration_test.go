package review_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	userA = "10000000-0000-4000-8000-000000000001"
	userB = "10000000-0000-4000-8000-000000000002"
	userC = "10000000-0000-4000-8000-000000000003"
)

func TestPostgresScenarioReviewPersistsContextScoresAndPreciseEvidence(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	command := ensureCommand(userA, "scenario-review-session")
	command.ImplementationVersion = "qianwen-scenario-review-v2"
	command.SourceTurnVersion = "conversation-turn:evidence-v1"
	command.EvaluationContext = review.EvaluationContext{
		SchemaVersion:             review.EvaluationContextSchemaVersion,
		ContextType:               review.ContextInterviewProjectDeepDive,
		SceneKey:                  "interview",
		ScenarioDefinitionID:      "programmer-interview",
		ScenarioDefinitionVersion: 1,
		PracticeOptionType:        "project_deep_dive",
		DifficultyRef:             "difficulty.intermediate.v1",
		AssistanceRef:             "assistance.standard.v1",
		TurnPolicyRef:             "interview.project_deep_dive.turn.v1",
		SessionPolicyRef:          "interview.project_deep_dive.session.v1",
		SceneSpecificContext: review.SceneSpecificContext{
			Type: review.ContextInterviewProjectDeepDive,
			Interview: &review.InterviewProjectDeepDiveV1{
				Version:       "interview.project_deep_dive.v1",
				ProjectBrief:  "A resilient checkout service.",
				CandidateRole: "Backend engineer",
				FocusPoints:   []string{"trade-offs", "impact"},
			},
		},
	}
	pending, err := repository.EnsurePending(context.Background(), command)
	if err != nil {
		t.Fatalf("ensure scenario Review: %v", err)
	}
	if pending.EvaluationContext.ContextType !=
		review.ContextInterviewProjectDeepDive {
		t.Fatalf("pending evaluation context = %+v", pending.EvaluationContext)
	}
	_, claim, claimed, err := repository.ClaimGeneration(
		context.Background(),
		command.Actor,
		pending.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf("claim scenario Review: claimed=%v err=%v", claimed, err)
	}

	dimensions := []struct {
		key   string
		score int
	}{
		{"relevance_structure", 80},
		{"technical_depth", 70},
		{"ownership_decisions", 90},
		{"evidence_impact", 60},
		{"language_clarity", 80},
	}
	result := review.ReviewResult{
		SummaryEligibility: review.SummaryEligible,
		Summary:            "The answer is structured and evidence-based.",
		FeedbackItems: []review.ReviewFeedbackItem{{
			Key:     "feedback-impact",
			Kind:    review.FeedbackImprovement,
			Message: "Quantify the impact.",
		}},
		RepracticeSuggestionRefs: []string{"feedback-impact"},
	}
	evidence := make([]review.ReviewEvidence, 0, len(dimensions)+1)
	start, end := 2, 13
	for _, dimension := range dimensions {
		key := "conclusion-" + dimension.key
		result.Conclusions = append(result.Conclusions, review.ReviewConclusion{
			Key:      key,
			Category: dimension.key,
			Score:    dimension.score,
			Message:  "Grounded conclusion.",
		})
		evidence = append(evidence, review.ReviewEvidence{
			TargetKind:    review.EvidenceTargetConclusion,
			TargetKey:     key,
			SourceType:    review.SourceTypeConversationTurn,
			SourceID:      "turn-3",
			SourceVersion: command.SourceTurnVersion,
			Field:         "answer_text",
			AnchorKind:    review.EvidenceAnchorExactQuote,
			Quote:         "chose café",
			StartUTF8Byte: &start,
			EndUTF8Byte:   &end,
		})
	}
	evidence = append(evidence, review.ReviewEvidence{
		TargetKind:    review.EvidenceTargetFeedback,
		TargetKey:     "feedback-impact",
		SourceType:    review.SourceTypeConversationTurn,
		SourceID:      "turn-3",
		SourceVersion: command.SourceTurnVersion,
		Field:         "answer_text",
		AnchorKind:    review.EvidenceAnchorExactQuote,
		Quote:         "chose café",
		StartUTF8Byte: &start,
		EndUTF8Byte:   &end,
	})
	completed, err := repository.CompleteGeneration(
		context.Background(),
		claim,
		result,
		evidence,
	)
	if err != nil {
		t.Fatalf("complete scenario Review: %v", err)
	}
	recovered, err := repository.Get(
		context.Background(),
		command.Actor,
		completed.ID,
	)
	if err != nil {
		t.Fatalf("get scenario Review: %v", err)
	}
	if recovered.Result == nil ||
		recovered.Result.OverallScorePresent ||
		recovered.Result.OverallScore != 0 ||
		recovered.EvaluationContext.SessionPolicyRef !=
			command.EvaluationContext.SessionPolicyRef ||
		len(recovered.Evidence) != len(evidence) ||
		recovered.Evidence[len(recovered.Evidence)-1].TargetKind !=
			review.EvidenceTargetFeedback {
		t.Fatalf("recovered scenario Review = %+v", recovered)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE reviews
		    SET result = jsonb_set(result, '{overall_score}', '76'::jsonb)
		  WHERE id = $1`,
		completed.ID,
	); err == nil {
		t.Fatal("PostgreSQL accepted a non-IELTS overall score")
	}
}

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
		"provider_timeout",
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

func TestPostgresReviewConcurrentTerminalFailureStopsAllWaiters(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	command := ensureCommand(userA, "session-concurrent-quota-exhausted")
	generator := &terminalCountingGenerator{delay: 100 * time.Millisecond}
	service := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		generator,
	)

	const callerCount = 16
	start := make(chan struct{})
	errs := make(chan error, callerCount)
	var callers sync.WaitGroup
	callers.Add(callerCount)
	for range callerCount {
		go func() {
			defer callers.Done()
			<-start
			_, err := service.EnsureReview(context.Background(), command)
			errs <- err
		}()
	}
	close(start)
	callers.Wait()
	close(errs)

	for err := range errs {
		if !errors.Is(err, review.ErrGenerationFailed) {
			t.Fatalf("concurrent quota failure error = %v", err)
		}
		var categorized review.StableGenerationError
		if !errors.As(err, &categorized) ||
			categorized.StableCategory() != "quota_exhausted" {
			t.Fatalf("concurrent quota failure lost category: %v", err)
		}
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("generator calls = %d, want 1", got)
	}

	repository := review.NewPostgresRepository(pool)
	persisted, err := repository.EnsurePending(context.Background(), command)
	if err != nil {
		t.Fatalf("read persisted concurrent quota failure: %v", err)
	}
	attempts, err := repository.ListAttempts(
		context.Background(),
		command.Actor,
		persisted.ID,
	)
	if err != nil {
		t.Fatalf("list concurrent quota attempts: %v", err)
	}
	if len(attempts) != 1 ||
		attempts[0].StableErrorCategory != "quota_exhausted" {
		t.Fatalf("concurrent quota attempts = %+v", attempts)
	}
}

func TestPostgresReviewPersistsCompletionAfterRequestCancellation(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	command := ensureCommand(userA, "session-cancelled-after-success")
	ctx, cancel := context.WithCancel(context.Background())
	generator := &cancelingGenerator{cancel: cancel}

	completed, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		generator,
	).EnsureReview(ctx, command)
	if err != nil {
		t.Fatalf("complete after request cancellation: %v", err)
	}
	if completed.Status != review.FormalReviewCompleted ||
		len(completed.Evidence) != 1 {
		t.Fatalf("completed Review = %+v", completed)
	}

	replayGenerator := &countingGenerator{}
	replayed, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		replayGenerator,
	).EnsureReview(context.Background(), command)
	if err != nil {
		t.Fatalf("recover completion after cancellation: %v", err)
	}
	if replayed.ID != completed.ID || replayed.Status != review.FormalReviewCompleted {
		t.Fatalf("replayed Review = %+v, want completed id %s", replayed, completed.ID)
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("canceling generator calls = %d, want 1", got)
	}
	if got := replayGenerator.calls.Load(); got != 0 {
		t.Fatalf("replay generator calls = %d, want 0", got)
	}
}

func TestPostgresReviewPersistsTerminalFailureAfterRequestCancellation(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	command := ensureCommand(userA, "session-cancelled-after-quota")
	ctx, cancel := context.WithCancel(context.Background())
	generator := &cancelingGenerator{
		cancel:          cancel,
		terminalFailure: true,
	}

	if _, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		generator,
	).EnsureReview(ctx, command); !errors.Is(err, review.ErrGenerationFailed) {
		t.Fatalf("quota failure after request cancellation: %v", err)
	}

	replayGenerator := &countingGenerator{}
	for attempt := 0; attempt < 3; attempt++ {
		_, err := review.NewEnsureService(
			review.NewPostgresRepository(pool),
			sourceReader{},
			replayGenerator,
		).EnsureReview(context.Background(), command)
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
		t.Fatalf("canceling generator calls = %d, want 1", got)
	}
	if got := replayGenerator.calls.Load(); got != 0 {
		t.Fatalf("replay generator calls = %d, want 0", got)
	}

	repository := review.NewPostgresRepository(pool)
	persisted, err := repository.EnsurePending(context.Background(), command)
	if err != nil {
		t.Fatalf("read persisted quota failure: %v", err)
	}
	attempts, err := repository.ListAttempts(
		context.Background(),
		command.Actor,
		persisted.ID,
	)
	if err != nil {
		t.Fatalf("list persisted quota attempts: %v", err)
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

func TestPostgresReviewPersistsInvalidResultAfterRequestCancellation(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	command := ensureCommand(userA, "session-cancelled-after-invalid-result")
	ctx, cancel := context.WithCancel(context.Background())
	generator := &cancelingGenerator{
		cancel:        cancel,
		invalidResult: true,
	}

	if _, err := review.NewEnsureService(
		review.NewPostgresRepository(pool),
		sourceReader{},
		generator,
	).EnsureReview(ctx, command); !errors.Is(err, review.ErrGenerationFailed) {
		t.Fatalf("invalid result after request cancellation: %v", err)
	}

	repository := review.NewPostgresRepository(pool)
	persisted, err := repository.EnsurePending(context.Background(), command)
	if err != nil {
		t.Fatalf("read persisted invalid result failure: %v", err)
	}
	attempts, err := repository.ListAttempts(
		context.Background(),
		command.Actor,
		persisted.ID,
	)
	if err != nil {
		t.Fatalf("list persisted invalid result attempts: %v", err)
	}
	if persisted.Status != review.FormalReviewFailed ||
		persisted.StableErrorCategory != "invalid_result" ||
		len(attempts) != 1 ||
		attempts[0].StableErrorCategory != "invalid_result" {
		t.Fatalf(
			"persisted invalid result = %+v, attempts = %+v",
			persisted,
			attempts,
		)
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("canceling generator calls = %d, want 1", got)
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
	if _, err := pool.Exec(
		context.Background(),
		`DELETE FROM identity_users WHERE id = $1`,
		userC,
	); err != nil {
		t.Fatalf("physically delete user C identity: %v", err)
	}
	if err := repository.DeleteUserData(context.Background(), review.DeleteUserReviewsCommand{
		UserID:             userC,
		DeletionGeneration: 1,
	}); err != nil {
		t.Fatalf("replay deletion after physical identity removal: %v", err)
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

func TestPostgresCompletedHistoryUsesOwnerScopedStableKeysetPagination(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA, userB)

	repository := review.NewPostgresRepository(pool)
	ensure := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{},
	)
	history := review.NewHistoryService(repository)

	var ownerReviews []review.FormalReview
	for index := 1; index <= 3; index++ {
		item, err := ensure.EnsureReview(
			context.Background(),
			ensureCommand(userA, fmt.Sprintf("history-session-%d", index)),
		)
		if err != nil {
			t.Fatalf("ensure owner Review %d: %v", index, err)
		}
		ownerReviews = append(ownerReviews, item)
	}
	foreign, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userB, "foreign-newer-history-session"),
	)
	if err != nil {
		t.Fatalf("ensure foreign Review: %v", err)
	}
	if _, err := repository.EnsurePending(
		context.Background(),
		ensureCommand(userA, "history-pending"),
	); err != nil {
		t.Fatalf("ensure pending Review: %v", err)
	}

	newerCreatedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(
		time.Microsecond,
	)
	olderCreatedAt := newerCreatedAt.Add(-time.Hour)
	for _, item := range ownerReviews[:2] {
		if _, err := pool.Exec(
			context.Background(),
			`UPDATE reviews SET created_at = $1 WHERE id = $2`,
			newerCreatedAt,
			item.ID,
		); err != nil {
			t.Fatalf("align owner Review created_at: %v", err)
		}
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE reviews SET created_at = $1 WHERE id = $2`,
		olderCreatedAt,
		ownerReviews[2].ID,
	); err != nil {
		t.Fatalf("set older owner Review created_at: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE reviews SET created_at = $1 WHERE id = $2`,
		newerCreatedAt.Add(time.Hour),
		foreign.ID,
	); err != nil {
		t.Fatalf("set foreign Review created_at: %v", err)
	}

	newerIDs := []string{ownerReviews[0].ID, ownerReviews[1].ID}
	slices.Sort(newerIDs)
	slices.Reverse(newerIDs)
	first, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 2},
	)
	if err != nil {
		t.Fatalf("list first history page: %v", err)
	}
	if len(first.Items) != 2 ||
		first.Items[0].ID != newerIDs[0] ||
		first.Items[1].ID != newerIDs[1] ||
		first.Next == nil ||
		first.Next.ReviewID != newerIDs[1] ||
		!first.Next.CreatedAt.Equal(newerCreatedAt) {
		t.Fatalf("first history page = %+v", first)
	}
	for _, item := range first.Items {
		if item.OwnerUserID != userA ||
			item.Status != review.FormalReviewCompleted {
			t.Fatalf("history leaked non-owner/non-result: %+v", item)
		}
	}

	second, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 2, Before: first.Next},
	)
	if err != nil {
		t.Fatalf("list second history page: %v", err)
	}
	if len(second.Items) != 1 ||
		second.Items[0].ID != ownerReviews[2].ID ||
		second.Next != nil {
		t.Fatalf("second history page = %+v", second)
	}

	// Results committed before the write budget was introduced remain valid
	// history. New writes are stricter, but repository reads preserve the
	// previous production validation contract.
	legacyResult := validResult()
	legacyResult.Summary = strings.Repeat("s", 2049)
	legacyPayload, err := json.Marshal(legacyResult)
	if err != nil {
		t.Fatalf("marshal legacy persisted Review: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE reviews SET result = $1::jsonb WHERE id = $2`,
		legacyPayload,
		ownerReviews[0].ID,
	); err != nil {
		t.Fatalf("stage legacy persisted Review: %v", err)
	}
	legacy, err := repository.Get(
		context.Background(),
		actor(userA),
		ownerReviews[0].ID,
	)
	if err != nil || legacy.Result == nil ||
		legacy.Result.Summary != legacyResult.Summary {
		t.Fatalf("legacy persisted Review = %+v, %v", legacy, err)
	}
	legacyPage, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 2},
	)
	legacyIndex := slices.IndexFunc(
		legacyPage.Items,
		func(item review.FormalReview) bool {
			return item.ID == ownerReviews[0].ID
		},
	)
	if err != nil || len(legacyPage.Items) != 2 ||
		legacyIndex < 0 ||
		legacyPage.Items[legacyIndex].Result == nil ||
		legacyPage.Items[legacyIndex].Result.Summary != legacyResult.Summary {
		t.Fatalf("legacy persisted Review history = %+v, %v", legacyPage, err)
	}

	setAccountStatus(t, pool, userA, "deleting")
	if err := repository.DeleteUserData(
		context.Background(),
		review.DeleteUserReviewsCommand{
			UserID:             userA,
			DeletionGeneration: 1,
		},
	); err != nil {
		t.Fatalf("delete owner Review data: %v", err)
	}
	if _, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 2},
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("deleted account history error = %v", err)
	}
}

func TestPostgresReviewRejectsNULBeforeJSONBWrite(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	command := ensureCommand(userA, "nul-result-session")
	pending, err := repository.EnsurePending(
		context.Background(),
		command,
	)
	if err != nil {
		t.Fatalf("ensure pending NUL Review: %v", err)
	}
	_, claim, claimed, err := repository.ClaimGeneration(
		context.Background(),
		command.Actor,
		pending.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf("claim NUL Review: claimed=%v err=%v", claimed, err)
	}
	nulResult := validResult()
	nulResult.Summary = "clear\x00answer"
	if _, err := repository.CompleteGeneration(
		context.Background(),
		claim,
		nulResult,
		validEvidence(),
	); !errors.Is(err, review.ErrInvalidReview) {
		t.Fatalf("NUL Result completion error = %v", err)
	}
	persisted, err := repository.Get(
		context.Background(),
		command.Actor,
		pending.ID,
	)
	if err != nil {
		t.Fatalf("read Review after rejected NUL completion: %v", err)
	}
	if persisted.Status != review.FormalReviewGenerating ||
		persisted.Result != nil {
		t.Fatalf("rejected NUL completion changed Review: %+v", persisted)
	}

	_, err = pool.Exec(
		context.Background(),
		`SELECT $1::jsonb`,
		`{"summary":"clear\u0000answer"}`,
	)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) ||
		postgresError.Code != "22P05" {
		t.Fatalf(
			"PostgreSQL NUL jsonb error = %v, want SQLSTATE 22P05",
			err,
		)
	}
}

func TestPostgresMaximumReviewResultCompletesAndReadsBackAtEveryBoundary(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	command := ensureCommand(userA, "maximum-result-session")
	pending, err := repository.EnsurePending(
		context.Background(),
		command,
	)
	if err != nil {
		t.Fatalf("ensure maximum Result Review: %v", err)
	}
	_, claim, claimed, err := repository.ClaimGeneration(
		context.Background(),
		command.Actor,
		pending.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf(
			"claim maximum Result Review: claimed=%v err=%v",
			claimed,
			err,
		)
	}
	maximum := maximumPersistedReviewResult(t)
	assertReviewResultJSONBytes(t, maximum, 12*1024)
	completed, err := repository.CompleteGeneration(
		context.Background(),
		claim,
		maximum,
		evidenceForReviewResult(maximum),
	)
	if err != nil {
		t.Fatalf("complete maximum Result Review: %v", err)
	}
	if completed.Status != review.FormalReviewCompleted ||
		completed.Result == nil {
		t.Fatalf("completed maximum Result Review = %+v", completed)
	}
	assertReviewResultJSONBytes(t, *completed.Result, 12*1024)

	recovered, err := repository.Get(
		context.Background(),
		command.Actor,
		completed.ID,
	)
	if err != nil {
		t.Fatalf("get maximum Result Review: %v", err)
	}
	if recovered.ID != completed.ID || recovered.Result == nil {
		t.Fatalf("recovered maximum Result Review = %+v", recovered)
	}
	assertReviewResultJSONBytes(t, *recovered.Result, 12*1024)

	page, err := review.NewHistoryService(repository).ListCompleted(
		context.Background(),
		command.Actor,
		review.HistoryQuery{Limit: 1},
	)
	if err != nil {
		t.Fatalf("list maximum Result Review: %v", err)
	}
	if len(page.Items) != 1 ||
		page.Items[0].ID != completed.ID ||
		page.Items[0].Result == nil ||
		page.Next != nil {
		t.Fatalf("maximum Result history page = %+v", page)
	}
	assertReviewResultJSONBytes(t, *page.Items[0].Result, 12*1024)
}

func TestPostgresCompletedHistoryExcludesEveryNonCompletedState(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	ensure := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{},
	)
	completed, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userA, "status-completed"),
	)
	if err != nil {
		t.Fatalf("ensure completed status fixture: %v", err)
	}
	pending, err := repository.EnsurePending(
		context.Background(),
		ensureCommand(userA, "status-pending"),
	)
	if err != nil {
		t.Fatalf("ensure pending status fixture: %v", err)
	}
	generating, err := repository.EnsurePending(
		context.Background(),
		ensureCommand(userA, "status-generating"),
	)
	if err != nil {
		t.Fatalf("ensure generating status fixture: %v", err)
	}
	generating, _, claimed, err := repository.ClaimGeneration(
		context.Background(),
		actor(userA),
		generating.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf(
			"claim generating status fixture: claimed=%v err=%v",
			claimed,
			err,
		)
	}
	failed, err := repository.EnsurePending(
		context.Background(),
		ensureCommand(userA, "status-failed"),
	)
	if err != nil {
		t.Fatalf("ensure failed status fixture: %v", err)
	}
	failed, failedClaim, claimed, err := repository.ClaimGeneration(
		context.Background(),
		actor(userA),
		failed.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf(
			"claim failed status fixture: claimed=%v err=%v",
			claimed,
			err,
		)
	}
	if err := repository.FailGeneration(
		context.Background(),
		failedClaim,
		"provider_timeout",
	); err != nil {
		t.Fatalf("fail status fixture: %v", err)
	}

	expectedStates := map[string]review.FormalReviewStatus{
		pending.ID:    review.FormalReviewPending,
		generating.ID: review.FormalReviewGenerating,
		failed.ID:     review.FormalReviewFailed,
	}
	for reviewID, expectedStatus := range expectedStates {
		item, err := repository.Get(
			context.Background(),
			actor(userA),
			reviewID,
		)
		if err != nil {
			t.Fatalf("get %s status fixture: %v", expectedStatus, err)
		}
		if item.Status != expectedStatus {
			t.Fatalf(
				"status fixture %s = %s",
				reviewID,
				item.Status,
			)
		}
	}

	page, err := review.NewHistoryService(repository).ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 50},
	)
	if err != nil {
		t.Fatalf("list completed-only status page: %v", err)
	}
	if len(page.Items) != 1 ||
		page.Items[0].ID != completed.ID ||
		page.Items[0].Status != review.FormalReviewCompleted ||
		page.Next != nil {
		t.Fatalf("completed-only status page = %+v", page)
	}
}

func TestPostgresCompletedHistoryCursorKeepsItsKeysetBoundaryAfterNewerInsert(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	ensure := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{},
	)
	history := review.NewHistoryService(repository)

	baseCreatedAt := time.Now().UTC().Add(-time.Hour).Truncate(
		time.Microsecond,
	)
	originals := make([]review.FormalReview, 4)
	for index := range originals {
		item, err := ensure.EnsureReview(
			context.Background(),
			ensureCommand(
				userA,
				fmt.Sprintf("cursor-boundary-original-%d", index),
			),
		)
		if err != nil {
			t.Fatalf("ensure original Review %d: %v", index, err)
		}
		createdAt := baseCreatedAt.Add(
			-time.Duration(index) * time.Minute,
		)
		if _, err := pool.Exec(
			context.Background(),
			`UPDATE reviews SET created_at = $1 WHERE id = $2`,
			createdAt,
			item.ID,
		); err != nil {
			t.Fatalf("set original Review %d key: %v", index, err)
		}
		item.CreatedAt = createdAt
		originals[index] = item
	}

	first, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 2},
	)
	if err != nil {
		t.Fatalf("list keyset first page: %v", err)
	}
	if len(first.Items) != 2 ||
		first.Items[0].ID != originals[0].ID ||
		first.Items[1].ID != originals[1].ID ||
		first.Next == nil {
		t.Fatalf("keyset first page = %+v", first)
	}

	inserted, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userA, "cursor-boundary-newer-insert"),
	)
	if err != nil {
		t.Fatalf("ensure newly inserted Review: %v", err)
	}
	insertedCreatedAt := baseCreatedAt.Add(time.Minute)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE reviews SET created_at = $1 WHERE id = $2`,
		insertedCreatedAt,
		inserted.ID,
	); err != nil {
		t.Fatalf("set newly inserted Review key: %v", err)
	}

	// The cursor is a stable lower keyset boundary, not an MVCC snapshot:
	// a newly inserted row ahead of that boundary belongs only to a fresh
	// traversal, while the old continuation still covers the original tail.
	continuation, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 2, Before: first.Next},
	)
	if err != nil {
		t.Fatalf("continue from old keyset cursor: %v", err)
	}
	if len(continuation.Items) != 2 ||
		continuation.Items[0].ID != originals[2].ID ||
		continuation.Items[1].ID != originals[3].ID ||
		continuation.Next != nil {
		t.Fatalf("old keyset continuation = %+v", continuation)
	}
	for _, item := range continuation.Items {
		if item.ID == inserted.ID ||
			item.ID == first.Items[0].ID ||
			item.ID == first.Items[1].ID {
			t.Fatalf(
				"old keyset continuation repeated/mixed item: %+v",
				item,
			)
		}
	}

	fresh, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 2},
	)
	if err != nil {
		t.Fatalf("list fresh keyset page: %v", err)
	}
	if len(fresh.Items) != 2 ||
		fresh.Items[0].ID != inserted.ID ||
		fresh.Items[1].ID != originals[0].ID {
		t.Fatalf("fresh keyset page after insert = %+v", fresh)
	}
}

func TestPostgresCompletedHistoryAppliesForeignCursorOnlyAsActorKeyset(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA, userB)
	repository := review.NewPostgresRepository(pool)
	ensure := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{},
	)
	history := review.NewHistoryService(repository)
	baseCreatedAt := time.Now().UTC().Add(-time.Hour).Truncate(
		time.Microsecond,
	)

	ownerReviews := make([]review.FormalReview, 2)
	for index := range ownerReviews {
		item, err := ensure.EnsureReview(
			context.Background(),
			ensureCommand(
				userA,
				fmt.Sprintf("foreign-cursor-owner-%d", index),
			),
		)
		if err != nil {
			t.Fatalf("ensure cursor owner Review %d: %v", index, err)
		}
		createdAt := baseCreatedAt.Add(
			-time.Duration(index*3) * time.Minute,
		)
		if _, err := pool.Exec(
			context.Background(),
			`UPDATE reviews SET created_at = $1 WHERE id = $2`,
			createdAt,
			item.ID,
		); err != nil {
			t.Fatalf("set cursor owner Review %d key: %v", index, err)
		}
		ownerReviews[index] = item
	}
	ownerFirst, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 1},
	)
	if err != nil {
		t.Fatalf("list cursor owner first page: %v", err)
	}
	if len(ownerFirst.Items) != 1 ||
		ownerFirst.Items[0].ID != ownerReviews[0].ID ||
		ownerFirst.Next == nil {
		t.Fatalf("cursor owner first page = %+v", ownerFirst)
	}

	foreignReviews := make([]review.FormalReview, 3)
	foreignOffsets := []time.Duration{
		time.Minute,
		-time.Minute,
		-2 * time.Minute,
	}
	for index := range foreignReviews {
		item, err := ensure.EnsureReview(
			context.Background(),
			ensureCommand(
				userB,
				fmt.Sprintf("foreign-cursor-reader-%d", index),
			),
		)
		if err != nil {
			t.Fatalf("ensure cursor reader Review %d: %v", index, err)
		}
		if _, err := pool.Exec(
			context.Background(),
			`UPDATE reviews SET created_at = $1 WHERE id = $2`,
			baseCreatedAt.Add(foreignOffsets[index]),
			item.ID,
		); err != nil {
			t.Fatalf("set cursor reader Review %d key: %v", index, err)
		}
		foreignReviews[index] = item
	}

	reused, err := history.ListCompleted(
		context.Background(),
		actor(userB),
		review.HistoryQuery{Limit: 50, Before: ownerFirst.Next},
	)
	if err != nil {
		t.Fatalf("reuse owner cursor as foreign Actor: %v", err)
	}
	if len(reused.Items) != 2 ||
		reused.Items[0].ID != foreignReviews[1].ID ||
		reused.Items[1].ID != foreignReviews[2].ID ||
		reused.Next != nil {
		t.Fatalf("foreign Actor reused cursor page = %+v", reused)
	}
	for _, item := range reused.Items {
		if item.OwnerUserID != userB ||
			item.ID == ownerReviews[0].ID ||
			item.ID == ownerReviews[1].ID ||
			item.ID == foreignReviews[0].ID {
			t.Fatalf("foreign cursor leaked/crossed keyset: %+v", item)
		}
	}

	fresh, err := history.ListCompleted(
		context.Background(),
		actor(userB),
		review.HistoryQuery{Limit: 50},
	)
	if err != nil {
		t.Fatalf("list fresh foreign Actor page: %v", err)
	}
	if len(fresh.Items) != 3 ||
		fresh.Items[0].ID != foreignReviews[0].ID ||
		fresh.Items[1].ID != foreignReviews[1].ID ||
		fresh.Items[2].ID != foreignReviews[2].ID {
		t.Fatalf("fresh foreign Actor page = %+v", fresh)
	}
}

func TestPostgresHistoryRestoresLegacyResultAboveNewWriteBudget(t *testing.T) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA)
	repository := review.NewPostgresRepository(pool)
	ensure := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{},
	)
	history := review.NewHistoryService(repository)
	item, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userA, "legacy-large-result"),
	)
	if err != nil {
		t.Fatalf("ensure legacy Review: %v", err)
	}
	legacyResult := validResult()
	legacyResult.Summary = strings.Repeat("legacy summary ", 1100)
	encoded, err := json.Marshal(legacyResult)
	if err != nil {
		t.Fatalf("marshal legacy result: %v", err)
	}
	if len(encoded) <= 12*1024 {
		t.Fatalf("legacy result fixture is only %d bytes", len(encoded))
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE reviews
		SET result = $2::jsonb
		WHERE id = $1
	`, item.ID, encoded); err != nil {
		t.Fatalf("persist legacy result: %v", err)
	}

	recovered, err := history.Get(
		context.Background(),
		actor(userA),
		item.ID,
	)
	if err != nil {
		t.Fatalf("restore legacy Review: %v", err)
	}
	if recovered.Result == nil ||
		recovered.Result.Summary != legacyResult.Summary {
		t.Fatalf("restored legacy Review = %+v", recovered)
	}
	page, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 20},
	)
	if err != nil {
		t.Fatalf("list legacy Review: %v", err)
	}
	if len(page.Items) != 1 ||
		page.Items[0].ID != item.ID ||
		page.Items[0].Result == nil ||
		page.Items[0].Result.Summary != legacyResult.Summary {
		t.Fatalf("legacy history page = %+v", page)
	}
}

func TestPostgresHistoryListAndDeleteUserLinearizeOnSharedFence(
	t *testing.T,
) {
	pool := reviewDatabase(t)
	insertUsers(t, pool, userA, userB)
	repository := review.NewPostgresRepository(pool)
	ensure := review.NewEnsureService(
		repository,
		sourceReader{},
		&countingGenerator{},
	)
	history := review.NewHistoryService(repository)
	ownerReview, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userA, "list-delete-owner"),
	)
	if err != nil {
		t.Fatalf("ensure list/delete owner Review: %v", err)
	}
	if _, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userA, "list-delete-owner-older"),
	); err != nil {
		t.Fatalf("ensure older list/delete owner Review: %v", err)
	}
	foreignReview, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userB, "list-delete-foreign"),
	)
	if err != nil {
		t.Fatalf("ensure list/delete foreign Review: %v", err)
	}
	preDeletePage, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 1},
	)
	if err != nil {
		t.Fatalf("list pre-delete owner cursor page: %v", err)
	}
	if len(preDeletePage.Items) != 1 ||
		preDeletePage.Next == nil {
		t.Fatalf("pre-delete owner cursor page = %+v", preDeletePage)
	}
	preDeleteCursor := *preDeletePage.Next
	setAccountStatus(t, pool, userA, "deleting")

	blocker, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin shared Review lock blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	var blockerPID int
	if err := blocker.QueryRow(
		context.Background(),
		`SELECT pg_backend_pid()`,
	).Scan(&blockerPID); err != nil {
		t.Fatalf("read shared Review lock blocker PID: %v", err)
	}
	if _, err := blocker.Exec(
		context.Background(),
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		userA,
	); err != nil {
		t.Fatalf("hold shared Review user lock: %v", err)
	}
	monitorConfig, err := pgxpool.ParseConfig(
		os.Getenv("TEST_DATABASE_URL"),
	)
	if err != nil {
		t.Fatalf("parse Review lock monitor config: %v", err)
	}
	monitor, err := pgx.ConnectConfig(
		context.Background(),
		monitorConfig.ConnConfig,
	)
	if err != nil {
		t.Fatalf("connect Review lock monitor: %v", err)
	}
	defer func() { _ = monitor.Close(context.Background()) }()

	type listResult struct {
		page review.HistoryPage
		err  error
	}
	type getResult struct {
		item review.FormalReview
		err  error
	}
	listed := make(chan listResult, 1)
	got := make(chan getResult, 1)
	deleted := make(chan error, 1)
	go func() {
		page, listErr := history.ListCompleted(
			context.Background(),
			actor(userA),
			review.HistoryQuery{Limit: 50},
		)
		listed <- listResult{page: page, err: listErr}
	}()
	go func() {
		item, getErr := history.Get(
			context.Background(),
			actor(userA),
			ownerReview.ID,
		)
		got <- getResult{item: item, err: getErr}
	}()
	go func() {
		deleted <- repository.DeleteUserData(
			context.Background(),
			review.DeleteUserReviewsCommand{
				UserID:             userA,
				DeletionGeneration: 1,
			},
		)
	}()
	waitForReviewAdvisoryWaiters(t, monitor, blockerPID, 3)
	if err := blocker.Commit(context.Background()); err != nil {
		t.Fatalf("release shared Review user lock: %v", err)
	}

	select {
	case result := <-listed:
		if !errors.Is(result.err, review.ErrAccountDeleted) ||
			len(result.page.Items) != 0 ||
			result.page.Next != nil {
			t.Fatalf(
				"concurrent history after deletion intent = %+v, %v",
				result.page,
				result.err,
			)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent history did not resume")
	}
	select {
	case result := <-got:
		if !errors.Is(result.err, review.ErrAccountDeleted) ||
			result.item.ID != "" {
			t.Fatalf(
				"concurrent Review get after deletion intent = %+v, %v",
				result.item,
				result.err,
			)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Review get did not resume")
	}
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatalf("concurrent DeleteUserData: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent DeleteUserData did not resume")
	}

	assertReviewCount(t, pool, userA, 0)
	assertReviewCount(t, pool, userB, 1)
	var fenceGeneration int64
	if err := pool.QueryRow(context.Background(), `
		SELECT deletion_generation
		FROM review_deletion_fences
		WHERE owner_user_id = $1
	`, userA).Scan(&fenceGeneration); err != nil {
		t.Fatalf("read committed Review deletion fence: %v", err)
	}
	if fenceGeneration != 1 {
		t.Fatalf("Review deletion fence = %d, want 1", fenceGeneration)
	}
	if _, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{Limit: 50},
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("post-delete owner history error = %v", err)
	}
	if _, err := history.ListCompleted(
		context.Background(),
		actor(userA),
		review.HistoryQuery{
			Limit:  50,
			Before: &preDeleteCursor,
		},
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("post-delete old cursor history error = %v", err)
	}
	if _, err := ensure.EnsureReview(
		context.Background(),
		ensureCommand(userA, "list-delete-resurrection"),
	); !errors.Is(err, review.ErrAccountDeleted) {
		t.Fatalf("post-delete Review resurrection error = %v", err)
	}
	assertReviewCount(t, pool, userA, 0)
	foreignPage, err := history.ListCompleted(
		context.Background(),
		actor(userB),
		review.HistoryQuery{Limit: 50},
	)
	if err != nil {
		t.Fatalf("list foreign history after owner deletion: %v", err)
	}
	if len(foreignPage.Items) != 1 ||
		foreignPage.Items[0].ID != foreignReview.ID ||
		foreignPage.Items[0].ID == ownerReview.ID {
		t.Fatalf(
			"owner deletion changed/leaked foreign history: %+v",
			foreignPage,
		)
	}
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
	if _, err := pool.Exec(context.Background(), `
		UPDATE reviews
		SET status = 'failed',
		    stable_error_category = 'raw provider response'
		WHERE id = $1
	`, pending.ID); err == nil {
		t.Fatal("raw provider failure category unexpectedly passed CHECK")
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
			target_kind,
			target_key,
			source_type,
			source_id,
			source_version
		)
		VALUES (
			$1, $2, 'conclusion', 'summary', 'turn', 'turn-1', 'v1'
		)
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
					target_kind,
					target_key,
					source_type,
					source_id,
					source_version
				)
				VALUES (
					$1, $2, 'conclusion', 'summary',
					'conversation_turn', 'turn-3', 'v1'
				)
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
		"000011_review_formal_reviews.down.sql",
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
		"000011_review_formal_reviews.up.sql",
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
	delay time.Duration
}

func (generator *terminalCountingGenerator) GenerateReview(
	ctx context.Context,
	_ review.ReviewGenerationInput,
) (review.GeneratedReview, error) {
	generator.calls.Add(1)
	if generator.delay > 0 {
		timer := time.NewTimer(generator.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return review.GeneratedReview{}, ctx.Err()
		case <-timer.C:
		}
	}
	return review.GeneratedReview{}, terminalCategorizedError{}
}

type terminalCategorizedError struct{}

func (terminalCategorizedError) Error() string {
	return "provider quota exhausted"
}

func (terminalCategorizedError) StableCategory() string {
	return "quota_exhausted"
}

type cancelingGenerator struct {
	calls           atomic.Int32
	cancel          context.CancelFunc
	terminalFailure bool
	invalidResult   bool
}

func (generator *cancelingGenerator) GenerateReview(
	context.Context,
	review.ReviewGenerationInput,
) (review.GeneratedReview, error) {
	generator.calls.Add(1)
	generator.cancel()
	if generator.terminalFailure {
		return review.GeneratedReview{}, terminalCategorizedError{}
	}
	if generator.invalidResult {
		return review.GeneratedReview{Result: validResult()}, nil
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

func maximumPersistedReviewResult(t *testing.T) review.ReviewResult {
	t.Helper()
	const (
		maximumResultBytes = 12 * 1024
		maximumSummary     = 2048
		maximumConclusions = 8
		maximumLabel       = 64
		maximumText        = 2048
	)
	result := review.ReviewResult{
		OverallScore: 100,
		Summary:      strings.Repeat("s", maximumSummary),
		Conclusions: make(
			[]review.ReviewConclusion,
			maximumConclusions,
		),
	}
	for index := range result.Conclusions {
		result.Conclusions[index] = review.ReviewConclusion{
			Key: fmt.Sprintf("%02d", index) +
				strings.Repeat("k", maximumLabel-2),
			Category: strings.Repeat("c", maximumLabel),
			Message:  strings.Repeat("m", 700),
			Suggestion: strings.Repeat(
				"s",
				300,
			),
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal maximum persisted Result fixture: %v", err)
	}
	remaining := maximumResultBytes - len(encoded)
	last := &result.Conclusions[len(result.Conclusions)-1]
	if remaining < 0 ||
		len(last.Suggestion)+remaining > maximumText {
		t.Fatalf(
			"maximum persisted Result fixture bytes=%d remaining=%d",
			len(encoded),
			remaining,
		)
	}
	last.Suggestion += strings.Repeat("x", remaining)
	return result
}

func evidenceForReviewResult(
	result review.ReviewResult,
) []review.ReviewEvidence {
	evidence := make(
		[]review.ReviewEvidence,
		len(result.Conclusions),
	)
	for index, conclusion := range result.Conclusions {
		evidence[index] = review.ReviewEvidence{
			ConclusionKey: conclusion.Key,
			SourceType:    review.SourceTypeConversationTurn,
			SourceID:      "turn-3",
			SourceVersion: "confirmed-v2",
		}
	}
	return evidence
}

func assertReviewResultJSONBytes(
	t *testing.T,
	result review.ReviewResult,
	want int,
) {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal persisted Review Result: %v", err)
	}
	if len(encoded) != want {
		t.Fatalf(
			"persisted Review Result bytes = %d, want %d",
			len(encoded),
			want,
		)
	}
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
		"000011_review_formal_reviews.up.sql",
	)
	if err != nil {
		t.Fatalf("read Review migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply Review migration: %v", err)
	}
	scenarioUp, err := migrations.Files.ReadFile(
		"000028_review_scenario_policies.up.sql",
	)
	if err != nil {
		t.Fatalf("read scenario Review migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(scenarioUp)); err != nil {
		t.Fatalf("apply scenario Review migration: %v", err)
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

func waitForReviewAdvisoryWaiters(
	t *testing.T,
	monitor *pgx.Conn,
	blockerPID int,
	want int,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var blocked int
		err := monitor.QueryRow(context.Background(), `
			WITH RECURSIVE blocked(pid) AS (
				SELECT activity.pid
				FROM pg_stat_activity activity
				WHERE $1 = ANY(pg_blocking_pids(activity.pid))
				UNION
				SELECT activity.pid
				FROM pg_stat_activity activity
				JOIN blocked blocker
				  ON blocker.pid =
				     ANY(pg_blocking_pids(activity.pid))
			)
			SELECT count(*)
			FROM blocked
			JOIN pg_stat_activity activity USING (pid)
			WHERE activity.wait_event_type = 'Lock'
			  AND position(
			      'pg_advisory_xact_lock(hashtextextended($1, 0))'
			      IN activity.query
			  ) > 0
		`, blockerPID).Scan(&blocked)
		if err != nil {
			t.Fatalf("inspect shared Review lock waiters: %v", err)
		}
		if blocked >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf(
		"shared Review user lock waiters did not reach %d",
		want,
	)
}
