package main

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
)

func TestThreadSummaryWorkerWakesWithoutWaitingForRecoveryInterval(
	t *testing.T,
) {
	t.Parallel()

	wakeup := newWorkerWakeup()
	firstSweepDone := make(chan struct{})
	wokenSweepDone := make(chan struct{})
	var calls atomic.Int32
	processor := threadSummaryProcessorFunc(func(
		context.Context,
		int,
	) (agentsummary.SweepResult, error) {
		switch calls.Add(1) {
		case 1:
			close(firstSweepDone)
		case 2:
			close(wokenSweepDone)
		}
		return agentsummary.SweepResult{}, nil
	})
	worker, err := newThreadSummaryWorker(
		processor,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Hour,
		time.Second,
		1,
		wakeup.Events(),
	)
	if err != nil {
		t.Fatalf("newThreadSummaryWorker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	<-firstSweepDone
	wakeup.Notify()
	select {
	case <-wokenSweepDone:
	case <-time.After(time.Second):
		t.Fatal("Summary Worker did not respond to wakeup")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Summary Worker did not stop after cancellation")
	}
}

func TestThreadSummaryWorkerDrainsFullBatchWithoutWaiting(t *testing.T) {
	t.Parallel()

	secondSweep := make(chan struct{})
	var calls atomic.Int32
	processor := threadSummaryProcessorFunc(func(
		context.Context,
		int,
	) (agentsummary.SweepResult, error) {
		if calls.Add(1) == 1 {
			return agentsummary.SweepResult{Claimed: 1}, nil
		}
		close(secondSweep)
		return agentsummary.SweepResult{}, nil
	})
	worker, err := newThreadSummaryWorker(
		processor,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Hour,
		time.Second,
		1,
		nil,
	)
	if err != nil {
		t.Fatalf("newThreadSummaryWorker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	select {
	case <-secondSweep:
	case <-time.After(time.Second):
		t.Fatal("Summary Worker waited after a full batch")
	}
	cancel()
	<-done
}

type threadSummaryProcessorFunc func(
	context.Context,
	int,
) (agentsummary.SweepResult, error)

func (process threadSummaryProcessorFunc) ProcessPending(
	ctx context.Context,
	limit int,
) (agentsummary.SweepResult, error) {
	return process(ctx, limit)
}
