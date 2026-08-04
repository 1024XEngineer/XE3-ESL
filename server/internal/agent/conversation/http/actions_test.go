package conversationhttp

import (
	"encoding/json"
	"testing"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

func TestInterviewPreparationActionsRequireSucceededRealGoal(t *testing.T) {
	t.Parallel()
	goalID := "10000000-0000-4000-8000-000000000001"
	result, err := json.Marshal(map[string]any{
		"content": map[string]any{
			"goal": map[string]any{
				"goal_id": goalID,
				"title":   "Java Interview Practice",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	actions := interviewPreparationActions([]agentrun.ToolCall{{
		Name:   goalCreateCapabilityName,
		Status: agentrun.ToolCallSucceeded,
		Result: result,
		SourceRefs: []agentrun.ToolSourceRef{{
			Type: "goal",
			ID:   goalID,
		}},
	}})
	if len(actions) != 1 ||
		actions[0]["goal_id"] != goalID ||
		actions[0]["type"] != "open_interview_preparation" {
		t.Fatalf("actions = %#v", actions)
	}

	mockResult := []byte(`{"content":{"goal":{"goal_id":"mock-created","title":"Mock"}}}`)
	if got := interviewPreparationActions([]agentrun.ToolCall{{
		Name:   goalCreateCapabilityName,
		Status: agentrun.ToolCallSucceeded,
		Result: mockResult,
		SourceRefs: []agentrun.ToolSourceRef{{
			Type: "mock_goal",
			ID:   "mock-created",
		}},
	}}); len(got) != 0 {
		t.Fatalf("mock actions = %#v, want none", got)
	}
}
