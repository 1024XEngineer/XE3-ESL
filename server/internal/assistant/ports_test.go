package assistant_test

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/assistant"
)

type conversationStoreStub struct{}

func (*conversationStoreStub) GetThread(context.Context, string) (assistant.AssistantThread, error) {
	return assistant.AssistantThread{}, nil
}

func (*conversationStoreStub) SaveThread(context.Context, assistant.AssistantThread) error {
	return nil
}

func (*conversationStoreStub) SaveTaskRun(context.Context, assistant.TaskRun) error {
	return nil
}

func (*conversationStoreStub) SaveToolCall(context.Context, assistant.ToolCall) error {
	return nil
}

func (*conversationStoreStub) GetPendingConfirmationRequest(
	context.Context,
	string,
) (assistant.ConfirmationRequest, error) {
	return assistant.ConfirmationRequest{}, nil
}

func (*conversationStoreStub) SaveConfirmationRequest(
	context.Context,
	assistant.ConfirmationRequest,
) error {
	return nil
}

var _ assistant.ConversationStore = (*conversationStoreStub)(nil)

func TestToolInvocationKeepsAuthenticatedActorSeparateFromArguments(t *testing.T) {
	invocation := assistant.ToolInvocation{
		ActorUserID: "user-1",
		Arguments: map[string]any{
			"historyLimit": 10,
		},
	}

	if invocation.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor user ID: %q", invocation.ActorUserID)
	}
	if _, exists := invocation.Arguments["userId"]; exists {
		t.Fatal("actor identity must not be supplied through model-generated arguments")
	}
}
