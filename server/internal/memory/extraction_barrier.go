package memory

import (
	"context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type ExtractionBarrierRequest struct {
	Actor  requestcontext.Actor
	Cutoff time.Time
}

func (request ExtractionBarrierRequest) Valid() bool {
	return request.Actor.Valid() &&
		!request.Cutoff.IsZero() &&
		request.Cutoff.Location() == time.UTC
}

type ExtractionBarrierSnapshot struct {
	Cutoff                               time.Time
	JobCount                             int64
	PendingCount                         int64
	RunningCount                         int64
	CompletedCount                       int64
	FailedCount                          int64
	DiscardedCount                       int64
	LatestSourceCompletedAt              time.Time
	EarliestNonTerminalSourceCompletedAt time.Time
}

func (snapshot ExtractionBarrierSnapshot) Valid() bool {
	if snapshot.Cutoff.IsZero() ||
		snapshot.Cutoff.Location() != time.UTC ||
		snapshot.JobCount < 0 ||
		snapshot.PendingCount < 0 ||
		snapshot.RunningCount < 0 ||
		snapshot.CompletedCount < 0 ||
		snapshot.FailedCount < 0 ||
		snapshot.DiscardedCount < 0 ||
		snapshot.JobCount != snapshot.PendingCount+
			snapshot.RunningCount+
			snapshot.CompletedCount+
			snapshot.FailedCount+
			snapshot.DiscardedCount {
		return false
	}
	if snapshot.JobCount == 0 {
		return snapshot.LatestSourceCompletedAt.IsZero() &&
			snapshot.EarliestNonTerminalSourceCompletedAt.IsZero()
	}
	if !validBarrierTimestamp(
		snapshot.LatestSourceCompletedAt,
		snapshot.Cutoff,
	) {
		return false
	}
	nonTerminalCount := snapshot.PendingCount + snapshot.RunningCount
	if nonTerminalCount == 0 {
		return snapshot.EarliestNonTerminalSourceCompletedAt.IsZero()
	}
	return validBarrierTimestamp(
		snapshot.EarliestNonTerminalSourceCompletedAt,
		snapshot.LatestSourceCompletedAt,
	)
}

func (snapshot ExtractionBarrierSnapshot) Ready() bool {
	return snapshot.Valid() &&
		snapshot.PendingCount == 0 &&
		snapshot.RunningCount == 0
}

func validBarrierTimestamp(value time.Time, maximum time.Time) bool {
	return !value.IsZero() &&
		value.Location() == time.UTC &&
		!value.After(maximum)
}

type ExtractionBarrierReader interface {
	ReadExtractionBarrier(
		context.Context,
		ExtractionBarrierRequest,
	) (ExtractionBarrierSnapshot, error)
}
