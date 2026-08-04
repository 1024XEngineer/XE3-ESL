package evaluation

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestIELTSSpeakingShadowCoordinatorFreezesThenCreatesFixedEvaluation(
	t *testing.T,
) {
	t.Parallel()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
	evaluationValue := validEvaluation()
	evaluationValue.PracticeSessionID = snapshot.PracticeSessionID
	evaluationValue.InputSnapshotID = snapshot.ID
	evaluationValue.InputRevision = snapshot.InputRevision
	evaluationValue.SceneType = SceneIELTSSpeaking
	evaluationValue.Revision.SceneStrategyRef =
		IELTSSpeakingShadowStrategyRef
	evaluationValue.Revision.PipelineVersion =
		IELTSSpeakingShadowPipelineVersion
	freezer := &ieltsEvidenceFreezerStub{snapshot: snapshot}
	creator := &ieltsEvaluationCreatorStub{
		evaluation: evaluationValue,
		replayed:   true,
	}
	coordinator, err := NewIELTSSpeakingShadowCoordinator(
		freezer,
		creator,
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowCoordinator: %v", err)
	}
	actor := testActor(testOwnerA)
	ctx := requestcontext.WithActor(context.Background(), actor)
	result, replayed, err :=
		coordinator.EnsureForCompletedIELTSSpeaking(
			ctx,
			actor,
			snapshot.PracticeSessionID,
		)
	if err != nil {
		t.Fatalf("EnsureForCompletedIELTSSpeaking: %v", err)
	}
	if !replayed || result.ID != evaluationValue.ID {
		t.Fatalf("result = %#v, replayed = %v", result, replayed)
	}
	if freezer.calls != 1 ||
		freezer.practiceSessionID != snapshot.PracticeSessionID ||
		freezer.scope != ScopeSession ||
		freezer.sceneType != SceneIELTSSpeaking {
		t.Fatalf("freeze call = %#v", freezer)
	}
	request := creator.request
	if creator.calls != 1 ||
		request.PracticeSessionID != snapshot.PracticeSessionID ||
		request.InputSnapshotID != snapshot.ID ||
		request.InputRevision != snapshot.InputRevision ||
		request.Scope != ScopeSession ||
		request.SceneType != SceneIELTSSpeaking ||
		len(request.Channels) != 1 ||
		request.Channels[0] != ChannelScene ||
		request.SceneStrategyRef != IELTSSpeakingShadowStrategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion !=
			IELTSSpeakingShadowPipelineVersion {
		t.Fatalf("create request = %#v", request)
	}
}

func TestIELTSSpeakingShadowCoordinatorStopsWhenFreezeFails(
	t *testing.T,
) {
	t.Parallel()
	want := errors.New("freeze unavailable")
	freezer := &ieltsEvidenceFreezerStub{err: want}
	creator := &ieltsEvaluationCreatorStub{}
	coordinator, err := NewIELTSSpeakingShadowCoordinator(
		freezer,
		creator,
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowCoordinator: %v", err)
	}
	actor := testActor(testOwnerA)
	ctx := requestcontext.WithActor(context.Background(), actor)
	_, _, err = coordinator.EnsureForCompletedIELTSSpeaking(
		ctx,
		actor,
		"practice-session-1",
	)
	if !errors.Is(err, want) || creator.calls != 0 {
		t.Fatalf("error = %v, create calls = %d", err, creator.calls)
	}
}

type ieltsEvidenceFreezerStub struct {
	snapshot          EvidenceSnapshot
	err               error
	calls             int
	practiceSessionID string
	scope             Scope
	sceneType         SceneType
}

func (stub *ieltsEvidenceFreezerStub) Freeze(
	_ context.Context,
	_ requestcontext.Actor,
	practiceSessionID string,
	scope Scope,
	sceneType SceneType,
) (EvidenceSnapshot, bool, error) {
	stub.calls++
	stub.practiceSessionID = practiceSessionID
	stub.scope = scope
	stub.sceneType = sceneType
	return stub.snapshot, false, stub.err
}

type ieltsEvaluationCreatorStub struct {
	evaluation Evaluation
	replayed   bool
	err        error
	request    CreateRequest
	calls      int
}

func (stub *ieltsEvaluationCreatorStub) Create(
	_ context.Context,
	_ requestcontext.Actor,
	request CreateRequest,
) (Evaluation, bool, error) {
	stub.calls++
	stub.request = request
	return stub.evaluation, stub.replayed, stub.err
}
