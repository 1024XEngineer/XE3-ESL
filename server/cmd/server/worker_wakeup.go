package main

import (
	"context"
	"time"
)

// workerWakeup is an edge-triggered hint only. The database remains the source
// of truth, so concurrent hints may be safely coalesced.
type workerWakeup struct {
	events chan struct{}
}

func newWorkerWakeup() *workerWakeup {
	return &workerWakeup{events: make(chan struct{}, 1)}
}

func (wakeup *workerWakeup) Notify() {
	if wakeup == nil {
		return
	}
	select {
	case wakeup.events <- struct{}{}:
	default:
	}
}

func (wakeup *workerWakeup) Events() <-chan struct{} {
	if wakeup == nil {
		return nil
	}
	return wakeup.events
}

func waitForWorkerWork(
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
