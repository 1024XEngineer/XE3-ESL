package agenttool

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
	ctx context.Context,
	call tool.CallContext,
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
		json.RawMessage(`{"scenario_query":"IELTS"}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if port.input.ScenarioQuery != "IELTS" ||
		result.Content["status"] != "preview_ready" ||
		result.Content["practice_plan_id"] != "plan-1" ||
		len(result.SourceRefs) != 1 {
		t.Fatalf("tool result = %#v, input = %#v", result, port.input)
	}
}

func TestPreviewToolSchemaAllowsNeedsInputRequest(t *testing.T) {
	definition := NewPreviewTool(&previewPortStub{}).Definition()
	if required := definition.InputSchema["required"].([]string); len(required) != 0 {
		t.Fatalf("required = %#v, want optional discovery fields", required)
	}
	if definition.ReadOnly || definition.Risk != tool.RiskLowRiskWrite {
		t.Fatalf("definition = %#v", definition)
	}
	properties := definition.InputSchema["properties"].(map[string]any)
	if properties["background_summary"] == nil ||
		properties["preparation_profile_id"] != nil ||
		properties["preparation_snapshot_id"] != nil {
		t.Fatalf("properties expose internal Preparation identifiers: %#v", properties)
	}
}
