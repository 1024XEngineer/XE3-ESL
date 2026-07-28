package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
)

func TestMemoryExtractionWorkerRunsImmediatelyAndSanitizesErrors(
	t *testing.T,
) {
	t.Parallel()

	var calls int
	processor := memoryExtractionProcessorFunc(func(
		context.Context,
		int,
	) (memory.ExtractionSweepResult, error) {
		calls++
		return memory.ExtractionSweepResult{Claimed: 1},
			errors.New("user said my password is secret")
	})
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	worker, err := newMemoryExtractionWorker(
		processor,
		logger,
		time.Second,
		time.Second,
		1,
		func(context.Context, time.Duration) bool { return false },
	)
	if err != nil {
		t.Fatalf("newMemoryExtractionWorker: %v", err)
	}
	worker.Run(context.Background())
	if calls != 1 {
		t.Fatalf("processor calls = %d, want 1", calls)
	}
	output := logs.String()
	if strings.Contains(output, "password") ||
		strings.Contains(output, "secret") ||
		!strings.Contains(output, `"error_kind":"dependency"`) {
		t.Fatalf("sanitized logs = %s", output)
	}
}

func TestMemoryExtractionWorkerStopsWithContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	processor := memoryExtractionProcessorFunc(func(
		context.Context,
		int,
	) (memory.ExtractionSweepResult, error) {
		cancel()
		return memory.ExtractionSweepResult{}, nil
	})
	worker, err := newMemoryExtractionWorker(
		processor,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		time.Second,
		time.Second,
		1,
		waitForMemoryExtraction,
	)
	if err != nil {
		t.Fatalf("newMemoryExtractionWorker: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Memory extraction worker did not stop")
	}
}

func TestBuildMemoryExtractionWorkerRequiresDependency(t *testing.T) {
	t.Parallel()

	if _, err := buildMemoryExtractionWorker(
		nil,
		slog.Default(),
	); !errors.Is(err, errMemoryExtractionDependency) {
		t.Fatalf("build error = %v", err)
	}
}

type memoryExtractionProcessorFunc func(
	context.Context,
	int,
) (memory.ExtractionSweepResult, error)

func (function memoryExtractionProcessorFunc) ProcessPending(
	ctx context.Context,
	limit int,
) (memory.ExtractionSweepResult, error) {
	return function(ctx, limit)
}
