package review

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type PolicyScope string

const (
	PolicyScopeTurn    PolicyScope = "turn"
	PolicyScopeSession PolicyScope = "session"
)

type EvaluationContextType string

const (
	ContextInterviewProjectDeepDive EvaluationContextType = "interview.project_deep_dive"
	ContextIELTSSpeakingPart2       EvaluationContextType = "ielts.speaking_part2"
	ContextWorkplaceProgressRisk    EvaluationContextType = "workplace.progress_risk_update"
	ContextDailyHotelCheckin        EvaluationContextType = "daily.hotel_checkin_issue"
	ContextGenericPractice          EvaluationContextType = "generic.practice"
)

type AggregationMode string

const (
	AggregationNone         AggregationMode = "none"
	AggregationIELTSOverall AggregationMode = "ielts_overall"
)

type RubricDimension struct {
	Key    string `json:"key"`
	Weight int    `json:"weight,omitempty"`
}

type EvaluationPolicy struct {
	Ref                  string
	Scope                PolicyScope
	ContextType          EvaluationContextType
	Dimensions           []RubricDimension
	Aggregation          AggregationMode
	MinimumEvidenceWords int
}

type PolicyRegistry struct {
	policies map[string]EvaluationPolicy
}

var ErrInvalidPolicyRegistry = errors.New("review: invalid policy registry")

func NewPolicyRegistry(policies []EvaluationPolicy) (*PolicyRegistry, error) {
	registry := &PolicyRegistry{
		policies: make(map[string]EvaluationPolicy, len(policies)),
	}
	for _, policy := range policies {
		if err := validatePolicy(policy); err != nil {
			return nil, err
		}
		if _, exists := registry.policies[policy.Ref]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate ref %q",
				ErrInvalidPolicyRegistry,
				policy.Ref,
			)
		}
		policy.Dimensions = slices.Clone(policy.Dimensions)
		registry.policies[policy.Ref] = policy
	}
	return registry, nil
}

func DefaultPolicyRegistry() *PolicyRegistry {
	registry, err := NewPolicyRegistry(defaultEvaluationPolicies())
	if err != nil {
		panic(err)
	}
	return registry
}

func (r *PolicyRegistry) Resolve(
	ref string,
	scope PolicyScope,
	contextType EvaluationContextType,
) (EvaluationPolicy, error) {
	if r == nil {
		return EvaluationPolicy{}, ErrInvalidPolicyRegistry
	}
	policy, exists := r.policies[strings.TrimSpace(ref)]
	if !exists || policy.Scope != scope || policy.ContextType != contextType {
		return EvaluationPolicy{}, ErrInvalidPolicyRegistry
	}
	policy.Dimensions = slices.Clone(policy.Dimensions)
	return policy, nil
}

func validatePolicy(policy EvaluationPolicy) error {
	if strings.TrimSpace(policy.Ref) == "" ||
		policy.Ref != strings.TrimSpace(policy.Ref) ||
		(policy.Scope != PolicyScopeTurn &&
			policy.Scope != PolicyScopeSession) ||
		!validEvaluationContextType(policy.ContextType) {
		return ErrInvalidPolicyRegistry
	}
	if policy.Scope == PolicyScopeTurn {
		if len(policy.Dimensions) != 0 ||
			policy.Aggregation != "" ||
			policy.MinimumEvidenceWords != 0 {
			return ErrInvalidPolicyRegistry
		}
		return nil
	}
	if len(policy.Dimensions) == 0 ||
		policy.MinimumEvidenceWords < 1 {
		return ErrInvalidPolicyRegistry
	}
	if policy.Aggregation == "" {
		policy.Aggregation = AggregationNone
	}
	if policy.Aggregation != AggregationNone &&
		(policy.Aggregation != AggregationIELTSOverall ||
			policy.ContextType != ContextIELTSSpeakingPart2) {
		return ErrInvalidPolicyRegistry
	}
	total := 0
	seen := make(map[string]struct{}, len(policy.Dimensions))
	for _, dimension := range policy.Dimensions {
		if strings.TrimSpace(dimension.Key) == "" ||
			dimension.Key != strings.TrimSpace(dimension.Key) ||
			dimension.Weight < 0 {
			return ErrInvalidPolicyRegistry
		}
		if policy.Aggregation == AggregationNone && dimension.Weight != 0 {
			return ErrInvalidPolicyRegistry
		}
		if policy.Aggregation == AggregationIELTSOverall &&
			dimension.Weight <= 0 {
			return ErrInvalidPolicyRegistry
		}
		if _, exists := seen[dimension.Key]; exists {
			return ErrInvalidPolicyRegistry
		}
		seen[dimension.Key] = struct{}{}
		total += dimension.Weight
	}
	if (policy.Aggregation == AggregationNone && total != 0) ||
		(policy.Aggregation == AggregationIELTSOverall && total != 100) {
		return ErrInvalidPolicyRegistry
	}
	return nil
}

func validEvaluationContextType(contextType EvaluationContextType) bool {
	switch contextType {
	case ContextInterviewProjectDeepDive,
		ContextIELTSSpeakingPart2,
		ContextWorkplaceProgressRisk,
		ContextDailyHotelCheckin,
		ContextGenericPractice:
		return true
	default:
		return false
	}
}

func defaultEvaluationPolicies() []EvaluationPolicy {
	return []EvaluationPolicy{
		turnPolicy(
			"interview.project_deep_dive.turn.v1",
			ContextInterviewProjectDeepDive,
		),
		sessionPolicy(
			"interview.project_deep_dive.session.v1",
			ContextInterviewProjectDeepDive,
			[]RubricDimension{
				{Key: "relevance_structure"},
				{Key: "technical_depth"},
				{Key: "ownership_decisions"},
				{Key: "evidence_impact"},
				{Key: "language_clarity"},
			},
			AggregationNone,
		),
		turnPolicy(
			"ielts.speaking_part2.turn.v1",
			ContextIELTSSpeakingPart2,
		),
		sessionPolicy(
			"ielts.speaking_part2.session.v1",
			ContextIELTSSpeakingPart2,
			[]RubricDimension{
				{Key: "task_coverage_development", Weight: 25},
				{Key: "coherence", Weight: 25},
				{Key: "lexical_resource", Weight: 25},
				{Key: "grammar_range_accuracy", Weight: 25},
			},
			AggregationIELTSOverall,
		),
		turnPolicy(
			"workplace.progress_risk_update.turn.v1",
			ContextWorkplaceProgressRisk,
		),
		sessionPolicy(
			"workplace.progress_risk_update.session.v1",
			ContextWorkplaceProgressRisk,
			[]RubricDimension{
				{Key: "progress_clarity"},
				{Key: "risk_specificity"},
				{Key: "impact_priority"},
				{Key: "next_step_ask"},
				{Key: "language_clarity"},
			},
			AggregationNone,
		),
		turnPolicy(
			"daily.hotel_checkin_issue.turn.v1",
			ContextDailyHotelCheckin,
		),
		sessionPolicy(
			"daily.hotel_checkin_issue.session.v1",
			ContextDailyHotelCheckin,
			[]RubricDimension{
				{Key: "intent_clarity"},
				{Key: "information_completeness"},
				{Key: "politeness_tone"},
				{Key: "resolution_effectiveness"},
				{Key: "language_clarity"},
			},
			AggregationNone,
		),
		turnPolicy("generic.practice.turn.v1", ContextGenericPractice),
		sessionPolicy(
			"generic.practice.session.v1",
			ContextGenericPractice,
			[]RubricDimension{
				{Key: "task_relevance"},
				{Key: "clarity"},
				{Key: "lexical_resource"},
				{Key: "grammar_accuracy"},
				{Key: "interaction_effectiveness"},
			},
			AggregationNone,
		),
	}
}

func turnPolicy(
	ref string,
	contextType EvaluationContextType,
) EvaluationPolicy {
	return EvaluationPolicy{
		Ref:         ref,
		Scope:       PolicyScopeTurn,
		ContextType: contextType,
	}
}

func sessionPolicy(
	ref string,
	contextType EvaluationContextType,
	dimensions []RubricDimension,
	aggregation AggregationMode,
) EvaluationPolicy {
	return EvaluationPolicy{
		Ref:                  ref,
		Scope:                PolicyScopeSession,
		ContextType:          contextType,
		Dimensions:           dimensions,
		Aggregation:          aggregation,
		MinimumEvidenceWords: 3,
	}
}
