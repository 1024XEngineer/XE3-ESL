package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
)

func TestExtractionBarrierRequestValid(t *testing.T) {
	t.Parallel()
	request := ExtractionBarrierRequest{
		Actor: requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		Cutoff: time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC),
	}
	if !request.Valid() {
		t.Fatal("valid Extraction Barrier request was rejected")
	}
	request.Cutoff = request.Cutoff.In(time.FixedZone("UTC+8", 8*60*60))
	if request.Valid() {
		t.Fatal("non-UTC Extraction Barrier cutoff was accepted")
	}
}

func TestExtractionBarrierSnapshotValidAndReady(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	latest := cutoff.Add(-time.Second)
	pending := cutoff.Add(-2 * time.Second)
	snapshot := ExtractionBarrierSnapshot{
		Cutoff:                               cutoff,
		JobCount:                             3,
		PendingCount:                         1,
		CompletedCount:                       1,
		FailedCount:                          1,
		LatestSourceCompletedAt:              latest,
		EarliestNonTerminalSourceCompletedAt: pending,
	}
	if !snapshot.Valid() {
		t.Fatal("valid Extraction Barrier snapshot was rejected")
	}
	if snapshot.Ready() {
		t.Fatal("snapshot with a pending job reported ready")
	}
	snapshot.PendingCount = 0
	snapshot.CompletedCount = 2
	snapshot.EarliestNonTerminalSourceCompletedAt = time.Time{}
	if !snapshot.Ready() {
		t.Fatal("terminal Extraction Barrier snapshot did not report ready")
	}
}

func TestExtractionBarrierSnapshotRejectsInconsistentState(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	valid := ExtractionBarrierSnapshot{
		Cutoff:                  cutoff,
		JobCount:                1,
		CompletedCount:          1,
		LatestSourceCompletedAt: cutoff.Add(-time.Second),
	}
	cases := map[string]func(ExtractionBarrierSnapshot) ExtractionBarrierSnapshot{
		"count mismatch": func(snapshot ExtractionBarrierSnapshot) ExtractionBarrierSnapshot {
			snapshot.JobCount++
			return snapshot
		},
		"missing latest timestamp": func(snapshot ExtractionBarrierSnapshot) ExtractionBarrierSnapshot {
			snapshot.LatestSourceCompletedAt = time.Time{}
			return snapshot
		},
		"latest after cutoff": func(snapshot ExtractionBarrierSnapshot) ExtractionBarrierSnapshot {
			snapshot.LatestSourceCompletedAt = cutoff.Add(time.Second)
			return snapshot
		},
		"unexpected non-terminal timestamp": func(snapshot ExtractionBarrierSnapshot) ExtractionBarrierSnapshot {
			snapshot.EarliestNonTerminalSourceCompletedAt = cutoff.Add(-time.Second)
			return snapshot
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if mutate(valid).Valid() {
				t.Fatalf("%s snapshot was accepted", name)
			}
		})
	}
}

func TestReadExtractionBarrierMapsDatabaseError(t *testing.T) {
	t.Parallel()
	repository := &PostgresRepository{database: barrierErrorDatabase{}}
	_, err := repository.ReadExtractionBarrier(
		context.Background(),
		ExtractionBarrierRequest{
			Actor: requestcontext.Actor{
				UserID:    "10000000-0000-4000-8000-000000000001",
				SessionID: "20000000-0000-4000-8000-000000000001",
			},
			Cutoff: time.Date(
				2026,
				time.July,
				29,
				10,
				0,
				0,
				0,
				time.UTC,
			),
		},
	)
	if !errors.Is(err, ErrRepository) {
		t.Fatalf("ReadExtractionBarrier error = %v", err)
	}
}

type barrierErrorDatabase struct{}

func (barrierErrorDatabase) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected Begin")
}

func (barrierErrorDatabase) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (barrierErrorDatabase) QueryRow(
	context.Context,
	string,
	...any,
) pgx.Row {
	return barrierErrorRow{}
}

type barrierErrorRow struct{}

func (barrierErrorRow) Scan(...any) error {
	return errors.New("database unavailable")
}
