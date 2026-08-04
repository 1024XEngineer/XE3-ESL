package agentcapability

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
)

type previewPortStub struct {
	input PreviewInput
}

func (stub *previewPortStub) PreviewPractice(
	_ context.Context,
	_ capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	stub.input = input
	handoff, err := agenthandoff.NewConfirmPracticePlan(agenthandoff.Item{
		Label:                    "确认并开始练习",
		PracticePlanID:           "10000000-0000-4000-8000-000000000001",
		PlanRevision:             1,
		Target:                   "练习项目影响力表达",
		SceneName:                "项目经历深挖",
		SceneFamily:              "INTERVIEW",
		SceneModel:               "PROJECT_EXPERIENCE_DEEP_DIVE",
		Roles:                    []string{"面试官"},
		PracticeScope:            "完整模拟",
		SuggestedDurationSeconds: 600,
		MinEffectiveTurns:        3,
		MaxEffectiveTurns:        6,
		ExecutableStatus:         agenthandoff.PracticePlanReadyStatus,
		ConfirmationPrompt:       "确认后将创建练习会话；确认前不会开始练习。",
	})
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{
		Status:  "preview_ready",
		Handoff: handoff,
		SourceRefs: []capability.SourceRef{{
			Type: "practice_plan",
			ID:   handoff.PracticePlanID,
		}},
	}, nil
}

func TestPreviewToolMapsReadyResult(t *testing.T) {
	port := &previewPortStub{}
	result, err := NewPreviewTool(port).Execute(
		context.Background(),
		previewCallContext(),
		json.RawMessage(`{"scene_query":"IELTS"}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if port.input.SceneQuery != "IELTS" ||
		result.Content["status"] != "preview_ready" ||
		result.Content["practice_plan_id"] != nil ||
		len(result.SourceRefs) != 1 || len(result.Handoffs) != 1 ||
		result.Handoffs[0].Type != agenthandoff.ConfirmPracticePlanType {
		t.Fatalf("tool result = %#v, input = %#v", result, port.input)
	}
}

func TestPreviewToolDoesNotExposePreparationIdentifiers(t *testing.T) {
	definition := NewPreviewTool(&previewPortStub{}).Definition()
	if required := definition.InputSchema["required"].([]string); len(required) != 0 {
		t.Fatalf("required = %#v, want optional discovery fields", required)
	}
	properties := definition.InputSchema["properties"].(map[string]any)
	if properties["background_summary"] == nil ||
		properties["preparation_profile_id"] != nil ||
		properties["preparation_snapshot_id"] != nil {
		t.Fatalf("properties expose internal Preparation identifiers: %#v", properties)
	}
}

func TestPreviewToolClassifiesDiscoveryAndCreation(t *testing.T) {
	registry, err := capability.NewRegistry(NewPreviewTool(&previewPortStub{}))
	if err != nil {
		t.Fatalf("capability.NewRegistry() error = %v", err)
	}
	readOnly := registry.InvocationEffect(capability.Invocation{
		Name:  PracticePreviewToolName,
		Input: json.RawMessage(`{"scene_query":"AI product manager interview"}`),
	})
	if readOnly != capability.InvocationEffectReadOnly {
		t.Fatalf("discovery effect = %v", readOnly)
	}
	mayWrite := registry.InvocationEffect(capability.Invocation{
		Name: PracticePreviewToolName,
		Input: json.RawMessage(
			`{"background_summary":"AI product manager","max_effective_turns":3}`,
		),
	})
	if mayWrite != capability.InvocationEffectMayWrite {
		t.Fatalf("creation effect = %v", mayWrite)
	}
}
