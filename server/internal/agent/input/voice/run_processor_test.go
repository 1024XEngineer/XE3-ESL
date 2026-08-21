package voice

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestDeferredRunProcessorReturnsPendingBeforeModelCompletes(t *testing.T) {
	lifecycle, stop := context.WithCancel(context.Background())
	defer stop()
	delegate := &blockingRunProcessor{
		started:   make(chan struct{}),
		release:   make(chan struct{}),
		completed: make(chan struct{}),
	}
	processor, err := NewDeferredRunProcessor(
		lifecycle,
		delegate,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewDeferredRunProcessor() error = %v", err)
	}
	pendingRun := run.Run{
		ID:       "30000000-0000-4000-8000-000000000001",
		Status:   run.StatusPending,
		OwnerID:  "10000000-0000-4000-8000-000000000001",
		ThreadID: "20000000-0000-4000-8000-000000000001",
	}
	requestContext, cancelRequest := context.WithCancel(context.Background())
	returned, err := processor.ProcessPending(
		requestContext,
		requestcontext.Actor{
			UserID:    pendingRun.OwnerID,
			SessionID: "40000000-0000-4000-8000-000000000001",
		},
		pendingRun,
	)
	if err != nil {
		t.Fatalf("ProcessPending() error = %v", err)
	}
	if returned.Status != run.StatusPending {
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

type blockingRunProcessor struct {
	started   chan struct{}
	release   chan struct{}
	completed chan struct{}
}

func (processor *blockingRunProcessor) ProcessPending(
	ctx context.Context,
	_ requestcontext.Actor,
	pendingRun run.Run,
) (run.Run, error) {
	close(processor.started)
	select {
	case <-ctx.Done():
		return run.Run{}, ctx.Err()
	case <-processor.release:
	}
	pendingRun.Status = run.StatusCompleted
	close(processor.completed)
	return pendingRun, nil
}

func (processor *blockingRunProcessor) ProcessPendingStream(
	ctx context.Context,
	actor requestcontext.Actor,
	pendingRun run.Run,
	_ run.StreamObserver,
) (run.Run, error) {
	return processor.ProcessPending(ctx, actor, pendingRun)
}
