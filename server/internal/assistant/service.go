package assistant

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("assistant: not implemented")

// AssistantService is the application entry point used by HTTP or WebSocket
// delivery adapters. Concrete orchestration is added after the tool contracts
// of the four business modules are available.
type AssistantService interface {
	StartTask(context.Context, StartTaskCommand) (TaskRun, error)
	ResumeTask(context.Context, ResumeTaskCommand) (TaskRun, error)
	GetThread(context.Context, GetThreadQuery) (AssistantThread, error)
}

type Dependencies struct {
	Planner           Planner
	Tools             ToolRegistry
	ConversationStore ConversationStore
}

type Service struct {
	dependencies Dependencies
}

func NewService(dependencies Dependencies) *Service {
	return &Service{dependencies: dependencies}
}

type StartTaskCommand struct {
	ActorUserID    string
	ThreadID       string
	UserMessage    string
	IdempotencyKey string
}

type ResumeTaskCommand struct {
	ActorUserID string
	TaskRunID   string
}

type GetThreadQuery struct {
	ActorUserID string
	ThreadID    string
}

func (*Service) StartTask(context.Context, StartTaskCommand) (TaskRun, error) {
	return TaskRun{}, ErrNotImplemented
}

func (*Service) ResumeTask(context.Context, ResumeTaskCommand) (TaskRun, error) {
	return TaskRun{}, ErrNotImplemented
}

func (*Service) GetThread(context.Context, GetThreadQuery) (AssistantThread, error) {
	return AssistantThread{}, ErrNotImplemented
}
