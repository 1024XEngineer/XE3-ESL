package scoring

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestCompletionIntakeCreatesOneEvaluationAndAcknowledgesHandoff(
	t *testing.T,
) {
	claim := completionHandoffFixture(InterviewEvaluationPolicyRef)
	completions := &completionHandoffRepositoryStub{claim: claim}
	evidence := &completedEvidenceFreezerStub{
		snapshot: completionEvidenceFixture(claim),
	}
	evaluations := &completedEvaluationCreatorStub{}
	intake, err := NewCompletionIntake(
		completions,
		evidence,
		evaluations,
		NewEvaluationPolicyRegistry(),
		completionIntakeConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := intake.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (CompletionIntakeSweepResult{Claimed: 1, Delivered: 1}) ||
		completions.completed != 1 || completions.failed != 0 ||
		evaluations.calls != 1 ||
		evaluations.request.InputRevision != 1 ||
		evaluations.request.SceneType != evaluation.SceneInterview ||
		evaluations.request.SceneStrategyRef != InterviewShadowStrategyRef {
		t.Fatalf(
			"sweep=%#v completions=%d failures=%d request=%#v",
			sweep,
			completions.completed,
			completions.failed,
			evaluations.request,
		)
	}
}

func TestCompletionIntakeRejectsEvidenceFromAnotherSessionVersion(
	t *testing.T,
) {
	claim := completionHandoffFixture(IELTSSpeakingFullMockEvaluationPolicyRef)
	completions := &completionHandoffRepositoryStub{claim: claim}
	snapshot := completionEvidenceFixture(claim)
	snapshot.Payload = []byte(`{"practice_context":{"session_version":3}}`)
	evaluations := &completedEvaluationCreatorStub{}
	intake, err := NewCompletionIntake(
		completions,
		&completedEvidenceFreezerStub{snapshot: snapshot},
		evaluations,
		NewEvaluationPolicyRegistry(),
		completionIntakeConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := intake.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (CompletionIntakeSweepResult{Claimed: 1, Failed: 1}) ||
		completions.completed != 0 || completions.failed != 1 ||
		completions.failure.Code != "invalid_completion" ||
		completions.failure.Retryable || evaluations.calls != 0 {
		t.Fatalf(
			"sweep=%#v completed=%d failure=%#v evaluations=%d",
			sweep,
			completions.completed,
			completions.failure,
			evaluations.calls,
		)
	}
}

func TestCompletionIntakeRoutesDailyPracticeToGeneralSceneEvaluation(
	t *testing.T,
) {
	claim := completionHandoffFixture(DailyEvaluationPolicyRef)
	completions := &completionHandoffRepositoryStub{claim: claim}
	intake, err := NewCompletionIntake(
		completions,
		&completedEvidenceFreezerStub{
			snapshot: completionEvidenceFixture(claim),
		},
		&completedEvaluationCreatorStub{},
		NewEvaluationPolicyRegistry(),
		completionIntakeConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := intake.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (CompletionIntakeSweepResult{Claimed: 1, Delivered: 1}) {
		t.Fatalf("sweep=%#v failure=%#v", sweep, completions.failure)
	}
}

func TestEvaluationPolicyRegistryCoversEveryFormalPolicy(t *testing.T) {
	t.Parallel()
	registry := NewEvaluationPolicyRegistry()
	tests := []struct {
		reference string
		scene     evaluation.SceneType
		strategy  string
	}{
		{InterviewEvaluationPolicyRef, evaluation.SceneInterview, InterviewShadowStrategyRef},
		{IELTSSpeakingPracticeEvaluationPolicyRef, evaluation.SceneIELTSSpeaking, GeneralSceneStrategyRef},
		{IELTSSpeakingFullMockEvaluationPolicyRef, evaluation.SceneIELTSSpeaking, IELTSSpeakingShadowStrategyRef},
		{WorkplaceEvaluationPolicyRef, evaluation.SceneOverseasWorkplace, GeneralSceneStrategyRef},
		{DailyEvaluationPolicyRef, evaluation.SceneOverseasDaily, GeneralSceneStrategyRef},
	}
	for _, test := range tests {
		policy, err := registry.resolve(test.reference)
		if err != nil || policy.SceneType != test.scene ||
			policy.StrategyRef != test.strategy ||
			policy.PipelineVersion == "" {
			t.Errorf("reference=%s policy=%#v error=%v", test.reference, policy, err)
		}
	}
}

func TestEvaluationPolicyRegistryRejectsUnknownAndDisabledReferences(
	t *testing.T,
) {
	t.Parallel()
	registry := &EvaluationPolicyRegistry{
		policies: map[string]evaluationPolicySpec{
			"disabled.fixture.evaluation.v1": {
				SceneType:       evaluation.SceneInterview,
				StrategyRef:     InterviewShadowStrategyRef,
				PipelineVersion: InterviewShadowPipelineVersion,
				Enabled:         false,
			},
		},
	}
	for _, reference := range []string{
		"unknown.fixture.evaluation.v1",
		"disabled.fixture.evaluation.v1",
	} {
		if err := registry.ValidateEvaluationPolicyReference(reference); !errors.Is(err, ErrStrategyNotAvailable) {
			t.Errorf("reference=%s error=%v", reference, err)
		}
	}
}

func TestCompletionIntakeRejectsUnknownPolicyBeforeCreatingEvidence(
	t *testing.T,
) {
	claim := completionHandoffFixture("unknown.pipeline.evaluation.v1")
	completions := &completionHandoffRepositoryStub{claim: claim}
	evidence := &completedEvidenceFreezerStub{}
	evaluations := &completedEvaluationCreatorStub{}
	intake, err := NewCompletionIntake(
		completions,
		evidence,
		evaluations,
		NewEvaluationPolicyRegistry(),
		completionIntakeConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := intake.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (CompletionIntakeSweepResult{Claimed: 1, Failed: 1}) ||
		completions.completed != 0 || completions.failed != 1 ||
		completions.failure.Code != "strategy_not_available" ||
		completions.failure.Retryable || evidence.calls != 0 ||
		evaluations.calls != 0 {
		t.Fatalf(
			"sweep=%#v completions=%d failure=%#v evidence=%d evaluations=%d",
			sweep,
			completions.completed,
			completions.failure,
			evidence.calls,
			evaluations.calls,
		)
	}
}

func completionIntakeConfigurationFixture() CompletionIntakeConfiguration {
	return CompletionIntakeConfiguration{
		MaxAttempts:   3,
		LeaseDuration: time.Minute,
		RetryDelay:    time.Second,
	}
}

func completionHandoffFixture(
	evaluationPolicyRef string,
) practice.CompletionHandoffClaim {
	now := time.Date(2026, time.August, 4, 8, 0, 0, 0, time.UTC)
	return practice.CompletionHandoffClaim{
		OwnerUserID: "10000000-0000-4000-8000-000000000001",
		Completion: practice.PracticeCompleted{
			SessionID:       "practice-session-1",
			FinalTurnID:     "turn-3",
			SessionVersion:  4,
			CompletionToken: "practice-session:practice-session-1:completed:v4",
			CreatedAt:       now,
		},
		EvaluationPolicyRef: evaluationPolicyRef,
		AttemptCount:        1,
		FencingToken:        1,
		LeaseExpiresAt:      now.Add(time.Minute),
	}
}

func completionEvidenceFixture(
	claim practice.CompletionHandoffClaim,
) evidence.EvidenceSnapshot {
	policy, err := NewEvaluationPolicyRegistry().resolve(
		claim.EvaluationPolicyRef,
	)
	if err != nil {
		panic(err)
	}
	return evidence.EvidenceSnapshot{
		ID:                 "evaluation-snapshot-1",
		OwnerUserID:        claim.OwnerUserID,
		PracticeSessionID:  claim.Completion.SessionID,
		InputRevision:      1,
		Scope:              evaluation.ScopeSession,
		SceneType:          policy.SceneType,
		SourceManifestHash: [32]byte{1},
		Payload: []byte(
			`{"practice_context":{"session_version":` +
				fmt.Sprint(claim.Completion.SessionVersion) + `}}`,
		),
		CreatedAt: claim.Completion.CreatedAt,
	}
}

type completionHandoffRepositoryStub struct {
	claim     practice.CompletionHandoffClaim
	claimed   bool
	completed int
	failed    int
	failure   practice.CompletionHandoffFailure
}

func (stub *completionHandoffRepositoryStub) ClaimCompletionHandoff(
	context.Context,
	time.Duration,
	int,
) (practice.CompletionHandoffClaim, bool, error) {
	if stub.claimed {
		return practice.CompletionHandoffClaim{}, false, nil
	}
	stub.claimed = true
	return stub.claim, true, nil
}

func (stub *completionHandoffRepositoryStub) CompleteCompletionHandoff(
	context.Context,
	practice.CompletionHandoffClaim,
) error {
	stub.completed++
	return nil
}

func (stub *completionHandoffRepositoryStub) FailCompletionHandoff(
	_ context.Context,
	_ practice.CompletionHandoffClaim,
	failure practice.CompletionHandoffFailure,
	_ time.Duration,
	_ int,
) error {
	stub.failed++
	stub.failure = failure
	return nil
}

type completedEvidenceFreezerStub struct {
	snapshot evidence.EvidenceSnapshot
	calls    int
}

func (stub *completedEvidenceFreezerStub) FreezeCompleted(
	context.Context,
	string,
	string,
	evaluation.Scope,
	evaluation.SceneType,
) (evidence.EvidenceSnapshot, bool, error) {
	stub.calls++
	return stub.snapshot, false, nil
}

type completedEvaluationCreatorStub struct {
	request evaluation.CreateRequest
	calls   int
}

func (stub *completedEvaluationCreatorStub) CreateCompleted(
	_ context.Context,
	_ string,
	request evaluation.CreateRequest,
) (evaluation.Evaluation, bool, error) {
	stub.request = request
	stub.calls++
	return evaluation.Evaluation{}, false, nil
}
