package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestDeferredAgentVoiceRunReturnsPendingBeforeModelCompletes(t *testing.T) {
	lifecycle, stop := context.WithCancel(context.Background())
	defer stop()
	delegate := &blockingAgentVoiceRunProcessor{
		started:   make(chan struct{}),
		release:   make(chan struct{}),
		completed: make(chan struct{}),
	}
	processor, err := newDeferredAgentVoiceRunProcessor(
		lifecycle,
		delegate,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("newDeferredAgentVoiceRunProcessor() error = %v", err)
	}
	run := agentrun.Run{
		ID:       "30000000-0000-4000-8000-000000000001",
		Status:   agentrun.StatusPending,
		OwnerID:  "10000000-0000-4000-8000-000000000001",
		ThreadID: "20000000-0000-4000-8000-000000000001",
	}
	requestContext, cancelRequest := context.WithCancel(context.Background())
	returned, err := processor.ProcessPending(
		requestContext,
		requestcontext.Actor{
			UserID:    run.OwnerID,
			SessionID: "40000000-0000-4000-8000-000000000001",
		},
		run,
	)
	if err != nil {
		t.Fatalf("ProcessPending() error = %v", err)
	}
	if returned.Status != agentrun.StatusPending {
		t.Fatalf("ProcessPending() returned %#v, want pending", returned)
	}
	cancelRequest()

	select {
	case <-delegate.started:
	case <-time.After(time.Second):
		t.Fatal("deferred Run did not start")
	}
	close(delegate.release)
	select {
	case <-delegate.completed:
	case <-time.After(time.Second):
		t.Fatal("deferred Run inherited the completed HTTP request context")
	}
}

type blockingAgentVoiceRunProcessor struct {
	started   chan struct{}
	release   chan struct{}
	completed chan struct{}
}

func (processor *blockingAgentVoiceRunProcessor) ProcessPending(
	ctx context.Context,
	_ requestcontext.Actor,
	run agentrun.Run,
) (agentrun.Run, error) {
	close(processor.started)
	select {
	case <-ctx.Done():
		return agentrun.Run{}, ctx.Err()
	case <-processor.release:
	}
	run.Status = agentrun.StatusCompleted
	close(processor.completed)
	return run, nil
}
