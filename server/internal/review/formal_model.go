package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type FormalReviewStatus string

const (
	FormalReviewPending    FormalReviewStatus = "pending"
	FormalReviewGenerating FormalReviewStatus = "generating"
	FormalReviewCompleted  FormalReviewStatus = "completed"
	FormalReviewFailed     FormalReviewStatus = "failed"

	SourceTypeConversationTurn = "conversation_turn"
)

var (
	ErrInvalidReview                = errors.New("invalid review")
	ErrReviewNotFound               = errors.New("review not found")
	ErrAccountDeleted               = errors.New("review account data deleted")
	ErrGenerationFailed             = errors.New("review generation failed")
	ErrGenerationClaimLost          = errors.New("review generation claim lost")
	ErrEvidenceRequired             = errors.New("review evidence required")
	ErrReviewSourceConflict         = errors.New("review source conflict")
	ErrReviewImplementationConflict = errors.New("review implementation conflict")
	ErrDeletionGenerationStale      = errors.New("review deletion generation stale")
)

// Actor is the trusted identity projection consumed by Review. Delivery and
// composition layers must derive it from the authenticated request context.
type Actor struct {
	UserID             string
	DeletionGeneration int64
}

func (a Actor) validate() error {
	if !validUUID(a.UserID) || a.DeletionGeneration < 0 {
		return ErrInvalidReview
	}
	return nil
}

// FormalReview is the session-level authoritative Review owned by this module.
type FormalReview struct {
	ID                        string
	OwnerUserID               string
	PracticeSessionID         string
	ImplementationVersion     string
	SourceTurnID              string
	SourceTurnVersion         string
	SourceManifestFingerprint string
	DeletionGeneration        int64
	Status                    FormalReviewStatus
	Result                    *ReviewResult
	Evidence                  []ReviewEvidence
	StableErrorCategory       string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	CompletedAt               *time.Time
}

type ReviewResult struct {
	OverallScore int                `json:"overall_score"`
	Summary      string             `json:"summary"`
	Conclusions  []ReviewConclusion `json:"conclusions"`
}

type ReviewConclusion struct {
	Key        string `json:"key"`
	Category   string `json:"category"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// SourceObject is a minimal, immutable source snapshot supplied through the
// consumer-owned reader port. Snapshot must not contain a full foreign record.
type SourceObject struct {
	SourceType    string
	SourceID      string
	SourceVersion string
	Checksum      string
	Snapshot      json.RawMessage
}

type ReviewSourceSnapshot struct {
	PracticeSessionID   string
	SessionVersion      string
	SourceTurnID        string
	SourceTurnVersion   string
	ManifestFingerprint string
	Sources             []SourceObject
}

type EvidenceLink struct {
	ConclusionKey string
	SourceType    string
	SourceID      string
	SourceVersion string
}

type GeneratedReview struct {
	Result        ReviewResult
	EvidenceLinks []EvidenceLink
}

type ReviewEvidence struct {
	ID            string
	ReviewID      string
	OwnerUserID   string
	ConclusionKey string
	SourceType    string
	SourceID      string
	SourceVersion string
	Checksum      string
	Snapshot      json.RawMessage
	CreatedAt     time.Time
}

type GenerationAttempt struct {
	ID                  string
	ReviewID            string
	AttemptNumber       int
	Status              string
	StableErrorCategory string
	StartedAt           time.Time
	FinishedAt          *time.Time
}

// GenerationJobContext is minted from a persisted attempt and verified again
// on every worker write. It is distinct from an online Actor.
type GenerationJobContext struct {
	AttemptID          string
	ReviewID           string
	OwnerUserID        string
	WorkerToken        string
	DeletionGeneration int64
	AttemptNumber      int
	LeaseUntil         time.Time
}

type EnsureReviewCommand struct {
	Actor                     Actor
	PracticeSessionID         string
	ImplementationVersion     string
	SourceTurnID              string
	SourceTurnVersion         string
	SourceManifestFingerprint string
}

type DeleteUserReviewsCommand struct {
	UserID             string
	DeletionGeneration int64
}

func (c EnsureReviewCommand) validate() error {
	if err := c.Actor.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.PracticeSessionID) == "" ||
		strings.TrimSpace(c.ImplementationVersion) == "" ||
		strings.TrimSpace(c.SourceTurnID) == "" ||
		strings.TrimSpace(c.SourceTurnVersion) == "" ||
		strings.TrimSpace(c.SourceManifestFingerprint) == "" {
		return ErrInvalidReview
	}
	return nil
}

func validateSource(command EnsureReviewCommand, source ReviewSourceSnapshot) error {
	if source.PracticeSessionID != command.PracticeSessionID ||
		source.SourceTurnID != command.SourceTurnID ||
		source.SourceTurnVersion != command.SourceTurnVersion ||
		source.ManifestFingerprint != command.SourceManifestFingerprint ||
		strings.TrimSpace(source.SessionVersion) == "" ||
		len(source.Sources) == 0 {
		return ErrInvalidReview
	}

	seen := make(map[string]struct{}, len(source.Sources))
	triggerSourceFound := false
	for _, item := range source.Sources {
		if strings.TrimSpace(item.SourceType) == "" ||
			strings.TrimSpace(item.SourceID) == "" ||
			strings.TrimSpace(item.SourceVersion) == "" ||
			len(item.Snapshot) > 16*1024 ||
			(len(item.Snapshot) > 0 && !json.Valid(item.Snapshot)) {
			return ErrInvalidReview
		}
		key := sourceKey(item.SourceType, item.SourceID, item.SourceVersion)
		if _, exists := seen[key]; exists {
			return ErrInvalidReview
		}
		seen[key] = struct{}{}
		if item.SourceType == SourceTypeConversationTurn &&
			item.SourceID == command.SourceTurnID &&
			item.SourceVersion == command.SourceTurnVersion {
			triggerSourceFound = true
		}
	}
	if !triggerSourceFound {
		return ErrInvalidReview
	}
	return nil
}

func validateGenerated(
	source ReviewSourceSnapshot,
	generated GeneratedReview,
) ([]ReviewEvidence, error) {
	if err := validateReviewResult(generated.Result); err != nil {
		return nil, err
	}

	sourceByKey := make(map[string]SourceObject, len(source.Sources))
	for _, item := range source.Sources {
		sourceByKey[sourceKey(
			item.SourceType,
			item.SourceID,
			item.SourceVersion,
		)] = item
	}

	conclusions := make(map[string]struct{}, len(generated.Result.Conclusions))
	for _, conclusion := range generated.Result.Conclusions {
		key := strings.TrimSpace(conclusion.Key)
		if key == "" ||
			strings.TrimSpace(conclusion.Category) == "" ||
			strings.TrimSpace(conclusion.Message) == "" {
			return nil, ErrInvalidReview
		}
		if _, exists := conclusions[key]; exists {
			return nil, ErrInvalidReview
		}
		conclusions[key] = struct{}{}
	}

	evidence := make([]ReviewEvidence, 0, len(generated.EvidenceLinks))
	covered := make(map[string]bool, len(conclusions))
	seenLinks := make(map[string]struct{}, len(generated.EvidenceLinks))
	for _, link := range generated.EvidenceLinks {
		if _, exists := conclusions[link.ConclusionKey]; !exists {
			return nil, ErrInvalidReview
		}
		sourceItem, exists := sourceByKey[sourceKey(
			link.SourceType,
			link.SourceID,
			link.SourceVersion,
		)]
		if !exists {
			return nil, ErrInvalidReview
		}
		linkKey := link.ConclusionKey + "\x00" + sourceKey(
			link.SourceType,
			link.SourceID,
			link.SourceVersion,
		)
		if _, exists := seenLinks[linkKey]; exists {
			continue
		}
		seenLinks[linkKey] = struct{}{}
		covered[link.ConclusionKey] = true
		evidence = append(evidence, ReviewEvidence{
			ConclusionKey: link.ConclusionKey,
			SourceType:    sourceItem.SourceType,
			SourceID:      sourceItem.SourceID,
			SourceVersion: sourceItem.SourceVersion,
			Checksum:      sourceItem.Checksum,
			Snapshot:      append(json.RawMessage(nil), sourceItem.Snapshot...),
		})
	}

	for key := range conclusions {
		if !covered[key] {
			return nil, fmt.Errorf("%w: conclusion %q", ErrEvidenceRequired, key)
		}
	}
	return evidence, nil
}

func validateCompletionPayload(
	result ReviewResult,
	evidence []ReviewEvidence,
) error {
	if err := validateReviewResult(result); err != nil {
		return err
	}
	if len(evidence) == 0 {
		return ErrEvidenceRequired
	}

	conclusions := make(map[string]bool, len(result.Conclusions))
	for _, conclusion := range result.Conclusions {
		conclusions[conclusion.Key] = false
	}
	for _, item := range evidence {
		if strings.TrimSpace(item.ConclusionKey) == "" ||
			strings.TrimSpace(item.SourceType) == "" ||
			strings.TrimSpace(item.SourceID) == "" ||
			strings.TrimSpace(item.SourceVersion) == "" ||
			len(item.Snapshot) > 16*1024 ||
			(len(item.Snapshot) > 0 && !json.Valid(item.Snapshot)) {
			return ErrInvalidReview
		}
		if _, exists := conclusions[item.ConclusionKey]; !exists {
			return ErrInvalidReview
		}
		conclusions[item.ConclusionKey] = true
	}
	for key, covered := range conclusions {
		if !covered {
			return fmt.Errorf("%w: conclusion %q", ErrEvidenceRequired, key)
		}
	}
	return nil
}

func validateReviewResult(result ReviewResult) error {
	if result.OverallScore < 0 ||
		result.OverallScore > 100 ||
		strings.TrimSpace(result.Summary) == "" ||
		len(result.Conclusions) == 0 {
		return ErrInvalidReview
	}
	conclusions := make(map[string]struct{}, len(result.Conclusions))
	for _, conclusion := range result.Conclusions {
		key := strings.TrimSpace(conclusion.Key)
		if key == "" ||
			key != conclusion.Key ||
			strings.TrimSpace(conclusion.Category) == "" ||
			strings.TrimSpace(conclusion.Message) == "" {
			return ErrInvalidReview
		}
		if _, exists := conclusions[key]; exists {
			return ErrInvalidReview
		}
		conclusions[key] = struct{}{}
	}
	return nil
}

func validStableErrorCategory(category string) bool {
	if len(category) == 0 || len(category) > 64 {
		return false
	}
	for index, character := range category {
		if (character >= 'a' && character <= 'z') ||
			(index > 0 && character >= '0' && character <= '9') ||
			(index > 0 && character == '_') {
			continue
		}
		return false
	}
	return true
}

func sourceKey(sourceType, sourceID, sourceVersion string) string {
	return sourceType + "\x00" + sourceID + "\x00" + sourceVersion
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return false
			}
		default:
			if !((character >= '0' && character <= '9') ||
				(character >= 'a' && character <= 'f') ||
				(character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}
