package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testRunConfiguration = RunConfiguration{
	Provider:           "fake",
	Model:              "configured-model",
	MaxOutputTokens:    256,
	MaxInputCharacters: 12000,
}

func TestPostgresAgentRunSuccessReplayAuditAndOwnership(t *testing.T) {
	database := newAgentTestDatabase(t)
	generator := &recordingTextGenerator{result: successfulTextResult()}
	matterService, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		generator,
		testRunConfiguration,
	)
	actorA := testActorA()
	actorB := testActorB()

	activeMatter, err := matterService.Create(
		context.Background(),
		actorA,
		"Renewal meeting",
	)
	if err != nil {
		t.Fatalf("create Matter: %v", err)
	}
	thread, err := dataService.CreateThread(
		context.Background(),
		actorA,
		activeMatter.ID,
	)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		actorA,
		thread.ID,
		"ios-message-0001",
		"Help me open the meeting with confidence.",
	)
	if err != nil {
		t.Fatalf("submit text: %v", err)
	}
	if !submission.Created ||
		submission.Run.Status != RunStatusCompleted ||
		submission.Run.AssistantMessageID == "" {
		t.Fatalf("unexpected completed submission: %#v", submission)
	}
	if generator.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", generator.CallCount())
	}

	replayed, err := runService.SubmitText(
		context.Background(),
		actorA,
		thread.ID,
		"ios-message-0001",
		"Help me open the meeting with confidence.",
	)
	if err != nil {
		t.Fatalf("replay text: %v", err)
	}
	if replayed.Created ||
		replayed.Run.ID != submission.Run.ID ||
		replayed.UserMessage.ID != submission.UserMessage.ID ||
		generator.CallCount() != 1 {
		t.Fatalf(
			"replay created duplicate work: %#v calls=%d",
			replayed,
			generator.CallCount(),
		)
	}
	if _, err := runService.SubmitText(
		context.Background(),
		actorA,
		thread.ID,
		"ios-message-0001",
		"Changed content",
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v, want idempotency conflict", err)
	}
	rawThread, err := dataService.CreateThread(
		context.Background(),
		actorA,
		"",
	)
	if err != nil {
		t.Fatalf("create raw-message Thread: %v", err)
	}
	if _, err := dataService.AppendUserMessage(
		context.Background(),
		actorA,
		rawThread.ID,
		"raw-message-without-run",
		"This was committed outside the Run command.",
	); err != nil {
		t.Fatalf("append raw Message: %v", err)
	}
	if _, err := runService.SubmitText(
		context.Background(),
		actorA,
		rawThread.ID,
		"raw-message-without-run",
		"This was committed outside the Run command.",
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-atomic existing Message error = %v, want conflict", err)
	}

	messages, err := dataService.ListMessages(
		context.Background(),
		actorA,
		thread.ID,
	)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 ||
		messages[0].Role != MessageRoleUser ||
		messages[1].Role != MessageRoleAssistant ||
		messages[1].ProducedByRunID != submission.Run.ID {
		t.Fatalf("unexpected committed messages: %#v", messages)
	}
	manifest, err := runService.GetContextManifest(
		context.Background(),
		actorA,
		submission.Run.ID,
	)
	if err != nil {
		t.Fatalf("get ContextManifest: %v", err)
	}
	if manifest.ActiveMatterID != activeMatter.ID ||
		manifest.ActiveMatterVersion != activeMatter.Version ||
		manifest.RequestedProvider != testRunConfiguration.Provider ||
		manifest.RequestedModel != testRunConfiguration.Model ||
		manifest.MaxOutputTokens != testRunConfiguration.MaxOutputTokens ||
		manifest.InstructionVersion != instructionV1 ||
		len(manifest.SelectedMessages) != 1 ||
		manifest.SelectedMessages[0].MessageID != submission.UserMessage.ID ||
		manifest.TrimReason != contextTrimNone {
		t.Fatalf("unexpected ContextManifest: %#v", manifest)
	}
	requests := generator.Requests()
	if len(requests) != 1 ||
		len(requests[0].Messages) != 2 ||
		requests[0].Messages[0].Role != ai.TextRoleSystem ||
		!strings.Contains(requests[0].Messages[0].Content, activeMatter.Title) ||
		requests[0].Messages[1].Role != ai.TextRoleUser {
		t.Fatalf("unexpected provider request: %#v", requests)
	}

	var messageCount int
	var runCount int
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    (SELECT COUNT(*) FROM agent_messages
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_runs
     WHERE owner_user_id = $1 AND thread_id = $2)`,
		actorA.UserID,
		thread.ID,
	).Scan(&messageCount, &runCount); err != nil {
		t.Fatalf("count durable records: %v", err)
	}
	if messageCount != 2 || runCount != 1 {
		t.Fatalf("durable counts = messages %d runs %d, want 2/1", messageCount, runCount)
	}

	if _, err := runService.GetRun(
		context.Background(),
		actorB,
		submission.Run.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Run error = %v, want not found", err)
	}
	if _, err := runService.GetContextManifest(
		context.Background(),
		actorB,
		submission.Run.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner manifest error = %v, want not found", err)
	}
	threadB, err := dataService.CreateThread(
		context.Background(),
		actorB,
		"",
	)
	if err != nil {
		t.Fatalf("create Thread B: %v", err)
	}
	messageB, err := dataService.AppendUserMessage(
		context.Background(),
		actorB,
		threadB.ID,
		"private-user-b-message",
		"User B owns this input.",
	)
	if err != nil {
		t.Fatalf("append Message B: %v", err)
	}
	assertPostgresConstraint(
		t,
		database.pool,
		`INSERT INTO agent_runs (
    id,
    owner_user_id,
    thread_id,
    input_message_id,
    attempt_no,
    status,
	    requested_provider,
	    requested_model,
	    max_output_tokens,
	    max_input_characters
) VALUES (
    '40000000-0000-4000-8000-000000000001',
    $1,
    $2,
    $3,
    1,
    'pending',
    'fake',
    'configured-model',
	    256,
	    12000
)`,
		[]any{actorA.UserID, threadB.ID, messageB.ID},
		"23503",
		"agent_runs_thread_owner_fkey",
	)

	database.pool.Close()
	reopenedPool := database.reopen(t)
	_, recoveredData, recoveredRuns, _ := newAgentRunServices(
		t,
		reopenedPool,
		generator,
		testRunConfiguration,
	)
	recovered, err := recoveredRuns.GetRun(
		context.Background(),
		actorA,
		submission.Run.ID,
	)
	if err != nil || recovered.Status != RunStatusCompleted {
		t.Fatalf("recovered Run = %#v, %v", recovered, err)
	}
	recoveredMessages, err := recoveredData.ListMessages(
		context.Background(),
		actorA,
		thread.ID,
	)
	if err != nil || len(recoveredMessages) != 2 {
		t.Fatalf("recovered messages = %#v, %v", recoveredMessages, err)
	}
}

func TestPostgresAgentRunConcurrentReplayDoesNotDuplicatePendingWork(t *testing.T) {
	database := newAgentTestDatabase(t)
	generator := newBlockingTextGenerator()
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		generator,
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}

	type submitResult struct {
		submission RunSubmission
		err        error
	}
	firstResult := make(chan submitResult, 1)
	go func() {
		submission, submitErr := runService.SubmitText(
			context.Background(),
			actor,
			thread.ID,
			"concurrent-replay-message",
			"Generate this exactly once.",
		)
		firstResult <- submitResult{submission: submission, err: submitErr}
	}()
	<-generator.started

	replayed, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"concurrent-replay-message",
		"Generate this exactly once.",
	)
	if err != nil {
		t.Fatalf("concurrent replay: %v", err)
	}
	if replayed.Created || replayed.Run.Status != RunStatusRunning {
		t.Fatalf("concurrent replay should restore running Run: %#v", replayed)
	}
	close(generator.release)
	first := <-firstResult
	if first.err != nil || first.submission.Run.Status != RunStatusCompleted {
		t.Fatalf("first submission = %#v, %v", first.submission, first.err)
	}
	if replayed.Run.ID != first.submission.Run.ID || generator.CallCount() != 1 {
		t.Fatalf(
			"concurrent replay Run IDs differ or provider duplicated: %s/%s calls=%d",
			replayed.Run.ID,
			first.submission.Run.ID,
			generator.CallCount(),
		)
	}
	var messages int
	var runs int
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    (SELECT COUNT(*) FROM agent_messages
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_runs
     WHERE owner_user_id = $1 AND thread_id = $2)`,
		actor.UserID,
		thread.ID,
	).Scan(&messages, &runs); err != nil {
		t.Fatalf("count replay records: %v", err)
	}
	if messages != 2 || runs != 1 {
		t.Fatalf("concurrent replay counts = messages %d runs %d", messages, runs)
	}
}

func TestPostgresAgentRunRejectsConcurrentDifferentInputOnThread(t *testing.T) {
	database := newAgentTestDatabase(t)
	generator := newBlockingTextGenerator()
	t.Cleanup(func() {
		select {
		case <-generator.release:
		default:
			close(generator.release)
		}
	})
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		generator,
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}

	type submitResult struct {
		submission RunSubmission
		err        error
	}
	firstResult := make(chan submitResult, 1)
	go func() {
		submission, submitErr := runService.SubmitText(
			context.Background(),
			actor,
			thread.ID,
			"concurrent-distinct-message-1",
			"Keep this Run active while a second input arrives.",
		)
		firstResult <- submitResult{submission: submission, err: submitErr}
	}()
	<-generator.started

	if _, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"concurrent-distinct-message-2",
		"This input must roll back while the first Run is active.",
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("concurrent distinct input error = %v, want conflict", err)
	}
	var messages int
	var runs int
	var nonterminalRuns int
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    (SELECT COUNT(*) FROM agent_messages
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_runs
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_runs
     WHERE owner_user_id = $1
       AND thread_id = $2
       AND status IN ('pending', 'running'))`,
		actor.UserID,
		thread.ID,
	).Scan(&messages, &runs, &nonterminalRuns); err != nil {
		t.Fatalf("count concurrent distinct records: %v", err)
	}
	if messages != 1 || runs != 1 || nonterminalRuns != 1 {
		t.Fatalf(
			"concurrent distinct counts = messages %d runs %d nonterminal %d",
			messages,
			runs,
			nonterminalRuns,
		)
	}

	close(generator.release)
	first := <-firstResult
	if first.err != nil || first.submission.Run.Status != RunStatusCompleted {
		t.Fatalf("first submission = %#v, %v", first.submission, first.err)
	}
	if generator.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", generator.CallCount())
	}
}

func TestPostgresAgentRunRejectsConcurrentRetryOnThread(t *testing.T) {
	database := newAgentTestDatabase(t)
	matterService, dataService := newAgentDataServices(t, database.pool)
	repository, err := NewPostgresRepository(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new Agent repository: %v", err)
	}
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	failingService := newRunService(
		t,
		repository,
		matterService,
		fake.NewFailingTextGenerator(ai.NewGenerationError(
			ai.ErrorTimeout,
			0,
			"",
			"",
			context.DeadlineExceeded,
		)),
		testRunConfiguration,
	)
	original, err := failingService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"concurrent-retry-message",
		"Create one retryable failed Run.",
	)
	if err != nil || original.Run.Status != RunStatusFailed {
		t.Fatalf("create failed Run = %#v, %v", original, err)
	}

	generator := newBlockingTextGenerator()
	t.Cleanup(func() {
		select {
		case <-generator.release:
		default:
			close(generator.release)
		}
	})
	retryService := newRunService(
		t,
		repository,
		matterService,
		generator,
		testRunConfiguration,
	)
	type retryResult struct {
		retry RunRetry
		err   error
	}
	firstResult := make(chan retryResult, 1)
	go func() {
		retry, retryErr := retryService.RetryText(
			context.Background(),
			actor,
			original.Run.ID,
			"concurrent-retry-command-1",
		)
		firstResult <- retryResult{retry: retry, err: retryErr}
	}()
	<-generator.started

	if _, err := retryService.RetryText(
		context.Background(),
		actor,
		original.Run.ID,
		"concurrent-retry-command-2",
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("concurrent retry error = %v, want conflict", err)
	}
	var runs int
	var nonterminalRuns int
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    COUNT(*),
    COUNT(*) FILTER (WHERE status IN ('pending', 'running'))
FROM agent_runs
WHERE owner_user_id = $1 AND thread_id = $2`,
		actor.UserID,
		thread.ID,
	).Scan(&runs, &nonterminalRuns); err != nil {
		t.Fatalf("count concurrent retry records: %v", err)
	}
	if runs != 2 || nonterminalRuns != 1 {
		t.Fatalf(
			"concurrent retry counts = runs %d nonterminal %d",
			runs,
			nonterminalRuns,
		)
	}

	close(generator.release)
	first := <-firstResult
	if first.err != nil ||
		first.retry.Run.Status != RunStatusCompleted ||
		first.retry.Run.Attempt != 2 {
		t.Fatalf("first retry = %#v, %v", first.retry, first.err)
	}
	if generator.CallCount() != 1 {
		t.Fatalf("retry provider calls = %d, want 1", generator.CallCount())
	}
}

func TestPostgresAgentRunPendingClaimHasOneWinner(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	submission, err := repository.CreateInitialRun(
		context.Background(),
		actor.UserID,
		thread.ID,
		"claim-race-message",
		"Only one caller may claim this pending Run.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create pending Run: %v", err)
	}

	const claimers = 12
	start := make(chan struct{})
	acquired := make(chan bool, claimers)
	failures := make(chan error, claimers)
	var waitGroup sync.WaitGroup
	for range claimers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			run, won, claimErr := repository.ClaimRun(
				context.Background(),
				actor.UserID,
				submission.Run.ID,
			)
			if claimErr != nil {
				failures <- claimErr
				return
			}
			if run.ID != submission.Run.ID || run.Status != RunStatusRunning {
				failures <- errors.New("claim did not restore the running Run")
				return
			}
			acquired <- won
		}()
	}
	close(start)
	waitGroup.Wait()
	close(acquired)
	close(failures)
	for claimErr := range failures {
		t.Errorf("claim pending Run: %v", claimErr)
	}
	winners := 0
	for won := range acquired {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners = %d, want 1", winners)
	}
	claimed, err := repository.FindRun(
		context.Background(),
		actor.UserID,
		submission.Run.ID,
	)
	if err != nil {
		t.Fatalf("find claimed Run: %v", err)
	}
	if _, err := repository.FailRun(
		context.Background(),
		actor.UserID,
		submission.Run.ID,
		claimed.WorkerLeaseToken,
		RunFailureInternal,
		true,
	); err != nil {
		t.Fatalf("clean up claimed Run: %v", err)
	}
}

func TestPostgresAgentRunRollsBackUserMessageWhenPendingRunCannotCommit(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	invalidConfiguration := testRunConfiguration
	invalidConfiguration.Provider = "INVALID"
	if _, err := repository.CreateInitialRun(
		context.Background(),
		actor.UserID,
		thread.ID,
		"must-rollback-message",
		"Neither this Message nor the pending Run may survive.",
		invalidConfiguration,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid Run configuration error = %v, want invalid request", err)
	}
	var messageCount int
	var runCount int
	var nextSequence int64
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    (SELECT COUNT(*) FROM agent_messages
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_runs
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT next_message_sequence FROM agent_threads
     WHERE owner_user_id = $1 AND id = $2)`,
		actor.UserID,
		thread.ID,
	).Scan(&messageCount, &runCount, &nextSequence); err != nil {
		t.Fatalf("read rolled-back state: %v", err)
	}
	if messageCount != 0 || runCount != 0 || nextSequence != 1 {
		t.Fatalf(
			"partial transaction survived: messages=%d runs=%d next=%d",
			messageCount,
			runCount,
			nextSequence,
		)
	}
}

func TestPostgresAgentRunPanicRollsBackAndReleasesConnection(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create panic rollback Thread: %v", err)
	}
	panicRepository, err := NewPostgresRepository(
		database.pool,
		idGeneratorFunc(func() (string, error) {
			panic("test Run ID generator panic")
		}),
	)
	if err != nil {
		t.Fatalf("new panic Run repository: %v", err)
	}
	acquiredBeforePanic := database.pool.Stat().AcquiredConns()
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_, _ = panicRepository.CreateInitialRun(
			context.Background(),
			actor.UserID,
			thread.ID,
			"panic-run-message",
			"this Run transaction must be released",
			testRunConfiguration,
		)
	}()
	if recovered == nil {
		t.Fatal("panic Run ID generator did not panic")
	}
	if acquiredAfterPanic := database.pool.Stat().AcquiredConns(); acquiredAfterPanic !=
		acquiredBeforePanic {
		t.Fatalf(
			"acquired connections after Run panic = %d, want %d",
			acquiredAfterPanic,
			acquiredBeforePanic,
		)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"after-run-panic",
		"the Thread lock was released",
	)
	if err != nil || submission.Run.Status != RunStatusCompleted {
		t.Fatalf("submit after Run panic = %#v, %v", submission, err)
	}
}

func TestPostgresAgentRunRejectsPartialResultsOnNonterminalRuns(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	submission, err := repository.CreateInitialRun(
		context.Background(),
		actor.UserID,
		thread.ID,
		"partial-nonterminal-result",
		"Do not permit partial result audit fields.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create pending Run: %v", err)
	}

	assertPostgresConstraint(
		t,
		database.pool,
		`UPDATE agent_runs
SET provider_completion_id = 'partial-pending-result'
WHERE id = $1 AND owner_user_id = $2`,
		[]any{submission.Run.ID, actor.UserID},
		"23514",
		"agent_runs_state_shape_check",
	)
	claimed, acquired, err := repository.ClaimRun(
		context.Background(),
		actor.UserID,
		submission.Run.ID,
	)
	if err != nil || !acquired {
		t.Fatalf("claim pending Run: acquired=%v err=%v", acquired, err)
	}
	assertPostgresConstraint(
		t,
		database.pool,
		`UPDATE agent_runs
SET provider_model = 'configured-model'
WHERE id = $1 AND owner_user_id = $2`,
		[]any{submission.Run.ID, actor.UserID},
		"23514",
		"agent_runs_state_shape_check",
	)
	if _, err := repository.FailRun(
		context.Background(),
		actor.UserID,
		submission.Run.ID,
		claimed.WorkerLeaseToken,
		RunFailureInternal,
		true,
	); err != nil {
		t.Fatalf("clean up running Run: %v", err)
	}
}

func TestPostgresAgentRunRollsBackAssistantWhenCompletionCannotCommit(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	submission, err := repository.CreateInitialRun(
		context.Background(),
		actor.UserID,
		thread.ID,
		"completion-rollback-message",
		"Do not leave an orphan Assistant Message.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create pending Run: %v", err)
	}
	claimed, acquired, err := repository.ClaimRun(
		context.Background(),
		actor.UserID,
		submission.Run.ID,
	)
	if err != nil || !acquired {
		t.Fatalf("claim pending Run: acquired=%v err=%v", acquired, err)
	}
	invalidResult := successfulTextResult()
	invalidResult.Model = "other-model"
	if _, err := repository.CompleteRun(
		context.Background(),
		actor.UserID,
		submission.Run.ID,
		claimed.WorkerLeaseToken,
		"Must be rolled back with the failed state transition.",
		invalidResult,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid completion error = %v, want invalid request", err)
	}
	var messageCount int
	var nextSequence int64
	var status string
	var assistantMessageID *string
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    (SELECT COUNT(*) FROM agent_messages
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT next_message_sequence FROM agent_threads
     WHERE owner_user_id = $1 AND id = $2),
    status,
    assistant_message_id::text
FROM agent_runs
WHERE owner_user_id = $1 AND id = $3`,
		actor.UserID,
		thread.ID,
		submission.Run.ID,
	).Scan(
		&messageCount,
		&nextSequence,
		&status,
		&assistantMessageID,
	); err != nil {
		t.Fatalf("read rolled-back completion: %v", err)
	}
	if messageCount != 1 ||
		nextSequence != 2 ||
		status != string(RunStatusRunning) ||
		assistantMessageID != nil {
		t.Fatalf(
			"partial completion survived: messages=%d next=%d status=%s assistant=%v",
			messageCount,
			nextSequence,
			status,
			assistantMessageID,
		)
	}
	if _, err := repository.FailRun(
		context.Background(),
		actor.UserID,
		submission.Run.ID,
		claimed.WorkerLeaseToken,
		RunFailureInternal,
		true,
	); err != nil {
		t.Fatalf("clean up running Run: %v", err)
	}
}

func TestPostgresAgentRunPersistsStableProviderFailuresAndRetryHistory(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	matterService, dataService := newAgentDataServices(t, database.pool)
	repository, err := NewPostgresRepository(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new Agent repository: %v", err)
	}
	actor := testActorA()
	timeoutGenerator := &recordingTextGenerator{
		err: ai.NewGenerationError(
			ai.ErrorTimeout,
			0,
			"",
			"",
			context.DeadlineExceeded,
		),
	}
	oversizedUsage := successfulTextResult()
	oversizedUsage.Usage.InputTokens = maxPersistedTokenCount + 1
	oversizedContent := successfulTextResult()
	oversizedContent.Content = strings.Repeat(
		" ",
		maxMessageContentBytes,
	) + "visible"
	mismatchedModelResult := successfulTextResult()
	mismatchedModelResult.Model = "other-model"
	overBudgetResult := successfulTextResult()
	overBudgetResult.Usage.OutputTokens =
		testRunConfiguration.MaxOutputTokens + 1
	overBudgetResult.Usage.TotalTokens =
		overBudgetResult.Usage.InputTokens +
			overBudgetResult.Usage.OutputTokens
	inconsistentUsageResult := successfulTextResult()
	inconsistentUsageResult.Usage.TotalTokens--

	tests := []struct {
		name      string
		generator ai.TextGenerator
		wantKind  string
	}{
		{
			name:      "timeout",
			generator: timeoutGenerator,
			wantKind:  string(ai.ErrorTimeout),
		},
		{
			name: "rate limited",
			generator: fake.NewFailingTextGenerator(ai.NewGenerationError(
				ai.ErrorRateLimited,
				http.StatusTooManyRequests,
				"Throttling",
				"req-safe",
				nil,
			)),
			wantKind: string(ai.ErrorRateLimited),
		},
		{
			name:      "invalid response",
			generator: fake.NewTextGenerator(ai.TextResult{}),
			wantKind:  string(ai.ErrorInvalidResponse),
		},
		{
			name:      "token usage exceeds persistence range",
			generator: fake.NewTextGenerator(oversizedUsage),
			wantKind:  string(ai.ErrorInvalidResponse),
		},
		{
			name:      "raw content exceeds persistence range",
			generator: fake.NewTextGenerator(oversizedContent),
			wantKind:  string(ai.ErrorInvalidResponse),
		},
		{
			name:      "provider switched model",
			generator: fake.NewTextGenerator(mismatchedModelResult),
			wantKind:  string(ai.ErrorInvalidResponse),
		},
		{
			name:      "provider exceeded output budget",
			generator: fake.NewTextGenerator(overBudgetResult),
			wantKind:  string(ai.ErrorInvalidResponse),
		},
		{
			name:      "provider returned inconsistent usage",
			generator: fake.NewTextGenerator(inconsistentUsageResult),
			wantKind:  string(ai.ErrorInvalidResponse),
		},
	}
	var timeoutRun Run
	var timeoutThread Thread
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			thread, createErr := dataService.CreateThread(
				context.Background(),
				actor,
				"",
			)
			if createErr != nil {
				t.Fatalf("create Thread: %v", createErr)
			}
			runService := newRunService(
				t,
				repository,
				matterService,
				test.generator,
				testRunConfiguration,
			)
			submission, submitErr := runService.SubmitText(
				context.Background(),
				actor,
				thread.ID,
				"failure-message",
				"Please answer this request.",
			)
			if submitErr != nil {
				t.Fatalf("submit failed run: %v", submitErr)
			}
			if submission.Run.Status != RunStatusFailed ||
				submission.Run.FailureKind != test.wantKind ||
				!submission.Run.FailureRetryable {
				t.Fatalf("unexpected failed Run: %#v", submission.Run)
			}
			messages, listErr := dataService.ListMessages(
				context.Background(),
				actor,
				thread.ID,
			)
			if listErr != nil || len(messages) != 1 {
				t.Fatalf("failed Run messages = %#v, %v", messages, listErr)
			}
			if index == 0 {
				timeoutRun = submission.Run
				timeoutThread = thread
			}
		})
	}
	timeoutService := newRunService(
		t,
		repository,
		matterService,
		timeoutGenerator,
		testRunConfiguration,
	)
	replayedFailure, err := timeoutService.SubmitText(
		context.Background(),
		actor,
		timeoutThread.ID,
		"failure-message",
		"Please answer this request.",
	)
	if err != nil ||
		replayedFailure.Run.ID != timeoutRun.ID ||
		replayedFailure.Run.Status != RunStatusFailed ||
		timeoutGenerator.CallCount() != 1 {
		t.Fatalf(
			"failed Run replay = %#v, %v; provider calls=%d",
			replayedFailure,
			err,
			timeoutGenerator.CallCount(),
		)
	}

	successService := newRunService(
		t,
		repository,
		matterService,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	retry, err := successService.RetryText(
		context.Background(),
		actor,
		timeoutRun.ID,
		"ios-retry-0001",
	)
	if err != nil {
		t.Fatalf("retry timeout Run: %v", err)
	}
	if !retry.Created ||
		retry.Run.Status != RunStatusCompleted ||
		retry.Run.Attempt != 2 ||
		retry.Run.RetryOfRunID != timeoutRun.ID {
		t.Fatalf("unexpected retry Run: %#v", retry)
	}
	replayedRetry, err := successService.RetryText(
		context.Background(),
		actor,
		timeoutRun.ID,
		"ios-retry-0001",
	)
	if err != nil ||
		replayedRetry.Created ||
		replayedRetry.Run.ID != retry.Run.ID {
		t.Fatalf("replayed retry = %#v, %v", replayedRetry, err)
	}
	original, err := successService.GetRun(
		context.Background(),
		actor,
		timeoutRun.ID,
	)
	if err != nil ||
		original.Status != RunStatusFailed ||
		original.FailureKind != string(ai.ErrorTimeout) {
		t.Fatalf("original retry history changed: %#v, %v", original, err)
	}
}

func TestPostgresAgentRunRetryCannotChangeInputMessage(t *testing.T) {
	database := newAgentTestDatabase(t)
	matterService, dataService := newAgentDataServices(t, database.pool)
	repository, err := NewPostgresRepository(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new Agent repository: %v", err)
	}
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	failingService := newRunService(
		t,
		repository,
		matterService,
		fake.NewFailingTextGenerator(ai.NewGenerationError(
			ai.ErrorTimeout,
			0,
			"",
			"",
			context.DeadlineExceeded,
		)),
		testRunConfiguration,
	)
	original, err := failingService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"retry-parent-message",
		"This is the original retry input.",
	)
	if err != nil || original.Run.Status != RunStatusFailed {
		t.Fatalf("create failed parent Run = %#v, %v", original, err)
	}
	differentInput, err := dataService.AppendUserMessage(
		context.Background(),
		actor,
		thread.ID,
		"different-retry-input",
		"A retry must not point at this different Message.",
	)
	if err != nil {
		t.Fatalf("append different input Message: %v", err)
	}

	assertPostgresConstraint(
		t,
		database.pool,
		`INSERT INTO agent_runs (
    id,
    owner_user_id,
    thread_id,
    input_message_id,
    attempt_no,
    retry_of_run_id,
    retry_client_id,
    status,
	    requested_provider,
	    requested_model,
	    max_output_tokens,
	    max_input_characters,
	    failure_kind,
    failure_retryable,
    created_at,
    started_at,
    completed_at,
    updated_at
) VALUES (
    '40000000-0000-4000-8000-000000000002',
    $1,
    $2,
    $3,
    2,
    $4,
    'different-input-retry',
    'failed',
    'fake',
	    'configured-model',
	    256,
	    12000,
    'timeout',
    true,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
)`,
		[]any{
			actor.UserID,
			thread.ID,
			differentInput.ID,
			original.Run.ID,
		},
		"23503",
		"agent_runs_retry_of_fkey",
	)
}

func TestPostgresAgentRunPersistsCallerCancellationAsRetryable(t *testing.T) {
	database := newAgentTestDatabase(t)
	generator := newBlockingTextGenerator()
	t.Cleanup(func() {
		select {
		case <-generator.release:
		default:
			close(generator.release)
		}
	})
	matterService, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		generator,
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	handler, err := NewHTTPHandlerWithRuns(
		dataService,
		runService,
		matterService,
		authenticatorFunc(func(
			_ context.Context,
			token string,
		) (requestcontext.Actor, error) {
			if token != "token-a" {
				return requestcontext.Actor{}, identity.ErrAuthenticationRequired
			}
			return actor, nil
		}),
		func() string { return "corr_cancelled_agent_run_test" },
	)
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	module, err := NewModule(handler)
	if err != nil {
		t.Fatalf("new Agent module: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	module.RegisterRoutes(router)

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-threads/"+thread.ID+"/runs",
		strings.NewReader(
			`{"client_message_id":"cancelled-caller-message",`+
				`"content":"Allow this request to be retried after the caller disconnects."}`,
		),
	).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer token-a")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	served := make(chan struct{})
	go func() {
		router.ServeHTTP(response, request)
		close(served)
	}()
	<-generator.started
	cancel()
	<-served

	var cancelled struct {
		ID      string    `json:"run_id"`
		Status  RunStatus `json:"status"`
		Failure struct {
			Kind      string `json:"kind"`
			Retryable bool   `json:"retryable"`
		} `json:"failure"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &cancelled); err != nil {
		t.Fatalf("decode cancelled HTTP Run: %v", err)
	}
	if response.Code != http.StatusCreated ||
		cancelled.Status != RunStatusFailed ||
		cancelled.Failure.Kind != string(ai.ErrorCancelled) ||
		!cancelled.Failure.Retryable {
		t.Fatalf("cancelled HTTP Run = %d %#v", response.Code, cancelled)
	}

	successService := newRunService(
		t,
		repository,
		matterService,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	retry, err := successService.RetryText(
		context.Background(),
		actor,
		cancelled.ID,
		"cancelled-caller-retry",
	)
	if err != nil ||
		retry.Run.Status != RunStatusCompleted ||
		retry.Run.Attempt != 2 {
		t.Fatalf("retry cancelled Run = %#v, %v", retry, err)
	}
}

func TestPostgresAgentRunRecoversRunningAndResumesPendingAfterRestart(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	runningThread, err := dataService.CreateThread(
		context.Background(),
		actor,
		"",
	)
	if err != nil {
		t.Fatalf("create running Thread: %v", err)
	}
	running, err := repository.CreateInitialRun(
		context.Background(),
		actor.UserID,
		runningThread.ID,
		"running-before-restart",
		"Persist this in-flight request.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create running submission: %v", err)
	}
	claimed, acquired, err := repository.ClaimRun(
		context.Background(),
		actor.UserID,
		running.Run.ID,
	)
	if err != nil || !acquired {
		t.Fatalf("claim running submission: acquired=%v err=%v", acquired, err)
	}
	if claimed.WorkerLeaseToken == "" ||
		!claimed.WorkerLeaseExpiresAt.After(claimed.StartedAt) {
		t.Fatalf("claimed Run has invalid worker lease: %#v", claimed)
	}

	pendingThread, err := dataService.CreateThread(
		context.Background(),
		actor,
		"",
	)
	if err != nil {
		t.Fatalf("create pending Thread: %v", err)
	}
	pending, err := repository.CreateInitialRun(
		context.Background(),
		actor.UserID,
		pendingThread.ID,
		"pending-before-restart",
		"Resume this pending request.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create pending submission: %v", err)
	}

	database.pool.Close()
	reopenedPool := database.reopen(t)
	recoveryGenerator := &recordingTextGenerator{result: successfulTextResult()}
	_, _, recoveredService, recoveredRepository := newAgentRunServices(
		t,
		reopenedPool,
		recoveryGenerator,
		testRunConfiguration,
	)
	recoveredCount, err := recoveredService.RecoverInterruptedRuns(
		context.Background(),
	)
	if err != nil || recoveredCount != 0 {
		t.Fatalf(
			"recover live lease count = %d, %v; want 0",
			recoveredCount,
			err,
		)
	}
	live, err := recoveredService.GetRun(
		context.Background(),
		actor,
		running.Run.ID,
	)
	if err != nil || live.Status != RunStatusRunning {
		t.Fatalf("live leased Run = %#v, %v", live, err)
	}
	if _, err := reopenedPool.Exec(context.Background(), `
UPDATE agent_runs
SET worker_lease_expires_at = started_at + INTERVAL '1 microsecond'
WHERE id = $1 AND owner_user_id = $2`,
		running.Run.ID,
		actor.UserID,
	); err != nil {
		t.Fatalf("expire worker lease: %v", err)
	}
	if _, err := recoveredRepository.CompleteRun(
		context.Background(),
		actor.UserID,
		running.Run.ID,
		claimed.WorkerLeaseToken,
		"Expired workers must not persist this result.",
		successfulTextResult(),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired worker completion error = %v, want conflict", err)
	}
	recoveredCount, err = recoveredService.RecoverInterruptedRuns(
		context.Background(),
	)
	if err != nil || recoveredCount != 1 {
		t.Fatalf("recover expired lease count = %d, %v; want 1", recoveredCount, err)
	}
	if recoveryGenerator.CallCount() != 0 {
		t.Fatalf(
			"startup recovery repeated provider calls: %d",
			recoveryGenerator.CallCount(),
		)
	}
	interrupted, err := recoveredService.GetRun(
		context.Background(),
		actor,
		running.Run.ID,
	)
	if err != nil ||
		interrupted.Status != RunStatusFailed ||
		interrupted.FailureKind != RunFailureInterrupted ||
		!interrupted.FailureRetryable {
		t.Fatalf("interrupted Run = %#v, %v", interrupted, err)
	}
	if _, err := recoveredRepository.CompleteRun(
		context.Background(),
		actor.UserID,
		running.Run.ID,
		claimed.WorkerLeaseToken,
		"Stale workers must not persist this result.",
		successfulTextResult(),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale worker completion error = %v, want conflict", err)
	}
	resumed, err := recoveredService.SubmitText(
		context.Background(),
		actor,
		pendingThread.ID,
		"pending-before-restart",
		"Resume this pending request.",
	)
	if err != nil ||
		resumed.Run.ID != pending.Run.ID ||
		resumed.Run.Status != RunStatusCompleted {
		t.Fatalf("resumed pending Run = %#v, %v", resumed, err)
	}
	if recoveryGenerator.CallCount() != 1 {
		t.Fatalf(
			"explicit pending replay provider calls = %d, want 1",
			recoveryGenerator.CallCount(),
		)
	}
}

func TestPostgresAgentRunRejectsPendingReplayAfterConfigurationDrift(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(
		context.Background(),
		actor,
		"",
	)
	if err != nil {
		t.Fatalf("create pending replay Thread: %v", err)
	}
	pending, err := repository.CreateInitialRun(
		context.Background(),
		actor.UserID,
		thread.ID,
		"pending-before-configuration-drift",
		"Do not invoke a reconfigured provider for this durable Run.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create pending replay submission: %v", err)
	}

	database.pool.Close()
	reopenedPool := database.reopen(t)
	reconfigured := testRunConfiguration
	reconfigured.Provider = "fake_reconfigured"
	reconfigured.Model = "fake-reconfigured-model"
	reconfigured.MaxOutputTokens++
	reconfigured.MaxInputCharacters++
	reconfiguredResult := successfulTextResult()
	reconfiguredResult.Provider = reconfigured.Provider
	reconfiguredResult.Model = reconfigured.Model
	generator := &recordingTextGenerator{result: reconfiguredResult}
	_, _, recoveredService, _ := newAgentRunServices(
		t,
		reopenedPool,
		generator,
		reconfigured,
	)

	replayed, err := recoveredService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"pending-before-configuration-drift",
		"Do not invoke a reconfigured provider for this durable Run.",
	)
	if err != nil {
		t.Fatalf("replay pending Run after configuration drift: %v", err)
	}
	if replayed.Created ||
		replayed.Run.ID != pending.Run.ID ||
		replayed.Run.Status != RunStatusFailed ||
		replayed.Run.FailureKind != RunFailureConfigurationDrift ||
		!replayed.Run.FailureRetryable {
		t.Fatalf("configuration-drift replay = %#v", replayed)
	}
	if replayed.Run.RequestedProvider != testRunConfiguration.Provider ||
		replayed.Run.RequestedModel != testRunConfiguration.Model ||
		replayed.Run.MaxOutputTokens !=
			testRunConfiguration.MaxOutputTokens ||
		replayed.Run.MaxInputCharacters !=
			testRunConfiguration.MaxInputCharacters {
		t.Fatalf(
			"configuration-drift replay changed the durable request: %#v",
			replayed.Run,
		)
	}
	if generator.CallCount() != 0 {
		t.Fatalf(
			"configuration-drift replay invoked provider %d times",
			generator.CallCount(),
		)
	}
	if _, err := recoveredService.GetContextManifest(
		context.Background(),
		actor,
		replayed.Run.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf(
			"configuration-drift manifest error = %v, want not found",
			err,
		)
	}

	retry, err := recoveredService.RetryText(
		context.Background(),
		actor,
		replayed.Run.ID,
		"retry-after-configuration-drift",
	)
	if err != nil {
		t.Fatalf("retry after configuration drift: %v", err)
	}
	if !retry.Created ||
		retry.Run.Status != RunStatusCompleted ||
		retry.Run.Attempt != 2 ||
		retry.Run.RetryOfRunID != replayed.Run.ID ||
		retry.Run.RequestedProvider != reconfigured.Provider ||
		retry.Run.RequestedModel != reconfigured.Model ||
		retry.Run.MaxOutputTokens != reconfigured.MaxOutputTokens ||
		retry.Run.MaxInputCharacters != reconfigured.MaxInputCharacters {
		t.Fatalf("configuration-drift retry = %#v", retry)
	}
	if generator.CallCount() != 1 {
		t.Fatalf(
			"configuration-drift retry provider calls = %d, want 1",
			generator.CallCount(),
		)
	}
}

func TestPostgresContextAssemblerKeepsCurrentInputWithinBudget(t *testing.T) {
	database := newAgentTestDatabase(t)
	configuration := testRunConfiguration
	configuration.MaxInputCharacters = 5000
	generator := &recordingTextGenerator{result: successfulTextResult()}
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		generator,
		configuration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if _, err := dataService.AppendUserMessage(
		context.Background(),
		actor,
		thread.ID,
		"older-large-message",
		strings.Repeat("a", 4000),
	); err != nil {
		t.Fatalf("append older message: %v", err)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"current-budget-message",
		strings.Repeat("b", 1000),
	)
	if err != nil {
		t.Fatalf("submit budget message: %v", err)
	}
	manifest, err := runService.GetContextManifest(
		context.Background(),
		actor,
		submission.Run.ID,
	)
	if err != nil {
		t.Fatalf("get budget manifest: %v", err)
	}
	if manifest.TrimReason != contextTrimBudget ||
		manifest.OmittedMessageCount != 1 ||
		len(manifest.SelectedMessages) != 1 ||
		manifest.SelectedMessages[0].MessageID != submission.UserMessage.ID ||
		manifest.UsedInputCharacters > configuration.MaxInputCharacters {
		t.Fatalf("unexpected budget manifest: %#v", manifest)
	}
	requests := generator.Requests()
	if len(requests) != 1 || len(requests[0].Messages) != 2 {
		t.Fatalf("budget provider requests: %#v", requests)
	}
}

func TestPostgresContextAssemblerIncludesRecentCommittedConversation(t *testing.T) {
	database := newAgentTestDatabase(t)
	generator := &recordingTextGenerator{result: successfulTextResult()}
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		generator,
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if _, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"recent-message-1",
		"Help with my first sentence.",
	); err != nil {
		t.Fatalf("submit first Run: %v", err)
	}
	second, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"recent-message-2",
		"Now make it more concise.",
	)
	if err != nil {
		t.Fatalf("submit second Run: %v", err)
	}
	manifest, err := runService.GetContextManifest(
		context.Background(),
		actor,
		second.Run.ID,
	)
	if err != nil {
		t.Fatalf("get second manifest: %v", err)
	}
	wantRoles := []MessageRole{
		MessageRoleUser,
		MessageRoleAssistant,
		MessageRoleUser,
	}
	if len(manifest.SelectedMessages) != len(wantRoles) {
		t.Fatalf("selected messages = %#v", manifest.SelectedMessages)
	}
	for index, want := range wantRoles {
		if manifest.SelectedMessages[index].Role != want {
			t.Fatalf(
				"selected role[%d] = %q, want %q",
				index,
				manifest.SelectedMessages[index].Role,
				want,
			)
		}
	}
	requests := generator.Requests()
	if len(requests) != 2 || len(requests[1].Messages) != 4 {
		t.Fatalf("recent provider requests: %#v", requests)
	}
	if requests[1].Messages[1].Role != ai.TextRoleUser ||
		requests[1].Messages[2].Role != ai.TextRoleAssistant ||
		requests[1].Messages[3].Role != ai.TextRoleUser {
		t.Fatalf("recent provider roles: %#v", requests[1].Messages)
	}
}

func TestPostgresAgentRunRevalidatesActiveMatterBeforeProviderCall(t *testing.T) {
	database := newAgentTestDatabase(t)
	generator := &recordingTextGenerator{result: successfulTextResult()}
	matterService, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		generator,
		testRunConfiguration,
	)
	actor := testActorA()
	activeMatter, err := matterService.Create(
		context.Background(),
		actor,
		"Archived before generation",
	)
	if err != nil {
		t.Fatalf("create Matter: %v", err)
	}
	thread, err := dataService.CreateThread(
		context.Background(),
		actor,
		activeMatter.ID,
	)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if _, err := matterService.ChangeStatus(
		context.Background(),
		actor,
		activeMatter.ID,
		activeMatter.Version,
		matter.StatusArchived,
	); err != nil {
		t.Fatalf("archive Matter: %v", err)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"archived-context-message",
		"Do not call the provider with stale Matter context.",
	)
	if err != nil {
		t.Fatalf("submit archived-context Run: %v", err)
	}
	if submission.Run.Status != RunStatusFailed ||
		submission.Run.FailureKind != RunFailureInvalidContext ||
		submission.Run.FailureRetryable ||
		generator.CallCount() != 0 {
		t.Fatalf(
			"archived-context Run = %#v, provider calls=%d",
			submission.Run,
			generator.CallCount(),
		)
	}
	if _, err := runService.GetContextManifest(
		context.Background(),
		actor,
		submission.Run.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid-context manifest error = %v, want not found", err)
	}
}

func TestPostgresAgentRunProtectedHTTP(t *testing.T) {
	database := newAgentTestDatabase(t)
	matterService, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actorA := testActorA()
	activeMatter, err := matterService.Create(
		context.Background(),
		actorA,
		"Private acquisition discussion",
	)
	if err != nil {
		t.Fatalf("create HTTP Matter: %v", err)
	}
	thread, err := dataService.CreateThread(
		context.Background(),
		actorA,
		activeMatter.ID,
	)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	actors := map[string]requestcontext.Actor{
		"token-a": actorA,
		"token-b": testActorB(),
	}
	handler, err := NewHTTPHandlerWithRuns(
		dataService,
		runService,
		matterService,
		authenticatorFunc(func(
			_ context.Context,
			token string,
		) (requestcontext.Actor, error) {
			actor, ok := actors[token]
			if !ok {
				return requestcontext.Actor{}, identity.ErrAuthenticationRequired
			}
			return actor, nil
		}),
		func() string { return "corr_agent_run_test" },
	)
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	module, err := NewModule(handler)
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	module.RegisterRoutes(router)

	nulResponse := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+thread.ID+"/runs",
		`{"client_message_id":"http-message-nul","content":"invalid\u0000content"}`,
		"token-a",
	)
	if nulResponse.Code != http.StatusBadRequest {
		t.Fatalf(
			"NUL Run content response: %d %s",
			nulResponse.Code,
			nulResponse.Body,
		)
	}
	maxEscapedResponse := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+thread.ID+"/runs",
		`{"client_message_id":"http-message-max-escaped","content":"`+
			strings.Repeat(`\ud83d\ude00`, maxMessageContentRunes)+
			`"}`,
		"token-a",
	)
	if maxEscapedResponse.Code != http.StatusCreated {
		t.Fatalf(
			"maximum escaped Run response: %d %s",
			maxEscapedResponse.Code,
			maxEscapedResponse.Body,
		)
	}

	response := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+thread.ID+"/runs",
		`{"client_message_id":"http-message-1","content":"Coach my opening."}`,
		"token-a",
	)
	if response.Code != http.StatusCreated ||
		!strings.Contains(response.Body.String(), `"status":"completed"`) {
		t.Fatalf("submit Run response: %d %s", response.Code, response.Body)
	}
	var runID string
	if err := database.pool.QueryRow(context.Background(), `
SELECT id::text
FROM agent_runs
WHERE owner_user_id = $1 AND thread_id = $2`,
		actorA.UserID,
		thread.ID,
	).Scan(&runID); err != nil {
		t.Fatalf("find HTTP Run: %v", err)
	}
	privateRun := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-runs/"+runID,
		"",
		"token-b",
	)
	if privateRun.Code != http.StatusNotFound {
		t.Fatalf("cross-owner Run response: %d %s", privateRun.Code, privateRun.Body)
	}
	manifest := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-runs/"+runID+"/context-manifest",
		"",
		"token-a",
	)
	if manifest.Code != http.StatusOK ||
		!strings.Contains(manifest.Body.String(), `"selected_messages"`) ||
		strings.Contains(manifest.Body.String(), activeMatter.Title) {
		t.Fatalf("manifest response: %d %s", manifest.Code, manifest.Body)
	}
	messages := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+thread.ID+"/messages",
		"",
		"token-a",
	)
	if messages.Code != http.StatusOK ||
		!strings.Contains(messages.Body.String(), `"role":"assistant"`) ||
		!strings.Contains(messages.Body.String(), `"produced_by_run_id":"`+runID+`"`) {
		t.Fatalf("Run messages response: %d %s", messages.Code, messages.Body)
	}
}

type recordingTextGenerator struct {
	mu       sync.Mutex
	result   ai.TextResult
	err      error
	requests []ai.TextRequest
}

type blockingTextGenerator struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func newBlockingTextGenerator() *blockingTextGenerator {
	return &blockingTextGenerator{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (generator *blockingTextGenerator) Generate(
	ctx context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	if err := ai.ValidateTextRequest(request); err != nil {
		return ai.TextResult{}, err
	}
	generator.mu.Lock()
	generator.calls++
	generator.mu.Unlock()
	generator.once.Do(func() { close(generator.started) })
	select {
	case <-ctx.Done():
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorCancelled,
			0,
			"",
			"",
			ctx.Err(),
		)
	case <-generator.release:
		return successfulTextResult(), nil
	}
}

func (generator *blockingTextGenerator) CallCount() int {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	return generator.calls
}

func (generator *recordingTextGenerator) Generate(
	ctx context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	generator.mu.Lock()
	generator.requests = append(generator.requests, request)
	generator.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ai.TextResult{}, err
	}
	return generator.result, generator.err
}

func (generator *recordingTextGenerator) CallCount() int {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	return len(generator.requests)
}

func (generator *recordingTextGenerator) Requests() []ai.TextRequest {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	result := make([]ai.TextRequest, len(generator.requests))
	copy(result, generator.requests)
	return result
}

func successfulTextResult() ai.TextResult {
	return ai.TextResult{
		ID:           "fake-completion-1",
		Provider:     "fake",
		Model:        "configured-model",
		Content:      "Open with the shared goal, then ask what success looks like.",
		FinishReason: "stop",
		Usage: ai.TokenUsage{
			InputTokens:  32,
			OutputTokens: 14,
			TotalTokens:  46,
		},
	}
}

func newAgentRunServices(
	t *testing.T,
	pool *pgxpool.Pool,
	generator ai.TextGenerator,
	configuration RunConfiguration,
) (*matter.Service, *Service, *RunService, *PostgresRepository) {
	t.Helper()
	ids := identity.NewUUIDv4Generator(nil)
	matterRepository, err := matter.NewPostgresRepository(pool, ids)
	if err != nil {
		t.Fatalf("new Matter repository: %v", err)
	}
	matterService, err := matter.NewService(matterRepository)
	if err != nil {
		t.Fatalf("new Matter service: %v", err)
	}
	repository, err := NewPostgresRepository(pool, ids)
	if err != nil {
		t.Fatalf("new Agent repository: %v", err)
	}
	dataService, err := NewService(repository, matterService)
	if err != nil {
		t.Fatalf("new Agent data service: %v", err)
	}
	runService := newRunService(
		t,
		repository,
		matterService,
		generator,
		configuration,
	)
	return matterService, dataService, runService, repository
}

func newRunService(
	t *testing.T,
	repository *PostgresRepository,
	matterService *matter.Service,
	generator ai.TextGenerator,
	configuration RunConfiguration,
) *RunService {
	t.Helper()
	assembler, err := NewContextAssembler(repository, matterService)
	if err != nil {
		t.Fatalf("new ContextAssembler: %v", err)
	}
	service, err := NewRunService(
		repository,
		assembler,
		generator,
		configuration,
	)
	if err != nil {
		t.Fatalf("new Run service: %v", err)
	}
	return service
}

func testActorA() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    agentTestUserA,
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
}

func testActorB() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    agentTestUserB,
		SessionID: "20000000-0000-4000-8000-000000000002",
	}
}
