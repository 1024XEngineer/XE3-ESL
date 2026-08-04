package summary

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

func TestWorkerSkipsBelowThresholdWithoutGeneration(t *testing.T) {
	t.Parallel()

	jobs := &jobRepositoryStub{claim: jobClaimFixture(39)}
	checkpoints := &checkpointRepositoryStub{
		findErr: conversation.ErrNotFound,
	}
	generator := &checkpointGeneratorStub{}
	worker := newWorkerForTest(t, jobs, checkpoints, generator)

	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if result.Skipped != 1 || generator.calls != 0 {
		t.Fatalf("result = %#v generator calls = %d", result, generator.calls)
	}
	if jobs.finishedStatus != JobSkipped ||
		jobs.finishedTarget != 0 ||
		jobs.finishedReason != "below_threshold" {
		t.Fatalf("unexpected finish: %#v", jobs)
	}
}

func TestWorkerRetainsRecentMessagesAndCompletes(t *testing.T) {
	t.Parallel()

	claim := jobClaimFixture(40)
	jobs := &jobRepositoryStub{claim: claim}
	checkpoints := &checkpointRepositoryStub{findErr: conversation.ErrNotFound}
	generator := &checkpointGeneratorStub{}
	generator.generate = func(
		command GenerateCheckpointCommand,
	) (Checkpoint, error) {
		if command.CoveredThroughSequence != 20 {
			t.Fatalf(
				"covered through = %d, want 20",
				command.CoveredThroughSequence,
			)
		}
		return checkpointForTarget(claim, 20), nil
	}
	worker := newWorkerForTest(t, jobs, checkpoints, generator)

	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if result.Completed != 1 || generator.calls != 1 {
		t.Fatalf("result = %#v generator calls = %d", result, generator.calls)
	}
	if jobs.completedTarget != 20 ||
		jobs.completedCheckpoint.CoveredThroughSequence != 20 {
		t.Fatalf("unexpected completion: %#v", jobs)
	}
}

func TestWorkerCapsBacklogAtSummarySourceLimit(t *testing.T) {
	t.Parallel()

	claim := jobClaimFixture(200)
	jobs := &jobRepositoryStub{claim: claim}
	previous := checkpointForTarget(claim, 20)
	checkpoints := &checkpointRepositoryStub{latest: previous}
	generator := &checkpointGeneratorStub{}
	generator.generate = func(
		command GenerateCheckpointCommand,
	) (Checkpoint, error) {
		if command.CoveredThroughSequence != 120 {
			t.Fatalf(
				"covered through = %d, want 120",
				command.CoveredThroughSequence,
			)
		}
		return checkpointForTarget(claim, 120), nil
	}
	worker := newWorkerForTest(t, jobs, checkpoints, generator)

	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if result.Completed != 1 || jobs.completedTarget != 120 {
		t.Fatalf("unexpected result = %#v jobs = %#v", result, jobs)
	}
}

func TestWorkerSupersedesAlreadyCoveredJob(t *testing.T) {
	t.Parallel()

	claim := jobClaimFixture(40)
	jobs := &jobRepositoryStub{claim: claim}
	checkpoints := &checkpointRepositoryStub{
		latest: checkpointForTarget(claim, 40),
	}
	generator := &checkpointGeneratorStub{}
	worker := newWorkerForTest(t, jobs, checkpoints, generator)

	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if result.Superseded != 1 ||
		generator.calls != 0 ||
		jobs.finishedReason != "already_covered" {
		t.Fatalf(
			"result = %#v generator calls = %d jobs = %#v",
			result,
			generator.calls,
			jobs,
		)
	}
}

func TestWorkerRecordsRetryableAndTerminalFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		generation error
		wantStatus JobStatus
		wantReason string
	}{
		{
			name: "retryable provider",
			generation: testGenerationFailure{
				category:  "provider_unavailable",
				retryable: true,
			},
			wantStatus: JobPending,
			wantReason: "provider_unavailable",
		},
		{
			name:       "invalid response",
			generation: ErrInvalidResponse,
			wantStatus: JobFailed,
			wantReason: "invalid_response",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			jobs := &jobRepositoryStub{
				claim:      jobClaimFixture(40),
				failedWith: test.wantStatus,
			}
			checkpoints := &checkpointRepositoryStub{
				findErr: conversation.ErrNotFound,
			}
			generator := &checkpointGeneratorStub{
				generate: func(
					GenerateCheckpointCommand,
				) (Checkpoint, error) {
					return Checkpoint{}, test.generation
				},
			}
			worker := newWorkerForTest(
				t,
				jobs,
				checkpoints,
				generator,
			)

			result, err := worker.ProcessPending(
				context.Background(),
				1,
			)
			if err != nil {
				t.Fatalf("ProcessPending: %v", err)
			}
			if jobs.failureReason != test.wantReason ||
				jobs.failureRetryable !=
					(test.wantStatus == JobPending) {
				t.Fatalf("unexpected failure recording: %#v", jobs)
			}
			if test.wantStatus == JobPending && result.Retried != 1 {
				t.Fatalf("result = %#v", result)
			}
			if test.wantStatus == JobFailed && result.Failed != 1 {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

type testGenerationFailure struct {
	category  string
	retryable bool
}

func (failure testGenerationFailure) Error() string {
	return "generation failed"
}

func (failure testGenerationFailure) StableCategory() string {
	return failure.category
}

func (failure testGenerationFailure) Retryable() bool {
	return failure.retryable
}

func newWorkerForTest(
	t *testing.T,
	jobs JobRepository,
	checkpoints CheckpointRepository,
	generator CheckpointGenerator,
) *Worker {
	t.Helper()

	worker, err := NewWorker(
		jobs,
		checkpoints,
		generator,
		workerConfigurationFixture(),
	)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return worker
}

func workerConfigurationFixture() WorkerConfiguration {
	return WorkerConfiguration{
		TriggerPolicyVersion: TriggerPolicyV2,
		TriggerMessages:      DefaultTriggerMessages,
		RetainRecentMessages: DefaultRetainedMessages,
		LeaseDuration:        time.Minute,
		MaxAttempts:          DefaultWorkerMaxAttempts,
		Summary: Configuration{
			PolicyVersion: "summary-policy-v1",
			PromptVersion: "summary-prompt-v1",
			Provider:      "fake",
			Model:         "fake-model",
		},
	}
}

func jobClaimFixture(observedThrough int64) JobClaim {
	return JobClaim{Job: Job{
		SourceRunID:             "11111111-1111-4111-8111-111111111111",
		OwnerID:                 "22222222-2222-4222-8222-222222222222",
		ThreadID:                "33333333-3333-4333-8333-333333333333",
		ObservedThroughSequence: observedThrough,
		SourceCompletedAt:       time.Unix(1, 0).UTC(),
		Status:                  JobRunning,
		AttemptCount:            1,
		LeaseToken:              "44444444-4444-4444-8444-444444444444",
		LeaseExpiresAt:          time.Now().Add(time.Minute),
		TriggerPolicyVersion:    TriggerPolicyV2,
		SummaryPolicyVersion:    "summary-policy-v1",
		PromptVersion:           "summary-prompt-v1",
		Provider:                "fake",
		Model:                   "fake-model",
	}}
}

func checkpointForTarget(
	claim JobClaim,
	target int64,
) Checkpoint {
	sourceFrom := int64(1)
	previousID := ""
	if target > 40 {
		sourceFrom = 21
		previousID = "55555555-5555-4555-8555-555555555555"
	}
	return Checkpoint{
		ID:                     "66666666-6666-4666-8666-666666666666",
		OwnerID:                claim.OwnerID,
		ThreadID:               claim.ThreadID,
		PreviousCheckpointID:   previousID,
		SourceFromSequence:     sourceFrom,
		CoveredThroughSequence: target,
		Content: Content{
			Goals:         []string{"Continue the conversation"},
			Background:    []string{},
			Progress:      []string{},
			Decisions:     []string{},
			OpenQuestions: []string{},
			NextSteps:     []string{},
		},
		PolicyVersion:  "summary-policy-v1",
		PromptVersion:  "summary-prompt-v1",
		Provider:       "fake",
		Model:          "fake-model",
		SourceChecksum: sha256.Sum256([]byte("source")),
		CreatedAt:      time.Now(),
	}
}

type jobRepositoryStub struct {
	claim               JobClaim
	claimed             bool
	completedTarget     int64
	completedCheckpoint Checkpoint
	finishedStatus      JobStatus
	finishedTarget      int64
	finishedReason      string
	failedWith          JobStatus
	failureTarget       int64
	failureReason       string
	failureRetryable    bool
}

func (repository *jobRepositoryStub) ClaimJob(
	context.Context,
	WorkerConfiguration,
) (JobClaim, bool, error) {
	if repository.claimed {
		return JobClaim{}, false, nil
	}
	repository.claimed = true
	return repository.claim, true, nil
}

func (repository *jobRepositoryStub) CompleteJob(
	_ context.Context,
	claim JobClaim,
	target int64,
	checkpoint Checkpoint,
) (Job, error) {
	repository.completedTarget = target
	repository.completedCheckpoint = checkpoint
	job := claim.Job
	job.Status = JobCompleted
	return job, nil
}

func (repository *jobRepositoryStub) FinishJob(
	_ context.Context,
	claim JobClaim,
	status JobStatus,
	target int64,
	reason string,
) (Job, error) {
	repository.finishedStatus = status
	repository.finishedTarget = target
	repository.finishedReason = reason
	job := claim.Job
	job.Status = status
	return job, nil
}

func (repository *jobRepositoryStub) FailJob(
	_ context.Context,
	claim JobClaim,
	target int64,
	reason string,
	retryable bool,
	_ WorkerConfiguration,
) (Job, error) {
	repository.failureTarget = target
	repository.failureReason = reason
	repository.failureRetryable = retryable
	job := claim.Job
	job.Status = repository.failedWith
	return job, nil
}

type checkpointRepositoryStub struct {
	latest  Checkpoint
	findErr error
}

func (*checkpointRepositoryStub) CreateCheckpoint(
	context.Context,
	CreateCheckpointCommand,
) (Checkpoint, error) {
	return Checkpoint{}, errors.New("unexpected create")
}

func (repository *checkpointRepositoryStub) FindLatestCheckpoint(
	context.Context,
	string,
	string,
	int64,
) (Checkpoint, error) {
	return repository.latest, repository.findErr
}

type checkpointGeneratorStub struct {
	calls    int
	generate func(
		GenerateCheckpointCommand,
	) (Checkpoint, error)
}

func (generator *checkpointGeneratorStub) GenerateCheckpoint(
	_ context.Context,
	command GenerateCheckpointCommand,
) (Checkpoint, error) {
	generator.calls++
	if generator.generate == nil {
		return Checkpoint{}, errors.New(
			"unexpected generation",
		)
	}
	return generator.generate(command)
}
