package evaluation

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestInterviewShadowWorkerCompletesEvidenceBoundResult(t *testing.T) {
	t.Parallel()
	claim := validInterviewShadowClaim(t)
	repository := &interviewShadowRuntimeRepositoryStub{
		claim:    claim,
		acquired: true,
	}
	provider := &stubInterviewShadowProvider{}
	worker, err := NewInterviewShadowWorker(
		repository,
		NewInterviewShadowEngine(provider),
		validInterviewShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewInterviewShadowWorker: %v", err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if sweep != (InterviewShadowSweepResult{
		Claimed:   1,
		Completed: 1,
	}) {
		t.Fatalf("sweep = %#v", sweep)
	}
	if repository.completeCalls != 1 ||
		repository.failCalls != 0 ||
		ValidateInterviewShadowResult(
			claim.Snapshot,
			repository.result,
		) != nil {
		t.Fatalf("repository = %#v", repository)
	}
}

func TestInterviewShadowWorkerRetriesTechnicalFailure(t *testing.T) {
	t.Parallel()
	claim := validInterviewShadowClaim(t)
	repository := &interviewShadowRuntimeRepositoryStub{
		claim:      claim,
		acquired:   true,
		failStatus: InterviewShadowRuntimePending,
	}
	providerErr := errors.New("temporary dependency")
	worker, err := NewInterviewShadowWorker(
		repository,
		NewInterviewShadowEngine(
			&stubInterviewShadowProvider{err: providerErr},
		),
		validInterviewShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewInterviewShadowWorker: %v", err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if sweep.Retried != 1 ||
		repository.failCalls != 1 ||
		repository.failure.Code != "dependency_error" ||
		!repository.failure.Retryable ||
		repository.completeCalls != 0 {
		t.Fatalf(
			"sweep = %#v, failure = %#v",
			sweep,
			repository.failure,
		)
	}
}

func TestInterviewShadowWorkerFailsInvalidProviderPayload(t *testing.T) {
	t.Parallel()
	claim := validInterviewShadowClaim(t)
	repository := &interviewShadowRuntimeRepositoryStub{
		claim:      claim,
		acquired:   true,
		failStatus: InterviewShadowRuntimeFailed,
	}
	worker, err := NewInterviewShadowWorker(
		repository,
		NewInterviewShadowEngine(
			&stubInterviewShadowProvider{
				payload: []byte(`{"overall":100}`),
			},
		),
		validInterviewShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewInterviewShadowWorker: %v", err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if sweep.Failed != 1 ||
		repository.failure.Code != "provider_invalid_response" ||
		!repository.failure.Retryable ||
		repository.completeCalls != 0 {
		t.Fatalf(
			"sweep = %#v, failure = %#v",
			sweep,
			repository.failure,
		)
	}
}

func TestInterviewShadowWorkerRejectsRuntimeDrift(t *testing.T) {
	t.Parallel()
	claim := validInterviewShadowClaim(t)
	claim.Model = "different-model"
	repository := &interviewShadowRuntimeRepositoryStub{
		claim:    claim,
		acquired: true,
	}
	worker, err := NewInterviewShadowWorker(
		repository,
		NewInterviewShadowEngine(&stubInterviewShadowProvider{}),
		validInterviewShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewInterviewShadowWorker: %v", err)
	}
	if _, err := worker.ProcessPending(
		context.Background(),
		1,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ProcessPending error = %v", err)
	}
	if repository.completeCalls != 0 || repository.failCalls != 0 {
		t.Fatalf("drift reached persistence: %#v", repository)
	}
}

func TestInterviewShadowReadStateRequiresHonestTerminalShape(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	fullConfigHash := validInterviewShadowRuntimeConfiguration().
		FullConfigHash
	tests := []struct {
		name  string
		state InterviewShadowReadState
		valid bool
	}{
		{
			name: "pending",
			state: InterviewShadowReadState{
				ModuleStatus: InterviewShadowRuntimePending,
			},
			valid: true,
		},
		{
			name: "ready",
			state: InterviewShadowReadState{
				ModuleStatus:   InterviewShadowRuntimeReady,
				FullConfigHash: fullConfigHash,
				Result:         &result,
			},
			valid: true,
		},
		{
			name: "failed",
			state: InterviewShadowReadState{
				ModuleStatus:   InterviewShadowRuntimeFailed,
				FullConfigHash: fullConfigHash,
				Failure: &InterviewShadowFailure{
					Code: "provider_timeout",
				},
			},
			valid: true,
		},
		{
			name: "ready without result",
			state: InterviewShadowReadState{
				ModuleStatus: InterviewShadowRuntimeReady,
			},
		},
		{
			name: "pending with failure",
			state: InterviewShadowReadState{
				ModuleStatus: InterviewShadowRuntimePending,
				Failure: &InterviewShadowFailure{
					Code: "provider_timeout",
				},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.state.Valid(&snapshot); got != test.valid {
				t.Fatalf("Valid() = %v, want %v", got, test.valid)
			}
		})
	}
}

func validInterviewShadowRuntimeConfiguration() InterviewShadowRuntimeConfiguration {
	return InterviewShadowRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Minute,
		StrategyRef:     InterviewShadowStrategyRef,
		PipelineVersion: InterviewShadowPipelineVersion,
		FullConfigHash:  sha256.Sum256([]byte("shadow-config-v1")),
		PromptVersion:   InterviewShadowPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
}

func validInterviewShadowClaim(t *testing.T) InterviewShadowClaim {
	t.Helper()
	configuration := validInterviewShadowRuntimeConfiguration()
	return InterviewShadowClaim{
		OutboxID:             "60000000-0000-4000-8000-000000000006",
		ModuleRunID:          "70000000-0000-4000-8000-000000000007",
		EvaluationID:         testEvalID,
		EvaluationRevisionID: testRevID,
		OwnerUserID:          testOwnerA,
		Revision:             1,
		StrategyRef:          configuration.StrategyRef,
		PipelineVersion:      configuration.PipelineVersion,
		AttemptCount:         1,
		FencingToken:         1,
		LeaseExpiresAt:       time.Now().Add(time.Minute),
		FullConfigHash:       configuration.FullConfigHash,
		PromptVersion:        configuration.PromptVersion,
		Provider:             configuration.Provider,
		Model:                configuration.Model,
		Snapshot: interviewShadowTestSnapshot(
			t,
			"I led a careful migration.",
			interviewFollowUpNone,
		),
	}
}

type interviewShadowRuntimeRepositoryStub struct {
	claim         InterviewShadowClaim
	acquired      bool
	claimErr      error
	failStatus    InterviewShadowRuntimeStatus
	completeErr   error
	failErr       error
	completeCalls int
	failCalls     int
	result        InterviewShadowResult
	failure       InterviewShadowFailure
}

func (stub *interviewShadowRuntimeRepositoryStub) ClaimInterviewShadow(
	_ context.Context,
	_ InterviewShadowRuntimeConfiguration,
) (InterviewShadowClaim, bool, error) {
	if !stub.acquired {
		return InterviewShadowClaim{}, false, stub.claimErr
	}
	stub.acquired = false
	return stub.claim, true, stub.claimErr
}

func (stub *interviewShadowRuntimeRepositoryStub) CompleteInterviewShadow(
	_ context.Context,
	_ InterviewShadowClaim,
	result InterviewShadowResult,
) error {
	stub.completeCalls++
	stub.result = result
	return stub.completeErr
}

func (stub *interviewShadowRuntimeRepositoryStub) FailInterviewShadow(
	_ context.Context,
	_ InterviewShadowClaim,
	failure InterviewShadowFailure,
	_ InterviewShadowRuntimeConfiguration,
) (InterviewShadowRuntimeStatus, error) {
	stub.failCalls++
	stub.failure = failure
	return stub.failStatus, stub.failErr
}

func (stub *interviewShadowRuntimeRepositoryStub) GetInterviewShadowState(
	context.Context,
	string,
	string,
	string,
) (InterviewShadowReadState, error) {
	return InterviewShadowReadState{}, ErrNotFound
}
