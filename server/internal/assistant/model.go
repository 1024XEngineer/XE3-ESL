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

// AssistantThread is the long-lived conversation between a user and SpeakUp.
// PracticeSession remains owned by the practice module.
type AssistantThread struct {
	ID             string
	UserID         string
	Status         ThreadStatus
	ContextSummary string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TaskRun tracks one user goal being orchestrated inside an AssistantThread.
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

// ToolCall records one structured invocation of a registered business tool.
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
	Status    string
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
