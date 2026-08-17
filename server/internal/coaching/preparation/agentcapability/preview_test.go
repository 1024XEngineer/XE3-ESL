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

func TestPreviewNeedsInputKeepsCurrentTurnOpen(t *testing.T) {
	result := previewToolResult(PreviewResult{
		Status: "needs_input",
	})
	if result.TurnOutcome != capability.TurnOutcomeContinue {
		t.Fatalf("TurnOutcome = %v, want continue", result.TurnOutcome)
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
	if result.Status != "needs_input" || result.AssistantText == "" ||
		len(result.Candidates) != 0 || plans.request.SceneID != "" {
		t.Fatalf("unresolved preview result = %#v, request = %#v", result, plans.request)
	}
}

var errPlanCaptured = errors.New("plan request captured")

type capturingPlanApplication struct {
	request preparation.CreatePlanRequest
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

type singleWorkplaceCatalog struct{}

type emptyPreviewCatalog struct{}

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
