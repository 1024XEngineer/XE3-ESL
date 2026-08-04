package conversationhttp

import (
	"errors"
	"testing"

	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

func TestMessageHandoffsUseOnlySucceededTrustedRecords(t *testing.T) {
	t.Parallel()
	item, err := agenthandoff.NewConfirmPracticePlan(agenthandoff.Item{
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
		t.Fatalf("NewConfirmPracticePlan() error = %v", err)
	}

	got, err := messageHandoffs([]agentrun.ToolCall{
		{Status: agentrun.ToolCallSucceeded, Handoffs: []agenthandoff.Item{item}},
		{Status: agentrun.ToolCallFailed, Handoffs: []agenthandoff.Item{item}},
	})
	if err != nil {
		t.Fatalf("messageHandoffs() error = %v", err)
	}
	if len(got) != 1 || got[0].PracticePlanID != item.PracticePlanID {
		t.Fatalf("handoffs = %#v", got)
	}
}

func TestMessageHandoffsRejectInvalidPersistedProjection(t *testing.T) {
	_, err := messageHandoffs([]agentrun.ToolCall{{
		Status:   agentrun.ToolCallSucceeded,
		Handoffs: []agenthandoff.Item{{Type: agenthandoff.ConfirmPracticePlanType}},
	}})
	if !errors.Is(err, agentrun.ErrRepository) {
		t.Fatalf("messageHandoffs() error = %v, want ErrRepository", err)
	}
}
