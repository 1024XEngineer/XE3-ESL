package title

import (
	"context"
	"errors"
)

const maxSweepLimit = 20

type Worker struct {
	jobs          JobRepository
	generator     TitleGenerator
	configuration WorkerConfiguration
}

func NewWorker(
	jobs JobRepository,
	generator TitleGenerator,
	configuration WorkerConfiguration,
) (*Worker, error) {
	if jobs == nil || generator == nil || !configuration.Valid() {
		return nil, ErrInvalidArgument
	}
	return &Worker{
		jobs:          jobs,
		generator:     generator,
		configuration: configuration,
	}, nil
}

func (worker *Worker) ProcessPending(
	ctx context.Context,
	limit int,
) (SweepResult, error) {
	if ctx == nil || limit < 1 || limit > maxSweepLimit {
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
		title, err := worker.generator.GenerateTitle(ctx, claim)
		if err != nil {
			status, recordErr := worker.recordFailure(ctx, claim, err)
			if recordErr != nil {
				return result, recordErr
			}
			if status == JobPending {
				result.Retried++
			} else {
				result.Failed++
			}
			continue
		}
		job, err := worker.jobs.CompleteJob(ctx, claim, title)
		if err != nil {
			return result, err
		}
		if job.Status != JobCompleted {
			return result, ErrInvalidResponse
		}
		result.Completed++
	}
	return result, nil
}

func (worker *Worker) recordFailure(
	ctx context.Context,
	claim JobClaim,
	cause error,
) (JobStatus, error) {
	kind, retryable := jobFailure(cause)
	job, err := worker.jobs.FailJob(
		ctx,
		claim,
		kind,
		retryable,
		worker.configuration,
	)
	if err != nil {
		return "", err
	}
	return job.Status, nil
}

func jobFailure(cause error) (string, bool) {
	switch {
	case errors.Is(cause, context.Canceled):
		return "canceled", true
	case errors.Is(cause, context.DeadlineExceeded):
		return "timeout", true
	case errors.Is(cause, ErrInvalidArgument):
		return "invalid_argument", false
	case errors.Is(cause, ErrInvalidResponse):
		return "invalid_response", false
	}
	var generationError GenerationFailure
	if errors.As(cause, &generationError) {
		kind := generationError.StableCategory()
		if !failurePattern.MatchString(kind) {
			kind = "provider_error"
		}
		return kind, generationError.Retryable()
	}
	return "dependency", true
}

var _ Processor = (*Worker)(nil)
