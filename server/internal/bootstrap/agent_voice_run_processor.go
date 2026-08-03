package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	agentVoiceRunQueueCapacity = 32
	agentVoiceRunTimeout       = 75 * time.Second
)

type deferredAgentVoiceRun struct {
	actor requestcontext.Actor
	run   agentrun.Run
}

// deferredAgentVoiceRunProcessor separates the durable voice confirmation
// write from model execution. The confirmation response can therefore return
// the committed user Message immediately while the persisted pending Run is
// processed under the server lifecycle rather than the HTTP request context.
type deferredAgentVoiceRunProcessor struct {
	ctx      context.Context
	delegate agentvoice.PendingRunProcessor
	logger   *slog.Logger
	queue    chan deferredAgentVoiceRun
}

func newDeferredAgentVoiceRunProcessor(
	ctx context.Context,
	delegate agentvoice.PendingRunProcessor,
	logger *slog.Logger,
) (*deferredAgentVoiceRunProcessor, error) {
	if ctx == nil || delegate == nil || logger == nil {
		return nil, errors.New(
			"bootstrap: deferred Agent voice Run dependency is required",
		)
	}
	processor := &deferredAgentVoiceRunProcessor{
		ctx:      ctx,
		delegate: delegate,
		logger:   logger,
		queue:    make(chan deferredAgentVoiceRun, agentVoiceRunQueueCapacity),
	}
	go processor.run()
	return processor, nil
}

func (processor *deferredAgentVoiceRunProcessor) ProcessPending(
	requestContext context.Context,
	actor requestcontext.Actor,
	run agentrun.Run,
) (agentrun.Run, error) {
	if requestContext == nil {
		return agentrun.Run{}, errors.New(
			"bootstrap: Agent voice Run request context is required",
		)
	}
	request := deferredAgentVoiceRun{actor: actor, run: run}
	select {
	case processor.queue <- request:
		return run, nil
	case <-requestContext.Done():
		return agentrun.Run{}, requestContext.Err()
	case <-processor.ctx.Done():
		return agentrun.Run{}, processor.ctx.Err()
	}
}

func (processor *deferredAgentVoiceRunProcessor) run() {
	for {
		select {
		case <-processor.ctx.Done():
			return
		case request := <-processor.queue:
			processor.process(request)
		}
	}
}

func (processor *deferredAgentVoiceRunProcessor) process(
	request deferredAgentVoiceRun,
) {
	ctx, cancel := context.WithTimeout(
		processor.ctx,
		agentVoiceRunTimeout,
	)
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

var _ agentvoice.PendingRunProcessor = (*deferredAgentVoiceRunProcessor)(nil)
