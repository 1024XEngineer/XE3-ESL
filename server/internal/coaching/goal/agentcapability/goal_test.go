package agentcapability

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type fakeGoalPort struct {
	createInput GoalCreateInput
	searchInput GoalSearchInput
}

func (port *fakeGoalPort) CreateGoal(
	ctx context.Context,
	call CallContext,
	input GoalCreateInput,
) (GoalResult, error) {
	port.createInput = input
	return GoalResult{
		GoalID:  "goal-1",
		Title:   input.Title,
		Status:  "active",
		Version: 1,
		SourceRefs: []SourceRef{{
			Type: "goal",
			ID:   "goal-1",
		}},
	}, nil
}

func (port *fakeGoalPort) SearchGoals(
	ctx context.Context,
	call CallContext,
	input GoalSearchInput,
) ([]GoalResult, error) {
	port.searchInput = input
	return []GoalResult{{
		GoalID:  "goal-1",
		Title:   "PM interview",
		Status:  "active",
		Version: 1,
	}}, nil
}

func TestGoalCreateCapabilityMapsInput(t *testing.T) {
	port := &fakeGoalPort{}
	result, err := NewGoalCreateCapability(port).Execute(
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
	if result.Content["goal"] == nil {
		t.Fatalf("result.Content = %#v, want goal", result.Content)
	}
}

func TestGoalSearchCapabilityMapsInput(t *testing.T) {
	port := &fakeGoalPort{}
	result, err := NewGoalSearchCapability(port).Execute(
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
	if result.Content["goals"] == nil {
		t.Fatalf("result.Content = %#v, want goals", result.Content)
	}
}

func TestGoalCreateDefinitionOnlyAcceptsGoalTitle(t *testing.T) {
	definition := NewGoalCreateCapability(&fakeGoalPort{}).Definition()
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
