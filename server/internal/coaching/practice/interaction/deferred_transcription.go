package interaction

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	deferredTranscriptionQueueCapacity = 32
	deferredTranscriptionTimeout       = 3 * time.Minute
)

type deferredTranscriptionRequest struct {
	actor       requestcontext.Actor
	reservation TranscriptionReservation
}

// DeferredTranscriptionProcessor keeps provider work outside the HTTP request
// lifecycle. The recording and reservation are durable before Enqueue returns;
// a later status read can safely enqueue the same reservation again.
type DeferredTranscriptionProcessor struct {
	ctx          context.Context
	orchestrator *RoundOrchestrator
	logger       *slog.Logger
	queue        chan deferredTranscriptionRequest
	mu           sync.Mutex
	pending      map[string]struct{}
}

func NewDeferredTranscriptionProcessor(
	ctx context.Context,
	orchestrator *RoundOrchestrator,
	logger *slog.Logger,
) (*DeferredTranscriptionProcessor, error) {
	if ctx == nil || orchestrator == nil || logger == nil {
		return nil, errors.New(
			"practice interaction: deferred transcription dependencies are required",
		)
	}
	processor := &DeferredTranscriptionProcessor{
		ctx: ctx, orchestrator: orchestrator, logger: logger,
		queue:   make(chan deferredTranscriptionRequest, deferredTranscriptionQueueCapacity),
		pending: make(map[string]struct{}),
	}
	go processor.run()
	return processor, nil
}

func (processor *DeferredTranscriptionProcessor) Enqueue(
	ctx context.Context,
	actor requestcontext.Actor,
	reservation TranscriptionReservation,
) error {
	if processor == nil || ctx == nil || !actor.Valid() || reservation.ID == "" {
		return ErrInvalidRequest
	}
	processor.mu.Lock()
	if _, exists := processor.pending[reservation.ID]; exists {
		processor.mu.Unlock()
		return nil
	}
	processor.pending[reservation.ID] = struct{}{}
	processor.mu.Unlock()
	request := deferredTranscriptionRequest{
		actor: actor, reservation: reservation,
	}
	select {
	case <-ctx.Done():
		processor.removePending(reservation.ID)
		return ctx.Err()
	case <-processor.ctx.Done():
		processor.removePending(reservation.ID)
		return processor.ctx.Err()
	default:
	}
	select {
	case processor.queue <- request:
		return nil
	default:
		processor.removePending(reservation.ID)
		// The reservation and audio are durable. A later status read can retry
		// dispatch without making this request wait behind provider work.
		return nil
	}
}

func (processor *DeferredTranscriptionProcessor) removePending(id string) {
	processor.mu.Lock()
	delete(processor.pending, id)
	processor.mu.Unlock()
}

func (processor *DeferredTranscriptionProcessor) run() {
	for {
		select {
		case <-processor.ctx.Done():
			return
		case request := <-processor.queue:
			processor.process(request)
		}
	}
}

func (processor *DeferredTranscriptionProcessor) process(
	request deferredTranscriptionRequest,
) {
	defer processor.removePending(request.reservation.ID)
	ctx, cancel := context.WithTimeout(
		processor.ctx, deferredTranscriptionTimeout,
	)
	defer cancel()
	_, err := processor.orchestrator.ProcessDeferredTranscription(
		ctx, request.actor, request.reservation,
	)
	if err != nil && !errors.Is(err, ErrVoiceRoundProcessing) {
		processor.logger.Error(
			"deferred Practice transcription failed",
			slog.String("reservation_id", request.reservation.ID),
			slog.String("session_id", request.reservation.SessionID),
			slog.Any("error", err),
		)
	}
}
