package agenttool

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type fakeScenarioPort struct {
	createInput ScenarioCreateInput
	searchInput ScenarioSearchInput
}

func (port *fakeScenarioPort) CreateScenario(
	ctx context.Context,
	call CallContext,
	input ScenarioCreateInput,
) (MatterResult, error) {
	port.createInput = input
	return MatterResult{
		MatterID: "matter-1",
		Title:    input.Title,
		Status:   "active",
		Version:  1,
		SourceRefs: []SourceRef{{
			Type: "matter",
			ID:   "matter-1",
		}},
	}, nil
}

func (port *fakeScenarioPort) SearchScenarios(
	ctx context.Context,
	call CallContext,
	input ScenarioSearchInput,
) ([]MatterResult, error) {
	port.searchInput = input
	return []MatterResult{{
		MatterID: "matter-1",
		Title:    "PM interview",
		Status:   "active",
		Version:  1,
	}}, nil
}

func TestScenarioCreateToolMapsInput(t *testing.T) {
	port := &fakeScenarioPort{}
	result, err := NewScenarioCreateTool(port).Execute(
		context.Background(),
		validCallContext(),
		json.RawMessage(`{"title":"PM interview"}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := port.createInput.Title, "PM interview"; got != want {
		t.Fatalf("createInput.Title = %q, want %q", got, want)
	}
	if result.Content["matter"] == nil {
		t.Fatalf("result.Content = %#v, want matter", result.Content)
	}
}

func TestScenarioSearchToolMapsInput(t *testing.T) {
	port := &fakeScenarioPort{}
	result, err := NewScenarioSearchTool(port).Execute(
		context.Background(),
		validCallContext(),
		json.RawMessage(`{"query":"上次面试","limit":3}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := port.searchInput.Query, "上次面试"; got != want {
		t.Fatalf("searchInput.Query = %q, want %q", got, want)
	}
	if result.Content["matters"] == nil {
		t.Fatalf("result.Content = %#v, want matters", result.Content)
	}
}

func TestScenarioCreateDefinitionOnlyAcceptsMatterTitle(t *testing.T) {
	definition := NewScenarioCreateTool(&fakeScenarioPort{}).Definition()
	properties := definition.InputSchema["properties"].(map[string]any)
	if len(properties) != 1 || properties["title"] == nil {
		t.Fatalf("properties = %#v, want only title", properties)
	}
	required := definition.InputSchema["required"].([]string)
	if len(required) != 1 || required[0] != "title" {
		t.Fatalf("required = %#v, want title", required)
	}
}

func validCallContext() CallContext {
	return CallContext{
		Actor: requestcontext.Actor{
			UserID:    "user-1",
			SessionID: "session-1",
		},
		ThreadID:   "thread-1",
		RunID:      "run-1",
		ToolCallID: "tool-call-1",
		RequestID:  "request-1",
	}
}
