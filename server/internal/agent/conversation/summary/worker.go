package summary

import (
	"context"
	"errors"
	"math"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

const maxSummarySweepLimit = 20

type Worker struct {
	jobs          JobRepository
	checkpoints   CheckpointRepository
	generator     CheckpointGenerator
	configuration WorkerConfiguration
}

func NewWorker(
	jobs JobRepository,
	checkpoints CheckpointRepository,
	generator CheckpointGenerator,
	configuration WorkerConfiguration,
) (*Worker, error) {
	if jobs == nil ||
		checkpoints == nil ||
		generator == nil ||
		!configuration.Valid() {
		return nil, ErrInvalidArgument
	}
	return &Worker{
		jobs:          jobs,
		checkpoints:   checkpoints,
		generator:     generator,
		configuration: configuration,
	}, nil
}

func (worker *Worker) ProcessPending(
	ctx context.Context,
	limit int,
) (SweepResult, error) {
	if ctx == nil || limit < 1 || limit > maxSummarySweepLimit {
		return SweepResult{}, ErrInvalidArgument
	}
	var result SweepResult
	for result.Claimed < limit {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claim, acquired, err := worker.jobs.ClaimJob(
			ctx,
			worker.configuration,
		)
		if err != nil {
			return result, err
		}
		if !acquired {
			return result, nil
		}
		result.Claimed++
		status, err := worker.processClaim(ctx, claim)
		if err != nil {
			return result, err
		}
		switch status {
		case JobCompleted:
			result.Completed++
		case JobPending:
			result.Retried++
		case JobSkipped:
			result.Skipped++
		case JobSuperseded:
			result.Superseded++
		case JobFailed:
			result.Failed++
		default:
			return result, conversation.ErrRepository
		}
	}
	return result, nil
}

func (worker *Worker) processClaim(
	ctx context.Context,
	claim JobClaim,
) (JobStatus, error) {
	previous, err := worker.checkpoints.FindLatestCheckpoint(
		ctx,
		claim.OwnerID,
		claim.ThreadID,
		math.MaxInt64,
	)
	hasPrevious := err == nil
	if err != nil && !errors.Is(err, conversation.ErrNotFound) {
		return worker.recordFailure(ctx, claim, 0, err)
	}
	previousCoverage := int64(0)
	if hasPrevious {
		previousCoverage = previous.CoveredThroughSequence
	}
	if previousCoverage >= claim.ObservedThroughSequence {
		return worker.finish(
			ctx,
			claim,
			JobSuperseded,
			claim.ObservedThroughSequence,
			"already_covered",
		)
	}
	uncoveredMessages := claim.ObservedThroughSequence - previousCoverage
	if uncoveredMessages < worker.configuration.TriggerMessages {
		return worker.finish(
			ctx,
			claim,
			JobSkipped,
			0,
			"below_threshold",
		)
	}

	target := claim.ObservedThroughSequence -
		worker.configuration.RetainRecentMessages
	maxTarget := int64(math.MaxInt64)
	if previousCoverage <=
		math.MaxInt64-int64(MaxSourceMessages) {
		maxTarget = previousCoverage +
			int64(MaxSourceMessages)
	}
	if target > maxTarget {
		target = maxTarget
	}
	if target <= previousCoverage {
		return worker.finish(
			ctx,
			claim,
			JobSuperseded,
			target,
			"no_new_coverage",
		)
	}

	checkpoint, err := worker.generator.GenerateCheckpoint(
		ctx,
		GenerateCheckpointCommand{
			OwnerID:                claim.OwnerID,
			ThreadID:               claim.ThreadID,
			CoveredThroughSequence: target,
		},
	)
	if err != nil {
		if errors.Is(err, conversation.ErrConflict) {
			latest, latestErr := worker.checkpoints.FindLatestCheckpoint(
				ctx,
				claim.OwnerID,
				claim.ThreadID,
				math.MaxInt64,
			)
			if latestErr == nil &&
				latest.CoveredThroughSequence >= target {
				return worker.finish(
					ctx,
					claim,
					JobSuperseded,
					target,
					"concurrently_covered",
				)
			}
			if latestErr != nil &&
				!errors.Is(latestErr, conversation.ErrNotFound) {
				return worker.recordFailure(
					ctx,
					claim,
					target,
					latestErr,
				)
			}
		}
		return worker.recordFailure(ctx, claim, target, err)
	}
	if !checkpoint.Valid() ||
		checkpoint.OwnerID != claim.OwnerID ||
		checkpoint.ThreadID != claim.ThreadID ||
		checkpoint.CoveredThroughSequence != target {
		return worker.recordFailure(
			ctx,
			claim,
			target,
			ErrInvalidResponse,
		)
	}
	job, err := worker.jobs.CompleteJob(
		ctx,
		claim,
		target,
		checkpoint,
	)
	if err != nil {
		return "", err
	}
	return job.Status, nil
}

func (worker *Worker) finish(
	ctx context.Context,
	claim JobClaim,
	status JobStatus,
	target int64,
	reason string,
) (JobStatus, error) {
	job, err := worker.jobs.FinishJob(
		ctx,
		claim,
		status,
		target,
		reason,
	)
	if err != nil {
		return "", err
	}
	return job.Status, nil
}

func (worker *Worker) recordFailure(
	ctx context.Context,
	claim JobClaim,
	target int64,
	cause error,
) (JobStatus, error) {
	kind, retryable := summaryJobFailure(cause)
	job, err := worker.jobs.FailJob(
		ctx,
		claim,
		target,
		kind,
		retryable,
		worker.configuration,
	)
	if err != nil {
		return "", err
	}
	return job.Status, nil
}

func summaryJobFailure(cause error) (string, bool) {
	switch {
	case errors.Is(cause, context.Canceled):
		return "canceled", true
	case errors.Is(cause, context.DeadlineExceeded):
		return "timeout", true
	case errors.Is(cause, ErrInvalidArgument),
		errors.Is(cause, conversation.ErrInvalidRequest):
		return "invalid_argument", false
	case errors.Is(cause, ErrInvalidResponse):
		return "invalid_response", false
	case errors.Is(cause, conversation.ErrNotFound):
		return "source_not_found", false
	case errors.Is(cause, conversation.ErrConflict):
		return "concurrent_update", true
	}
	var generationError GenerationFailure
	if errors.As(cause, &generationError) {
		kind := generationError.StableCategory()
		if !summaryJobFailurePattern.MatchString(kind) {
			kind = "provider_error"
		}
		return kind, generationError.Retryable()
	}
	return "dependency", true
}

var _ Processor = (*Worker)(nil)
