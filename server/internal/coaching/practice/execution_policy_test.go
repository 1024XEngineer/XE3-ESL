package practice

import (
	"errors"
	"testing"
)

func TestResolveSessionPolicyUsesExactReference(t *testing.T) {
	t.Parallel()
	prompt := ScenePrompt{
		SuggestedDurationSeconds: 600,
		TurnBlueprints:           []string{"one", "two", "three", "four"},
	}
	option := PracticeOption{Type: PracticeOptionFullSimulation}
	tests := []struct {
		name        string
		reference   string
		retry       bool
		translation bool
		followUps   int
		wantErr     error
	}{
		{"daily", DailyPracticeSessionPolicy, true, false, 1, nil},
		{"workplace", WorkplacePracticeSessionPolicy, true, false, 1, nil},
		{"interview", InterviewPracticeSessionPolicy, false, true, 1, nil},
		{"interview deep dive", InterviewProjectDeepDiveSessionPolicy, false, true, 3, nil},
		{"exam", ExamPracticeSessionPolicy, false, false, 1, nil},
		{"unknown", "unknown.practice.session.v1", false, false, 0, ErrExecutionPolicyNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy, err := ResolveSessionPolicy(test.reference, prompt, option, 0)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("ResolveSessionPolicy() error = %v", err)
				}
				return
			}
			if err != nil || policy.RetryAllowed != test.retry ||
				policy.QuestionTranslationAllowed != test.translation ||
				policy.MaxFollowUpsPerQuestion != test.followUps ||
				!ValidSessionPolicy(
					test.reference,
					option.Type,
					len(prompt.TurnBlueprints),
					policy,
				) {
				t.Fatalf("ResolveSessionPolicy() = %#v, %v", policy, err)
			}
		})
	}
}

func TestValidSessionPolicyRejectsPolicyValueThatContradictsReference(
	t *testing.T,
) {
	t.Parallel()
	prompt := ScenePrompt{
		SuggestedDurationSeconds: 480,
		TurnBlueprints:           []string{"one", "two", "three", "four"},
	}
	option := PracticeOption{Type: PracticeOptionFullSimulation}
	policy, err := ResolveSessionPolicy(
		DailyPracticeSessionPolicy,
		prompt,
		option,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	policy.RetryAllowed = false
	if ValidSessionPolicy(
		DailyPracticeSessionPolicy,
		option.Type,
		len(prompt.TurnBlueprints),
		policy,
	) {
		t.Fatal("daily policy accepted contradictory retry value")
	}
}

func TestResolveSessionPolicyFreezesIELTSBlueprintCount(t *testing.T) {
	t.Parallel()
	prompt := ScenePrompt{
		SuggestedDurationSeconds: 900,
		TurnBlueprints:           []string{"one", "two", "three"},
	}
	policy, err := ResolveSessionPolicy(
		IELTSSpeakingPart2SessionPolicy,
		prompt,
		PracticeOption{Type: PracticeOptionFullSimulation},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if policy.MinEffectiveTurns != 3 || policy.MaxEffectiveTurns != 3 ||
		policy.CoverageCheckpointTurn != 3 ||
		policy.MaxFollowUpsPerQuestion != 0 || policy.RetryAllowed {
		t.Fatalf("IELTS policy = %#v", policy)
	}
}
