package preparation

import "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"

const (
	genericPracticeSessionPolicyRef             = "generic.practice.session.v1"
	dailyHotelCheckinIssueSessionPolicyRef      = "daily.hotel_checkin_issue.session.v1"
	interviewProjectDeepDiveSessionPolicyRef    = "interview.project_deep_dive.session.v1"
	workplaceProgressRiskUpdateSessionPolicyRef = "workplace.progress_risk_update.session.v1"
	ieltsSpeakingPart1SessionPolicyRef          = "ielts.speaking_part1.session.v1"
	ieltsSpeakingPart2SessionPolicyRef          = "ielts.speaking_part2.session.v1"
	ieltsSpeakingPart3SessionPolicyRef          = "ielts.speaking_part3.session.v1"
	ieltsSpeakingFullMockSessionPolicyRef       = "ielts.speaking_full_mock.session.v1"
)

type registeredSessionPolicy struct {
	minEffectiveTurns       int
	maxEffectiveTurns       int
	coverageCheckpointTurn  int
	maxFollowUpsPerQuestion int
	turnsFromBlueprints     bool
}

// PolicyCatalog is the production resolver for Scene-owned policy refs. Every
// accepted ref is registered explicitly; an unregistered ref fails closed.
type PolicyCatalog struct {
	policies map[string]registeredSessionPolicy
}

func NewPolicyCatalog() *PolicyCatalog {
	standard := registeredSessionPolicy{
		minEffectiveTurns:       4,
		maxEffectiveTurns:       6,
		coverageCheckpointTurn:  4,
		maxFollowUpsPerQuestion: 1,
	}
	ielts := registeredSessionPolicy{turnsFromBlueprints: true}
	return &PolicyCatalog{policies: map[string]registeredSessionPolicy{
		genericPracticeSessionPolicyRef:             standard,
		dailyHotelCheckinIssueSessionPolicyRef:      standard,
		workplaceProgressRiskUpdateSessionPolicyRef: standard,
		interviewProjectDeepDiveSessionPolicyRef: {
			minEffectiveTurns:       4,
			maxEffectiveTurns:       6,
			coverageCheckpointTurn:  4,
			maxFollowUpsPerQuestion: 3,
		},
		ieltsSpeakingPart1SessionPolicyRef:    ielts,
		ieltsSpeakingPart2SessionPolicyRef:    ielts,
		ieltsSpeakingPart3SessionPolicyRef:    ielts,
		ieltsSpeakingFullMockSessionPolicyRef: ielts,
	}}
}

func (c *PolicyCatalog) ResolveSessionPolicy(
	definition scene.SceneDefinition,
	option scene.PracticeOption,
) (SessionPolicy, error) {
	if c == nil || definition.Prompt.SuggestedDurationSeconds < 1 ||
		len(definition.Prompt.TurnBlueprints) == 0 {
		return SessionPolicy{}, ErrPlanConflict
	}
	registered, found := c.policies[definition.SessionPolicyRef]
	if !found {
		return SessionPolicy{}, ErrPlanConflict
	}
	if registered.turnsFromBlueprints {
		turns := len(definition.Prompt.TurnBlueprints)
		registered.minEffectiveTurns = turns
		registered.maxEffectiveTurns = turns
		registered.coverageCheckpointTurn = turns
	}
	policy := SessionPolicy{
		SuggestedDurationSeconds: definition.Prompt.SuggestedDurationSeconds,
		MinEffectiveTurns:        registered.minEffectiveTurns,
		MaxEffectiveTurns:        registered.maxEffectiveTurns,
		CoverageCheckpointTurn:   registered.coverageCheckpointTurn,
		MaxFollowUpsPerQuestion:  registered.maxFollowUpsPerQuestion,
		EarlyCompletionRule:      EarlyCompletionCoverageSatisfiedAfterCheckpoint,
	}
	switch option.Type {
	case scene.PracticeOptionFullSimulation:
		if option.RoleDefinitionID != "" {
			return SessionPolicy{}, ErrPlanInvalid
		}
	case scene.PracticeOptionFocus:
		if option.RoleDefinitionID == "" {
			return SessionPolicy{}, ErrPlanInvalid
		}
		policy.MinEffectiveTurns = 1
		policy.MaxEffectiveTurns = 3
		policy.CoverageCheckpointTurn = 1
	default:
		return SessionPolicy{}, ErrPlanInvalid
	}
	return policy, nil
}

var _ PolicyResolver = (*PolicyCatalog)(nil)
