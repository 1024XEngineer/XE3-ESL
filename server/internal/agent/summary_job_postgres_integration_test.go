package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompletedRunQueuesAndProcessesThreadSummaryJob(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	thread, err := dataService.CreateThread(
		context.Background(),
		testActorA(),
		"",
	)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	for index := range 19 {
		seedSummarySourceMessages(
			t,
			database.pool,
			thread.ID,
			fmt.Sprintf("summary-job-source-%d", index),
		)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		testActorA(),
		thread.ID,
		"summary-job-trigger",
		"Create a checkpoint after this reply.",
	)
	if err != nil {
		t.Fatalf("complete triggering Run: %v", err)
	}
	if submission.Run.Status != RunStatusCompleted {
		t.Fatalf("Run status = %s", submission.Run.Status)
	}
	assertSummaryJobRow(
		t,
		database.pool,
		submission.Run.ID,
		"pending",
		40,
		0,
		"",
	)

	summaryGenerator := &recordingTextGenerator{
		result: ai.TextResult{
			ID:           "summary-completion-1",
			Provider:     "fake",
			Model:        "configured-model",
			Content:      summaryJobValidJSON(),
			FinishReason: "stop",
		},
	}
	summaryService, err := agentsummary.NewService(
		repository,
		summaryGenerator,
		agentsummary.Configuration{
			PolicyVersion: "summary-policy-v1",
			PromptVersion: "summary-prompt-v1",
			Provider:      "fake",
			Model:         "configured-model",
		},
	)
	if err != nil {
		t.Fatalf("new Summary service: %v", err)
	}
	worker, err := agentsummary.NewWorker(
		repository,
		repository,
		summaryService,
		summaryWorkerConfiguration(),
	)
	if err != nil {
		t.Fatalf("new Summary worker: %v", err)
	}
	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("process Summary Job: %v", err)
	}
	if result.Completed != 1 || summaryGenerator.CallCount() != 1 {
		t.Fatalf(
			"result = %#v generator calls = %d",
			result,
			summaryGenerator.CallCount(),
		)
	}
	checkpoint, err := repository.FindLatestSummaryCheckpoint(
		context.Background(),
		agentTestUserA,
		thread.ID,
		40,
	)
	if err != nil {
		t.Fatalf("find generated Checkpoint: %v", err)
	}
	if checkpoint.CoveredThroughSequence != 28 ||
		checkpoint.SourceFromSequence != 1 {
		t.Fatalf("unexpected Checkpoint: %#v", checkpoint)
	}
	assertSummaryJobRow(
		t,
		database.pool,
		submission.Run.ID,
		"completed",
		40,
		28,
		checkpoint.ID,
	)

	replay, err := runService.SubmitText(
		context.Background(),
		testActorA(),
		thread.ID,
		"summary-job-trigger",
		"Create a checkpoint after this reply.",
	)
	if err != nil {
		t.Fatalf("replay triggering Run: %v", err)
	}
	if replay.Created || replay.Run.ID != submission.Run.ID {
		t.Fatalf("unexpected replay: %#v", replay)
	}
	var jobCount int
	if err := database.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM agent_thread_summary_jobs
WHERE source_run_id = $1`,
		submission.Run.ID,
	).Scan(&jobCount); err != nil {
		t.Fatalf("count Summary Jobs: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("Summary Job count = %d, want 1", jobCount)
	}
}

func TestThreadSummaryJobSkipsBelowThresholdAndSerializesThreadClaims(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	thread, err := dataService.CreateThread(
		context.Background(),
		testActorA(),
		"",
	)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	first, err := runService.SubmitText(
		context.Background(),
		testActorA(),
		thread.ID,
		"summary-job-small-1",
		"First short exchange.",
	)
	if err != nil {
		t.Fatalf("complete first Run: %v", err)
	}
	second, err := runService.SubmitText(
		context.Background(),
		testActorA(),
		thread.ID,
		"summary-job-small-2",
		"Second short exchange.",
	)
	if err != nil {
		t.Fatalf("complete second Run: %v", err)
	}

	configuration := summaryWorkerConfiguration()
	type claimResult struct {
		claim    agentsummary.JobClaim
		acquired bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			claim, acquired, claimErr := repository.ClaimSummaryJob(
				context.Background(),
				configuration,
			)
			results <- claimResult{
				claim:    claim,
				acquired: acquired,
				err:      claimErr,
			}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var firstClaim agentsummary.JobClaim
	acquiredCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent ClaimSummaryJob: %v", result.err)
		}
		if result.acquired {
			acquiredCount++
			firstClaim = result.claim
		}
	}
	if acquiredCount != 1 || firstClaim.ThreadID != thread.ID {
		t.Fatalf(
			"acquired count = %d claim = %#v",
			acquiredCount,
			firstClaim,
		)
	}
	if _, err := repository.FinishSummaryJob(
		context.Background(),
		firstClaim,
		agentsummary.JobSkipped,
		0,
		"below_threshold",
	); err != nil {
		t.Fatalf("finish first Job: %v", err)
	}

	generator := &recordingTextGenerator{
		result: ai.TextResult{
			ID:           "summary-unused",
			Provider:     "fake",
			Model:        "configured-model",
			Content:      summaryJobValidJSON(),
			FinishReason: "stop",
		},
	}
	service, err := agentsummary.NewService(
		repository,
		generator,
		configuration.Summary,
	)
	if err != nil {
		t.Fatalf("new Summary service: %v", err)
	}
	worker, err := agentsummary.NewWorker(
		repository,
		repository,
		service,
		configuration,
	)
	if err != nil {
		t.Fatalf("new Summary worker: %v", err)
	}
	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("process second Job: %v", err)
	}
	if result.Skipped != 1 || generator.CallCount() != 0 {
		t.Fatalf(
			"result = %#v generator calls = %d",
			result,
			generator.CallCount(),
		)
	}
	assertSummaryJobRow(
		t,
		database.pool,
		first.Run.ID,
		"skipped",
		2,
		0,
		"",
	)
	assertSummaryJobRow(
		t,
		database.pool,
		second.Run.ID,
		"skipped",
		4,
		0,
		"",
	)
}

func TestThreadSummaryJobRecoversExpiredLeaseAndExhaustsRetries(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	thread, err := dataService.CreateThread(
		context.Background(),
		testActorA(),
		"",
	)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		testActorA(),
		thread.ID,
		"summary-job-recovery",
		"Create a recoverable Summary Job.",
	)
	if err != nil {
		t.Fatalf("complete Run: %v", err)
	}
	configuration := summaryWorkerConfiguration()
	firstClaim, acquired, err := repository.ClaimSummaryJob(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim first lease: acquired=%t err=%v", acquired, err)
	}
	if _, err := database.pool.Exec(
		context.Background(),
		`UPDATE agent_thread_summary_jobs
SET
    updated_at = created_at,
    lease_expires_at = created_at + INTERVAL '1 microsecond'
WHERE source_run_id = $1`,
		submission.Run.ID,
	); err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	recovered, acquired, err := repository.ClaimSummaryJob(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("recover expired lease: acquired=%t err=%v", acquired, err)
	}
	if recovered.AttemptCount != 2 ||
		recovered.LeaseToken == firstClaim.LeaseToken {
		t.Fatalf("unexpected recovered claim: %#v", recovered)
	}
	retried, err := repository.FailSummaryJob(
		context.Background(),
		recovered,
		0,
		"dependency",
		true,
		configuration,
	)
	if err != nil {
		t.Fatalf("requeue recovered Job: %v", err)
	}
	if retried.Status != agentsummary.JobPending ||
		retried.OutcomeReason != "dependency" {
		t.Fatalf("unexpected retried Job: %#v", retried)
	}
	if _, err := database.pool.Exec(
		context.Background(),
		`UPDATE agent_thread_summary_jobs
SET next_attempt_at = created_at
WHERE source_run_id = $1`,
		submission.Run.ID,
	); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	finalClaim, acquired, err := repository.ClaimSummaryJob(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim final attempt: acquired=%t err=%v", acquired, err)
	}
	if finalClaim.AttemptCount != configuration.MaxAttempts {
		t.Fatalf(
			"attempt count = %d, want %d",
			finalClaim.AttemptCount,
			configuration.MaxAttempts,
		)
	}
	failed, err := repository.FailSummaryJob(
		context.Background(),
		finalClaim,
		0,
		"dependency",
		true,
		configuration,
	)
	if err != nil {
		t.Fatalf("exhaust retry: %v", err)
	}
	if failed.Status != agentsummary.JobFailed ||
		failed.CompletedAt.IsZero() ||
		failed.OutcomeReason != "dependency" {
		t.Fatalf("unexpected failed Job: %#v", failed)
	}
}

func summaryWorkerConfiguration() agentsummary.WorkerConfiguration {
	return agentsummary.WorkerConfiguration{
		TriggerPolicyVersion: agentsummary.TriggerPolicyV1,
		TriggerMessages:      agentsummary.DefaultTriggerMessages,
		RetainRecentMessages: agentsummary.DefaultRetainedMessages,
		LeaseDuration:        time.Minute,
		MaxAttempts:          agentsummary.DefaultWorkerMaxAttempts,
		Summary: agentsummary.Configuration{
			PolicyVersion: "summary-policy-v1",
			PromptVersion: "summary-prompt-v1",
			Provider:      "fake",
			Model:         "configured-model",
		},
	}
}

func summaryJobValidJSON() string {
	return `{"goals":["Continue the conversation"],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`
}

func assertSummaryJobRow(
	t *testing.T,
	database *pgxpool.Pool,
	runID string,
	wantStatus string,
	wantObserved int64,
	wantTarget int64,
	wantCheckpointID string,
) {
	t.Helper()

	var status string
	var observed int64
	var target int64
	var checkpointID string
	if err := database.QueryRow(
		context.Background(),
		`SELECT
    status,
    observed_through_sequence,
    COALESCE(target_covered_through_sequence, 0),
    COALESCE(checkpoint_id::text, '')
FROM agent_thread_summary_jobs
WHERE source_run_id = $1`,
		runID,
	).Scan(
		&status,
		&observed,
		&target,
		&checkpointID,
	); err != nil {
		t.Fatalf("read Summary Job: %v", err)
	}
	if status != wantStatus ||
		observed != wantObserved ||
		target != wantTarget ||
		checkpointID != wantCheckpointID {
		t.Fatalf(
			"Summary Job = status:%s observed:%d target:%d checkpoint:%s",
			status,
			observed,
			target,
			checkpointID,
		)
	}
}
