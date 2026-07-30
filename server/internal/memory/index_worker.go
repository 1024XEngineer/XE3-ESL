package memory

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const maxIndexSweepLimit = 20

type IndexWorker struct {
	repository IndexRepository
	embedder   ai.Embedder
	config     IndexConfig
}

func NewIndexWorker(
	repository IndexRepository,
	embedder ai.Embedder,
	configuration IndexConfig,
) (*IndexWorker, error) {
	if repository == nil || embedder == nil || !configuration.Valid() {
		return nil, ErrInvalidArgument
	}
	return &IndexWorker{
		repository: repository,
		embedder:   embedder,
		config:     configuration,
	}, nil
}

func (worker *IndexWorker) ProcessPendingIndexes(
	ctx context.Context,
	limit int,
) (IndexSweepResult, error) {
	if ctx == nil || limit < 1 || limit > maxIndexSweepLimit {
		return IndexSweepResult{}, ErrInvalidArgument
	}
	var result IndexSweepResult
	for result.Claimed < limit {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claim, acquired, err := worker.repository.ClaimIndex(
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
		outcome, err := worker.processClaim(ctx, claim)
		if err != nil {
			return result, err
		}
		switch outcome {
		case IndexCompleted:
			result.Completed++
		case IndexPending:
			result.Retried++
		case IndexFailed:
			result.Failed++
		case IndexDiscarded:
			result.Discarded++
		default:
			return result, ErrRepository
		}
	}
	return result, nil
}

func (worker *IndexWorker) processClaim(
	ctx context.Context,
	claim IndexClaim,
) (IndexStatus, error) {
	source, err := worker.repository.ReadIndexSource(ctx, claim)
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	result, err := worker.embedder.Embed(ctx, ai.EmbeddingRequest{
		Inputs:     []string{source.Content},
		Dimensions: worker.config.Dimensions,
	})
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if result.Provider != claim.Provider ||
		result.Model != claim.Model ||
		result.Dimensions != claim.Dimensions {
		return worker.recordFailure(ctx, claim, ErrIndexResponse)
	}
	job, err := worker.repository.CompleteIndex(ctx, claim, result)
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	return job.Status, nil
}

func (worker *IndexWorker) recordFailure(
	ctx context.Context,
	claim IndexClaim,
	cause error,
) (IndexStatus, error) {
	kind, retryable, discard := indexFailure(cause)
	if discard {
		job, err := worker.repository.DiscardIndex(ctx, claim, kind)
		if err != nil {
			return "", err
		}
		return job.Status, nil
	}
	job, err := worker.repository.FailIndex(
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

type stableProviderFailure interface {
	StableCategory() string
	Retryable() bool
}

func indexFailure(cause error) (
	kind string,
	retryable bool,
	discard bool,
) {
	switch {
	case errors.Is(cause, ErrAccountDeleted):
		return "account_deleted", false, true
	case errors.Is(cause, ErrNotFound):
		return "superseded", false, true
	case errors.Is(cause, ErrIndexResponse),
		errors.Is(cause, ErrInvalidArgument):
		return "invalid_response", false, false
	case errors.Is(cause, context.Canceled):
		return "canceled", true, false
	case errors.Is(cause, context.DeadlineExceeded):
		return "timeout", true, false
	}
	var providerError stableProviderFailure
	if errors.As(cause, &providerError) {
		kind := providerError.StableCategory()
		if !stableFailurePattern.MatchString(kind) {
			kind = "provider_error"
		}
		return kind, providerError.Retryable(), false
	}
	return "dependency", true, false
}

var _ IndexProcessor = (*IndexWorker)(nil)
