package memory

import (
	"context"
	"regexp"
	"time"
)

const maxExtractionCandidates = 5

var stableFailurePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type ExtractionStatus string

const (
	ExtractionPending   ExtractionStatus = "pending"
	ExtractionRunning   ExtractionStatus = "running"
	ExtractionCompleted ExtractionStatus = "completed"
	ExtractionFailed    ExtractionStatus = "failed"
	ExtractionDiscarded ExtractionStatus = "discarded"
)

func (status ExtractionStatus) Valid() bool {
	switch status {
	case ExtractionPending,
		ExtractionRunning,
		ExtractionCompleted,
		ExtractionFailed,
		ExtractionDiscarded:
		return true
	default:
		return false
	}
}

type ExtractionConfig struct {
	Provider      string
	Model         string
	PolicyVersion string
	PromptVersion string
	LeaseDuration time.Duration
	TopicTTL      time.Duration
	MaxAttempts   int
}

func (configuration ExtractionConfig) Valid() bool {
	return providerIdentifierPattern.MatchString(configuration.Provider) &&
		modelIdentifierPattern.MatchString(configuration.Model) &&
		validPolicyVersion(configuration.PolicyVersion) &&
		validPolicyVersion(configuration.PromptVersion) &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.TopicTTL >= 24*time.Hour &&
		configuration.TopicTTL <= 365*24*time.Hour &&
		configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10
}

var (
	providerIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	modelIdentifierPattern    = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
)

type ExtractionJob struct {
	RunID              string
	OwnerID            string
	ThreadID           string
	InputMessageID     string
	AssistantMessageID string
	SourceAttempt      int
	SourceCompletedAt  time.Time
	Status             ExtractionStatus
	AttemptCount       int
	LeaseToken         string
	LeaseExpiresAt     time.Time
	NextAttemptAt      time.Time
	PolicyVersion      string
	PromptVersion      string
	Provider           string
	Model              string
	CandidateCount     int
	AppliedCount       int
	RejectedCount      int
	FailureKind        string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	CompletedAt        time.Time
}

type ExtractionClaim struct {
	ExtractionJob
}

func (claim ExtractionClaim) Valid() bool {
	return validUUID(claim.RunID) &&
		validUUID(claim.OwnerID) &&
		validUUID(claim.ThreadID) &&
		validUUID(claim.InputMessageID) &&
		validUUID(claim.AssistantMessageID) &&
		claim.SourceAttempt > 0 &&
		claim.Status == ExtractionRunning &&
		claim.AttemptCount > 0 &&
		validUUID(claim.LeaseToken) &&
		!claim.LeaseExpiresAt.IsZero() &&
		validPolicyVersion(claim.PolicyVersion) &&
		validPolicyVersion(claim.PromptVersion) &&
		providerIdentifierPattern.MatchString(claim.Provider) &&
		modelIdentifierPattern.MatchString(claim.Model)
}

type CompletedRunSource struct {
	OwnerID            string
	RunID              string
	ThreadID           string
	InputMessageID     string
	AssistantMessageID string
	MatterID           string
	UserText           string
	AssistantText      string
	Attempt            int
	CompletedAt        time.Time
}

func (source CompletedRunSource) Valid() bool {
	return validUUID(source.OwnerID) &&
		validUUID(source.RunID) &&
		validUUID(source.ThreadID) &&
		validUUID(source.InputMessageID) &&
		validUUID(source.AssistantMessageID) &&
		(source.MatterID == "" || validUUID(source.MatterID)) &&
		validContent(source.UserText) &&
		validContent(source.AssistantText) &&
		source.Attempt > 0 &&
		!source.CompletedAt.IsZero()
}

type CompletedRunReader interface {
	ReadCompletedRun(
		context.Context,
		string,
		string,
	) (CompletedRunSource, error)
}

type CandidateAction string

const (
	CandidateUpsert     CandidateAction = "upsert"
	CandidateInactivate CandidateAction = "inactivate"
)

func (action CandidateAction) Valid() bool {
	return action == CandidateUpsert || action == CandidateInactivate
}

type ExtractedCandidate struct {
	Action         CandidateAction `json:"action"`
	Type           Type            `json:"type"`
	CanonicalKey   string          `json:"canonical_key"`
	Content        string          `json:"content"`
	Scope          ScopeType       `json:"scope"`
	Evidence       string          `json:"evidence"`
	InteractionUse bool            `json:"interaction_use"`
}

type ExtractionOutput struct {
	Candidates []ExtractedCandidate `json:"candidates"`
}

type CandidateExtractor interface {
	Extract(
		context.Context,
		CompletedRunSource,
	) (ExtractionOutput, error)
}

type MemoryDecision struct {
	Action       CandidateAction
	Type         Type
	CanonicalKey string
	Content      string
	Scope        ScopeType
	MatterID     string
	ExpiresAt    *time.Time
}

func (decision MemoryDecision) Valid() bool {
	if !decision.Action.Valid() ||
		!decision.Type.Valid() ||
		!validCanonicalKey(decision.CanonicalKey) ||
		!validScope(decision.Scope, decision.MatterID) ||
		!validOptionalTime(decision.ExpiresAt) {
		return false
	}
	if decision.Action == CandidateUpsert {
		return validContent(decision.Content)
	}
	return decision.Content == ""
}

type ExtractionBatch struct {
	CandidateCount int
	Decisions      []MemoryDecision
	Source         SourceInput
}

func (batch ExtractionBatch) Valid() bool {
	if batch.CandidateCount < 0 ||
		batch.CandidateCount > maxExtractionCandidates ||
		len(batch.Decisions) > batch.CandidateCount ||
		!batch.Source.Valid() {
		return false
	}
	for _, decision := range batch.Decisions {
		if !decision.Valid() {
			return false
		}
	}
	return true
}

type ExtractionRepository interface {
	ClaimExtraction(
		context.Context,
		ExtractionConfig,
	) (ExtractionClaim, bool, error)
	CompleteExtraction(
		context.Context,
		ExtractionClaim,
		ExtractionBatch,
	) (ExtractionJob, error)
	FailExtraction(
		context.Context,
		ExtractionClaim,
		string,
		bool,
		ExtractionConfig,
	) (ExtractionJob, error)
	DiscardExtraction(
		context.Context,
		ExtractionClaim,
		string,
	) (ExtractionJob, error)
}

type ExtractionSweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
	Discarded int
}

type ExtractionProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (ExtractionSweepResult, error)
}
