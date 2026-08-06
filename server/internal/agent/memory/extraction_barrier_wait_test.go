package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestExtractionBarrierCoordinatorReadyWithoutWaiting(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	reader := &scriptedExtractionBarrierReader{snapshots: []ExtractionBarrierSnapshot{{
		Cutoff: cutoff,
	}}}
	scheduler := &fakeExtractionBarrierScheduler{now: cutoff.Add(time.Second)}
	coordinator := newTestExtractionBarrierCoordinator(t, reader, scheduler)

	result, err := coordinator.Await(
		context.Background(),
		testExtractionBarrierRequest(cutoff),
	)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if result.Status != ExtractionBarrierReady || result.Waited != 0 ||
		reader.calls != 1 || scheduler.waits != 0 {
		t.Fatalf("unexpected immediate result: %#v", result)
	}
}

func TestExtractionBarrierCoordinatorRechecksDatabaseUntilCompleted(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	sourceCompletedAt := cutoff.Add(-time.Second)
	reader := &scriptedExtractionBarrierReader{snapshots: []ExtractionBarrierSnapshot{
		{
			Cutoff: cutoff, JobCount: 1, PendingCount: 1,
			LatestSourceCompletedAt:              sourceCompletedAt,
			EarliestNonTerminalSourceCompletedAt: sourceCompletedAt,
		},
		{
			Cutoff: cutoff, JobCount: 1, CompletedCount: 1,
			LatestSourceCompletedAt: sourceCompletedAt,
		},
	}}
	scheduler := &fakeExtractionBarrierScheduler{now: cutoff.Add(time.Second)}
	coordinator := newTestExtractionBarrierCoordinator(t, reader, scheduler)

	result, err := coordinator.Await(
		context.Background(),
		testExtractionBarrierRequest(cutoff),
	)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if result.Status != ExtractionBarrierWaited ||
		result.Waited != 100*time.Millisecond ||
		!result.CoveredThrough.Equal(sourceCompletedAt) ||
		reader.calls != 2 || scheduler.waits != 1 {
		t.Fatalf("unexpected waited result: %#v", result)
	}
}

func TestExtractionBarrierCoordinatorFailsExplicitly(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	sourceCompletedAt := cutoff.Add(-time.Second)
	tests := map[string]struct {
		snapshot ExtractionBarrierSnapshot
		readErr  error
		wantErr  error
	}{
		"failed": {
			snapshot: ExtractionBarrierSnapshot{
				Cutoff: cutoff, JobCount: 1, FailedCount: 1,
				LatestSourceCompletedAt: sourceCompletedAt,
			},
			wantErr: ErrExtractionBarrierUnavailable,
		},
		"discarded": {
			snapshot: ExtractionBarrierSnapshot{
				Cutoff: cutoff, JobCount: 1, DiscardedCount: 1,
				LatestSourceCompletedAt: sourceCompletedAt,
			},
			wantErr: ErrExtractionBarrierRejected,
		},
		"repository": {
			readErr: ErrRepository,
			wantErr: ErrExtractionBarrierUnavailable,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &scriptedExtractionBarrierReader{
				snapshots: []ExtractionBarrierSnapshot{test.snapshot},
				err:       test.readErr,
			}
			coordinator := newTestExtractionBarrierCoordinator(
				t,
				reader,
				&fakeExtractionBarrierScheduler{now: cutoff.Add(time.Second)},
			)
			_, err := coordinator.Await(
				context.Background(),
				testExtractionBarrierRequest(cutoff),
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Await error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestExtractionBarrierCoordinatorTimesOutAtBudget(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	sourceCompletedAt := cutoff.Add(-time.Second)
	reader := &scriptedExtractionBarrierReader{repeat: ExtractionBarrierSnapshot{
		Cutoff: cutoff, JobCount: 1, RunningCount: 1,
		LatestSourceCompletedAt:              sourceCompletedAt,
		EarliestNonTerminalSourceCompletedAt: sourceCompletedAt,
	}}
	scheduler := &fakeExtractionBarrierScheduler{now: cutoff.Add(time.Second)}
	coordinator := newTestExtractionBarrierCoordinator(t, reader, scheduler)

	_, err := coordinator.Await(
		context.Background(),
		testExtractionBarrierRequest(cutoff),
	)
	if !errors.Is(err, ErrExtractionBarrierUnavailable) {
		t.Fatalf("Await error = %v", err)
	}
	if elapsed := scheduler.now.Sub(cutoff.Add(time.Second)); elapsed != time.Second {
		t.Fatalf("elapsed = %s, want 1s", elapsed)
	}
}

func newTestExtractionBarrierCoordinator(
	t *testing.T,
	reader ExtractionBarrierReader,
	scheduler ExtractionBarrierScheduler,
) *ExtractionBarrierCoordinator {
	t.Helper()
	coordinator, err := NewExtractionBarrierCoordinator(
		reader,
		scheduler,
		ExtractionBarrierWaitPolicy{
			MaximumWait:  time.Second,
			PollInterval: 100 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("NewExtractionBarrierCoordinator: %v", err)
	}
	return coordinator
}

func testExtractionBarrierRequest(cutoff time.Time) ExtractionBarrierRequest {
	return ExtractionBarrierRequest{
		Actor: requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		Cutoff: cutoff,
	}
}

type scriptedExtractionBarrierReader struct {
	snapshots []ExtractionBarrierSnapshot
	repeat    ExtractionBarrierSnapshot
	err       error
	calls     int
}

func (reader *scriptedExtractionBarrierReader) ReadExtractionBarrier(
	context.Context,
	ExtractionBarrierRequest,
) (ExtractionBarrierSnapshot, error) {
	reader.calls++
	if reader.err != nil {
		return ExtractionBarrierSnapshot{}, reader.err
	}
	if len(reader.snapshots) > 0 {
		result := reader.snapshots[0]
		reader.snapshots = reader.snapshots[1:]
		return result, nil
	}
	return reader.repeat, nil
}

type fakeExtractionBarrierScheduler struct {
	now    time.Time
	waits  int
	onWait func()
}

func (scheduler *fakeExtractionBarrierScheduler) Now() time.Time {
	return scheduler.now
}

func (scheduler *fakeExtractionBarrierScheduler) Wait(
	_ context.Context,
	duration time.Duration,
) error {
	scheduler.waits++
	if scheduler.onWait != nil {
		scheduler.onWait()
	}
	scheduler.now = scheduler.now.Add(duration)
	return nil
}
