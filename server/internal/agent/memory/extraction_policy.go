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
		key := string(decision.Scope) + ":" + decision.GoalID + ":" +
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
	if candidate.Action == CandidateUpsert &&
		(contextualOrHypothetical(source.UserText) ||
			contextualOrHypothetical(candidate.Evidence)) {
		return MemoryDecision{}, RejectionContextualOrHypothetical
	}
	if sensitiveCandidate(candidate) {
		return MemoryDecision{}, RejectionSensitiveCandidate
	}
	if genderCandidate(candidate) &&
		(!candidate.InteractionUse ||
			!genderInteractionRequested(source.UserText)) {
		return MemoryDecision{}, RejectionGenderInteractionUseRequired
	}
	goalID := ""
	if candidate.Scope == ScopeGoal {
		if source.GoalID == "" {
			return MemoryDecision{}, RejectionMissingGoal
		}
		goalID = source.GoalID
	} else if candidate.Scope != ScopeUser {
		return MemoryDecision{}, RejectionInvalidScope
	}
	if candidate.Action == CandidateUpsert &&
		!durableCandidate(candidate) {
		return MemoryDecision{}, RejectionInsufficientDurability
	}
	decision := MemoryDecision{
		Action:       candidate.Action,
		Type:         candidate.Type,
		CanonicalKey: candidate.CanonicalKey,
		Scope:        candidate.Scope,
		GoalID:       goalID,
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

func contextualOrHypothetical(evidence string) bool {
	normalized := strings.ToLower(evidence)
	for _, marker := range []string{
		"虚构",
		"假设",
		"假想",
		"测试一下",
		"用于测试",
		"测试内容",
		"测试项目",
		"模拟项目",
		"角色扮演",
		"fictional",
		"hypothetical",
		"test scenario",
		"test project",
		"in this mock interview",
		"role-play",
		"roleplay",
		"pretend ",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func genderInteractionRequested(userText string) bool {
	normalized := strings.ToLower(userText)
	for _, marker := range []string{
		"称呼我",
		"叫我",
		"对话中",
		"交流中",
		"交流时",
		"和我互动",
		"根据我的性别",
		"address me",
		"refer to me",
		"in conversation",
		"when we talk",
		"use my gender",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func durableCandidate(candidate ExtractedCandidate) bool {
	switch {
	case candidate.Type == TypeGoal &&
		candidate.CanonicalKey == "goal.current":
		return containsDurabilityMarker(candidate.Evidence, []string{
			"我的目标",
			"我正在",
			"我正",
			"我要",
			"我想",
			"我希望",
			"我准备",
			"我计划",
			"我打算",
			"my goal",
			"i am preparing",
			"i'm preparing",
			"i plan",
			"i want",
			"i aim",
			"i'm working toward",
			"i am working toward",
		})
	case candidate.Type == TypePreference &&
		candidate.CanonicalKey == "coaching.style":
		return containsDurabilityMarker(candidate.Evidence, []string{
			"以后",
			"今后",
			"每次",
			"一直",
			"长期",
			"从现在开始",
			"之后都",
			"我偏好",
			"我习惯",
			"请始终",
			"总是",
			"from now on",
			"in the future",
			"going forward",
			"every time",
			"always",
			"i prefer",
			"my preference",
			"please consistently",
		})
	default:
		return true
	}
}

func containsDurabilityMarker(evidence string, markers []string) bool {
	normalized := strings.ToLower(evidence)
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
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
