package run

import (
	"encoding/json"
	"time"

	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
)

type ToolCallStatus string

const (
	ToolCallProposed  ToolCallStatus = "proposed"
	ToolCallRunning   ToolCallStatus = "running"
	ToolCallSucceeded ToolCallStatus = "succeeded"
	ToolCallFailed    ToolCallStatus = "failed"
	ToolCallRejected  ToolCallStatus = "rejected"
)

func (status ToolCallStatus) Valid() bool {
	switch status {
	case ToolCallProposed, ToolCallRunning, ToolCallSucceeded, ToolCallFailed, ToolCallRejected:
		return true
	default:
		return false
	}
}

type ToolCall struct {
	ID            string
	RunID         string
	OwnerID       string
	ThreadID      string
	Name          string
	SchemaVersion string
	Input         json.RawMessage
	Status        ToolCallStatus
	Result        json.RawMessage
	ErrorCategory string
	RequestID     string
	SourceRefs    []ToolSourceRef
	Handoffs      []agenthandoff.Item
	ProposedAt    time.Time
	StartedAt     time.Time
	CompletedAt   time.Time
	UpdatedAt     time.Time
}

func (call ToolCall) ValidIdentity() bool {
	return ValidModelID(call.ID) &&
		ValidUUID(call.RunID) &&
		ValidUUID(call.OwnerID) &&
		ValidUUID(call.ThreadID)
}

type ToolSourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
