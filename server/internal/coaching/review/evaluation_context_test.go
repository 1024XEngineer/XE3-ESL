package review

import (
	"bytes"
	"errors"
	"testing"
)

func TestEvaluationContextVariantsAndCanonicalFingerprint(t *testing.T) {
	t.Parallel()
	registry := DefaultPolicyRegistry()
	contexts := []EvaluationContext{
		testEvaluationContext(ContextInterviewProjectDeepDive),
		testEvaluationContext(ContextIELTSSpeakingPart2),
		testEvaluationContext(ContextWorkplaceProgressRisk),
		testEvaluationContext(ContextDailyHotelCheckin),
		testEvaluationContext(ContextGenericPractice),
	}
	for _, evaluationContext := range contexts {
		evaluationContext := evaluationContext
		t.Run(string(evaluationContext.ContextType), func(t *testing.T) {
			t.Parallel()
			first, err := evaluationContext.CanonicalJSON(registry)
			if err != nil {
				t.Fatal(err)
			}
			second, err := evaluationContext.CanonicalJSON(registry)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("canonical context changed: %s != %s", first, second)
			}
			firstFingerprint, err := evaluationContext.Fingerprint(registry)
			if err != nil {
				t.Fatal(err)
			}
			secondFingerprint, err := evaluationContext.Fingerprint(registry)
			if err != nil {
				t.Fatal(err)
			}
			if firstFingerprint != secondFingerprint {
				t.Fatalf(
					"fingerprint changed: %s != %s",
					firstFingerprint,
					secondFingerprint,
				)
			}
		})
	}
}

func TestEvaluationContextFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*EvaluationContext)
	}{
		{"unknown context", func(value *EvaluationContext) {
			value.ContextType = "unknown"
			value.SceneSpecificContext.Type = "unknown"
		}},
		{"mismatched turn policy", func(value *EvaluationContext) { value.TurnPolicyRef = "ielts.speaking_part2.turn.v1" }},
		{"mismatched session policy", func(value *EvaluationContext) { value.SessionPolicyRef = "ielts.speaking_part2.session.v1" }},
		{"multiple variants", func(value *EvaluationContext) {
			value.SceneSpecificContext.Generic = &GenericPracticeV1{Version: "generic.practice.v1", PracticeGoal: "goal"}
		}},
		{"unknown schema", func(value *EvaluationContext) { value.SchemaVersion = "evaluation-context.v2" }},
		{"oversized field", func(value *EvaluationContext) { value.SceneKey = string(make([]byte, 129)) }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := testEvaluationContext(ContextInterviewProjectDeepDive)
			test.mutate(&value)
			if err := value.Validate(DefaultPolicyRegistry()); !errors.Is(
				err,
				ErrInvalidEvaluationContext,
			) {
				t.Fatalf("error=%v, want invalid context", err)
			}
		})
	}
}

func testEvaluationContext(
	contextType EvaluationContextType,
) EvaluationContext {
	value := EvaluationContext{
		SchemaVersion:      EvaluationContextSchemaVersion,
		ContextType:        contextType,
		SceneKey:           "scene-key",
		SceneID:            "scenario-id",
		SceneVersion:       1,
		PracticeOptionType: "FULL_SIMULATION",
		DifficultyRef:      "difficulty.standard.v1",
		AssistanceRef:      "assistance.none.v1",
	}
	switch contextType {
	case ContextInterviewProjectDeepDive:
		value.TurnPolicyRef = "interview.project_deep_dive.turn.v1"
		value.SessionPolicyRef = "interview.project_deep_dive.session.v1"
		value.SceneSpecificContext = SceneSpecificContext{
			Type: contextType,
			Interview: &InterviewProjectDeepDiveV1{
				Version:       "interview.project_deep_dive.v1",
				ProjectBrief:  "A payments migration",
				CandidateRole: "backend engineer",
				FocusPoints:   []string{"trade-offs"},
			},
		}
	case ContextIELTSSpeakingPart2:
		value.TurnPolicyRef = "ielts.speaking_part2.turn.v1"
		value.SessionPolicyRef = "ielts.speaking_part2.session.v1"
		value.SceneSpecificContext = SceneSpecificContext{
			Type: contextType,
			IELTS: &IELTSSpeakingPart2V1{
				Version:          "ielts.speaking_part2.v1",
				CueCardTopic:     "Describe a useful object",
				CueCardPoints:    []string{"what it is"},
				StrictSimulation: true,
			},
		}
	case ContextWorkplaceProgressRisk:
		value.TurnPolicyRef = "workplace.progress_risk_update.turn.v1"
		value.SessionPolicyRef = "workplace.progress_risk_update.session.v1"
		value.SceneSpecificContext = SceneSpecificContext{
			Type: contextType,
			Workplace: &WorkplaceProgressRiskUpdateV1{
				Version:          "workplace.progress_risk_update.v1",
				InitiativeBrief:  "Release readiness",
				Audience:         "direct manager",
				ExpectedSections: []string{"progress", "risks"},
			},
		}
	case ContextDailyHotelCheckin:
		value.TurnPolicyRef = "daily.hotel_checkin_issue.turn.v1"
		value.SessionPolicyRef = "daily.hotel_checkin_issue.session.v1"
		value.SceneSpecificContext = SceneSpecificContext{
			Type: contextType,
			Daily: &DailyHotelCheckinIssueV1{
				Version:          "daily.hotel_checkin_issue.v1",
				ReservationBrief: "A two-night booking",
				Issue:            "The room is noisy",
				DesiredOutcome:   "A quieter room",
			},
		}
	case ContextGenericPractice:
		value.TurnPolicyRef = "generic.practice.turn.v1"
		value.SessionPolicyRef = "generic.practice.session.v1"
		value.SceneSpecificContext = SceneSpecificContext{
			Type: contextType,
			Generic: &GenericPracticeV1{
				Version:      "generic.practice.v1",
				PracticeGoal: "Practice a clear response",
			},
		}
	}
	return value
}
