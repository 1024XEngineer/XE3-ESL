package conversation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type audioAssetExpiredReclaimerFunc func(
	context.Context,
	int,
) (AudioAssetCleanupResult, error)

func (reclaim audioAssetExpiredReclaimerFunc) ReclaimExpired(
	ctx context.Context,
	limit int,
) (AudioAssetCleanupResult, error) {
	return reclaim(ctx, limit)
}

func TestAudioAssetCleanupWorkerSweepsImmediatelyWithBoundedClaim(t *testing.T) {
	events := make([]string, 0, 2)
	reclaimer := audioAssetExpiredReclaimerFunc(func(
		ctx context.Context,
		limit int,
	) (AudioAssetCleanupResult, error) {
		events = append(events, "sweep")
		if limit != defaultCleanupLimit {
			t.Errorf("claim limit = %d, want %d", limit, defaultCleanupLimit)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("sweep context has no deadline")
		} else if remaining := time.Until(deadline); remaining < 3*time.Minute ||
			remaining > audioAssetCleanupOperationTimeout {
			t.Errorf("sweep deadline remaining = %v", remaining)
		}
		return AudioAssetCleanupResult{Deleted: 1}, nil
	})
	worker, err := newAudioAssetCleanupWorker(
		reclaimer,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		time.Second,
		audioAssetCleanupOperationTimeout,
		defaultCleanupLimit,
		func(context.Context, time.Duration) bool {
			events = append(events, "wait")
			return false
		},
	)
	if err != nil {
		t.Fatalf("newAudioAssetCleanupWorker() error = %v", err)
	}

	worker.Run(context.Background())
	if strings.Join(events, ",") != "sweep,wait" {
		t.Fatalf("worker events = %v", events)
	}
}

func TestAudioAssetCleanupWorkerRetriesSeriallyWithoutLeakingError(t *testing.T) {
	const sensitive = "secret-owner/audio/v1/assets/private.wav"
	var (
		calls       atomic.Int32
		inFlight    atomic.Int32
		maxInFlight atomic.Int32
		waits       atomic.Int32
		logs        bytes.Buffer
	)
	reclaimer := audioAssetExpiredReclaimerFunc(func(
		context.Context,
		int,
	) (AudioAssetCleanupResult, error) {
		call := calls.Add(1)
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			seen := maxInFlight.Load()
			if current <= seen || maxInFlight.CompareAndSwap(seen, current) {
				break
			}
		}
		if call == 1 {
			return AudioAssetCleanupResult{Failed: 1}, errors.New(sensitive)
		}
		return AudioAssetCleanupResult{Deleted: 1}, nil
	})
	worker, err := newAudioAssetCleanupWorker(
		reclaimer,
		slog.New(slog.NewJSONHandler(&logs, nil)),
		time.Second,
		time.Minute,
		defaultCleanupLimit,
		func(context.Context, time.Duration) bool {
			return waits.Add(1) == 1
		},
	)
	if err != nil {
		t.Fatalf("newAudioAssetCleanupWorker() error = %v", err)
	}

	worker.Run(context.Background())
	if calls.Load() != 2 || maxInFlight.Load() != 1 {
		t.Fatalf(
			"calls = %d, max in flight = %d",
			calls.Load(),
			maxInFlight.Load(),
		)
	}
	if output := logs.String(); strings.Contains(output, sensitive) ||
		!strings.Contains(output, `"error_kind":"dependency"`) {
		t.Fatalf("unexpected sanitized logs: %s", output)
	}
}

func TestAudioAssetCleanupWorkerCancellationStopsActiveSweep(t *testing.T) {
	started := make(chan struct{})
	reclaimer := audioAssetExpiredReclaimerFunc(func(
		ctx context.Context,
		_ int,
	) (AudioAssetCleanupResult, error) {
		close(started)
		<-ctx.Done()
		return AudioAssetCleanupResult{}, ctx.Err()
	})
	worker, err := NewAudioAssetCleanupWorker(
		reclaimer,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewAudioAssetCleanupWorker() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	<-started
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}
