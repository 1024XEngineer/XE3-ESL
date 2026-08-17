package run

import "context"

type ToolStep struct {
	ID    string
	RunID string
	Name  string
}

type AssistantOutput struct {
	ID      string
	RunID   string
	Content string
}

type AssistantOutputDelta struct {
	OutputID string
	RunID    string
	Sequence int
	Delta    string
}

type StreamObserver interface {
	OnInputCommitted(context.Context, Submission) error
	OnToolStarted(context.Context, ToolStep) error
	OnToolCompleted(context.Context, ToolStep) error
	OnToolFailed(context.Context, ToolStep) error
	OnAssistantOutputStarted(context.Context, AssistantOutput) error
	OnAssistantOutputDelta(context.Context, AssistantOutputDelta) error
	OnAssistantOutputCompleted(context.Context, AssistantOutput) error
}
