package memory

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const maxExtractionSweepLimit = 20

type Worker struct {
	repository ExtractionRepository
	sources    CompletedRunReader
	extractor  CandidateExtractor
	policy     *ExtractionPolicy
	config     ExtractionConfig
}

func NewWorker(
	repository ExtractionRepository,
	sources CompletedRunReader,
	extractor CandidateExtractor,
	policy *ExtractionPolicy,
	configuration ExtractionConfig,
) (*Worker, error) {
	if repository == nil ||
		sources == nil ||
		extractor == nil ||
		policy == nil ||
		!configuration.Valid() {
		return nil, ErrInvalidArgument
	}
	return &Worker{
		repository: repository,
		sources:    sources,
		extractor:  extractor,
		policy:     policy,
		config:     configuration,
	}, nil
}

func (worker *Worker) ProcessPending(
	ctx context.Context,
	limit int,
) (ExtractionSweepResult, error) {
	if ctx == nil || limit < 1 || limit > maxExtractionSweepLimit {
		return ExtractionSweepResult{}, ErrInvalidArgument
	}
	var result ExtractionSweepResult
	for result.Claimed < limit {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claim, acquired, err := worker.repository.ClaimExtraction(
			ctx,
			worker.config,
		)
		if err != nil {
			return result, err
		}
		if !acquired {
			return result, nil
		}
		result.Claimed++
		outcome, rejections, err := worker.processClaim(ctx, claim)
		result.Rejections = append(result.Rejections, rejections...)
		if err != nil {
			return result, err
		}
		switch outcome {
		case ExtractionCompleted:
			result.Completed++
		case ExtractionPending:
			result.Retried++
		case ExtractionFailed:
			result.Failed++
		case ExtractionDiscarded:
			result.Discarded++
		default:
			return result, ErrRepository
		}
	}
	return result, nil
}

func (worker *Worker) processClaim(
	ctx context.Context,
	claim ExtractionClaim,
) (ExtractionStatus, []CandidateRejectionEvent, error) {
	source, err := worker.sources.ReadCompletedRun(
		ctx,
		claim.OwnerID,
		claim.RunID,
	)
	if err != nil {
		status, failureErr := worker.recordFailure(ctx, claim, err)
		return status, nil, failureErr
	}
	if !sourceMatchesClaim(source, claim) {
		status, failureErr := worker.recordFailure(
			ctx,
			claim,
			ErrExtractionResponse,
		)
		return status, nil, failureErr
	}
	output, err := worker.extractor.Extract(ctx, source)
	if err != nil {
		status, failureErr := worker.recordFailure(ctx, claim, err)
		return status, nil, failureErr
	}
	batch, err := worker.policy.Decide(source, output)
	if err != nil {
		status, failureErr := worker.recordFailure(ctx, claim, err)
		return status, nil, failureErr
	}
	_, err = worker.repository.CompleteExtraction(ctx, claim, batch)
	if errors.Is(err, ErrAccountDeleted) {
		job, discardErr := worker.repository.DiscardExtraction(
			ctx,
			claim,
			"account_deleted",
		)
		if discardErr != nil {
			return "", nil, discardErr
		}
		return job.Status, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	rejections := make(
		[]CandidateRejectionEvent,
		0,
		len(batch.Rejections),
	)
	for _, rejection := range batch.Rejections {
		event := CandidateRejectionEvent{
			RunID:          claim.RunID,
			CandidateIndex: rejection.CandidateIndex,
			Reason:         rejection.Reason,
		}
		if !event.Valid() {
			return "", nil, ErrRepository
		}
		rejections = append(rejections, event)
	}
	return ExtractionCompleted, rejections, nil
}

func (worker *Worker) recordFailure(
	ctx context.Context,
	claim ExtractionClaim,
	cause error,
) (ExtractionStatus, error) {
	kind, retryable, discard := extractionFailure(cause)
	if discard {
		job, err := worker.repository.DiscardExtraction(
			ctx,
			claim,
			kind,
		)
		if err != nil {
			return "", err
		}
		return job.Status, nil
	}
	job, err := worker.repository.FailExtraction(
		ctx,
		claim,
		kind,
		retryable,
		worker.config,
	)
	if err != nil {
		return "", err
	}
	return job.Status, nil
}

func sourceMatchesClaim(
	source CompletedRunSource,
	claim ExtractionClaim,
) bool {
	return source.Valid() &&
		source.OwnerID == claim.OwnerID &&
		source.RunID == claim.RunID &&
		source.ThreadID == claim.ThreadID &&
		source.InputMessageID == claim.InputMessageID &&
		source.AssistantMessageID == claim.AssistantMessageID &&
		source.Attempt == claim.SourceAttempt &&
		source.CompletedAt.Equal(claim.SourceCompletedAt)
}

func extractionFailure(cause error) (
	kind string,
	retryable bool,
	discard bool,
) {
	switch {
	case errors.Is(cause, ErrAccountDeleted):
		return "account_deleted", false, true
	case errors.Is(cause, ErrNotFound):
		return "source_not_found", false, true
	case errors.Is(cause, ErrExtractionResponse),
		errors.Is(cause, ErrInvalidArgument):
		return "invalid_response", false, false
	case errors.Is(cause, context.Canceled):
		return "canceled", true, false
	case errors.Is(cause, context.DeadlineExceeded):
		return "timeout", true, false
	}
	var generationError *ai.GenerationError
	if errors.As(cause, &generationError) {
		kind := generationError.StableCategory()
		if !stableFailurePattern.MatchString(kind) {
			kind = "provider_error"
		}
		return kind, generationError.Retryable(), false
	}
	return "dependency", true, false
}

var _ ExtractionProcessor = (*Worker)(nil)
