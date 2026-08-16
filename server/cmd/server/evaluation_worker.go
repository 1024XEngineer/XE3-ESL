package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

const (
	evaluationPollInterval      = 500 * time.Millisecond
	evaluationSpeechConcurrency = 4
)

type evaluationProcessor interface {
	ProcessSession(context.Context) (bool, error)
	ProcessSpeech(context.Context) (bool, error)
}

type evaluationRuntime struct {
	workers []*evaluationLaneWorker
}

type evaluationLaneWorker struct {
	name    string
	process func(context.Context) (bool, error)
	logger  *slog.Logger
}

func buildEvaluationRuntime(
	processor evaluationProcessor,
	logger *slog.Logger,
) (*evaluationRuntime, error) {
	if processor == nil || logger == nil {
		return nil, errors.New("evaluation worker dependencies are required")
	}
	workers := make([]*evaluationLaneWorker, 0, 1+evaluationSpeechConcurrency)
	workers = append(workers, &evaluationLaneWorker{
		name:    "session",
		process: processor.ProcessSession,
		logger:  logger,
	})
	for range evaluationSpeechConcurrency {
		workers = append(workers, &evaluationLaneWorker{
			name:    "speech",
			process: processor.ProcessSpeech,
			logger:  logger,
		})
	}
	return &evaluationRuntime{workers: workers}, nil
}

func (runtime *evaluationRuntime) Run(ctx context.Context) {
	if runtime == nil || ctx == nil {
		return
	}
	done := make(chan struct{}, len(runtime.workers))
	for _, worker := range runtime.workers {
		go func() {
			worker.Run(ctx)
			done <- struct{}{}
		}()
	}
	for range runtime.workers {
		<-done
	}
}

func (worker *evaluationLaneWorker) Run(ctx context.Context) {
	if worker == nil || worker.process == nil || ctx == nil {
		return
	}
	for ctx.Err() == nil {
		processed, err := worker.process(ctx)
		if err != nil {
			worker.logger.WarnContext(
				ctx,
				"evaluation processing failed",
				slog.String("lane", worker.name),
				slog.String("error_kind", evaluationErrorKind(err)),
			)
		}
		if processed {
			continue
		}
		if !waitForWorkerWork(ctx, evaluationPollInterval, nil) {
			return
		}
	}
}

func evaluationErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, evaluation.ErrClaimLost):
		return "claim_lost"
	case errors.Is(err, evaluation.ErrInvalidRequest):
		return "invalid_state"
	case errors.Is(err, evaluation.ErrAccountUnavailable):
		return "account_unavailable"
	default:
		return "processing"
	}
}
