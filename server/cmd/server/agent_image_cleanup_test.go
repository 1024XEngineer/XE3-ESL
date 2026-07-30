package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestBuildAgentImageCleanupWorkerFollowsStorageConfiguration(t *testing.T) {
	t.Parallel()

	disabled, err := buildAgentImageCleanupWorker(
		config.ObjectStorageConfig{},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil || disabled != nil {
		t.Fatalf("disabled worker = %#v, error = %v", disabled, err)
	}

	_, err = buildAgentImageCleanupWorker(
		config.ObjectStorageConfig{Enabled: true},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if !errors.Is(err, errAgentImageCleanupDependency) {
		t.Fatalf("enabled missing dependency error = %v", err)
	}
}

func TestAgentImageCleanupWorkerSweepsThroughApplicationBoundary(t *testing.T) {
	t.Parallel()

	reclaimer := &imageCleanupTestReclaimer{
		result: agentimage.CleanupResult{Deleted: 2, Failed: 1},
	}
	worker, err := buildAgentImageCleanupWorker(
		config.ObjectStorageConfig{Enabled: true},
		reclaimer,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("build worker: %v", err)
	}

	worker.sweep(context.Background())

	if reclaimer.calls != 1 ||
		reclaimer.limit != agentImageCleanupClaimLimit {
		t.Fatalf(
			"reclaimer calls = %d, limit = %d",
			reclaimer.calls,
			reclaimer.limit,
		)
	}
}

type imageCleanupTestReclaimer struct {
	result agentimage.CleanupResult
	calls  int
	limit  int
}

func (reclaimer *imageCleanupTestReclaimer) Reclaim(
	_ context.Context,
	intLimit int,
) (agentimage.CleanupResult, error) {
	reclaimer.calls++
	reclaimer.limit = intLimit
	return reclaimer.result, nil
}
