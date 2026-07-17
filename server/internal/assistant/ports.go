package assistant

import "context"

// Planner 将用户目标拆解为结构化、可审核的工具步骤。
type Planner interface {
	Plan(context.Context, PlanRequest) (Plan, error)
}

type PlanRequest struct {
	ThreadID       string
	UserMessage    string
	ContextSummary string
}

// ToolRegistry 只暴露显式注册的业务能力，不允许直接暴露 Repository。
type ToolRegistry interface {
	Execute(context.Context, ToolInvocation) (ToolResult, error)
}

type ToolInvocation struct {
	// ActorUserID 来自已认证的服务命令，工具适配器必须使用它鉴权。
	ActorUserID    string
	TaskRunID      string
	ToolName       string
	Arguments      map[string]any
	IdempotencyKey string
}

// ConversationStore 只持久化 assistant 模块拥有的编排状态。
type ConversationStore interface {
	GetThread(context.Context, string) (AssistantThread, error)
	SaveThread(context.Context, AssistantThread) error
	SaveTaskRun(context.Context, TaskRun) error
	SaveToolCall(context.Context, ToolCall) error
	// GetPendingConfirmationRequest 用于在进程重启后恢复未完成的确认请求。
	GetPendingConfirmationRequest(
		ctx context.Context,
		taskRunID string,
	) (ConfirmationRequest, error)
	// SaveConfirmationRequest 保存确认请求及其批准、拒绝或过期等状态变化。
	SaveConfirmationRequest(context.Context, ConfirmationRequest) error
}
