package title

import (
	"context"
	"testing"
	"time"
)

func TestWorkerRecordsRetryableAndTerminalFailures(t *testing.T) {
	tests := map[string]struct {
		cause      error
		wantStatus JobStatus
		wantResult SweepResult
	}{
		"retryable provider failure": {
			cause:      testGenerationFailure{kind: "provider_timeout", retryable: true},
			wantStatus: JobPending,
			wantResult: SweepResult{Claimed: 1, Retried: 1},
		},
		"invalid response": {
			cause:      ErrInvalidResponse,
			wantStatus: JobFailed,
			wantResult: SweepResult{Claimed: 1, Failed: 1},
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			repository := &workerRepository{
				claim:      testClaim(),
				failStatus: testCase.wantStatus,
			}
			worker, err := NewWorker(
				repository,
				titleGeneratorStub{err: testCase.cause},
				testWorkerConfiguration(),
			)
			if err != nil {
				t.Fatalf("NewWorker: %v", err)
			}
			result, err := worker.ProcessPending(context.Background(), 1)
			if err != nil {
				t.Fatalf("ProcessPending: %v", err)
			}
			if result != testCase.wantResult {
				t.Fatalf("result = %#v, want %#v", result, testCase.wantResult)
			}
			if repository.failureKind == "" {
				t.Fatal("failure kind was not recorded")
			}
		})
	}
}

type titleGeneratorStub struct {
	title string
	err   error
}

func (generator titleGeneratorStub) GenerateTitle(
	context.Context,
	JobClaim,
) (string, error) {
	return generator.title, generator.err
}

type workerRepository struct {
	claim       JobClaim
	claimed     bool
	failStatus  JobStatus
	failureKind string
}

func (repository *workerRepository) ClaimJob(
	context.Context,
	WorkerConfiguration,
) (JobClaim, bool, error) {
	if repository.claimed {
		return JobClaim{}, false, nil
	}
	repository.claimed = true
	return repository.claim, true, nil
}

func (repository *workerRepository) CompleteJob(
	context.Context,
	JobClaim,
	string,
) (Job, error) {
	return Job{Status: JobCompleted}, nil
}

func (repository *workerRepository) FailJob(
	_ context.Context,
	_ JobClaim,
	failureKind string,
	_ bool,
	_ WorkerConfiguration,
) (Job, error) {
	repository.failureKind = failureKind
	return Job{Status: repository.failStatus}, nil
}

type testGenerationFailure struct {
	kind      string
	retryable bool
}

func (failure testGenerationFailure) Error() string { return failure.kind }
func (failure testGenerationFailure) StableCategory() string {
	return failure.kind
}
func (failure testGenerationFailure) Retryable() bool { return failure.retryable }

func testWorkerConfiguration() WorkerConfiguration {
	return WorkerConfiguration{
		LeaseDuration: time.Minute,
		MaxAttempts:   DefaultWorkerMaxAttempts,
		Generation:    testConfiguration(),
	}
}

var _ GenerationFailure = testGenerationFailure{}
