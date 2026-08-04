package scoring

import (
	"context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestCompletionIntakeCreatesOneEvaluationAndAcknowledgesHandoff(
	t *testing.T,
) {
	claim := completionHandoffFixture(scene.SceneFamilyInterview,
		scene.SceneModelProjectExperienceDeepDive)
	completions := &completionHandoffRepositoryStub{claim: claim}
	evidence := &completedEvidenceFreezerStub{
		snapshot: completionEvidenceFixture(claim),
	}
	evaluations := &completedEvaluationCreatorStub{}
	intake, err := NewCompletionIntake(
		completions,
		evidence,
		evaluations,
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

func TestCompletionIntakeRoutesDailyPracticeToGeneralSceneEvaluation(
	t *testing.T,
) {
	claim := completionHandoffFixture(
		scene.SceneFamilyDaily,
		scene.SceneModelDailyBasicDialogue,
	)
	completions := &completionHandoffRepositoryStub{claim: claim}
	intake, err := NewCompletionIntake(
		completions,
		&completedEvidenceFreezerStub{
			snapshot: completionEvidenceFixture(claim),
		},
		&completedEvaluationCreatorStub{},
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

func TestCompletionEvaluationRouteCoversEveryFormalSceneModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		family   scene.SceneFamily
		model    scene.SceneModel
		scene    evaluation.SceneType
		strategy string
	}{
		{scene.SceneFamilyInterview, scene.SceneModelProjectExperienceDeepDive, evaluation.SceneInterview, InterviewShadowStrategyRef},
		{scene.SceneFamilyInterview, scene.SceneModelInterviewBasicDialogue, evaluation.SceneInterview, InterviewShadowStrategyRef},
		{scene.SceneFamilyExam, scene.SceneModelIELTSSpeakingPart1, evaluation.SceneIELTSSpeaking, GeneralSceneStrategyRef},
		{scene.SceneFamilyExam, scene.SceneModelIELTSSpeakingPart2, evaluation.SceneIELTSSpeaking, GeneralSceneStrategyRef},
		{scene.SceneFamilyExam, scene.SceneModelIELTSSpeakingPart3, evaluation.SceneIELTSSpeaking, GeneralSceneStrategyRef},
		{scene.SceneFamilyExam, scene.SceneModelIELTSSpeakingFullMock, evaluation.SceneIELTSSpeaking, IELTSSpeakingShadowStrategyRef},
		{scene.SceneFamilyExam, scene.SceneModelExamBasicDialogue, evaluation.SceneIELTSSpeaking, GeneralSceneStrategyRef},
		{scene.SceneFamilyWorkplace, scene.SceneModelProgressAndRiskUpdate, evaluation.SceneOverseasWorkplace, GeneralSceneStrategyRef},
		{scene.SceneFamilyWorkplace, scene.SceneModelWorkplaceBasicDialogue, evaluation.SceneOverseasWorkplace, GeneralSceneStrategyRef},
		{scene.SceneFamilyDaily, scene.SceneModelHotelCheckinAndIssueHandling, evaluation.SceneOverseasDaily, GeneralSceneStrategyRef},
		{scene.SceneFamilyDaily, scene.SceneModelDailyBasicDialogue, evaluation.SceneOverseasDaily, GeneralSceneStrategyRef},
	}
	for _, test := range tests {
		route, err := completionEvaluationRoute(test.family, test.model)
		if err != nil || route.SceneType != test.scene ||
			route.StrategyRef != test.strategy {
			t.Errorf("family=%s model=%s route=%#v error=%v", test.family, test.model, route, err)
		}
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
	family scene.SceneFamily,
	model scene.SceneModel,
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
		SceneFamily:    family,
		SceneModel:     model,
		AttemptCount:   1,
		FencingToken:   1,
		LeaseExpiresAt: now.Add(time.Minute),
	}
}

func completionEvidenceFixture(
	claim practice.CompletionHandoffClaim,
) evidence.EvidenceSnapshot {
	return evidence.EvidenceSnapshot{
		ID:                "evaluation-snapshot-1",
		OwnerUserID:       claim.OwnerUserID,
		PracticeSessionID: claim.Completion.SessionID,
		InputRevision:     claim.Completion.SessionVersion,
		Scope:             evaluation.ScopeSession,
		SceneType: completionSceneTypeFixture(
			claim.SceneFamily,
		),
		SourceManifestHash: [32]byte{1},
		Payload:            []byte(`{"schema":"fixture"}`),
		CreatedAt:          claim.Completion.CreatedAt,
	}
}

func completionSceneTypeFixture(family scene.SceneFamily) evaluation.SceneType {
	switch family {
	case scene.SceneFamilyInterview:
		return evaluation.SceneInterview
	case scene.SceneFamilyExam:
		return evaluation.SceneIELTSSpeaking
	case scene.SceneFamilyDaily:
		return evaluation.SceneOverseasDaily
	case scene.SceneFamilyWorkplace:
		return evaluation.SceneOverseasWorkplace
	default:
		return ""
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
}

func (stub *completedEvidenceFreezerStub) FreezeCompleted(
	context.Context,
	string,
	string,
	evaluation.Scope,
	evaluation.SceneType,
) (evidence.EvidenceSnapshot, bool, error) {
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
