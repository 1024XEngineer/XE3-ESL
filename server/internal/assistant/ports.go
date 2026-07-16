package assistant

import "context"

// Planner turns a user goal into structured, reviewable tool steps.
type Planner interface {
	Plan(context.Context, PlanRequest) (Plan, error)
}

type PlanRequest struct {
	ThreadID       string
	UserMessage    string
	ContextSummary string
}

// ToolRegistry exposes only explicitly registered business capabilities.
// Implementations adapt the public services of preparation, practice,
// conversation, and review; they must not expose repositories.
type ToolRegistry interface {
	Execute(context.Context, ToolInvocation) (ToolResult, error)
}

type ToolInvocation struct {
	// ActorUserID comes from the authenticated service command. Tool adapters
	// must use it for authorization instead of trusting model-generated arguments.
	ActorUserID    string
	TaskRunID      string
	ToolName       string
	Arguments      map[string]any
	IdempotencyKey string
}

// ConversationStore persists assistant-owned orchestration state only.
type ConversationStore interface {
	GetThread(context.Context, string) (AssistantThread, error)
	SaveThread(context.Context, AssistantThread) error
	SaveTaskRun(context.Context, TaskRun) error
	SaveToolCall(context.Context, ToolCall) error
	// GetPendingConfirmationRequest restores an unfinished confirmation when a
	// TaskRun resumes after a process restart.
	GetPendingConfirmationRequest(
		ctx context.Context,
		taskRunID string,
	) (ConfirmationRequest, error)
	// SaveConfirmationRequest persists both newly created requests and later
	// status transitions such as approval, rejection, or expiration.
	SaveConfirmationRequest(context.Context, ConfirmationRequest) error
}
