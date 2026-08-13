package agentcapability

import (
	"context"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
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
			ID:                  "10000000-0000-4000-8000-000000000001",
			PreparationSnapshot: profiles.snapshot,
			SceneSelection: scene.SelectionSnapshot{
				Scene:            candidate.Scene,
				SelectedRoleIDs:  append([]string(nil), candidate.DefaultRoleIDs...),
				PracticeOptionID: candidate.DefaultOption.ID,
			},
			SessionPolicy: preparation.SessionPolicy{
				SuggestedDurationSeconds: 600,
				MinEffectiveTurns:        2,
				MaxEffectiveTurns:        4,
			},
			Revision: 1,
			Status:   preparation.PlanStatusReady,
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
	if result.Status != "preview_ready" ||
		result.Handoff.PracticePlanID != plans.plan.ID ||
		result.Handoff.SceneName != candidate.Scene.Name ||
		result.Handoff.Roles[0] != "面试官" ||
		result.Handoff.PracticeScope != "完整模拟" ||
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

func TestServicePortIELTSSpecialtyWithoutTopicChoiceNeedsInput(t *testing.T) {
	candidate := ieltsPreviewCatalogCandidate()
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
			IELTSPracticeMode: string(scene.PracticeModePart2),
		},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if result.Status != "needs_input" ||
		!reflect.DeepEqual(
			result.RequiredMissingFields,
			[]string{"ielts_topic_choice"},
		) || plans.calls != 0 || profiles.profileCalls != 0 ||
		profiles.snapshotCalls != 0 {
		t.Fatalf("result = %#v, plans = %#v, profiles = %#v", result, plans, profiles)
	}
}

func TestServicePortIELTSMapsSemanticChoicesToOfficialPlan(t *testing.T) {
	tests := []struct {
		name        string
		mode        scene.PracticeMode
		topicChoice string
		wantOption  string
		wantCueType string
	}{
		{
			name:        "Part 1 random",
			mode:        scene.PracticeModePart1,
			topicChoice: "random",
			wantOption:  "option-ielts-part-1",
		},
		{
			name:        "Part 2 person",
			mode:        scene.PracticeModePart2,
			topicChoice: "person",
			wantOption:  "option-ielts-part-2",
			wantCueType: "person",
		},
		{
			name:        "Part 3 place",
			mode:        scene.PracticeModePart3,
			topicChoice: "place",
			wantOption:  "option-ielts-part-3",
			wantCueType: "place",
		},
		{
			name:       "full mock",
			mode:       scene.PracticeModeFullMock,
			wantOption: "option-ielts-full-mock",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := ieltsPreviewCatalogCandidate()
			profiles := &profileApplicationStub{
				profile:  preparation.Profile{ID: "profile-1", Version: 1},
				snapshot: preparation.Snapshot{ID: "snapshot-1", SourceVersion: 1},
			}
			plans := &planApplicationStub{
				plan: ieltsPreviewPlan(candidate, test.mode, test.wantOption),
			}
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
					IELTSPracticeMode: string(test.mode),
					IELTSTopicChoice:  test.topicChoice,
				},
			)
			if err != nil {
				t.Fatalf("PreviewPractice() error = %v", err)
			}
			if result.Status != "preview_ready" || plans.calls != 1 ||
				profiles.profileRequest.BackgroundSummary == "" ||
				plans.request.SceneID != candidate.Scene.ID ||
				plans.request.PracticeOptionID != test.wantOption ||
				plans.request.MaxEffectiveTurns != 0 {
				t.Fatalf("result = %#v, request = %#v", result, plans.request)
			}
			if test.wantCueType == "" {
				if plans.request.IELTSSelection != nil {
					t.Fatalf("IELTS selection = %#v, want nil", plans.request.IELTSSelection)
				}
			} else if plans.request.IELTSSelection == nil ||
				plans.request.IELTSSelection.CueCardType != test.wantCueType ||
				plans.request.IELTSSelection.Part1SetID != "" ||
				plans.request.IELTSSelection.TopicGroupID != "" {
				t.Fatalf("IELTS selection = %#v", plans.request.IELTSSelection)
			}
		})
	}
}

func TestServicePortIELTSUsesServerDerivedBackground(t *testing.T) {
	candidate := ieltsPreviewCatalogCandidate()
	profiles := &profileApplicationStub{
		profile:  preparation.Profile{ID: "profile-1", Version: 1},
		snapshot: preparation.Snapshot{ID: "snapshot-1", SourceVersion: 1},
	}
	plans := &planApplicationStub{
		plan: ieltsPreviewPlan(
			candidate,
			scene.PracticeModePart2,
			"option-ielts-part-2",
		),
	}
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
			BackgroundSummary: "Model-supplied background must not be trusted.",
			IELTSPracticeMode: string(scene.PracticeModePart2),
			IELTSTopicChoice:  "person",
		},
	)
	if err != nil || result.Status != "preview_ready" {
		t.Fatalf("PreviewPractice() = (%#v, %v)", result, err)
	}
	const want = "User requested IELTS Speaking PART_2 with person topic."
	if profiles.profileRequest.BackgroundSummary != want {
		t.Fatalf(
			"IELTS background = %q, want %q",
			profiles.profileRequest.BackgroundSummary,
			want,
		)
	}
}

func TestServicePortIELTSFullMockRejectsTopicChoiceWithoutWriting(t *testing.T) {
	candidate := ieltsPreviewCatalogCandidate()
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
			IELTSPracticeMode: string(scene.PracticeModeFullMock),
			IELTSTopicChoice:  "person",
		},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if result.Status != "needs_input" ||
		!reflect.DeepEqual(result.RequiredMissingFields, []string{"ielts_topic_choice"}) ||
		plans.calls != 0 || profiles.profileCalls != 0 {
		t.Fatalf("result = %#v, plans = %#v, profiles = %#v", result, plans, profiles)
	}
}

func previewCallContext() capability.CallContext {
	return capability.CallContext{
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
			ID:         "scene-1",
			Experience: scene.PracticeExperienceInterview,
			Category:   scene.SceneCategoryInterviewProfessional,
			Name:       "项目经历深挖",
			Version:    1,
			Prompt: scene.ScenePrompt{
				PracticeGoal: "练习清晰表达项目影响力",
			},
			Roles: []scene.RoleDefinition{{
				ID:          "role-1",
				DisplayName: "面试官",
			}},
			PracticeOptions: []scene.PracticeOption{{
				ID:                       "option-1",
				SceneID:                  "scene-1",
				Mode:                     scene.PracticeModeFullSimulation,
				DisplayName:              "完整模拟",
				SuggestedDurationSeconds: 600,
				TurnPolicyRef:            "interview.project_deep_dive.turn.v1",
				SessionPolicyRef:         "interview.project_deep_dive.session.v1",
				EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
			}},
		},
		DefaultRoleIDs: []string{"role-1"},
		DefaultOption: scene.PracticeOption{
			ID:                       "option-1",
			SceneID:                  "scene-1",
			Mode:                     scene.PracticeModeFullSimulation,
			DisplayName:              "完整模拟",
			SuggestedDurationSeconds: 600,
			TurnPolicyRef:            "interview.project_deep_dive.turn.v1",
			SessionPolicyRef:         "interview.project_deep_dive.session.v1",
			EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
		},
	}
}

func ieltsPreviewCatalogCandidate() scene.PreviewCatalogCandidate {
	candidate := previewCatalogCandidate()
	candidate.Scene.ID = "scene-ielts-speaking"
	candidate.Scene.Experience = scene.PracticeExperienceIELTSSpeaking
	candidate.Scene.Category = scene.SceneCategoryIELTSSpeaking
	candidate.Scene.Name = "IELTS Speaking"
	candidate.Scene.Roles[0].ID = "role-ielts-examiner"
	candidate.Scene.Roles[0].DisplayName = "IELTS 口语考官"
	candidate.DefaultRoleIDs = []string{"role-ielts-examiner"}
	candidate.Scene.PracticeOptions = []scene.PracticeOption{
		{ID: "option-ielts-full-mock", SceneID: candidate.Scene.ID, Mode: scene.PracticeModeFullMock, DisplayName: "完整模考"},
		{ID: "option-ielts-part-1", SceneID: candidate.Scene.ID, Mode: scene.PracticeModePart1, DisplayName: "Part 1"},
		{ID: "option-ielts-part-2", SceneID: candidate.Scene.ID, Mode: scene.PracticeModePart2, DisplayName: "Part 2"},
		{ID: "option-ielts-part-3", SceneID: candidate.Scene.ID, Mode: scene.PracticeModePart3, DisplayName: "Part 3"},
	}
	candidate.DefaultOption = candidate.Scene.PracticeOptions[0]
	return candidate
}

func ieltsPreviewPlan(
	candidate scene.PreviewCatalogCandidate,
	mode scene.PracticeMode,
	optionID string,
) preparation.PracticePlan {
	part := preparation.IELTSAssignmentPartSnapshot{
		Part:           mode,
		TopicTitle:     "People",
		TurnBlueprints: []string{"Part 1 question: Do you work or study?"},
	}
	switch mode {
	case scene.PracticeModePart2:
		part.CueCard = "Describe a person you admire."
		part.TurnBlueprints = []string{"Part 2 cue card: Describe a person you admire."}
	case scene.PracticeModePart3:
		part.TurnBlueprints = []string{"Part 3 question: Why do people need role models?"}
	case scene.PracticeModeFullMock:
		part.Part = scene.PracticeModePart1
	}
	return preparation.PracticePlan{
		ID:                  "10000000-0000-4000-8000-000000000009",
		PreparationSnapshot: preparation.Snapshot{ID: "snapshot-1"},
		SceneSelection: scene.SelectionSnapshot{
			Scene:            candidate.Scene,
			SelectedRoleIDs:  append([]string(nil), candidate.DefaultRoleIDs...),
			PracticeOptionID: optionID,
		},
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: 300,
			MinEffectiveTurns:        1,
			MaxEffectiveTurns:        1,
		},
		IELTSAssignment: &preparation.IELTSAssignmentSnapshot{
			Mode:  mode,
			Parts: []preparation.IELTSAssignmentPartSnapshot{part},
		},
		Revision: 1,
		Status:   preparation.PlanStatusReady,
	}
}
