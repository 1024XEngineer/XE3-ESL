package transport

import (
	"encoding/json"
	"testing"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

func TestInterviewPreparationActionsRequireSucceededRealMatter(t *testing.T) {
	t.Parallel()
	matterID := "10000000-0000-4000-8000-000000000001"
	result, err := json.Marshal(map[string]any{
		"content": map[string]any{
			"matter": map[string]any{
				"matter_id": matterID,
				"title":     "Java Interview Practice",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	actions := interviewPreparationActions([]agentrun.ToolCall{{
		Name:   scenarioCreateToolName,
		Status: agentrun.ToolCallSucceeded,
		Result: result,
		SourceRefs: []agentrun.ToolSourceRef{{
			Type: "matter",
			ID:   matterID,
		}},
	}})
	if len(actions) != 1 ||
		actions[0]["matter_id"] != matterID ||
		actions[0]["type"] != "open_interview_preparation" {
		t.Fatalf("actions = %#v", actions)
	}

	mockResult := []byte(`{"content":{"matter":{"matter_id":"mock-created","title":"Mock"}}}`)
	if got := interviewPreparationActions([]agentrun.ToolCall{{
		Name:   scenarioCreateToolName,
		Status: agentrun.ToolCallSucceeded,
		Result: mockResult,
		SourceRefs: []agentrun.ToolSourceRef{{
			Type: "mock_matter",
			ID:   "mock-created",
		}},
	}}); len(got) != 0 {
		t.Fatalf("mock actions = %#v, want none", got)
	}
}
