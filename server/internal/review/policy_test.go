package review

import (
	"errors"
	"testing"
)

func TestDefaultPolicyRegistryResolvesFrozenPolicies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ref         string
		scope       PolicyScope
		contextType EvaluationContextType
		dimensions  int
		aggregation AggregationMode
	}{
		{"interview.project_deep_dive.turn.v1", PolicyScopeTurn, ContextInterviewProjectDeepDive, 0, ""},
		{"interview.project_deep_dive.session.v1", PolicyScopeSession, ContextInterviewProjectDeepDive, 5, AggregationNone},
		{"ielts.speaking_part2.turn.v1", PolicyScopeTurn, ContextIELTSSpeakingPart2, 0, ""},
		{"ielts.speaking_part2.session.v1", PolicyScopeSession, ContextIELTSSpeakingPart2, 4, AggregationIELTSOverall},
		{"workplace.progress_risk_update.turn.v1", PolicyScopeTurn, ContextWorkplaceProgressRisk, 0, ""},
		{"workplace.progress_risk_update.session.v1", PolicyScopeSession, ContextWorkplaceProgressRisk, 5, AggregationNone},
		{"daily.hotel_checkin_issue.turn.v1", PolicyScopeTurn, ContextDailyHotelCheckin, 0, ""},
		{"daily.hotel_checkin_issue.session.v1", PolicyScopeSession, ContextDailyHotelCheckin, 5, AggregationNone},
		{"generic.practice.turn.v1", PolicyScopeTurn, ContextGenericPractice, 0, ""},
		{"generic.practice.session.v1", PolicyScopeSession, ContextGenericPractice, 5, AggregationNone},
	}
	registry := DefaultPolicyRegistry()
	for _, test := range tests {
		test := test
		t.Run(test.ref, func(t *testing.T) {
			t.Parallel()
			policy, err := registry.Resolve(
				test.ref,
				test.scope,
				test.contextType,
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(policy.Dimensions) != test.dimensions {
				t.Fatalf(
					"dimensions=%d, want %d",
					len(policy.Dimensions),
					test.dimensions,
				)
			}
			total := 0
			for _, dimension := range policy.Dimensions {
				total += dimension.Weight
			}
			if policy.Aggregation != test.aggregation {
				t.Fatalf(
					"aggregation=%q, want %q",
					policy.Aggregation,
					test.aggregation,
				)
			}
			wantTotal := 0
			if test.aggregation == AggregationIELTSOverall {
				wantTotal = 100
			}
			if total != wantTotal {
				t.Fatalf("weight total=%d, want %d", total, wantTotal)
			}
		})
	}
}

func TestPolicyRegistryFailsClosed(t *testing.T) {
	t.Parallel()
	registry := DefaultPolicyRegistry()
	tests := []struct {
		name        string
		ref         string
		scope       PolicyScope
		contextType EvaluationContextType
	}{
		{"unknown", "unknown.session.v1", PolicyScopeSession, ContextGenericPractice},
		{"scope mismatch", "generic.practice.turn.v1", PolicyScopeSession, ContextGenericPractice},
		{"context mismatch", "generic.practice.session.v1", PolicyScopeSession, ContextIELTSSpeakingPart2},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := registry.Resolve(test.ref, test.scope, test.contextType)
			if !errors.Is(err, ErrInvalidPolicyRegistry) {
				t.Fatalf("error=%v, want invalid registry", err)
			}
		})
	}
}

func TestPolicyRegistryRejectsDuplicateAndInvalidPolicies(t *testing.T) {
	t.Parallel()
	valid := turnPolicy("generic.practice.turn.v1", ContextGenericPractice)
	tests := []struct {
		name     string
		policies []EvaluationPolicy
	}{
		{"duplicate", []EvaluationPolicy{valid, valid}},
		{"invalid scope", []EvaluationPolicy{{Ref: "x", Scope: "other", ContextType: ContextGenericPractice}}},
		{"turn dimensions", []EvaluationPolicy{{Ref: "x", Scope: PolicyScopeTurn, ContextType: ContextGenericPractice, Dimensions: []RubricDimension{{Key: "x", Weight: 100}}}}},
		{"session weight", []EvaluationPolicy{{Ref: "x", Scope: PolicyScopeSession, ContextType: ContextGenericPractice, Dimensions: []RubricDimension{{Key: "x", Weight: 99}}}}},
		{"duplicate dimension", []EvaluationPolicy{{Ref: "x", Scope: PolicyScopeSession, ContextType: ContextGenericPractice, Dimensions: []RubricDimension{{Key: "x", Weight: 50}, {Key: "x", Weight: 50}}}}},
		{"non IELTS aggregation", []EvaluationPolicy{{Ref: "x", Scope: PolicyScopeSession, ContextType: ContextGenericPractice, Aggregation: AggregationIELTSOverall, MinimumEvidenceWords: 1, Dimensions: []RubricDimension{{Key: "x", Weight: 100}}}}},
		{"IELTS aggregation total", []EvaluationPolicy{{Ref: "x", Scope: PolicyScopeSession, ContextType: ContextIELTSSpeakingPart2, Aggregation: AggregationIELTSOverall, MinimumEvidenceWords: 1, Dimensions: []RubricDimension{{Key: "x", Weight: 99}}}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewPolicyRegistry(test.policies)
			if !errors.Is(err, ErrInvalidPolicyRegistry) {
				t.Fatalf("error=%v, want invalid registry", err)
			}
		})
	}
}
