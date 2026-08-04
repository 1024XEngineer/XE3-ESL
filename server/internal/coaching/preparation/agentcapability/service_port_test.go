package agentcapability

import (
	"context"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type planApplicationStub struct {
	calls          int
	actor          requestcontext.Actor
	idempotencyKey string
	request        preparation.CreatePlanRequest
	plan           preparation.PracticePlan
	replayed       bool
	err            error
}

func (stub *planApplicationStub) CreatePlan(
	_ context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	request preparation.CreatePlanRequest,
) (preparation.PracticePlan, bool, error) {
	stub.calls++
	stub.actor = actor
	stub.idempotencyKey = idempotencyKey
	stub.request = request
	return stub.plan, stub.replayed, stub.err
}

type previewCatalogStub struct {
	items []scene.PreviewCatalogCandidate
	err   error
}

func (stub previewCatalogStub) ResolvePreviewCatalog(
	context.Context,
	string,
) ([]scene.PreviewCatalogCandidate, error) {
	return stub.items, stub.err
}

type profileApplicationStub struct {
	profileCalls    int
	profileRequest  preparation.CreateProfileRequest
	profile         preparation.Profile
	profileErr      error
	snapshotCalls   int
	snapshotProfile string
	snapshotRequest preparation.CreateSnapshotRequest
	snapshot        preparation.Snapshot
	snapshotErr     error
}

func (stub *profileApplicationStub) CreateProfile(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	request preparation.CreateProfileRequest,
) (preparation.Profile, bool, error) {
	stub.profileCalls++
	stub.profileRequest = request
	return stub.profile, false, stub.profileErr
}

func (stub *profileApplicationStub) CreateSnapshot(
	_ context.Context,
	_ requestcontext.Actor,
	profileID string,
	_ string,
	request preparation.CreateSnapshotRequest,
) (preparation.Snapshot, bool, error) {
	stub.snapshotCalls++
	stub.snapshotProfile = profileID
	stub.snapshotRequest = request
	return stub.snapshot, false, stub.snapshotErr
}

func TestServicePortNeedsInputDoesNotWrite(t *testing.T) {
	plans := &planApplicationStub{}
	profiles := &profileApplicationStub{}
	port, err := NewServicePort(plans, previewCatalogStub{
		items: []scene.PreviewCatalogCandidate{previewCatalogCandidate()},
	}, profiles)
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	result, err := port.PreviewPractice(
		context.Background(),
		previewCallContext(),
		PreviewInput{SceneQuery: "项目经历深挖"},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	wantMissing := []string{"background_summary", "max_effective_turns"}
	if result.Status != "needs_input" ||
		!reflect.DeepEqual(result.RequiredMissingFields, wantMissing) ||
		plans.calls != 0 || profiles.profileCalls != 0 ||
		profiles.snapshotCalls != 0 {
		t.Fatalf("result = %#v, plans = %#v, profiles = %#v", result, plans, profiles)
	}
}

func TestServicePortCreatesSnapshotBeforeCanonicalPlan(t *testing.T) {
	candidate := previewCatalogCandidate()
	profiles := &profileApplicationStub{
		profile: preparation.Profile{ID: "profile-1", Version: 2},
		snapshot: preparation.Snapshot{
			ID:              "snapshot-1",
			SourceProfileID: "profile-1",
			SourceVersion:   2,
		},
	}
	plans := &planApplicationStub{
		replayed: true,
		plan: preparation.PracticePlan{
			ID:                  "plan-1",
			PreparationSnapshot: profiles.snapshot,
			SceneSelection: scene.SelectionSnapshot{
				Scene:            candidate.Scene,
				SelectedRoleIDs:  append([]string(nil), candidate.DefaultRoleIDs...),
				PracticeOptionID: candidate.DefaultOption.ID,
			},
			SessionPolicy: preparation.SessionPolicy{MaxEffectiveTurns: 4},
			Revision:      1,
			Status:        preparation.PlanStatusReady,
		},
	}
	port, err := NewServicePort(plans, previewCatalogStub{
		items: []scene.PreviewCatalogCandidate{candidate},
	}, profiles)
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	call := previewCallContext()
	result, err := port.PreviewPractice(
		context.Background(),
		call,
		PreviewInput{
			SceneQuery:        "项目经历深挖",
			BackgroundSummary: "AI 产品经理，重点练习项目影响力表达。",
			GoalID:            "goal-1",
			MaxEffectiveTurns: 4,
		},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if result.Status != "preview_ready" || result.PracticePlanID != "plan-1" ||
		!result.Replayed || len(result.SourceRefs) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if profiles.profileCalls != 1 || profiles.snapshotCalls != 1 ||
		profiles.profileRequest.BackgroundSummary == "" ||
		profiles.snapshotProfile != "profile-1" ||
		profiles.snapshotRequest.SourceVersion != 2 {
		t.Fatalf("profiles = %#v", profiles)
	}
	if plans.actor != call.Actor || plans.idempotencyKey != call.RequestID ||
		plans.request.SourceThreadID != call.ThreadID ||
		plans.request.GoalID != "goal-1" ||
		plans.request.PreparationSnapshotID != "snapshot-1" ||
		plans.request.SceneID != candidate.Scene.ID ||
		plans.request.SceneVersion != candidate.Scene.Version ||
		plans.request.MaxEffectiveTurns != 4 {
		t.Fatalf("plan request = %#v", plans.request)
	}
}

func TestServicePortIELTSWithoutQuestionSelectionNeedsInput(t *testing.T) {
	candidate := previewCatalogCandidate()
	candidate.Scene.Family = scene.SceneFamilyExam
	candidate.Scene.Model = scene.SceneModelIELTSSpeakingPart2
	candidate.Scene.Name = "IELTS Speaking Part 2"
	plans := &planApplicationStub{}
	profiles := &profileApplicationStub{}
	port, err := NewServicePort(plans, previewCatalogStub{
		items: []scene.PreviewCatalogCandidate{candidate},
	}, profiles)
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	result, err := port.PreviewPractice(
		context.Background(),
		previewCallContext(),
		PreviewInput{
			SceneQuery:        candidate.Scene.Name,
			BackgroundSummary: "Preparing for IELTS Speaking.",
			MaxEffectiveTurns: 4,
		},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if result.Status != "needs_input" ||
		!reflect.DeepEqual(
			result.RequiredMissingFields,
			[]string{"ielts_selection"},
		) || plans.calls != 0 || profiles.profileCalls != 0 ||
		profiles.snapshotCalls != 0 {
		t.Fatalf("result = %#v, plans = %#v, profiles = %#v", result, plans, profiles)
	}
}

func previewCallContext() tool.CallContext {
	return tool.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "user-1",
			SessionID: "session-1",
		},
		ThreadID:  "thread-1",
		RequestID: "request-1",
	}
}

func previewCatalogCandidate() scene.PreviewCatalogCandidate {
	return scene.PreviewCatalogCandidate{
		Scene: scene.SceneDefinition{
			ID:      "scene-1",
			Family:  scene.SceneFamilyInterview,
			Model:   scene.SceneModelProjectExperienceDeepDive,
			Name:    "项目经历深挖",
			Version: 1,
		},
		DefaultRoleIDs: []string{"role-1"},
		DefaultOption: scene.PracticeOption{
			ID: "option-1",
		},
	}
}
