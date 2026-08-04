package run

import (
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

func (status Status) Valid() bool {
	switch status {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

const (
	FailureInterrupted                  = "interrupted"
	FailureConfigurationDrift           = "configuration_drift"
	FailureInvalidContext               = "invalid_context"
	FailureMemoryConsistencyUnavailable = "memory_consistency_unavailable"
	FailureInternal                     = "internal_error"
)

type Run struct {
	ID                   string
	OwnerID              string
	ThreadID             string
	InputMessageID       string
	Attempt              int
	RetryOfRunID         string
	RetryClientID        string
	Status               Status
	RequestedProvider    string
	RequestedModel       string
	MaxOutputTokens      int
	MaxInputCharacters   int
	WorkerLeaseToken     string
	WorkerLeaseExpiresAt time.Time
	AssistantMessageID   string
	ProviderCompletionID string
	ProviderModel        string
	FinishReason         string
	Usage                ai.TokenUsage
	FailureKind          string
	FailureRetryable     bool
	CreatedAt            time.Time
	StartedAt            time.Time
	CompletedAt          time.Time
	UpdatedAt            time.Time
}

type Submission struct {
	Run         Run
	UserMessage conversation.Message
	Created     bool
}

type Retry struct {
	Run     Run
	Created bool
}
