package main

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const (
	agentVoiceCleanupInterval     = time.Minute
	agentVoiceCleanupSweepTimeout = 2 * time.Minute
	agentVoiceCleanupClaimLimit   = 4
)

var errAgentVoiceCleanupDependency = errors.New(
	"agent voice cleanup dependency is required",
)

type agentVoiceObjectReclaimer interface {
	ReclaimVoiceObjects(
		context.Context,
		int,
	) (agent.VoiceCleanupResult, error)
}

type agentVoiceCleanupWaiter func(context.Context, time.Duration) bool

// agentVoiceCleanupWorker performs an immediate recovery sweep at startup and
// then runs serially. PostgreSQL claims and fencing make concurrent server
// replicas safe.
type agentVoiceCleanupWorker struct {
	reclaimer    agentVoiceObjectReclaimer
	logger       *slog.Logger
	interval     time.Duration
	sweepTimeout time.Duration
	claimLimit   int
	wait         agentVoiceCleanupWaiter
}

func buildAgentVoiceCleanupWorker(
	storageConfig config.ObjectStorageConfig,
	reclaimer agentVoiceObjectReclaimer,
	logger *slog.Logger,
) (*agentVoiceCleanupWorker, error) {
	if !storageConfig.Enabled {
		if logger != nil {
			logger.Info(
				"agent voice cleanup disabled",
				slog.String("reason", "object_storage_disabled"),
			)
		}
		return nil, nil
	}
	return newAgentVoiceCleanupWorker(
		reclaimer,
		logger,
		agentVoiceCleanupInterval,
		agentVoiceCleanupSweepTimeout,
		agentVoiceCleanupClaimLimit,
		waitForAgentVoiceCleanup,
	)
}

func newAgentVoiceCleanupWorker(
	reclaimer agentVoiceObjectReclaimer,
	logger *slog.Logger,
	interval time.Duration,
	sweepTimeout time.Duration,
	claimLimit int,
	wait agentVoiceCleanupWaiter,
) (*agentVoiceCleanupWorker, error) {
	if nilAgentVoiceCleanupDependency(reclaimer) ||
		logger == nil ||
		interval <= 0 ||
		sweepTimeout <= 0 ||
		sweepTimeout >= 5*time.Minute ||
		claimLimit <= 0 ||
		wait == nil {
		return nil, errAgentVoiceCleanupDependency
	}
	return &agentVoiceCleanupWorker{
		reclaimer:    reclaimer,
		logger:       logger,
		interval:     interval,
		sweepTimeout: sweepTimeout,
		claimLimit:   claimLimit,
		wait:         wait,
	}, nil
}

func (worker *agentVoiceCleanupWorker) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		worker.sweep(ctx)
		if !worker.wait(ctx, worker.interval) {
			return
		}
	}
}

func (worker *agentVoiceCleanupWorker) sweep(parent context.Context) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, worker.sweepTimeout)
	defer cancel()

	result, err := worker.reclaimer.ReclaimVoiceObjects(
		ctx,
		worker.claimLimit,
	)
	attributes := []any{
		slog.Int("deleted", result.Deleted),
		slog.Int("failed", result.Failed),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if err != nil {
		attributes = append(
			attributes,
			slog.String("error_kind", agentVoiceCleanupErrorKind(err)),
		)
		worker.logger.WarnContext(
			parent,
			"agent voice cleanup sweep failed",
			attributes...,
		)
		return
	}
	if result.Failed > 0 {
		worker.logger.WarnContext(
			parent,
			"agent voice cleanup sweep completed with failures",
			attributes...,
		)
		return
	}
	worker.logger.InfoContext(
		parent,
		"agent voice cleanup sweep completed",
		attributes...,
	)
}

func waitForAgentVoiceCleanup(
	ctx context.Context,
	interval time.Duration,
) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func agentVoiceCleanupErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, objectstore.ErrDisabled),
		errors.Is(err, objectstore.ErrCredentials),
		errors.Is(err, objectstore.ErrOperationFailed):
		return "object_storage"
	case errors.Is(err, agent.ErrConflict):
		return "concurrent_update"
	case errors.Is(err, agent.ErrInvalidRequest):
		return "invalid_state"
	case errors.Is(err, agent.ErrRepository):
		return "repository"
	default:
		return "dependency"
	}
}

func nilAgentVoiceCleanupDependency(value any) bool {
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
