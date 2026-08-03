package run

import "context"

type StreamObserver interface {
	OnInputCommitted(context.Context, Submission) error
	OnAssistantStarted(context.Context, Run) error
	OnAssistantDelta(context.Context, string) error
}
