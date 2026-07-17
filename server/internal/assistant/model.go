package assistant

import "time"

type ThreadStatus string

const (
	ThreadStatusActive ThreadStatus = "active"
	ThreadStatusClosed ThreadStatus = "closed"
)

type TaskRunStatus string

const (
	TaskRunStatusPending         TaskRunStatus = "pending"
	TaskRunStatusRunning         TaskRunStatus = "running"
	TaskRunStatusAwaitingConfirm TaskRunStatus = "awaiting_confirmation"
	TaskRunStatusCompleted       TaskRunStatus = "completed"
	TaskRunStatusFailed          TaskRunStatus = "failed"
)

type ConfirmationStatus string

const (
	ConfirmationStatusPending  ConfirmationStatus = "pending"
	ConfirmationStatusApproved ConfirmationStatus = "approved"
	ConfirmationStatusRejected ConfirmationStatus = "rejected"
	ConfirmationStatusExpired  ConfirmationStatus = "expired"
)

// AssistantThread 表示用户与 SpeakUp 助手之间的长期对话，PracticeSession 仍由 practice 模块拥有。
type AssistantThread struct {
	ID             string
	UserID         string
	Status         ThreadStatus
	ContextSummary string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TaskRun 记录 AssistantThread 中一次用户目标的编排过程。
type TaskRun struct {
	ID          string
	ThreadID    string
	Intent      string
	Status      TaskRunStatus
	CurrentStep string
	Result      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ToolCall 记录一次已注册业务工具的结构化调用。
type ToolCall struct {
	ID             string
	TaskRunID      string
	ToolName       string
	Arguments      map[string]any
	Result         map[string]any
	IdempotencyKey string
	CreatedAt      time.Time
}

type ConfirmationRequest struct {
	ID        string
	TaskRunID string
	Action    string
	RiskLevel string
	Summary   string
	Status    ConfirmationStatus
	ExpiresAt time.Time
}

type Plan struct {
	Intent string
	Steps  []PlanStep
}

type PlanStep struct {
	ToolName  string
	Arguments map[string]any
}

type ToolResult struct {
	Output map[string]any
}
