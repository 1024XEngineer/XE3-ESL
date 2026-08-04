package main

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

const (
	evaluationShadowInterval     = 5 * time.Second
	evaluationShadowSweepTimeout = 60 * time.Second
	evaluationShadowClaimLimit   = 2
)

var errEvaluationShadowDependency = errors.New(
	"evaluation shadow dependency is required",
)

type evaluationShadowProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (scoring.InterviewShadowSweepResult, error)
}

type evaluationShadowWorker struct {
	processor    evaluationShadowProcessor
	logger       *slog.Logger
	interval     time.Duration
	sweepTimeout time.Duration
	claimLimit   int
}

func buildEvaluationShadowWorker(
	processor evaluationShadowProcessor,
	logger *slog.Logger,
) (*evaluationShadowWorker, error) {
	return newEvaluationShadowWorker(
		processor,
		logger,
		evaluationShadowInterval,
		evaluationShadowSweepTimeout,
		evaluationShadowClaimLimit,
	)
}

func newEvaluationShadowWorker(
	processor evaluationShadowProcessor,
	logger *slog.Logger,
	interval time.Duration,
	sweepTimeout time.Duration,
	claimLimit int,
) (*evaluationShadowWorker, error) {
	if nilEvaluationShadowDependency(processor) ||
		logger == nil ||
		interval <= 0 ||
		sweepTimeout <= 0 ||
		sweepTimeout > 5*time.Minute ||
		claimLimit < 1 ||
		claimLimit > 20 {
		return nil, errEvaluationShadowDependency
	}
	return &evaluationShadowWorker{
		processor:    processor,
		logger:       logger,
		interval:     interval,
		sweepTimeout: sweepTimeout,
		claimLimit:   claimLimit,
	}, nil
}

func (worker *evaluationShadowWorker) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		if worker.sweep(ctx) {
			continue
		}
		if !waitForWorkerWork(ctx, worker.interval, nil) {
			return
		}
	}
}

func (worker *evaluationShadowWorker) sweep(
	parent context.Context,
) bool {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, worker.sweepTimeout)
	defer cancel()
	result, err := worker.processor.ProcessPending(
		ctx,
		worker.claimLimit,
	)
	attributes := []any{
		slog.Int("claimed", result.Claimed),
		slog.Int("completed", result.Completed),
		slog.Int("retried", result.Retried),
		slog.Int("failed", result.Failed),
		slog.Int64(
			"duration_ms",
			time.Since(startedAt).Milliseconds(),
		),
	}
	if err != nil {
		attributes = append(
			attributes,
			slog.String(
				"error_kind",
				evaluationShadowErrorKind(err),
			),
		)
		worker.logger.WarnContext(
			parent,
			"evaluation shadow sweep failed",
			attributes...,
		)
		return false
	}
	if result.Failed > 0 {
		worker.logger.WarnContext(
			parent,
			"evaluation shadow sweep completed with terminal jobs",
			attributes...,
		)
		return result.Claimed == worker.claimLimit
	}
	if result.Claimed == 0 {
		worker.logger.DebugContext(
			parent,
			"evaluation shadow sweep idle",
			attributes...,
		)
		return false
	}
	worker.logger.InfoContext(
		parent,
		"evaluation shadow sweep completed",
		attributes...,
	)
	return result.Claimed == worker.claimLimit
}

func evaluationShadowErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, scoring.ErrRuntimeLeaseLost):
		return "lease_lost"
	case errors.Is(
		err,
		scoring.ErrRuntimeConfigurationConflict,
	):
		return "configuration_conflict"
	case errors.Is(err, evaluation.ErrInvalidRequest):
		return "invalid_state"
	default:
		return "repository"
	}
}

func nilEvaluationShadowDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
