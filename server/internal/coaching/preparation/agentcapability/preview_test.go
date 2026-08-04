package agentcapability

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

type previewPortStub struct {
	input PreviewInput
}

func (stub *previewPortStub) PreviewPractice(
	_ context.Context,
	_ tool.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	stub.input = input
	return PreviewResult{
		Status:             "preview_ready",
		PracticePlanID:     "plan-1",
		PlanRevision:       1,
		PracticePlanStatus: "ready",
		SourceRefs: []tool.SourceRef{{
			Type: "practice_plan",
			ID:   "plan-1",
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
		result.Content["practice_plan_id"] != "plan-1" ||
		len(result.SourceRefs) != 1 {
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
	registry, err := tool.NewRegistry(NewPreviewTool(&previewPortStub{}))
	if err != nil {
		t.Fatalf("tool.NewRegistry() error = %v", err)
	}
	readOnly := registry.InvocationEffect(tool.Invocation{
		Name:  PracticePreviewToolName,
		Input: json.RawMessage(`{"scene_query":"AI product manager interview"}`),
	})
	if readOnly != tool.InvocationEffectReadOnly {
		t.Fatalf("discovery effect = %v", readOnly)
	}
	mayWrite := registry.InvocationEffect(tool.Invocation{
		Name: PracticePreviewToolName,
		Input: json.RawMessage(
			`{"background_summary":"AI product manager","max_effective_turns":3}`,
		),
	})
	if mayWrite != tool.InvocationEffectMayWrite {
		t.Fatalf("creation effect = %v", mayWrite)
	}
}
