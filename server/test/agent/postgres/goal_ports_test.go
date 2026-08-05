package postgres_test

import (
	"testing"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	goalagentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentcontext"
	goalagentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentconversation"
)

func agentConversationGoals(
	t *testing.T,
	source goal.Reader,
) agentconversation.GoalReader {
	t.Helper()
	reader, err := goalagentconversation.New(source)
	if err != nil {
		t.Fatalf("new Agent Conversation Goal reader: %v", err)
	}
	return reader
}

func agentContextGoals(
	t *testing.T,
	source goal.Reader,
) agentcontext.GoalReader {
	t.Helper()
	reader, err := goalagentcontext.New(source)
	if err != nil {
		t.Fatalf("new Agent Context Goal reader: %v", err)
	}
	return reader
}
