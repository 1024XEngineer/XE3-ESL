package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

const (
	mediaCleanupInterval     = time.Minute
	mediaCleanupSweepTimeout = 2 * time.Minute
	mediaCleanupClaimLimit   = 8
)

var errMediaCleanupDependency = errors.New(
	"media cleanup dependency is required",
)

type mediaObjectReclaimer interface {
	Reclaim(context.Context, int) (sharedmedia.CleanupResult, error)
}

type mediaCleanupWorker struct {
	reclaimer mediaObjectReclaimer
	logger    *slog.Logger
}

func buildMediaCleanupWorker(
	storageConfig config.ObjectStorageConfig,
	reclaimer mediaObjectReclaimer,
	logger *slog.Logger,
) (*mediaCleanupWorker, error) {
	if !storageConfig.Enabled {
		if logger != nil {
			logger.Info(
				"media cleanup disabled",
				slog.String("reason", "object_storage_disabled"),
			)
		}
		return nil, nil
	}
	if reclaimer == nil || logger == nil {
		return nil, errMediaCleanupDependency
	}
	return &mediaCleanupWorker{reclaimer: reclaimer, logger: logger}, nil
}

func (worker *mediaCleanupWorker) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		worker.sweep(ctx)
		timer := time.NewTimer(mediaCleanupInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (worker *mediaCleanupWorker) sweep(parent context.Context) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, mediaCleanupSweepTimeout)
	defer cancel()
	result, err := worker.reclaimer.Reclaim(ctx, mediaCleanupClaimLimit)
	attributes := []any{
		slog.Int("deleted", result.Deleted),
		slog.Int("failed", result.Failed),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if err != nil {
		attributes = append(attributes, slog.String("error_kind", "dependency"))
		worker.logger.WarnContext(
			parent, "media cleanup sweep failed", attributes...,
		)
		return
	}
	if result.Failed > 0 {
		worker.logger.WarnContext(
			parent,
			"media cleanup sweep completed with failures",
			attributes...,
		)
		return
	}
	worker.logger.InfoContext(
		parent, "media cleanup sweep completed", attributes...,
	)
}
