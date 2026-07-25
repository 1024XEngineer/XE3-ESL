package conversation

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const (
	audioAssetCleanupInterval = time.Minute
)

type audioAssetCleanupWaiter func(context.Context, time.Duration) bool

// AudioAssetCleanupWorker serially performs an immediate sweep and then waits
// one complete interval after every sweep. Multiple process replicas are safe:
// PostgreSQL cleanup claims provide the cross-process lease and fencing.
type AudioAssetCleanupWorker struct {
	reclaimer    AudioAssetExpiredReclaimer
	logger       *slog.Logger
	interval     time.Duration
	sweepTimeout time.Duration
	claimLimit   int
	wait         audioAssetCleanupWaiter
}

func NewAudioAssetCleanupWorker(
	reclaimer AudioAssetExpiredReclaimer,
	logger *slog.Logger,
) (*AudioAssetCleanupWorker, error) {
	return newAudioAssetCleanupWorker(
		reclaimer,
		logger,
		audioAssetCleanupInterval,
		audioAssetCleanupOperationTimeout,
		defaultCleanupLimit,
		waitForAudioAssetCleanup,
	)
}

func newAudioAssetCleanupWorker(
	reclaimer AudioAssetExpiredReclaimer,
	logger *slog.Logger,
	interval time.Duration,
	sweepTimeout time.Duration,
	claimLimit int,
	wait audioAssetCleanupWaiter,
) (*AudioAssetCleanupWorker, error) {
	if nilDependency(reclaimer) ||
		nilDependency(logger) ||
		interval <= 0 ||
		sweepTimeout <= 0 ||
		sweepTimeout >= defaultCleanupLease ||
		claimLimit <= 0 ||
		wait == nil {
		return nil, ErrAudioAssetInvalidDependency
	}
	return &AudioAssetCleanupWorker{
		reclaimer:    reclaimer,
		logger:       logger,
		interval:     interval,
		sweepTimeout: sweepTimeout,
		claimLimit:   claimLimit,
		wait:         wait,
	}, nil
}

// Run blocks until ctx is canceled. Sweeps never overlap within one process.
func (w *AudioAssetCleanupWorker) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		w.sweep(ctx)
		if !w.wait(ctx, w.interval) {
			return
		}
	}
}

func (w *AudioAssetCleanupWorker) sweep(parent context.Context) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, w.sweepTimeout)
	defer cancel()

	result, err := w.reclaimer.ReclaimExpired(ctx, w.claimLimit)
	attributes := []any{
		slog.Int("deleted", result.Deleted),
		slog.Int("failed", result.Failed),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if err != nil {
		attributes = append(
			attributes,
			slog.String("error_kind", audioAssetCleanupErrorKind(err)),
		)
		w.logger.WarnContext(
			parent,
			"audio cleanup sweep failed",
			attributes...,
		)
		return
	}
	if result.Failed > 0 {
		w.logger.WarnContext(
			parent,
			"audio cleanup sweep completed with failures",
			attributes...,
		)
		return
	}
	w.logger.InfoContext(
		parent,
		"audio cleanup sweep completed",
		attributes...,
	)
}

func waitForAudioAssetCleanup(
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

func audioAssetCleanupErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, objectstore.ErrDisabled),
		errors.Is(err, objectstore.ErrCredentials),
		errors.Is(err, objectstore.ErrOperationFailed):
		return "object_storage"
	case errors.Is(err, ErrAudioAssetConcurrentUpdate):
		return "concurrent_update"
	case errors.Is(err, ErrAudioAssetInvalid):
		return "invalid_state"
	default:
		return "dependency"
	}
}
