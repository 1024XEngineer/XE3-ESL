package agentcapability

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestPreviewReadyCompletesCurrentTurn(t *testing.T) {
	result := previewToolResult(PreviewResult{
		Status: "preview_ready",
	})
	if result.TurnOutcome != capability.TurnOutcomeCompleted {
		t.Fatalf("TurnOutcome = %v, want completed", result.TurnOutcome)
	}
}

func TestEveryNonReadyResolutionCompletesCurrentTurn(t *testing.T) {
	result := previewToolResult(PreviewResult{
		Status:        "needs_details",
		AssistantText: "请补充具体情境。",
	})
	if result.TurnOutcome != capability.TurnOutcomeCompleted {
		t.Fatalf("TurnOutcome = %v, want completed", result.TurnOutcome)
	}
}

func TestUnresolvedSceneCompletesTurnWithOneClarification(t *testing.T) {
	const clarification = "请补充一个更具体的练习场景。"
	result := previewToolResult(PreviewResult{
		Status:        "needs_input",
		AssistantText: clarification,
	})
	if result.TurnOutcome != capability.TurnOutcomeCompleted ||
		result.AssistantText != clarification ||
		len(result.ClientActions) != 0 {
		t.Fatalf("clarification result = %#v", result)
	}
}

func TestSceneQueryInvocationIsClassifiedAsMayWrite(t *testing.T) {
	effect, err := (PreviewTool{}).ClassifyInvocationEffect(
		json.RawMessage(`{"scene_query":"职场会议意见分歧"}`),
	)
	if err != nil {
		t.Fatalf("ClassifyInvocationEffect() error = %v", err)
	}
	if effect != capability.InvocationEffectMayWrite {
		t.Fatalf("InvocationEffect = %v, want may write", effect)
	}
}

func TestUniqueSceneQueryCreatesPreviewWithoutAnotherModelRound(t *testing.T) {
	plans := &capturingPlanApplication{}
	port, err := NewServicePort(plans, singleWorkplaceCatalog{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}

	_, err = port.PreviewPractice(
		context.Background(),
		capability.CallContext{
			Actor: requestcontext.Actor{
				UserID:    "10000000-0000-4000-8000-000000000001",
				SessionID: "20000000-0000-4000-8000-000000000001",
			},
			ThreadID:       "30000000-0000-4000-8000-000000000001",
			RunID:          "40000000-0000-4000-8000-000000000001",
			InputMessageID: "50000000-0000-4000-8000-000000000001",
			ToolCallID:     "call-preview",
			RequestID:      "request-preview",
		},
		PreviewInput{SceneQuery: "职场会议意见分歧"},
	)
	if !errors.Is(err, errPlanCaptured) {
		t.Fatalf("PreviewPractice() error = %v, want captured request", err)
	}
	request := plans.request
	if request.BackgroundSummary !=
		"User requested practice for: 职场会议意见分歧" ||
		request.SceneID != "scn_workplace_meeting_disagreement" ||
		request.SceneVersion != 1 ||
		len(request.SelectedRoleIDs) != 1 ||
		request.SelectedRoleIDs[0] != "role-meeting-facilitator" ||
		request.PracticeOptionID != "option-full-simulation" ||
		request.MaxEffectiveTurns != 5 {
		t.Fatalf("captured CreatePlanRequest = %#v", request)
	}
}

func TestEveryActiveCatalogSceneReachesPlanCreation(t *testing.T) {
	catalog, err := scene.NewBuiltinCatalog(
		scene.EvaluationPolicyReferenceValidatorFunc(
			func(string) error { return nil },
		),
	)
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	if len(definitions) != 27 {
		t.Fatalf("active scene count = %d, want 27", len(definitions))
	}
	for _, definition := range definitions {
		t.Run(definition.ID, func(t *testing.T) {
			plans := &capturingPlanApplication{}
			port, portErr := NewServicePort(plans, catalog)
			if portErr != nil {
				t.Fatalf("NewServicePort() error = %v", portErr)
			}
			input := PreviewInput{SceneQuery: definition.Name}
			if definition.Experience == scene.PracticeExperienceIELTSSpeaking {
				input.IELTSPracticeMode = string(scene.PracticeModeFullMock)
			}
			_, previewErr := port.PreviewPractice(
				context.Background(),
				validPreviewCallContext(),
				input,
			)
			if !errors.Is(previewErr, errPlanCaptured) {
				t.Fatalf("PreviewPractice() error = %v, want captured request", previewErr)
			}
			if plans.request.SceneID != definition.ID ||
				plans.request.SceneVersion != definition.Version {
				t.Fatalf("captured request = %#v", plans.request)
			}
		})
	}
}

func TestFourExperienceEntrypointsAndEnglishAliasReachPlanCreation(t *testing.T) {
	catalog, err := scene.NewBuiltinCatalog(
		scene.EvaluationPolicyReferenceValidatorFunc(
			func(string) error { return nil },
		),
	)
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	tests := map[string]string{
		"帮我创建一个面试练习":            "scn_interview_self_introduction",
		"帮我创建一个雅思口语练习":          "scn_ielts_speaking",
		"帮我创建一个职场英语练习":          "scn_workplace_progress_risk_update",
		"帮我创建一个生活与旅行英语练习":       "scn_daily_small_talk",
		"disagree in a meeting": "scn_workplace_meeting_disagreement",
	}
	for query, wantSceneID := range tests {
		t.Run(query, func(t *testing.T) {
			plans := &capturingPlanApplication{}
			port, portErr := NewServicePort(plans, catalog)
			if portErr != nil {
				t.Fatalf("NewServicePort() error = %v", portErr)
			}
			input := PreviewInput{SceneQuery: query}
			if wantSceneID == "scn_ielts_speaking" {
				input.IELTSPracticeMode = string(scene.PracticeModeFullMock)
			}
			_, previewErr := port.PreviewPractice(
				context.Background(),
				validPreviewCallContext(),
				input,
			)
			if !errors.Is(previewErr, errPlanCaptured) ||
				plans.request.SceneID != wantSceneID {
				t.Fatalf("result error = %v, request = %#v", previewErr, plans.request)
			}
		})
	}
}

func TestCatalogSceneWithPersonalConditionsStaysCatalog(t *testing.T) {
	catalog, err := scene.NewBuiltinCatalog(
		scene.EvaluationPolicyReferenceValidatorFunc(
			func(string) error { return nil },
		),
	)
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	plans := &capturingPlanApplication{}
	port, err := NewServicePort(plans, catalog)
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	_, err = port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		PreviewInput{
			SceneQuery:        "酒店入住",
			BackgroundSummary: "用户预订了海景房，但前台查不到订单。",
			SceneIntent: &SceneIntent{
				Scenario:       "酒店入住时找不到海景房订单",
				UserRole:       "住客",
				AIRole:         "酒店前台",
				PracticeGoal:   "清楚说明预订并解决入住问题",
				ExperienceHint: "LIFE_AND_TRAVEL",
			},
		},
	)
	if !errors.Is(err, errPlanCaptured) ||
		plans.request.SceneID != "scn_travel_hotel_checkin" ||
		plans.customRequest.SceneSpec.Scenario != "" {
		t.Fatalf("catalog request = %#v, custom request = %#v, error = %v", plans.request, plans.customRequest, err)
	}
}

func validPreviewCallContext() capability.CallContext {
	return capability.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		ThreadID:       "30000000-0000-4000-8000-000000000001",
		RunID:          "40000000-0000-4000-8000-000000000001",
		InputMessageID: "50000000-0000-4000-8000-000000000001",
		ToolCallID:     "call-preview",
		RequestID:      "request-preview",
	}
}

func TestUnresolvedSceneQueryReturnsCanonicalClarificationWithoutPlan(
	t *testing.T,
) {
	plans := &capturingPlanApplication{}
	port, err := NewServicePort(plans, emptyPreviewCatalog{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}

	result, err := port.PreviewPractice(
		context.Background(),
		capability.CallContext{
			Actor: requestcontext.Actor{
				UserID:    "10000000-0000-4000-8000-000000000001",
				SessionID: "20000000-0000-4000-8000-000000000001",
			},
			ThreadID:       "30000000-0000-4000-8000-000000000001",
			RunID:          "40000000-0000-4000-8000-000000000001",
			InputMessageID: "50000000-0000-4000-8000-000000000001",
			ToolCallID:     "call-preview",
			RequestID:      "request-preview",
		},
		PreviewInput{SceneQuery: "完全不存在的练习主题"},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if result.Status != "needs_details" || result.AssistantText == "" ||
		len(result.Candidates) != 0 || plans.request.SceneID != "" {
		t.Fatalf("unresolved preview result = %#v, request = %#v", result, plans.request)
	}
}

func TestAmbiguousCatalogQueryCompletesWithoutPlanOrSecondToolCall(t *testing.T) {
	plans := &capturingPlanApplication{}
	port, err := NewServicePort(plans, ambiguousPreviewCatalog{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	preview, err := port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		PreviewInput{SceneQuery: "会议"},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	result := previewToolResult(preview)
	if preview.Status != "ambiguous" || len(preview.Candidates) != 2 ||
		preview.AssistantText == "" ||
		result.TurnOutcome != capability.TurnOutcomeCompleted ||
		plans.request.SceneID != "" || plans.customRequest.SceneSpec.Scenario != "" {
		t.Fatalf("ambiguous preview = %#v, tool result = %#v", preview, result)
	}
}

var errPlanCaptured = errors.New("plan request captured")

type capturingPlanApplication struct {
	request       preparation.CreatePlanRequest
	customRequest preparation.CreateCustomPlanRequest
}

func (application *capturingPlanApplication) PreviewPlan(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	request preparation.CreatePlanRequest,
) (preparation.PracticePlan, bool, error) {
	application.request = request
	return preparation.PracticePlan{}, false, errPlanCaptured
}

func (application *capturingPlanApplication) PreviewCustomPlan(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	request preparation.CreateCustomPlanRequest,
) (preparation.PracticePlan, bool, error) {
	application.customRequest = request
	return preparation.PracticePlan{}, false, errPlanCaptured
}

func TestUnmatchedCompleteIntentCreatesCustomPreview(t *testing.T) {
	plans := &capturingPlanApplication{}
	port, err := NewServicePort(plans, emptyPreviewCatalog{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}

	_, err = port.PreviewPractice(
		context.Background(),
		capability.CallContext{
			Actor: requestcontext.Actor{
				UserID:    "10000000-0000-4000-8000-000000000001",
				SessionID: "20000000-0000-4000-8000-000000000001",
			},
			ThreadID:  "30000000-0000-4000-8000-000000000001",
			RequestID: "request-preview",
		},
		PreviewInput{
			SceneQuery: "和宠物店沟通鹦鹉寄养",
			SceneIntent: &SceneIntent{
				Scenario:       "和宠物店沟通鹦鹉寄养",
				UserRole:       "宠物主人",
				AIRole:         "宠物店店员",
				PracticeGoal:   "确认寄养条件和注意事项",
				ExperienceHint: "LIFE_AND_TRAVEL",
			},
		},
	)
	if !errors.Is(err, errPlanCaptured) {
		t.Fatalf("PreviewPractice() error = %v, want captured request", err)
	}
	if plans.customRequest.SceneSpec.Scenario != "和宠物店沟通鹦鹉寄养" ||
		plans.customRequest.SceneSpec.ExperienceHint != scene.PracticeExperienceLifeAndTravel ||
		plans.customRequest.BackgroundSummary == "" {
		t.Fatalf("captured custom request = %#v", plans.customRequest)
	}
}

type singleWorkplaceCatalog struct{}

type emptyPreviewCatalog struct{}

type ambiguousPreviewCatalog struct{}

func (ambiguousPreviewCatalog) ResolvePreviewCatalog(
	context.Context,
	string,
) ([]scene.PreviewCatalogCandidate, error) {
	first, _ := (singleWorkplaceCatalog{}).ResolvePreviewCatalog(
		context.Background(),
		"",
	)
	second := first[0]
	second.Scene.ID = "scn_workplace_solution_presentation"
	second.Scene.Name = "方案介绍与问答"
	return []scene.PreviewCatalogCandidate{first[0], second}, nil
}

func (emptyPreviewCatalog) ResolvePreviewCatalog(
	context.Context,
	string,
) ([]scene.PreviewCatalogCandidate, error) {
	return nil, nil
}

func (singleWorkplaceCatalog) ResolvePreviewCatalog(
	context.Context,
	string,
) ([]scene.PreviewCatalogCandidate, error) {
	definition := scene.SceneDefinition{
		ID:         "scn_workplace_meeting_disagreement",
		Version:    1,
		Name:       "会议发言与表达异议",
		Experience: scene.PracticeExperienceWorkplace,
		Category:   scene.SceneCategoryWorkplaceGeneral,
		Roles: []scene.RoleDefinition{{
			ID:          "role-meeting-facilitator",
			DisplayName: "会议主持人",
		}},
		PracticeOptions: []scene.PracticeOption{{
			ID:          "option-full-simulation",
			DisplayName: "完整模拟",
			Mode:        scene.PracticeModeFullSimulation,
		}},
	}
	return []scene.PreviewCatalogCandidate{{
		Scene:          definition,
		DefaultRoleIDs: []string{"role-meeting-facilitator"},
		DefaultOption:  definition.PracticeOptions[0],
	}}, nil
}
