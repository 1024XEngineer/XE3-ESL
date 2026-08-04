package scoring

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"testing"
	"time"
)

func TestIELTSSpeakingShadowWorkerCompletesEvidenceBoundResult(
	t *testing.T,
) {
	t.Parallel()
	claim := validIELTSSpeakingShadowClaim(t)
	repository := &ieltsShadowRuntimeRepositoryStub{
		claim:    claim,
		acquired: true,
	}
	worker, err := NewIELTSSpeakingShadowWorker(
		repository,
		NewIELTSSpeakingShadowEngine(&ieltsProviderStub{}),
		validIELTSSpeakingShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowWorker: %v", err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if sweep != (IELTSSpeakingShadowSweepResult{
		Claimed:   1,
		Completed: 1,
	}) {
		t.Fatalf("sweep = %#v", sweep)
	}
	if repository.completeCalls != 1 ||
		repository.failCalls != 0 ||
		ValidateIELTSSpeakingShadowResult(
			claim.Snapshot,
			repository.result,
		) != nil {
		t.Fatalf("repository = %#v", repository)
	}
}

func TestIELTSSpeakingShadowWorkerRetriesTechnicalFailure(
	t *testing.T,
) {
	t.Parallel()
	claim := validIELTSSpeakingShadowClaim(t)
	repository := &ieltsShadowRuntimeRepositoryStub{
		claim:      claim,
		acquired:   true,
		failStatus: IELTSSpeakingShadowRuntimePending,
	}
	worker, err := NewIELTSSpeakingShadowWorker(
		repository,
		NewIELTSSpeakingShadowEngine(
			&ieltsProviderStub{
				err: errors.New("temporary dependency"),
			},
		),
		validIELTSSpeakingShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowWorker: %v", err)
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

func TestIELTSSpeakingShadowWorkerCompletesFallbackAfterInvalidProviderPayload(
	t *testing.T,
) {
	t.Parallel()
	claim := validIELTSSpeakingShadowClaim(t)
	claim.AttemptCount = 3
	repository := &ieltsShadowRuntimeRepositoryStub{
		claim:    claim,
		acquired: true,
	}
	worker, err := NewIELTSSpeakingShadowWorker(
		repository,
		NewIELTSSpeakingShadowEngine(
			&ieltsProviderStub{
				payload: []byte(`{"overall":100}`),
			},
		),
		validIELTSSpeakingShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowWorker: %v", err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if sweep.Completed != 1 ||
		repository.failCalls != 0 ||
		repository.completeCalls != 1 ||
		ValidateIELTSSpeakingShadowResult(
			claim.Snapshot,
			repository.result,
		) != nil {
		t.Fatalf(
			"sweep = %#v, failure = %#v",
			sweep,
			repository.failure,
		)
	}
}

func validIELTSSpeakingShadowRuntimeConfiguration() IELTSSpeakingShadowRuntimeConfiguration {
	return IELTSSpeakingShadowRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Minute,
		StrategyRef:     IELTSSpeakingShadowStrategyRef,
		PipelineVersion: IELTSSpeakingShadowPipelineVersion,
		FullConfigHash: sha256.Sum256(
			[]byte("ielts-shadow-config-v1"),
		),
		PromptVersion: IELTSSpeakingShadowPromptVersion,
		Provider:      "provider",
		Model:         "model",
	}
}

func validIELTSSpeakingShadowClaim(
	t *testing.T,
) IELTSSpeakingShadowClaim {
	t.Helper()
	configuration := validIELTSSpeakingShadowRuntimeConfiguration()
	return IELTSSpeakingShadowClaim{
		OutboxID:             "61000000-0000-4000-8000-000000000006",
		ModuleRunID:          "71000000-0000-4000-8000-000000000007",
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
		Snapshot: ieltsSpeakingTestSnapshot(
			t,
			IELTSQuestionCount,
		),
	}
}

type ieltsShadowRuntimeRepositoryStub struct {
	claim         IELTSSpeakingShadowClaim
	acquired      bool
	claimErr      error
	failStatus    IELTSSpeakingShadowRuntimeStatus
	completeErr   error
	failErr       error
	completeCalls int
	failCalls     int
	result        IELTSSpeakingShadowResult
	failure       IELTSSpeakingShadowFailure
}

func (stub *ieltsShadowRuntimeRepositoryStub) ClaimIELTSSpeakingShadow(
	_ context.Context,
	_ IELTSSpeakingShadowRuntimeConfiguration,
) (IELTSSpeakingShadowClaim, bool, error) {
	if !stub.acquired {
		return IELTSSpeakingShadowClaim{}, false, stub.claimErr
	}
	stub.acquired = false
	return stub.claim, true, stub.claimErr
}

func (stub *ieltsShadowRuntimeRepositoryStub) CompleteIELTSSpeakingShadow(
	_ context.Context,
	_ IELTSSpeakingShadowClaim,
	result IELTSSpeakingShadowResult,
) error {
	stub.completeCalls++
	stub.result = result
	return stub.completeErr
}

func (stub *ieltsShadowRuntimeRepositoryStub) FailIELTSSpeakingShadow(
	_ context.Context,
	_ IELTSSpeakingShadowClaim,
	failure IELTSSpeakingShadowFailure,
	_ IELTSSpeakingShadowRuntimeConfiguration,
) (IELTSSpeakingShadowRuntimeStatus, error) {
	stub.failCalls++
	stub.failure = failure
	return stub.failStatus, stub.failErr
}

func (stub *ieltsShadowRuntimeRepositoryStub) GetIELTSSpeakingShadowState(
	context.Context,
	string,
	string,
	string,
) (IELTSSpeakingShadowReadState, error) {
	return IELTSSpeakingShadowReadState{}, evaluation.ErrNotFound
}
