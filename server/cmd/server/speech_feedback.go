package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
)

const (
	speechFeedbackInterval     = 500 * time.Millisecond
	speechFeedbackSweepTimeout = 25 * time.Second
	speechFeedbackClaimLimit   = 1
)

type speechFeedbackProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (review.SpeechFeedbackSweepResult, error)
}

type speechFeedbackWorker struct {
	processor speechFeedbackProcessor
	logger    *slog.Logger
}

func buildSpeechFeedbackWorker(
	processor speechFeedbackProcessor,
	logger *slog.Logger,
) (*speechFeedbackWorker, error) {
	if processor == nil || logger == nil {
		return nil, errors.New(
			"speech feedback worker dependency is required",
		)
	}
	return &speechFeedbackWorker{
		processor: processor,
		logger:    logger,
	}, nil
}

func (worker *speechFeedbackWorker) Run(ctx context.Context) {
	if worker == nil || ctx == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		claimed := worker.sweep(ctx)
		if claimed == speechFeedbackClaimLimit {
			continue
		}
		if !waitForWorkerWork(ctx, speechFeedbackInterval, nil) {
			return
		}
	}
}

func (worker *speechFeedbackWorker) sweep(parent context.Context) int {
	ctx, cancel := context.WithTimeout(
		parent,
		speechFeedbackSweepTimeout,
	)
	defer cancel()
	result, err := worker.processor.ProcessPending(
		ctx,
		speechFeedbackClaimLimit,
	)
	if err != nil {
		worker.logger.WarnContext(
			parent,
			"speech feedback sweep failed",
			slog.String("error_kind", speechFeedbackErrorKind(err)),
		)
		return 0
	}
	if result.Claimed > 0 {
		worker.logger.InfoContext(
			parent,
			"speech feedback sweep completed",
			slog.Int("claimed", result.Claimed),
			slog.Int("completed", result.Completed),
			slog.Int("insufficient", result.Insufficient),
			slog.Int("retried", result.Retried),
			slog.Int("failed", result.Failed),
		)
	}
	return result.Claimed
}

func speechFeedbackErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, review.ErrSpeechFeedbackClaimLost):
		return "lease_lost"
	case errors.Is(err, review.ErrInvalidSpeechFeedback):
		return "invalid_state"
	default:
		return "repository"
	}
}
