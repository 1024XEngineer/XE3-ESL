package scoring

import (
	"context"
	"errors"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestInterviewShadowCoordinatorFreezesThenCreatesFixedEvaluation(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	evaluationValue := validEvaluation()
	evaluationValue.PracticeSessionID = snapshot.PracticeSessionID
	evaluationValue.InputSnapshotID = snapshot.ID
	evaluationValue.InputRevision = snapshot.InputRevision
	evaluationValue.Revision.SceneStrategyRef =
		InterviewShadowStrategyRef
	evaluationValue.Revision.PipelineVersion =
		InterviewShadowPipelineVersion
	freezer := &interviewEvidenceFreezerStub{snapshot: snapshot}
	creator := &interviewEvaluationCreatorStub{
		evaluation: evaluationValue,
		replayed:   true,
	}
	coordinator, err := NewInterviewShadowCoordinator(freezer, creator)
	if err != nil {
		t.Fatalf("NewInterviewShadowCoordinator: %v", err)
	}
	actor := testActor(testOwnerA)
	ctx := requestcontext.WithActor(context.Background(), actor)
	result, replayed, err :=
		coordinator.EnsureForCompletedInterview(
			ctx,
			actor,
			snapshot.PracticeSessionID,
		)
	if err != nil {
		t.Fatalf("EnsureForCompletedInterview: %v", err)
	}
	if !replayed || result.ID != evaluationValue.ID {
		t.Fatalf("result = %#v, replayed = %v", result, replayed)
	}
	if freezer.calls != 1 ||
		freezer.practiceSessionID != snapshot.PracticeSessionID ||
		freezer.scope != evaluation.ScopeSession ||
		freezer.sceneType != evaluation.SceneInterview {
		t.Fatalf("freeze call = %#v", freezer)
	}
	if creator.calls != 1 {
		t.Fatalf("create calls = %d", creator.calls)
	}
	request := creator.request
	if request.PracticeSessionID != snapshot.PracticeSessionID ||
		request.InputSnapshotID != snapshot.ID ||
		request.InputRevision != snapshot.InputRevision ||
		request.Scope != evaluation.ScopeSession ||
		request.SceneType != evaluation.SceneInterview ||
		len(request.Channels) != 1 ||
		request.Channels[0] != evaluation.ChannelScene ||
		request.SceneStrategyRef != InterviewShadowStrategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion != InterviewShadowPipelineVersion {
		t.Fatalf("create request = %#v", request)
	}
}

func TestInterviewShadowCoordinatorStopsWhenFreezeFails(t *testing.T) {
	t.Parallel()
	want := errors.New("freeze unavailable")
	freezer := &interviewEvidenceFreezerStub{err: want}
	creator := &interviewEvaluationCreatorStub{}
	coordinator, err := NewInterviewShadowCoordinator(freezer, creator)
	if err != nil {
		t.Fatalf("NewInterviewShadowCoordinator: %v", err)
	}
	actor := testActor(testOwnerA)
	ctx := requestcontext.WithActor(context.Background(), actor)
	_, _, err = coordinator.EnsureForCompletedInterview(
		ctx,
		actor,
		"practice-session-1",
	)
	if !errors.Is(err, want) || creator.calls != 0 {
		t.Fatalf("error = %v, create calls = %d", err, creator.calls)
	}
}

func TestInterviewShadowPolicyAllowsOnlyFrozenVertical(t *testing.T) {
	t.Parallel()
	validCreate := evaluation.CreateRequest{
		Scope:            evaluation.ScopeSession,
		SceneType:        evaluation.SceneInterview,
		Channels:         []evaluation.Channel{evaluation.ChannelScene},
		SceneStrategyRef: InterviewShadowStrategyRef,
		PipelineVersion:  InterviewShadowPipelineVersion,
	}
	if err := ValidateInterviewShadowCreateRequest(validCreate); err != nil {
		t.Fatalf("valid create policy: %v", err)
	}
	validReevaluation := evaluation.ReevaluateRequest{
		Channels:         []evaluation.Channel{evaluation.ChannelScene},
		SceneStrategyRef: InterviewShadowStrategyRef,
		PipelineVersion:  InterviewShadowPipelineVersion,
	}
	if err := ValidateInterviewShadowReevaluateRequest(
		validReevaluation,
	); err != nil {
		t.Fatalf("valid re-evaluation policy: %v", err)
	}

	invalidCreates := []evaluation.CreateRequest{
		func() evaluation.CreateRequest {
			value := validCreate
			value.Scope = evaluation.ScopeTurn
			return value
		}(),
		func() evaluation.CreateRequest {
			value := validCreate
			value.SceneType = evaluation.SceneIELTSSpeaking
			return value
		}(),
		func() evaluation.CreateRequest {
			value := validCreate
			value.Channels = []evaluation.Channel{evaluation.ChannelCore4D}
			return value
		}(),
		func() evaluation.CreateRequest {
			value := validCreate
			value.SceneStrategyRef = "interview-scene-shadow/v2"
			return value
		}(),
		func() evaluation.CreateRequest {
			value := validCreate
			value.PipelineVersion = "evaluation-pipeline-shadow/v2"
			return value
		}(),
	}
	for _, request := range invalidCreates {
		if err := ValidateInterviewShadowCreateRequest(request); !errors.Is(err, ErrStrategyNotAvailable) {
			t.Errorf("create policy error = %v for %#v", err, request)
		}
	}
}

type interviewEvidenceFreezerStub struct {
	snapshot          evidence.EvidenceSnapshot
	replayed          bool
	err               error
	calls             int
	practiceSessionID string
	scope             evaluation.Scope
	sceneType         evaluation.SceneType
}

func (stub *interviewEvidenceFreezerStub) Freeze(
	_ context.Context,
	_ requestcontext.Actor,
	practiceSessionID string,
	scope evaluation.Scope,
	sceneType evaluation.SceneType,
) (evidence.EvidenceSnapshot, bool, error) {
	stub.calls++
	stub.practiceSessionID = practiceSessionID
	stub.scope = scope
	stub.sceneType = sceneType
	return stub.snapshot, stub.replayed, stub.err
}

type interviewEvaluationCreatorStub struct {
	evaluation evaluation.Evaluation
	replayed   bool
	err        error
	request    evaluation.CreateRequest
	calls      int
}

func (stub *interviewEvaluationCreatorStub) Create(
	_ context.Context,
	_ requestcontext.Actor,
	request evaluation.CreateRequest,
) (evaluation.Evaluation, bool, error) {
	stub.calls++
	stub.request = request
	return stub.evaluation, stub.replayed, stub.err
}
