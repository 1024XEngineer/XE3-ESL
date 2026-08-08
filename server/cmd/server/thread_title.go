package main

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"time"

	agenttitle "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/title"
)

const (
	threadTitleInterval     = 30 * time.Second
	threadTitleSweepTimeout = 30 * time.Second
	threadTitleClaimLimit   = 4
)

var errThreadTitleDependency = errors.New(
	"thread title dependency is required",
)

type threadTitleProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (agenttitle.SweepResult, error)
}

type threadTitleWorker struct {
	processor    threadTitleProcessor
	logger       *slog.Logger
	interval     time.Duration
	sweepTimeout time.Duration
	claimLimit   int
	wakeup       <-chan struct{}
}

func buildThreadTitleWorker(
	processor threadTitleProcessor,
	logger *slog.Logger,
	wakeup <-chan struct{},
) (*threadTitleWorker, error) {
	return newThreadTitleWorker(
		processor,
		logger,
		threadTitleInterval,
		threadTitleSweepTimeout,
		threadTitleClaimLimit,
		wakeup,
	)
}

func newThreadTitleWorker(
	processor threadTitleProcessor,
	logger *slog.Logger,
	interval time.Duration,
	sweepTimeout time.Duration,
	claimLimit int,
	wakeup <-chan struct{},
) (*threadTitleWorker, error) {
	if nilThreadTitleDependency(processor) ||
		logger == nil ||
		interval <= 0 ||
		sweepTimeout <= 0 ||
		sweepTimeout > 5*time.Minute ||
		claimLimit < 1 ||
		claimLimit > 20 {
		return nil, errThreadTitleDependency
	}
	return &threadTitleWorker{
		processor:    processor,
		logger:       logger,
		interval:     interval,
		sweepTimeout: sweepTimeout,
		claimLimit:   claimLimit,
		wakeup:       wakeup,
	}, nil
}

func (worker *threadTitleWorker) Run(ctx context.Context) {
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

func (worker *threadTitleWorker) sweep(parent context.Context) bool {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, worker.sweepTimeout)
	defer cancel()
	result, err := worker.processor.ProcessPending(ctx, worker.claimLimit)
	attributes := []any{
		slog.Int("claimed", result.Claimed),
		slog.Int("completed", result.Completed),
		slog.Int("retried", result.Retried),
		slog.Int("failed", result.Failed),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if err != nil {
		attributes = append(
			attributes,
			slog.String("error_kind", threadTitleErrorKind(err)),
		)
		worker.logger.WarnContext(
			parent,
			"thread title sweep failed",
			attributes...,
		)
		return false
	}
	if result.Failed > 0 {
		worker.logger.WarnContext(
			parent,
			"thread title sweep completed with terminal jobs",
			attributes...,
		)
		return result.Claimed == worker.claimLimit
	}
	worker.logger.InfoContext(
		parent,
		"thread title sweep completed",
		attributes...,
	)
	return result.Claimed == worker.claimLimit
}

func threadTitleErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, agenttitle.ErrInvalidArgument):
		return "invalid_argument"
	default:
		return "dependency"
	}
}

func nilThreadTitleDependency(processor threadTitleProcessor) bool {
	if processor == nil {
		return true
	}
	value := reflect.ValueOf(processor)
	switch value.Kind() {
	case reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
