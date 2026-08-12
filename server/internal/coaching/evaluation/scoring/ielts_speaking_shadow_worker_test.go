package scoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
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

func TestIELTSSpeakingShadowWorkerCompletesInsufficientEvidenceResult(
	t *testing.T,
) {
	t.Parallel()
	claim := validIELTSSpeakingShadowClaim(t)
	claim.Snapshot = ieltsSpeakingSnapshotWithTranscript(
		t,
		"Yes, yes. 666 这是中文。",
	)
	var err error
	claim.AcousticSnapshot, err = BuildIELTSAcousticSnapshot(
		claim.EvaluationID,
		claim.Snapshot,
		IELTSSpeakingAcousticRead{},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	claim.AcousticSnapshot.CreatedAt = time.Now().UTC()
	claim.InputBundleHash = IELTSAcousticInputBundleHash(
		claim.Snapshot,
		claim.AcousticSnapshot,
	)
	repository := &ieltsShadowRuntimeRepositoryStub{
		claim:    claim,
		acquired: true,
	}
	provider := &ieltsProviderStub{}
	worker, err := NewIELTSSpeakingShadowWorker(
		repository,
		NewIELTSSpeakingShadowEngine(provider),
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
	}) || provider.calls != 0 || repository.completeCalls != 1 ||
		repository.failCalls != 0 ||
		repository.result.Scoreability !=
			IELTSSpeakingScoreabilityInsufficient ||
		ValidateIELTSSpeakingShadowResult(
			claim.Snapshot,
			repository.result,
		) != nil {
		t.Fatalf(
			"sweep = %#v; provider calls = %d; repository = %#v",
			sweep,
			provider.calls,
			repository,
		)
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

func TestIELTSSpeakingShadowWorkerRetryUsesIdenticalFrozenProviderInput(
	t *testing.T,
) {
	t.Parallel()
	claim := validIELTSSpeakingShadowClaim(t)
	repository := &ieltsShadowRuntimeRepositoryStub{
		claim:      claim,
		acquired:   true,
		failStatus: IELTSSpeakingShadowRuntimePending,
	}
	provider := &ieltsRetryProviderStub{failFirst: true}
	worker, err := NewIELTSSpeakingShadowWorker(
		repository,
		NewIELTSSpeakingShadowEngine(provider),
		validIELTSSpeakingShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := worker.ProcessPending(context.Background(), 1)
	if err != nil || first.Retried != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	repository.claim.AttemptCount = 2
	repository.claim.FencingToken = 2
	repository.acquired = true
	second, err := worker.ProcessPending(context.Background(), 1)
	if err != nil || second.Completed != 1 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	if len(provider.inputs) != 2 ||
		!bytes.Equal(provider.inputs[0], provider.inputs[1]) {
		t.Fatalf("provider inputs changed: %q %q", provider.inputs[0], provider.inputs[1])
	}
}

func TestIELTSSpeakingShadowWorkerFailsSchemaMismatchWithoutRetry(
	t *testing.T,
) {
	t.Parallel()
	claim := validIELTSSpeakingShadowClaim(t)
	repository := &ieltsShadowRuntimeRepositoryStub{
		claim:      claim,
		acquired:   true,
		failStatus: IELTSSpeakingShadowRuntimeFailed,
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
	if sweep.Failed != 1 ||
		repository.failCalls != 1 ||
		repository.failure.Code != "provider_schema_mismatch" ||
		repository.failure.Retryable ||
		repository.completeCalls != 0 ||
		repository.result.Provider != nil ||
		len(repository.result.Criteria) != 0 {
		t.Fatalf(
			"sweep = %#v, failure = %#v",
			sweep,
			repository.failure,
		)
	}
}

func TestIELTSSpeakingShadowWorkerLogsSafeSemanticRejectionStage(
	t *testing.T,
) {
	claim := validIELTSSpeakingShadowClaim(t)
	prepared, err := prepareIELTSSpeakingShadow(claim.Snapshot)
	if err != nil {
		t.Fatalf("prepareIELTSSpeakingShadow: %v", err)
	}
	payload := validIELTSProviderPayload(prepared.input)
	anchor := &payload.Criteria[0].Strengths[0].Evidence[0]
	anchor.EvidenceRefID = "missing-evidence-ref"
	anchor.Quote = "I explain"
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	repository := &ieltsShadowRuntimeRepositoryStub{
		claim:      claim,
		acquired:   true,
		failStatus: IELTSSpeakingShadowRuntimeFailed,
	}
	worker, err := NewIELTSSpeakingShadowWorker(
		repository,
		NewIELTSSpeakingShadowEngine(&ieltsProviderStub{payload: raw}),
		validIELTSSpeakingShadowRuntimeConfiguration(),
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowWorker: %v", err)
	}

	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	logged := output.String()
	if sweep.Failed != 1 ||
		!strings.Contains(logged, `"failure_code":"provider_invalid_response"`) ||
		!strings.Contains(logged, `"rejection_stage":"semantic_validation"`) ||
		strings.Contains(logged, "I explain") {
		t.Fatalf("sweep = %#v; log = %s", sweep, logged)
	}
}

func TestClassifyIELTSSpeakingShadowFailureUsesStableProviderCodes(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name      string
		cause     error
		code      string
		retryable bool
	}{
		{
			name:  "invalid JSON",
			cause: errIELTSSpeakingProviderInvalidJSON,
			code:  "provider_invalid_json",
		},
		{
			name:  "schema mismatch",
			cause: errIELTSSpeakingProviderSchemaMismatch,
			code:  "provider_schema_mismatch",
		},
		{
			name:  "semantic response rejection",
			cause: ErrInvalidIELTSSpeakingShadow,
			code:  "provider_invalid_response",
		},
		{
			name:  "invalid request",
			cause: evaluation.ErrInvalidRequest,
			code:  "provider_invalid_response",
		},
		{
			name:      "timeout",
			cause:     context.DeadlineExceeded,
			code:      "provider_timeout",
			retryable: true,
		},
		{
			name: "provider timeout category",
			cause: ieltsGenerationFailureStub{
				category:  "timeout",
				retryable: true,
			},
			code:      "provider_timeout",
			retryable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := classifyIELTSSpeakingShadowFailure(test.cause)
			if failure.Code != test.code ||
				failure.Retryable != test.retryable {
				t.Fatalf("failure = %#v", failure)
			}
		})
	}
}

type ieltsGenerationFailureStub struct {
	category  string
	retryable bool
}

func (failure ieltsGenerationFailureStub) Error() string {
	return "provider generation failed"
}

func (failure ieltsGenerationFailureStub) StableCategory() string {
	return failure.category
}

func (failure ieltsGenerationFailureStub) Retryable() bool {
	return failure.retryable
}

func validIELTSSpeakingShadowRuntimeConfiguration() IELTSSpeakingShadowRuntimeConfiguration {
	return IELTSSpeakingShadowRuntimeConfiguration{
		MaxAttempts:          3,
		LeaseDuration:        time.Minute,
		AcousticWaitDuration: 15 * time.Second,
		StrategyRef:          IELTSSpeakingShadowStrategyRef,
		PipelineVersion:      IELTSSpeakingShadowPipelineVersion,
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
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	acoustics, err := BuildIELTSAcousticSnapshot(
		testEvalID,
		snapshot,
		IELTSSpeakingAcousticRead{},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	acoustics.CreatedAt = time.Now().UTC()
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
		Snapshot:             snapshot,
		AcousticSnapshot:     acoustics,
		InputBundleHash: IELTSAcousticInputBundleHash(
			snapshot,
			acoustics,
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

type ieltsRetryProviderStub struct {
	failFirst bool
	inputs    [][]byte
}

func (stub *ieltsRetryProviderStub) AnalyzeIELTSSpeaking(
	ctx context.Context,
	input IELTSSpeakingShadowProviderInput,
) (IELTSSpeakingShadowProviderResult, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return IELTSSpeakingShadowProviderResult{}, err
	}
	stub.inputs = append(stub.inputs, encoded)
	if stub.failFirst {
		stub.failFirst = false
		return IELTSSpeakingShadowProviderResult{}, context.DeadlineExceeded
	}
	return (&ieltsProviderStub{}).AnalyzeIELTSSpeaking(ctx, input)
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
