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
	memoryIndexInterval     = 30 * time.Second
	memoryIndexSweepTimeout = 90 * time.Second
	memoryIndexClaimLimit   = 4
)

var errMemoryIndexDependency = errors.New(
	"memory index dependency is required",
)

type memoryIndexProcessor interface {
	ProcessPendingIndexes(
		context.Context,
		int,
	) (memory.IndexSweepResult, error)
}

type memoryIndexWorker struct {
	processor    memoryIndexProcessor
	logger       *slog.Logger
	interval     time.Duration
	sweepTimeout time.Duration
	claimLimit   int
	wakeup       <-chan struct{}
}

func buildMemoryIndexWorker(
	processor memoryIndexProcessor,
	logger *slog.Logger,
	wakeup <-chan struct{},
) (*memoryIndexWorker, error) {
	return newMemoryIndexWorker(
		processor,
		logger,
		memoryIndexInterval,
		memoryIndexSweepTimeout,
		memoryIndexClaimLimit,
		wakeup,
	)
}

func newMemoryIndexWorker(
	processor memoryIndexProcessor,
	logger *slog.Logger,
	interval time.Duration,
	sweepTimeout time.Duration,
	claimLimit int,
	wakeup <-chan struct{},
) (*memoryIndexWorker, error) {
	if nilMemoryIndexDependency(processor) ||
		logger == nil ||
		interval <= 0 ||
		sweepTimeout <= 0 ||
		sweepTimeout > 5*time.Minute ||
		claimLimit < 1 ||
		claimLimit > 20 {
		return nil, errMemoryIndexDependency
	}
	return &memoryIndexWorker{
		processor:    processor,
		logger:       logger,
		interval:     interval,
		sweepTimeout: sweepTimeout,
		claimLimit:   claimLimit,
		wakeup:       wakeup,
	}, nil
}

func (worker *memoryIndexWorker) Run(ctx context.Context) {
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
		if !waitForWorkerWork(ctx, worker.interval, worker.wakeup) {
			return
		}
	}
}

func (worker *memoryIndexWorker) sweep(parent context.Context) bool {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, worker.sweepTimeout)
	defer cancel()
	result, err := worker.processor.ProcessPendingIndexes(
		ctx,
		worker.claimLimit,
	)
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
			slog.String("error_kind", memoryIndexErrorKind(err)),
		)
		worker.logger.WarnContext(
			parent,
			"memory index sweep failed",
			attributes...,
		)
		return false
	}
	if result.Failed > 0 || result.Discarded > 0 {
		worker.logger.WarnContext(
			parent,
			"memory index sweep completed with terminal jobs",
			attributes...,
		)
		return result.Claimed == worker.claimLimit
	}
	worker.logger.InfoContext(
		parent,
		"memory index sweep completed",
		attributes...,
	)
	return result.Claimed == worker.claimLimit
}

func memoryIndexErrorKind(err error) string {
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
	default:
		return "dependency"
	}
}

func nilMemoryIndexDependency(value any) bool {
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
