package review

import (
	"context"
	"time"
)

// ReviewSourceReader is defined by Review and implemented by composition-layer
// adapters. It exposes only an authorized, confirmed, versioned snapshot.
type ReviewSourceReader interface {
	ReadReviewSource(
		ctx context.Context,
		actor Actor,
		practiceSessionID string,
	) (ReviewSourceSnapshot, error)
}

type ReviewGenerator interface {
	GenerateReview(
		ctx context.Context,
		input ReviewGenerationInput,
	) (GeneratedReview, error)
}

type ReviewGenerationInput struct {
	ReviewID              string
	ImplementationVersion string
	Source                ReviewSourceSnapshot
}

// ReviewRepository contains only Review-owned persistence operations.
type ReviewRepository interface {
	EnsurePending(
		ctx context.Context,
		command EnsureReviewCommand,
	) (FormalReview, error)
	ClaimGeneration(
		ctx context.Context,
		actor Actor,
		reviewID string,
		lease time.Duration,
	) (FormalReview, GenerationJobContext, bool, error)
	CompleteGeneration(
		ctx context.Context,
		job GenerationJobContext,
		result ReviewResult,
		evidence []ReviewEvidence,
	) (FormalReview, error)
	FailGeneration(
		ctx context.Context,
		job GenerationJobContext,
		stableErrorCategory string,
	) error
	Get(
		ctx context.Context,
		actor Actor,
		reviewID string,
	) (FormalReview, error)
	List(ctx context.Context, actor Actor) ([]FormalReview, error)
	ListAttempts(
		ctx context.Context,
		actor Actor,
		reviewID string,
	) ([]GenerationAttempt, error)
	DeleteUserData(
		ctx context.Context,
		command DeleteUserReviewsCommand,
	) error
}

type StableGenerationError interface {
	error
	StableCategory() string
}
