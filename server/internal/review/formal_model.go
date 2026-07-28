package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type FormalReviewStatus string

const (
	FormalReviewPending    FormalReviewStatus = "pending"
	FormalReviewGenerating FormalReviewStatus = "generating"
	FormalReviewCompleted  FormalReviewStatus = "completed"
	FormalReviewFailed     FormalReviewStatus = "failed"

	SourceTypeConversationTurn = "conversation_turn"

	maxReviewResultJSONBytes      = 12 * 1024
	maxReviewSummaryUTF8Bytes     = 2048
	maxReviewConclusions          = 8
	maxReviewFeedbackItems        = 16
	maxReviewConclusionLabelBytes = 64
	maxReviewConclusionTextBytes  = 2048
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
	EvaluationContext         EvaluationContext
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
	SummaryEligibility          SummaryEligibility   `json:"summary_eligibility"`
	OverallScore                int                  `json:"overall_score"`
	Summary                     string               `json:"summary"`
	Conclusions                 []ReviewConclusion   `json:"conclusions"`
	FeedbackItems               []ReviewFeedbackItem `json:"feedback_items,omitempty"`
	RepracticeSuggestionRefs    []string             `json:"repractice_suggestion_refs,omitempty"`
	InsufficientEvidenceReasons []string             `json:"insufficient_evidence_reasons,omitempty"`
	legacyFormat                bool
}

func (r ReviewResult) MarshalJSON() ([]byte, error) {
	type wireResult struct {
		SummaryEligibility          SummaryEligibility   `json:"summary_eligibility,omitempty"`
		OverallScore                *int                 `json:"overall_score,omitempty"`
		Summary                     string               `json:"summary"`
		Conclusions                 []ReviewConclusion   `json:"conclusions"`
		FeedbackItems               []ReviewFeedbackItem `json:"feedback_items,omitempty"`
		RepracticeSuggestionRefs    []string             `json:"repractice_suggestion_refs,omitempty"`
		InsufficientEvidenceReasons []string             `json:"insufficient_evidence_reasons,omitempty"`
	}
	eligibility := r.SummaryEligibility
	if r.legacyFormat && eligibility == SummaryEligible {
		eligibility = ""
	}
	var overall *int
	if eligibility != SummaryInsufficientEvidence {
		score := r.OverallScore
		overall = &score
	}
	return json.Marshal(wireResult{
		SummaryEligibility:          eligibility,
		OverallScore:                overall,
		Summary:                     r.Summary,
		Conclusions:                 r.Conclusions,
		FeedbackItems:               r.FeedbackItems,
		RepracticeSuggestionRefs:    r.RepracticeSuggestionRefs,
		InsufficientEvidenceReasons: r.InsufficientEvidenceReasons,
	})
}

func (r *ReviewResult) UnmarshalJSON(encoded []byte) error {
	type wireResult struct {
		SummaryEligibility          SummaryEligibility   `json:"summary_eligibility"`
		OverallScore                *int                 `json:"overall_score"`
		Summary                     string               `json:"summary"`
		Conclusions                 []ReviewConclusion   `json:"conclusions"`
		FeedbackItems               []ReviewFeedbackItem `json:"feedback_items"`
		RepracticeSuggestionRefs    []string             `json:"repractice_suggestion_refs"`
		InsufficientEvidenceReasons []string             `json:"insufficient_evidence_reasons"`
	}
	var wire wireResult
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return err
	}
	legacyFormat := wire.SummaryEligibility == ""
	if legacyFormat {
		wire.SummaryEligibility = SummaryEligible
	}
	if wire.SummaryEligibility == SummaryEligible && wire.OverallScore == nil {
		return ErrInvalidReview
	}
	*r = ReviewResult{
		SummaryEligibility:          wire.SummaryEligibility,
		Summary:                     wire.Summary,
		Conclusions:                 wire.Conclusions,
		FeedbackItems:               wire.FeedbackItems,
		RepracticeSuggestionRefs:    wire.RepracticeSuggestionRefs,
		InsufficientEvidenceReasons: wire.InsufficientEvidenceReasons,
		legacyFormat:                legacyFormat,
	}
	if wire.OverallScore != nil {
		r.OverallScore = *wire.OverallScore
	}
	return nil
}

type SummaryEligibility string

const (
	SummaryEligible             SummaryEligibility = "eligible"
	SummaryInsufficientEvidence SummaryEligibility = "insufficient_evidence"
)

type ReviewConclusion struct {
	Key        string `json:"key"`
	Category   string `json:"category"`
	Score      int    `json:"score,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type FeedbackKind string

const (
	FeedbackCorrection            FeedbackKind = "correction"
	FeedbackStrength              FeedbackKind = "strength"
	FeedbackImprovement           FeedbackKind = "improvement"
	FeedbackRecommendedExpression FeedbackKind = "recommended_expression"
)

type ReviewFeedbackItem struct {
	Key        string       `json:"key"`
	Kind       FeedbackKind `json:"kind"`
	Message    string       `json:"message"`
	Suggestion string       `json:"suggestion,omitempty"`
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
	EvaluationContext   EvaluationContext
	Sources             []SourceObject
}

type EvidenceTargetKind string

const (
	EvidenceTargetConclusion EvidenceTargetKind = "conclusion"
	EvidenceTargetFeedback   EvidenceTargetKind = "feedback_item"
)

type EvidenceAnchorKind string

const (
	EvidenceAnchorExactQuote EvidenceAnchorKind = "exact_quote"
	EvidenceAnchorWholeField EvidenceAnchorKind = "whole_field"
)

type EvidenceLink struct {
	TargetKind    EvidenceTargetKind
	TargetKey     string
	ConclusionKey string
	SourceType    string
	SourceID      string
	SourceVersion string
	Field         string
	AnchorKind    EvidenceAnchorKind
	Quote         string
	Occurrence    int
}

type GeneratedReview struct {
	Result        ReviewResult
	EvidenceLinks []EvidenceLink
}

type ReviewEvidence struct {
	ID            string
	ReviewID      string
	OwnerUserID   string
	TargetKind    EvidenceTargetKind
	TargetKey     string
	ConclusionKey string
	SourceType    string
	SourceID      string
	SourceVersion string
	Field         string
	AnchorKind    EvidenceAnchorKind
	Quote         string
	StartUTF8Byte *int
	EndUTF8Byte   *int
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
	EvaluationContext         EvaluationContext
}

type DeleteUserReviewsCommand struct {
	UserID             string
	DeletionGeneration int64
}

type persistedGenerationFailure struct {
	category string
}

func (failure persistedGenerationFailure) Error() string {
	return "review generation failed: " + failure.category
}

func (failure persistedGenerationFailure) StableCategory() string {
	return failure.category
}

func failedGenerationError(category string) error {
	category = strings.TrimSpace(category)
	if !validStableErrorCategory(category) {
		return ErrGenerationFailed
	}
	return errors.Join(
		ErrGenerationFailed,
		persistedGenerationFailure{category: category},
	)
}

func terminalGenerationCategory(category string) bool {
	switch strings.TrimSpace(category) {
	case "invalid_request",
		"configuration",
		"authentication",
		"authorization",
		"quota_exhausted":
		return true
	default:
		return false
	}
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
	if c.ImplementationVersion == "qianwen-scenario-review-v2" {
		return c.EvaluationContext.Validate(DefaultPolicyRegistry())
	}
	return nil
}

func validateSource(command EnsureReviewCommand, source ReviewSourceSnapshot) error {
	if source.PracticeSessionID != command.PracticeSessionID ||
		source.SourceTurnID != command.SourceTurnID ||
		source.SourceTurnVersion != command.SourceTurnVersion ||
		source.ManifestFingerprint != command.SourceManifestFingerprint ||
		(command.ImplementationVersion == "qianwen-scenario-review-v2" &&
			!sameEvaluationContext(
				source.EvaluationContext,
				command.EvaluationContext,
			)) ||
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

func sameEvaluationContext(left, right EvaluationContext) bool {
	registry := DefaultPolicyRegistry()
	leftJSON, leftErr := left.CanonicalJSON(registry)
	rightJSON, rightErr := right.CanonicalJSON(registry)
	return leftErr == nil &&
		rightErr == nil &&
		bytes.Equal(leftJSON, rightJSON)
}

func deriveSummaryEligibility(
	source ReviewSourceSnapshot,
) (SummaryEligibility, []string, error) {
	policy, err := DefaultPolicyRegistry().Resolve(
		source.EvaluationContext.SessionPolicyRef,
		PolicyScopeSession,
		source.EvaluationContext.ContextType,
	)
	if err != nil {
		return "", nil, ErrInvalidReview
	}
	wordCount := 0
	for _, item := range source.Sources {
		var snapshot struct {
			AnswerText string `json:"answer_text"`
		}
		if err := json.Unmarshal(item.Snapshot, &snapshot); err != nil {
			return "", nil, ErrInvalidReview
		}
		for _, field := range strings.Fields(snapshot.AnswerText) {
			if strings.IndexFunc(field, unicode.IsLetter) >= 0 {
				wordCount++
			}
		}
	}
	if wordCount < policy.MinimumEvidenceWords {
		return SummaryInsufficientEvidence,
			[]string{"confirmed_answer_too_short"},
			nil
	}
	return SummaryEligible, nil, nil
}

func insufficientEvidenceResult(reasons []string) ReviewResult {
	return ReviewResult{
		SummaryEligibility: SummaryInsufficientEvidence,
		Summary: "Not enough confirmed answer evidence is available for a " +
			"formal score. Complete another practice response and try again.",
		Conclusions:                 []ReviewConclusion{},
		InsufficientEvidenceReasons: slices.Clone(reasons),
	}
}

func validateGenerated(
	source ReviewSourceSnapshot,
	generated GeneratedReview,
) (ReviewResult, []ReviewEvidence, error) {
	policy, err := DefaultPolicyRegistry().Resolve(
		source.EvaluationContext.SessionPolicyRef,
		PolicyScopeSession,
		source.EvaluationContext.ContextType,
	)
	if err != nil {
		return ReviewResult{}, nil, ErrInvalidReview
	}
	if generated.Result.SummaryEligibility == "" {
		generated.Result.SummaryEligibility = SummaryEligible
	}
	if generated.Result.SummaryEligibility != SummaryEligible {
		return ReviewResult{}, nil, ErrInvalidReview
	}
	if err := validatePolicyConclusions(
		&generated.Result,
		policy,
	); err != nil {
		return ReviewResult{}, nil, err
	}

	sourceByKey := make(map[string]SourceObject, len(source.Sources))
	for _, item := range source.Sources {
		sourceByKey[sourceKey(
			item.SourceType,
			item.SourceID,
			item.SourceVersion,
		)] = item
	}

	targets := make(map[string]bool, len(generated.Result.Conclusions)+
		len(generated.Result.FeedbackItems))
	for _, conclusion := range generated.Result.Conclusions {
		targets[evidenceTargetKey(
			EvidenceTargetConclusion,
			conclusion.Key,
		)] = false
	}
	for _, item := range generated.Result.FeedbackItems {
		targets[evidenceTargetKey(EvidenceTargetFeedback, item.Key)] = false
	}

	evidence := make([]ReviewEvidence, 0, len(generated.EvidenceLinks))
	seenLinks := make(map[string]struct{}, len(generated.EvidenceLinks))
	for _, link := range generated.EvidenceLinks {
		link = normalizeEvidenceLink(link)
		targetKey := evidenceTargetKey(link.TargetKind, link.TargetKey)
		if _, exists := targets[targetKey]; !exists {
			return ReviewResult{}, nil, ErrInvalidReview
		}
		sourceItem, exists := sourceByKey[sourceKey(
			link.SourceType,
			link.SourceID,
			link.SourceVersion,
		)]
		if !exists {
			return ReviewResult{}, nil, ErrInvalidReview
		}
		start, end, anchorErr := validateEvidenceAnchor(
			sourceItem,
			link,
		)
		if anchorErr != nil {
			return ReviewResult{}, nil, anchorErr
		}
		linkKey := targetKey + "\x00" + sourceKey(
			link.SourceType,
			link.SourceID,
			link.SourceVersion,
		) + "\x00" + link.Field + "\x00" +
			string(link.AnchorKind) + "\x00" + link.Quote
		if _, exists := seenLinks[linkKey]; exists {
			continue
		}
		seenLinks[linkKey] = struct{}{}
		targets[targetKey] = true
		evidence = append(evidence, ReviewEvidence{
			TargetKind:    link.TargetKind,
			TargetKey:     link.TargetKey,
			SourceType:    sourceItem.SourceType,
			SourceID:      sourceItem.SourceID,
			SourceVersion: sourceItem.SourceVersion,
			Field:         link.Field,
			AnchorKind:    link.AnchorKind,
			Quote:         link.Quote,
			StartUTF8Byte: start,
			EndUTF8Byte:   end,
			Checksum:      sourceItem.Checksum,
			Snapshot:      append(json.RawMessage(nil), sourceItem.Snapshot...),
		})
	}

	for key, covered := range targets {
		if !covered {
			return ReviewResult{}, nil,
				fmt.Errorf("%w: target %q", ErrEvidenceRequired, key)
		}
	}
	if err := validateReviewResult(generated.Result); err != nil {
		return ReviewResult{}, nil, err
	}
	return generated.Result, evidence, nil
}

func validateGeneratedLegacy(
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
		if _, exists := conclusions[conclusion.Key]; exists {
			return nil, ErrInvalidReview
		}
		conclusions[conclusion.Key] = struct{}{}
	}
	evidence := make([]ReviewEvidence, 0, len(generated.EvidenceLinks))
	covered := make(map[string]bool, len(conclusions))
	for _, link := range generated.EvidenceLinks {
		key := link.ConclusionKey
		if key == "" {
			key = link.TargetKey
		}
		if _, exists := conclusions[key]; !exists {
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
		covered[key] = true
		evidence = append(evidence, ReviewEvidence{
			ConclusionKey: key,
			SourceType:    sourceItem.SourceType,
			SourceID:      sourceItem.SourceID,
			SourceVersion: sourceItem.SourceVersion,
			Checksum:      sourceItem.Checksum,
			Snapshot: append(
				json.RawMessage(nil),
				sourceItem.Snapshot...,
			),
		})
	}
	for key := range conclusions {
		if !covered[key] {
			return nil, fmt.Errorf(
				"%w: conclusion %q",
				ErrEvidenceRequired,
				key,
			)
		}
	}
	return evidence, nil
}

func validateCompletionPayload(
	result ReviewResult,
	evidence []ReviewEvidence,
) error {
	if result.SummaryEligibility == "" {
		return validateLegacyCompletionPayload(result, evidence)
	}
	if err := validateReviewResult(result); err != nil {
		return err
	}
	if result.SummaryEligibility == SummaryInsufficientEvidence {
		if len(evidence) != 0 {
			return ErrInvalidReview
		}
		return nil
	}

	targets := make(map[string]bool, len(result.Conclusions)+
		len(result.FeedbackItems))
	for _, conclusion := range result.Conclusions {
		targets[evidenceTargetKey(
			EvidenceTargetConclusion,
			conclusion.Key,
		)] = false
	}
	for _, feedback := range result.FeedbackItems {
		targets[evidenceTargetKey(
			EvidenceTargetFeedback,
			feedback.Key,
		)] = false
	}
	for _, item := range evidence {
		item = normalizeReviewEvidence(item)
		if strings.TrimSpace(item.TargetKey) == "" ||
			strings.TrimSpace(item.SourceType) == "" ||
			strings.TrimSpace(item.SourceID) == "" ||
			strings.TrimSpace(item.SourceVersion) == "" ||
			item.Field != "answer_text" ||
			(item.AnchorKind != EvidenceAnchorExactQuote &&
				item.AnchorKind != EvidenceAnchorWholeField) ||
			len(item.Snapshot) > 16*1024 ||
			(len(item.Snapshot) > 0 && !json.Valid(item.Snapshot)) {
			return ErrInvalidReview
		}
		key := evidenceTargetKey(item.TargetKind, item.TargetKey)
		if _, exists := targets[key]; !exists {
			return ErrInvalidReview
		}
		targets[key] = true
	}
	for key, covered := range targets {
		if !covered {
			return fmt.Errorf("%w: target %q", ErrEvidenceRequired, key)
		}
	}
	return nil
}

func validateLegacyCompletionPayload(
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
	if err := validatePersistedReviewResult(result); err != nil {
		return err
	}
	if !validReviewText(result.Summary, maxReviewSummaryUTF8Bytes) ||
		len(result.Conclusions) > maxReviewConclusions ||
		len(result.FeedbackItems) > maxReviewFeedbackItems {
		return ErrInvalidReview
	}
	for _, conclusion := range result.Conclusions {
		if !validReviewText(
			conclusion.Key,
			maxReviewConclusionLabelBytes,
		) ||
			!validReviewText(
				conclusion.Category,
				maxReviewConclusionLabelBytes,
			) ||
			!validReviewText(
				conclusion.Message,
				maxReviewConclusionTextBytes,
			) ||
			!validOptionalReviewText(
				conclusion.Suggestion,
				maxReviewConclusionTextBytes,
			) ||
			conclusion.Score < 0 ||
			conclusion.Score > 100 {
			return ErrInvalidReview
		}
	}
	feedbackKeys := make(map[string]struct{}, len(result.FeedbackItems))
	for _, item := range result.FeedbackItems {
		if !validReviewText(item.Key, maxReviewConclusionLabelBytes) ||
			!validFeedbackKind(item.Kind) ||
			!validReviewText(item.Message, maxReviewConclusionTextBytes) ||
			!validOptionalReviewText(
				item.Suggestion,
				maxReviewConclusionTextBytes,
			) {
			return ErrInvalidReview
		}
		if _, exists := feedbackKeys[item.Key]; exists {
			return ErrInvalidReview
		}
		feedbackKeys[item.Key] = struct{}{}
	}
	seenRefs := make(map[string]struct{}, len(result.RepracticeSuggestionRefs))
	for _, ref := range result.RepracticeSuggestionRefs {
		if _, exists := feedbackKeys[ref]; !exists {
			return ErrInvalidReview
		}
		if _, exists := seenRefs[ref]; exists {
			return ErrInvalidReview
		}
		seenRefs[ref] = struct{}{}
	}
	for _, reason := range result.InsufficientEvidenceReasons {
		if !validReviewText(reason, maxReviewConclusionLabelBytes) {
			return ErrInvalidReview
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxReviewResultJSONBytes {
		return ErrInvalidReview
	}
	return nil
}

// validatePersistedReviewResult preserves the validation contract that was in
// production before response budgets were introduced. Reads must continue to
// restore those committed results; the stricter limits apply only to new
// writes, while the HTTP layer still enforces its total response budget.
func validatePersistedReviewResult(result ReviewResult) error {
	if result.SummaryEligibility == "" {
		result.SummaryEligibility = SummaryEligible
	}
	if strings.TrimSpace(result.Summary) == "" {
		return ErrInvalidReview
	}
	if result.SummaryEligibility == SummaryInsufficientEvidence {
		if result.OverallScore != 0 ||
			len(result.Conclusions) != 0 ||
			len(result.FeedbackItems) != 0 ||
			len(result.InsufficientEvidenceReasons) == 0 {
			return ErrInvalidReview
		}
		return nil
	}
	if result.SummaryEligibility != SummaryEligible ||
		result.OverallScore < 0 ||
		result.OverallScore > 100 ||
		len(result.Conclusions) == 0 ||
		len(result.InsufficientEvidenceReasons) != 0 {
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

func validatePolicyConclusions(
	result *ReviewResult,
	policy EvaluationPolicy,
) error {
	if result == nil ||
		len(result.Conclusions) != len(policy.Dimensions) {
		return ErrInvalidReview
	}
	byDimension := make(map[string]ReviewConclusion, len(result.Conclusions))
	for _, conclusion := range result.Conclusions {
		if conclusion.Category == "" {
			return ErrInvalidReview
		}
		if _, exists := byDimension[conclusion.Category]; exists {
			return ErrInvalidReview
		}
		byDimension[conclusion.Category] = conclusion
	}
	weighted := 0
	ordered := make([]ReviewConclusion, 0, len(policy.Dimensions))
	for _, dimension := range policy.Dimensions {
		conclusion, exists := byDimension[dimension.Key]
		if !exists || conclusion.Score < 0 || conclusion.Score > 100 {
			return ErrInvalidReview
		}
		weighted += conclusion.Score * dimension.Weight
		ordered = append(ordered, conclusion)
	}
	result.Conclusions = ordered
	result.OverallScore = (weighted + 50) / 100
	return nil
}

func validateEvidenceAnchor(
	source SourceObject,
	link EvidenceLink,
) (*int, *int, error) {
	if link.Field != "answer_text" ||
		link.TargetKey == "" ||
		(link.TargetKind != EvidenceTargetConclusion &&
			link.TargetKind != EvidenceTargetFeedback) {
		return nil, nil, ErrInvalidReview
	}
	if link.AnchorKind == EvidenceAnchorWholeField {
		// No frozen P0 policy currently permits whole-field anchors. Keep the
		// wire enum closed while failing closed until a policy explicitly does.
		return nil, nil, ErrInvalidReview
	}
	if link.AnchorKind != EvidenceAnchorExactQuote ||
		link.Occurrence < 1 ||
		!validReviewText(link.Quote, maxReviewConclusionTextBytes) {
		return nil, nil, ErrInvalidReview
	}
	var snapshot struct {
		AnswerText string `json:"answer_text"`
	}
	if err := json.Unmarshal(source.Snapshot, &snapshot); err != nil ||
		!utf8.ValidString(snapshot.AnswerText) {
		return nil, nil, ErrInvalidReview
	}
	start := nthOccurrence(snapshot.AnswerText, link.Quote, link.Occurrence)
	if start < 0 {
		return nil, nil, ErrInvalidReview
	}
	end := start + len(link.Quote)
	return &start, &end, nil
}

func nthOccurrence(value string, quote string, occurrence int) int {
	offset := 0
	for current := 1; current <= occurrence; current++ {
		index := strings.Index(value[offset:], quote)
		if index < 0 {
			return -1
		}
		offset += index
		if current == occurrence {
			return offset
		}
		offset += len(quote)
	}
	return -1
}

func evidenceTargetKey(kind EvidenceTargetKind, key string) string {
	return string(kind) + "\x00" + key
}

func normalizeEvidenceLink(link EvidenceLink) EvidenceLink {
	if link.TargetKind == "" &&
		link.TargetKey == "" &&
		link.ConclusionKey != "" {
		link.TargetKind = EvidenceTargetConclusion
		link.TargetKey = link.ConclusionKey
	}
	return link
}

func normalizeReviewEvidence(item ReviewEvidence) ReviewEvidence {
	if item.TargetKind == "" &&
		item.TargetKey == "" &&
		item.ConclusionKey != "" {
		item.TargetKind = EvidenceTargetConclusion
		item.TargetKey = item.ConclusionKey
	}
	if item.Field == "" && item.AnchorKind == "" {
		item.Field = "answer_text"
		item.AnchorKind = EvidenceAnchorWholeField
	}
	return item
}

func validFeedbackKind(kind FeedbackKind) bool {
	switch kind {
	case FeedbackCorrection,
		FeedbackStrength,
		FeedbackImprovement,
		FeedbackRecommendedExpression:
		return true
	default:
		return false
	}
}

func validReviewText(value string, maximumBytes int) bool {
	return utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		len(value) <= maximumBytes &&
		strings.TrimSpace(value) != ""
}

func validOptionalReviewText(value string, maximumBytes int) bool {
	return utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		len(value) <= maximumBytes &&
		(value == "" || strings.TrimSpace(value) != "")
}

func validStableErrorCategory(category string) bool {
	switch strings.TrimSpace(category) {
	case "invalid_request",
		"configuration",
		"authentication",
		"authorization",
		"quota_exhausted",
		"rate_limited",
		"timeout",
		"provider_timeout",
		"provider_unavailable",
		"invalid_response",
		"cancelled",
		"source_unavailable",
		"invalid_source",
		"generation_failed",
		"invalid_result",
		"lease_expired":
		return true
	default:
		return false
	}
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
