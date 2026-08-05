package voice

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	deferredRunQueueCapacity = 32
	deferredRunTimeout       = 75 * time.Second
)

type deferredRunRequest struct {
	actor requestcontext.Actor
	run   run.Run
}

// deferredRunProcessor separates the durable voice confirmation write from
// model execution. The confirmation response can return the committed user
// Message immediately while the persisted pending Run is processed under the
// server lifecycle rather than the HTTP request context.
type deferredRunProcessor struct {
	ctx      context.Context
	delegate PendingRunProcessor
	logger   *slog.Logger
	queue    chan deferredRunRequest
}

// NewDeferredRunProcessor owns the production queue, capacity, and execution
// timeout used after a voice candidate has been confirmed.
func NewDeferredRunProcessor(
	ctx context.Context,
	delegate PendingRunProcessor,
	logger *slog.Logger,
) (PendingRunProcessor, error) {
	if ctx == nil || nilDependency(delegate) || logger == nil {
		return nil, errors.New(
			"agent voice input: deferred Run dependencies are required",
		)
	}
	processor := &deferredRunProcessor{
		ctx:      ctx,
		delegate: delegate,
		logger:   logger,
		queue:    make(chan deferredRunRequest, deferredRunQueueCapacity),
	}
	go processor.run()
	return processor, nil
}

func (processor *deferredRunProcessor) ProcessPending(
	requestContext context.Context,
	actor requestcontext.Actor,
	pendingRun run.Run,
) (run.Run, error) {
	if requestContext == nil {
		return run.Run{}, errors.New(
			"agent voice input: Run request context is required",
		)
	}
	request := deferredRunRequest{actor: actor, run: pendingRun}
	select {
	case processor.queue <- request:
		return pendingRun, nil
	case <-requestContext.Done():
		return run.Run{}, requestContext.Err()
	case <-processor.ctx.Done():
		return run.Run{}, processor.ctx.Err()
	}
}

func (processor *deferredRunProcessor) run() {
	for {
		select {
		case <-processor.ctx.Done():
			return
		case request := <-processor.queue:
			processor.process(request)
		}
	}
}

func (processor *deferredRunProcessor) process(request deferredRunRequest) {
	ctx, cancel := context.WithTimeout(processor.ctx, deferredRunTimeout)
	defer cancel()
	if _, err := processor.delegate.ProcessPending(
		ctx,
		request.actor,
		request.run,
	); err != nil {
		processor.logger.Error(
			"deferred Agent voice Run failed",
			slog.String("run_id", request.run.ID),
			slog.String("thread_id", request.run.ThreadID),
			slog.Any("error", err),
		)
	}
}

var _ PendingRunProcessor = (*deferredRunProcessor)(nil)
