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
	seen := make(map[string]struct{})
	for _, candidate := range output.Candidates {
		decision, accepted := policy.decideCandidate(source, candidate)
		if !accepted {
			continue
		}
		key := string(decision.Scope) + ":" + decision.MatterID + ":" +
			string(decision.Type) + ":" + decision.CanonicalKey
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		decisions = append(decisions, decision)
	}
	return ExtractionBatch{
		CandidateCount: len(output.Candidates),
		Decisions:      decisions,
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
) (MemoryDecision, bool) {
	if !candidate.Action.Valid() {
		return MemoryDecision{}, false
	}
	if _, allowed := conversationalTypes[candidate.Type]; !allowed {
		return MemoryDecision{}, false
	}
	if !validCanonicalKey(candidate.CanonicalKey) ||
		!compatibleKey(candidate.Type, candidate.CanonicalKey) ||
		!strings.Contains(source.UserText, candidate.Evidence) ||
		sensitiveCandidate(candidate) {
		return MemoryDecision{}, false
	}
	if candidate.Type == TypeIdentity &&
		candidate.CanonicalKey == "identity.gender" &&
		!candidate.InteractionUse {
		return MemoryDecision{}, false
	}
	matterID := ""
	if candidate.Scope == ScopeMatter {
		if source.MatterID == "" {
			return MemoryDecision{}, false
		}
		matterID = source.MatterID
	} else if candidate.Scope != ScopeUser {
		return MemoryDecision{}, false
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
			return MemoryDecision{}, false
		}
		decision.Content = candidate.Content
		if candidate.Type == TypeTopic {
			expiresAt := policy.now().UTC().Add(policy.topicTTL)
			decision.ExpiresAt = &expiresAt
		}
	} else if strings.TrimSpace(candidate.Content) != "" {
		return MemoryDecision{}, false
	}
	return decision, decision.Valid()
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
