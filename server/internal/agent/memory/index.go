package memory

import (
	"context"
	"time"
)

const MemoryEmbeddingDimensions = 1024

type IndexStatus string

const (
	IndexPending   IndexStatus = "pending"
	IndexRunning   IndexStatus = "running"
	IndexCompleted IndexStatus = "completed"
	IndexFailed    IndexStatus = "failed"
	IndexDiscarded IndexStatus = "discarded"
)

func (status IndexStatus) Valid() bool {
	switch status {
	case IndexPending, IndexRunning, IndexCompleted, IndexFailed, IndexDiscarded:
		return true
	default:
		return false
	}
}

type IndexConfig struct {
	Provider      string
	Model         string
	Dimensions    int
	PolicyVersion string
	LeaseDuration time.Duration
	MaxAttempts   int
}

func (configuration IndexConfig) Valid() bool {
	return providerIdentifierPattern.MatchString(configuration.Provider) &&
		modelIdentifierPattern.MatchString(configuration.Model) &&
		configuration.Dimensions == MemoryEmbeddingDimensions &&
		validPolicyVersion(configuration.PolicyVersion) &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10
}

type IndexJob struct {
	MemoryID       string
	OwnerID        string
	MemoryVersion  int64
	Status         IndexStatus
	AttemptCount   int
	LeaseToken     string
	LeaseExpiresAt time.Time
	NextAttemptAt  time.Time
	PolicyVersion  string
	Provider       string
	Model          string
	Dimensions     int
	FailureKind    string
	InputTokens    int
	TotalTokens    int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    time.Time
}

type IndexClaim struct {
	IndexJob
}

func (claim IndexClaim) Valid() bool {
	return validUUID(claim.MemoryID) &&
		validUUID(claim.OwnerID) &&
		claim.MemoryVersion > 0 &&
		claim.Status == IndexRunning &&
		claim.AttemptCount > 0 &&
		validUUID(claim.LeaseToken) &&
		!claim.LeaseExpiresAt.IsZero() &&
		validPolicyVersion(claim.PolicyVersion) &&
		providerIdentifierPattern.MatchString(claim.Provider) &&
		modelIdentifierPattern.MatchString(claim.Model) &&
		claim.Dimensions == MemoryEmbeddingDimensions
}

type IndexSource struct {
	MemoryID string
	OwnerID  string
	Version  int64
	Content  string
}

func (source IndexSource) Valid() bool {
	return validUUID(source.MemoryID) &&
		validUUID(source.OwnerID) &&
		source.Version > 0 &&
		validContent(source.Content)
}

type IndexRepository interface {
	ClaimIndex(context.Context, IndexConfig) (IndexClaim, bool, error)
	ReadIndexSource(context.Context, IndexClaim) (IndexSource, error)
	CompleteIndex(
		context.Context,
		IndexClaim,
		EmbeddingResult,
	) (IndexJob, error)
	FailIndex(
		context.Context,
		IndexClaim,
		string,
		bool,
		IndexConfig,
	) (IndexJob, error)
	DiscardIndex(
		context.Context,
		IndexClaim,
		string,
	) (IndexJob, error)
}

type IndexSweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
	Discarded int
}

type IndexProcessor interface {
	ProcessPendingIndexes(context.Context, int) (IndexSweepResult, error)
}
