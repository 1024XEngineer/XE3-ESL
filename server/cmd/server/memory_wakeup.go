package main

import (
	"context"
	"time"
)

// memoryWorkerWakeup is an edge-triggered hint only. The database remains the
// source of truth, so concurrent hints may be safely coalesced.
type memoryWorkerWakeup struct {
	events chan struct{}
}

func newMemoryWorkerWakeup() *memoryWorkerWakeup {
	return &memoryWorkerWakeup{events: make(chan struct{}, 1)}
}

func (wakeup *memoryWorkerWakeup) Notify() {
	if wakeup == nil {
		return
	}
	select {
	case wakeup.events <- struct{}{}:
	default:
	}
}

func (wakeup *memoryWorkerWakeup) Events() <-chan struct{} {
	if wakeup == nil {
		return nil
	}
	return wakeup.events
}

func waitForMemoryWork(
	ctx context.Context,
	interval time.Duration,
	wakeup <-chan struct{},
) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-wakeup:
		return true
	}
}
