package main

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
)

const (
	threadSummaryInterval     = 30 * time.Second
	threadSummarySweepTimeout = 90 * time.Second
	threadSummaryClaimLimit   = 4
)

var errThreadSummaryDependency = errors.New(
	"thread summary dependency is required",
)

type threadSummaryProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (agentsummary.SweepResult, error)
}

type threadSummaryWorker struct {
	processor    threadSummaryProcessor
	logger       *slog.Logger
	interval     time.Duration
	sweepTimeout time.Duration
	claimLimit   int
	wakeup       <-chan struct{}
}

func buildThreadSummaryWorker(
	processor threadSummaryProcessor,
	logger *slog.Logger,
	wakeup <-chan struct{},
) (*threadSummaryWorker, error) {
	return newThreadSummaryWorker(
		processor,
		logger,
		threadSummaryInterval,
		threadSummarySweepTimeout,
		threadSummaryClaimLimit,
		wakeup,
	)
}

func newThreadSummaryWorker(
	processor threadSummaryProcessor,
	logger *slog.Logger,
	interval time.Duration,
	sweepTimeout time.Duration,
	claimLimit int,
	wakeup <-chan struct{},
) (*threadSummaryWorker, error) {
	if nilThreadSummaryDependency(processor) ||
		logger == nil ||
		interval <= 0 ||
		sweepTimeout <= 0 ||
		sweepTimeout > 5*time.Minute ||
		claimLimit < 1 ||
		claimLimit > 20 {
		return nil, errThreadSummaryDependency
	}
	return &threadSummaryWorker{
		processor:    processor,
		logger:       logger,
		interval:     interval,
		sweepTimeout: sweepTimeout,
		claimLimit:   claimLimit,
		wakeup:       wakeup,
	}, nil
}

func (worker *threadSummaryWorker) Run(ctx context.Context) {
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

func (worker *threadSummaryWorker) sweep(parent context.Context) bool {
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
		slog.Int("skipped", result.Skipped),
		slog.Int("superseded", result.Superseded),
		slog.Int("failed", result.Failed),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if err != nil {
		attributes = append(
			attributes,
			slog.String("error_kind", threadSummaryErrorKind(err)),
		)
		worker.logger.WarnContext(
			parent,
			"thread summary sweep failed",
			attributes...,
		)
		return false
	}
	if result.Failed > 0 {
		worker.logger.WarnContext(
			parent,
			"thread summary sweep completed with terminal jobs",
			attributes...,
		)
		return result.Claimed == worker.claimLimit
	}
	worker.logger.InfoContext(
		parent,
		"thread summary sweep completed",
		attributes...,
	)
	return result.Claimed == worker.claimLimit
}

func threadSummaryErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, agentconversation.ErrConflict):
		return "concurrent_update"
	case errors.Is(err, agentsummary.ErrInvalidArgument),
		errors.Is(err, agentconversation.ErrInvalidRequest):
		return "invalid_argument"
	default:
		return "dependency"
	}
}

func nilThreadSummaryDependency(
	processor threadSummaryProcessor,
) bool {
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
