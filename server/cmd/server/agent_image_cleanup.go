package main

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"time"

	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

const (
	agentImageCleanupInterval     = time.Minute
	agentImageCleanupSweepTimeout = 2 * time.Minute
	agentImageCleanupClaimLimit   = 8
)

var errAgentImageCleanupDependency = errors.New(
	"agent image cleanup dependency is required",
)

type agentImageObjectReclaimer interface {
	Reclaim(context.Context, int) (agentimage.CleanupResult, error)
}

type agentImageCleanupWorker struct {
	reclaimer agentImageObjectReclaimer
	logger    *slog.Logger
}

func buildAgentImageCleanupWorker(
	storageConfig config.ObjectStorageConfig,
	reclaimer agentImageObjectReclaimer,
	logger *slog.Logger,
) (*agentImageCleanupWorker, error) {
	if !storageConfig.Enabled {
		if logger != nil {
			logger.Info(
				"agent image cleanup disabled",
				slog.String("reason", "object_storage_disabled"),
			)
		}
		return nil, nil
	}
	if nilAgentImageCleanupDependency(reclaimer) || logger == nil {
		return nil, errAgentImageCleanupDependency
	}
	return &agentImageCleanupWorker{
		reclaimer: reclaimer,
		logger:    logger,
	}, nil
}

func (worker *agentImageCleanupWorker) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		worker.sweep(ctx)
		timer := time.NewTimer(agentImageCleanupInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (worker *agentImageCleanupWorker) sweep(parent context.Context) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(
		parent,
		agentImageCleanupSweepTimeout,
	)
	defer cancel()
	result, err := worker.reclaimer.Reclaim(
		ctx,
		agentImageCleanupClaimLimit,
	)
	attributes := []any{
		slog.Int("deleted", result.Deleted),
		slog.Int("failed", result.Failed),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if err != nil {
		attributes = append(attributes, slog.String("error_kind", "dependency"))
		worker.logger.WarnContext(
			parent,
			"agent image cleanup sweep failed",
			attributes...,
		)
		return
	}
	if result.Failed > 0 {
		worker.logger.WarnContext(
			parent,
			"agent image cleanup sweep completed with failures",
			attributes...,
		)
		return
	}
	worker.logger.InfoContext(
		parent,
		"agent image cleanup sweep completed",
		attributes...,
	)
}

func nilAgentImageCleanupDependency(value any) bool {
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
