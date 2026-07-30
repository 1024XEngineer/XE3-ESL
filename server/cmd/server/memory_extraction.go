package main

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
)

const (
	memoryExtractionInterval     = 30 * time.Second
	memoryExtractionSweepTimeout = 90 * time.Second
	memoryExtractionClaimLimit   = 4
)

var errMemoryExtractionDependency = errors.New(
	"memory extraction dependency is required",
)

type memoryExtractionProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (memory.ExtractionSweepResult, error)
}

type memoryExtractionWaiter func(
	context.Context,
	time.Duration,
	<-chan struct{},
) bool

type memoryExtractionWorker struct {
	processor    memoryExtractionProcessor
	logger       *slog.Logger
	interval     time.Duration
	sweepTimeout time.Duration
	claimLimit   int
	wakeup       <-chan struct{}
	indexWakeup  interface{ Notify() }
	wait         memoryExtractionWaiter
}

func buildMemoryExtractionWorker(
	processor memoryExtractionProcessor,
	logger *slog.Logger,
	wakeup <-chan struct{},
	indexWakeup interface{ Notify() },
) (*memoryExtractionWorker, error) {
	return newMemoryExtractionWorker(
		processor,
		logger,
		memoryExtractionInterval,
		memoryExtractionSweepTimeout,
		memoryExtractionClaimLimit,
		wakeup,
		indexWakeup,
		waitForWorkerWork,
	)
}

func newMemoryExtractionWorker(
	processor memoryExtractionProcessor,
	logger *slog.Logger,
	interval time.Duration,
	sweepTimeout time.Duration,
	claimLimit int,
	wakeup <-chan struct{},
	indexWakeup interface{ Notify() },
	wait memoryExtractionWaiter,
) (*memoryExtractionWorker, error) {
	if nilMemoryExtractionDependency(processor) ||
		logger == nil ||
		interval <= 0 ||
		sweepTimeout <= 0 ||
		sweepTimeout > 5*time.Minute ||
		claimLimit < 1 ||
		claimLimit > 20 ||
		wait == nil {
		return nil, errMemoryExtractionDependency
	}
	return &memoryExtractionWorker{
		processor:    processor,
		logger:       logger,
		interval:     interval,
		sweepTimeout: sweepTimeout,
		claimLimit:   claimLimit,
		wakeup:       wakeup,
		indexWakeup:  indexWakeup,
		wait:         wait,
	}, nil
}

func (worker *memoryExtractionWorker) Run(ctx context.Context) {
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
		if !worker.wait(ctx, worker.interval, worker.wakeup) {
			return
		}
	}
}

func (worker *memoryExtractionWorker) sweep(parent context.Context) bool {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, worker.sweepTimeout)
	defer cancel()
	result, err := worker.processor.ProcessPending(
		ctx,
		worker.claimLimit,
	)
	if result.Completed > 0 && worker.indexWakeup != nil {
		worker.indexWakeup.Notify()
	}
	for _, rejection := range result.Rejections {
		worker.logger.DebugContext(
			parent,
			"memory extraction candidate rejected",
			slog.String("run_id", rejection.RunID),
			slog.Int("candidate_index", rejection.CandidateIndex),
			slog.String("reason", string(rejection.Reason)),
		)
	}
	attributes := []any{
		slog.Int("claimed", result.Claimed),
		slog.Int("completed", result.Completed),
		slog.Int("retried", result.Retried),
		slog.Int("failed", result.Failed),
		slog.Int("discarded", result.Discarded),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if err != nil {
		attributes = append(
			attributes,
			slog.String(
				"error_kind",
				memoryExtractionErrorKind(err),
			),
		)
		worker.logger.WarnContext(
			parent,
			"memory extraction sweep failed",
			attributes...,
		)
		return false
	}
	if result.Failed > 0 || result.Discarded > 0 {
		worker.logger.WarnContext(
			parent,
			"memory extraction sweep completed with terminal jobs",
			attributes...,
		)
		return result.Claimed == worker.claimLimit
	}
	worker.logger.InfoContext(
		parent,
		"memory extraction sweep completed",
		attributes...,
	)
	return result.Claimed == worker.claimLimit
}

func memoryExtractionErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, memory.ErrConflict):
		return "concurrent_update"
	case errors.Is(err, memory.ErrInvalidArgument):
		return "invalid_state"
	case errors.Is(err, memory.ErrRepository):
		return "repository"
	case errors.Is(err, memory.ErrExtractionExhausted):
		return "attempts_exhausted"
	default:
		return "dependency"
	}
}

func nilMemoryExtractionDependency(value any) bool {
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
