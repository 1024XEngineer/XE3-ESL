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
) (ScenarioResult, error) {
	port.createInput = input
	return ScenarioResult{
		ID:      "scenario-1",
		Title:   input.Title,
		Type:    input.Type,
		Status:  "active",
		Summary: input.Goal,
		SourceRef: []SourceRef{{
			Type: "scenario",
			ID:   "scenario-1",
		}},
	}, nil
}

func (port *fakeScenarioPort) SearchScenarios(
	ctx context.Context,
	call CallContext,
	input ScenarioSearchInput,
) ([]ScenarioResult, error) {
	port.searchInput = input
	return []ScenarioResult{{
		ID:     "scenario-1",
		Title:  "PM interview",
		Type:   "interview",
		Status: "active",
	}}, nil
}

func TestScenarioCreateToolMapsInput(t *testing.T) {
	port := &fakeScenarioPort{}
	result, err := NewScenarioCreateTool(port).Execute(
		context.Background(),
		validCallContext(),
		json.RawMessage(`{"type":"interview","title":"PM interview","goal":"prepare self introduction"}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := port.createInput.Type, "interview"; got != want {
		t.Fatalf("createInput.Type = %q, want %q", got, want)
	}
	if result.Content["scenario"] == nil {
		t.Fatalf("result.Content = %#v, want scenario", result.Content)
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
	if result.Content["scenarios"] == nil {
		t.Fatalf("result.Content = %#v, want scenarios", result.Content)
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
