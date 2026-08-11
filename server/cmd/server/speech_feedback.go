package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

const (
	speechFeedbackInterval              = 500 * time.Millisecond
	speechFeedbackClaimLimit            = 1
	speechFeedbackPersistenceTimeMargin = 30 * time.Second
	speechFeedbackLeaseTimeMargin       = 30 * time.Second
)

type speechFeedbackExecutionBudget struct {
	processingTimeout time.Duration
	leaseDuration     time.Duration
}

type speechFeedbackProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (speechfeedback.SpeechFeedbackSweepResult, error)
}

type speechFeedbackWorker struct {
	processor         speechFeedbackProcessor
	logger            *slog.Logger
	processingTimeout time.Duration
}

func deriveSpeechFeedbackExecutionBudget(
	acousticProviderTimeout time.Duration,
	textProviderTimeout time.Duration,
) speechFeedbackExecutionBudget {
	processingTimeout := acousticProviderTimeout +
		textProviderTimeout +
		speechFeedbackPersistenceTimeMargin
	return speechFeedbackExecutionBudget{
		processingTimeout: processingTimeout,
		leaseDuration: processingTimeout +
			speechFeedbackLeaseTimeMargin,
	}
}

func buildSpeechFeedbackWorker(
	processor speechFeedbackProcessor,
	logger *slog.Logger,
	processingTimeout time.Duration,
) (*speechFeedbackWorker, error) {
	if processor == nil || logger == nil || processingTimeout <= 0 {
		return nil, errors.New(
			"speech feedback worker dependency is required",
		)
	}
	return &speechFeedbackWorker{
		processor:         processor,
		logger:            logger,
		processingTimeout: processingTimeout,
	}, nil
}

func logSpeechFeedbackAcousticFallback(
	logger *slog.Logger,
	recordingStoreAvailable bool,
	iseConfigured bool,
) {
	switch {
	case !recordingStoreAvailable && iseConfigured:
		logger.Warn(
			"iFlytek ISE is configured but object storage is disabled; acoustic scoring is unavailable",
			slog.String("fallback", "transcript_only"),
			slog.String("reason", "object_storage_disabled"),
		)
	case recordingStoreAvailable && !iseConfigured:
		logger.Warn(
			"iFlytek ISE is not configured; acoustic scoring is unavailable",
			slog.String("fallback", "transcript_only"),
		)
	}
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
		worker.processingTimeout,
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
	case errors.Is(err, speechfeedback.ErrSpeechFeedbackClaimLost):
		return "lease_lost"
	case errors.Is(err, speechfeedback.ErrInvalidSpeechFeedback):
		return "invalid_state"
	default:
		return "repository"
	}
}
