package memory

import (
	"crypto/sha256"
	"strings"
	"time"
)

var conversationalTypes = map[Type]struct{}{
	TypeIdentity:   {},
	TypeProfile:    {},
	TypePreference: {},
	TypeGoal:       {},
	TypeInterest:   {},
	TypeTopic:      {},
}

type ExtractionPolicy struct {
	version  string
	topicTTL time.Duration
	now      func() time.Time
}

func NewExtractionPolicy(
	version string,
	topicTTL time.Duration,
	now func() time.Time,
) (*ExtractionPolicy, error) {
	if !validPolicyVersion(version) ||
		topicTTL < 24*time.Hour ||
		topicTTL > 365*24*time.Hour ||
		now == nil {
		return nil, ErrInvalidArgument
	}
	return &ExtractionPolicy{
		version:  version,
		topicTTL: topicTTL,
		now:      now,
	}, nil
}

func (policy *ExtractionPolicy) Decide(
	source CompletedRunSource,
	output ExtractionOutput,
) (ExtractionBatch, error) {
	if policy == nil || !source.Valid() ||
		len(output.Candidates) > maxExtractionCandidates {
		return ExtractionBatch{}, ErrInvalidArgument
	}
	decisions := make([]MemoryDecision, 0, len(output.Candidates))
	rejections := make([]CandidateRejection, 0, len(output.Candidates))
	seen := make(map[string]struct{})
	for index, candidate := range output.Candidates {
		decision, rejectionReason := policy.decideCandidate(source, candidate)
		if rejectionReason != "" {
			rejections = append(rejections, CandidateRejection{
				CandidateIndex: index,
				Reason:         rejectionReason,
			})
			continue
		}
		key := string(decision.Scope) + ":" + decision.MatterID + ":" +
			string(decision.Type) + ":" + decision.CanonicalKey
		if _, duplicate := seen[key]; duplicate {
			rejections = append(rejections, CandidateRejection{
				CandidateIndex: index,
				Reason:         RejectionDuplicateCandidate,
			})
			continue
		}
		seen[key] = struct{}{}
		decisions = append(decisions, decision)
	}
	return ExtractionBatch{
		CandidateCount: len(output.Candidates),
		Decisions:      decisions,
		Rejections:     rejections,
		Source: SourceInput{
			Type:     SourceAgentRun,
			SourceID: source.RunID,
			Version:  int64(source.Attempt),
			Checksum: sha256.Sum256([]byte(source.UserText)),
		},
	}, nil
}

func (policy *ExtractionPolicy) decideCandidate(
	source CompletedRunSource,
	candidate ExtractedCandidate,
) (MemoryDecision, CandidateRejectionReason) {
	if !candidate.Action.Valid() {
		return MemoryDecision{}, RejectionInvalidAction
	}
	if _, allowed := conversationalTypes[candidate.Type]; !allowed {
		return MemoryDecision{}, RejectionUnsupportedType
	}
	if !validCanonicalKey(candidate.CanonicalKey) {
		return MemoryDecision{}, RejectionInvalidCanonicalKey
	}
	if !compatibleKey(candidate.Type, candidate.CanonicalKey) {
		return MemoryDecision{}, RejectionIncompatibleKey
	}
	if !strings.Contains(source.UserText, candidate.Evidence) {
		return MemoryDecision{}, RejectionEvidenceMismatch
	}
	if sensitiveCandidate(candidate) {
		return MemoryDecision{}, RejectionSensitiveCandidate
	}
	if genderCandidate(candidate) &&
		!candidate.InteractionUse {
		return MemoryDecision{}, RejectionGenderInteractionUseRequired
	}
	matterID := ""
	if candidate.Scope == ScopeMatter {
		if source.MatterID == "" {
			return MemoryDecision{}, RejectionMissingMatter
		}
		matterID = source.MatterID
	} else if candidate.Scope != ScopeUser {
		return MemoryDecision{}, RejectionInvalidScope
	}
	decision := MemoryDecision{
		Action:       candidate.Action,
		Type:         candidate.Type,
		CanonicalKey: candidate.CanonicalKey,
		Scope:        candidate.Scope,
		MatterID:     matterID,
	}
	if candidate.Action == CandidateUpsert {
		if !validContent(candidate.Content) {
			return MemoryDecision{}, RejectionInvalidContent
		}
		decision.Content = candidate.Content
		if candidate.Type == TypeTopic {
			expiresAt := policy.now().UTC().Add(policy.topicTTL)
			decision.ExpiresAt = &expiresAt
		}
	} else if strings.TrimSpace(candidate.Content) != "" {
		return MemoryDecision{}, RejectionInactivateContentNotEmpty
	}
	if !decision.Valid() {
		return MemoryDecision{}, RejectionInvalidDecision
	}
	return decision, ""
}

func genderCandidate(candidate ExtractedCandidate) bool {
	return (candidate.Type == TypeIdentity &&
		candidate.CanonicalKey == "identity.gender") ||
		(candidate.Type == TypeProfile &&
			candidate.CanonicalKey == "profile.gender")
}

func compatibleKey(memoryType Type, key string) bool {
	switch memoryType {
	case TypeIdentity:
		return strings.HasPrefix(key, "identity.")
	case TypeProfile:
		return strings.HasPrefix(key, "profile.") ||
			strings.HasPrefix(key, "career.")
	case TypePreference:
		return strings.HasPrefix(key, "preference.") ||
			strings.HasPrefix(key, "coaching.")
	case TypeGoal:
		return key == "goal.current" ||
			strings.HasPrefix(key, "goal.")
	case TypeInterest:
		return strings.HasPrefix(key, "interest.")
	case TypeTopic:
		return strings.HasPrefix(key, "topic.")
	default:
		return false
	}
}

func sensitiveCandidate(candidate ExtractedCandidate) bool {
	combined := strings.ToLower(
		candidate.CanonicalKey + " " + candidate.Content + " " +
			candidate.Evidence,
	)
	for _, marker := range []string{
		"password",
		"passwd",
		"token",
		"api_key",
		"apikey",
		"secret",
		"credential",
	} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}
