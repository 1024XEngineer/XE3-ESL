package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agent "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

type agentVoiceObjectReclaimerFunc func(
	context.Context,
	int,
) (agent.VoiceCleanupResult, error)

func (reclaim agentVoiceObjectReclaimerFunc) ReclaimVoiceObjects(
	ctx context.Context,
	limit int,
) (agent.VoiceCleanupResult, error) {
	return reclaim(ctx, limit)
}

func TestBuildAgentVoiceCleanupWorkerDisablesSafelyWithoutStorage(
	t *testing.T,
) {
	const sensitiveBucket = "private-agent-voice-bucket"
	var logs bytes.Buffer
	worker, err := buildAgentVoiceCleanupWorker(
		config.ObjectStorageConfig{
			Enabled: false,
			Bucket:  sensitiveBucket,
		},
		nil,
		slog.New(slog.NewJSONHandler(&logs, nil)),
	)
	if err != nil || worker != nil {
		t.Fatalf("build disabled worker = %#v, %v", worker, err)
	}
	if output := logs.String(); strings.Contains(output, sensitiveBucket) ||
		!strings.Contains(output, `"reason":"object_storage_disabled"`) {
		t.Fatalf("unexpected disabled log: %s", output)
	}
}

func TestBuildAgentVoiceCleanupWorkerRejectsEnabledMissingReclaimer(
	t *testing.T,
) {
	worker, err := buildAgentVoiceCleanupWorker(
		config.ObjectStorageConfig{Enabled: true},
		nil,
		slog.Default(),
	)
	if worker != nil || !errors.Is(err, errAgentVoiceCleanupDependency) {
		t.Fatalf("build missing reclaimer = %#v, %v", worker, err)
	}
}

func TestAgentVoiceCleanupWorkerRecoversImmediatelyAndSanitizesLogs(
	t *testing.T,
) {
	const sensitive = "audio/v1/agent/private-object-key.wav"
	var (
		calls       atomic.Int32
		inFlight    atomic.Int32
		maxInFlight atomic.Int32
		waits       atomic.Int32
		logs        bytes.Buffer
	)
	reclaimer := agentVoiceObjectReclaimerFunc(func(
		ctx context.Context,
		limit int,
	) (agent.VoiceCleanupResult, error) {
		call := calls.Add(1)
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			seen := maxInFlight.Load()
			if current <= seen || maxInFlight.CompareAndSwap(seen, current) {
				break
			}
		}
		if limit != agentVoiceCleanupClaimLimit {
			t.Errorf(
				"cleanup claim limit = %d, want %d",
				limit,
				agentVoiceCleanupClaimLimit,
			)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("cleanup sweep context has no deadline")
		}
		if call == 1 {
			return agent.VoiceCleanupResult{Failed: 1},
				errors.New(sensitive)
		}
		return agent.VoiceCleanupResult{Deleted: 1}, nil
	})
	worker, err := newAgentVoiceCleanupWorker(
		reclaimer,
		slog.New(slog.NewJSONHandler(&logs, nil)),
		time.Second,
		time.Minute,
		agentVoiceCleanupClaimLimit,
		func(context.Context, time.Duration) bool {
			return waits.Add(1) == 1
		},
	)
	if err != nil {
		t.Fatalf("new cleanup worker: %v", err)
	}

	worker.Run(context.Background())
	if calls.Load() != 2 || maxInFlight.Load() != 1 {
		t.Fatalf(
			"cleanup calls = %d, max in flight = %d",
			calls.Load(),
			maxInFlight.Load(),
		)
	}
	if output := logs.String(); strings.Contains(output, sensitive) ||
		!strings.Contains(output, `"error_kind":"dependency"`) {
		t.Fatalf("unexpected sanitized cleanup log: %s", output)
	}
}

func TestAgentVoiceCleanupWorkerStopsWithApplicationContext(t *testing.T) {
	started := make(chan struct{})
	reclaimer := agentVoiceObjectReclaimerFunc(func(
		ctx context.Context,
		_ int,
	) (agent.VoiceCleanupResult, error) {
		close(started)
		<-ctx.Done()
		return agent.VoiceCleanupResult{}, ctx.Err()
	})
	worker, err := newAgentVoiceCleanupWorker(
		reclaimer,
		slog.Default(),
		time.Minute,
		time.Second,
		agentVoiceCleanupClaimLimit,
		waitForAgentVoiceCleanup,
	)
	if err != nil {
		t.Fatalf("new cleanup worker: %v", err)
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
		t.Fatal("cleanup worker did not stop after application cancellation")
	}
}
