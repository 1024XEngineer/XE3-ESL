package main

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
)

func TestMemoryWorkerWakeupCoalescesWithoutBlocking(t *testing.T) {
	t.Parallel()

	wakeup := newMemoryWorkerWakeup()
	var senders sync.WaitGroup
	for range 100 {
		senders.Add(1)
		go func() {
			defer senders.Done()
			wakeup.Notify()
		}()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		senders.Wait()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent notifications blocked")
	}
	select {
	case <-wakeup.Events():
	default:
		t.Fatal("notification was not retained")
	}
	select {
	case <-wakeup.Events():
		t.Fatal("burst notifications were not coalesced")
	default:
	}
}

func TestMemoryExtractionWorkerWakesDuringSweepAndNotifiesIndex(
	t *testing.T,
) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	extractionWakeup := newMemoryWorkerWakeup()
	indexWakeup := newMemoryWorkerWakeup()
	firstSweepStarted := make(chan struct{})
	releaseFirstSweep := make(chan struct{})
	var calls atomic.Int32
	processor := memoryExtractionProcessorFunc(func(
		context.Context,
		int,
	) (memory.ExtractionSweepResult, error) {
		switch calls.Add(1) {
		case 1:
			close(firstSweepStarted)
			<-releaseFirstSweep
			return memory.ExtractionSweepResult{Completed: 1}, nil
		default:
			cancel()
			return memory.ExtractionSweepResult{}, nil
		}
	})
	worker, err := newMemoryExtractionWorker(
		processor,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		time.Hour,
		time.Second,
		1,
		extractionWakeup.Events(),
		indexWakeup,
		waitForMemoryWork,
	)
	if err != nil {
		t.Fatalf("newMemoryExtractionWorker: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	<-firstSweepStarted
	extractionWakeup.Notify()
	close(releaseFirstSweep)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notification during sweep did not trigger another sweep")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("processor calls = %d, want 2", got)
	}
	select {
	case <-indexWakeup.Events():
	default:
		t.Fatal("completed extraction did not notify index worker")
	}
}

func TestMemoryIndexWorkerWakesWithoutWaitingForRecoveryInterval(
	t *testing.T,
) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wakeup := newMemoryWorkerWakeup()
	firstSweepDone := make(chan struct{})
	var calls atomic.Int32
	processor := memoryIndexProcessorFunc(func(
		context.Context,
		int,
	) (memory.IndexSweepResult, error) {
		if calls.Add(1) == 1 {
			close(firstSweepDone)
		} else {
			cancel()
		}
		return memory.IndexSweepResult{}, nil
	})
	worker, err := newMemoryIndexWorker(
		processor,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		time.Hour,
		time.Second,
		1,
		wakeup.Events(),
	)
	if err != nil {
		t.Fatalf("newMemoryIndexWorker: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	<-firstSweepDone
	wakeup.Notify()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("index worker waited for recovery interval")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("processor calls = %d, want 2", got)
	}
}

func TestMemoryIndexWorkerKeepsPeriodicRecoverySweep(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	processor := memoryIndexProcessorFunc(func(
		context.Context,
		int,
	) (memory.IndexSweepResult, error) {
		if calls.Add(1) == 2 {
			cancel()
		}
		return memory.IndexSweepResult{}, nil
	})
	worker, err := newMemoryIndexWorker(
		processor,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		10*time.Millisecond,
		time.Second,
		1,
		nil,
	)
	if err != nil {
		t.Fatalf("newMemoryIndexWorker: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("periodic recovery sweep did not run")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("processor calls = %d, want 2", got)
	}
}

func TestMemoryIndexWorkerDrainsFullBatchWithoutWaiting(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	processor := memoryIndexProcessorFunc(func(
		context.Context,
		int,
	) (memory.IndexSweepResult, error) {
		if calls.Add(1) == 1 {
			return memory.IndexSweepResult{Claimed: 1}, nil
		}
		cancel()
		return memory.IndexSweepResult{}, nil
	})
	worker, err := newMemoryIndexWorker(
		processor,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		time.Hour,
		time.Second,
		1,
		nil,
	)
	if err != nil {
		t.Fatalf("newMemoryIndexWorker: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("full batch did not trigger immediate drain sweep")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("processor calls = %d, want 2", got)
	}
}

type memoryIndexProcessorFunc func(
	context.Context,
	int,
) (memory.IndexSweepResult, error)

func (function memoryIndexProcessorFunc) ProcessPendingIndexes(
	ctx context.Context,
	limit int,
) (memory.IndexSweepResult, error) {
	return function(ctx, limit)
}
