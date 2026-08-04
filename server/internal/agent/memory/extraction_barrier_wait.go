package memory

import (
	"context"
	"time"
)

const ExtractionBarrierPolicyV1 = "memory-extraction-barrier-v1"

type ExtractionBarrierResultStatus string

const (
	ExtractionBarrierReady  ExtractionBarrierResultStatus = "ready"
	ExtractionBarrierWaited ExtractionBarrierResultStatus = "waited"
)

type ExtractionBarrierResult struct {
	PolicyVersion  string
	Cutoff         time.Time
	Status         ExtractionBarrierResultStatus
	Waited         time.Duration
	CoveredThrough time.Time
}

func (result ExtractionBarrierResult) Valid() bool {
	if result.PolicyVersion != ExtractionBarrierPolicyV1 ||
		result.Cutoff.IsZero() ||
		result.Cutoff.Location() != time.UTC ||
		result.Waited < 0 ||
		(!result.CoveredThrough.IsZero() &&
			(result.CoveredThrough.Location() != time.UTC ||
				result.CoveredThrough.After(result.Cutoff))) {
		return false
	}
	switch result.Status {
	case ExtractionBarrierReady:
		return result.Waited == 0
	case ExtractionBarrierWaited:
		return result.Waited > 0 && !result.CoveredThrough.IsZero()
	default:
		return false
	}
}

type ExtractionBarrierWaitPolicy struct {
	MaximumWait  time.Duration
	PollInterval time.Duration
}

func (policy ExtractionBarrierWaitPolicy) Valid() bool {
	return policy.MaximumWait > 0 &&
		policy.MaximumWait <= 30*time.Second &&
		policy.PollInterval > 0 &&
		policy.PollInterval <= policy.MaximumWait
}

type ExtractionBarrierScheduler interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

type SystemExtractionBarrierScheduler struct{}

func (SystemExtractionBarrierScheduler) Now() time.Time {
	return time.Now().UTC()
}

func (SystemExtractionBarrierScheduler) Wait(
	ctx context.Context,
	duration time.Duration,
) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type ExtractionBarrierCoordinator struct {
	reader    ExtractionBarrierReader
	scheduler ExtractionBarrierScheduler
	policy    ExtractionBarrierWaitPolicy
}

func NewExtractionBarrierCoordinator(
	reader ExtractionBarrierReader,
	scheduler ExtractionBarrierScheduler,
	policy ExtractionBarrierWaitPolicy,
) (*ExtractionBarrierCoordinator, error) {
	if reader == nil || scheduler == nil || !policy.Valid() {
		return nil, ErrInvalidArgument
	}
	return &ExtractionBarrierCoordinator{
		reader:    reader,
		scheduler: scheduler,
		policy:    policy,
	}, nil
}

func (coordinator *ExtractionBarrierCoordinator) Await(
	ctx context.Context,
	request ExtractionBarrierRequest,
) (ExtractionBarrierResult, error) {
	if ctx == nil || !request.Valid() {
		return ExtractionBarrierResult{}, ErrInvalidArgument
	}
	startedAt := coordinator.scheduler.Now().UTC()
	if startedAt.IsZero() {
		return ExtractionBarrierResult{}, ErrExtractionBarrierUnavailable
	}
	deadline := startedAt.Add(coordinator.policy.MaximumWait)
	waited := false
	for {
		snapshot, err := coordinator.reader.ReadExtractionBarrier(ctx, request)
		if err != nil || !snapshot.Valid() {
			return ExtractionBarrierResult{}, ErrExtractionBarrierUnavailable
		}
		switch {
		case snapshot.DiscardedCount > 0:
			return ExtractionBarrierResult{}, ErrExtractionBarrierRejected
		case snapshot.FailedCount > 0:
			return ExtractionBarrierResult{}, ErrExtractionBarrierUnavailable
		case snapshot.PendingCount == 0 && snapshot.RunningCount == 0:
			if waited && coordinator.scheduler.Now().UTC().After(deadline) {
				return ExtractionBarrierResult{}, ErrExtractionBarrierUnavailable
			}
			result := ExtractionBarrierResult{
				PolicyVersion:  ExtractionBarrierPolicyV1,
				Cutoff:         request.Cutoff,
				Status:         ExtractionBarrierReady,
				CoveredThrough: snapshot.LatestSourceCompletedAt,
			}
			if waited {
				result.Status = ExtractionBarrierWaited
				result.Waited = coordinator.scheduler.Now().UTC().Sub(startedAt)
			}
			if !result.Valid() {
				return ExtractionBarrierResult{}, ErrExtractionBarrierUnavailable
			}
			return result, nil
		}

		remaining := deadline.Sub(coordinator.scheduler.Now().UTC())
		if remaining <= 0 {
			return ExtractionBarrierResult{}, ErrExtractionBarrierUnavailable
		}
		pause := min(coordinator.policy.PollInterval, remaining)
		if err := coordinator.scheduler.Wait(ctx, pause); err != nil {
			return ExtractionBarrierResult{}, ErrExtractionBarrierUnavailable
		}
		waited = true
	}
}
