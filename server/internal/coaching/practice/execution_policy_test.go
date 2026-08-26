package practice

import (
	"errors"
	"testing"
)

func TestResolveSessionPolicyUsesExactReference(t *testing.T) {
	t.Parallel()
	prompt := ScenePrompt{
		TurnBlueprints: []string{"one", "two", "three", "four"},
	}
	option := PracticeOption{
		Mode:                     PracticeModeFullSimulation,
		SuggestedDurationSeconds: 600,
	}
	tests := []struct {
		name       string
		reference  string
		completion CompletionMode
		maxTurns   int
		retry      bool
		readAids   bool
		followUps  int
		wantErr    error
	}{
		{"daily", DailyPracticeSessionPolicy, CompletionModeUserControlled, 0, true, true, 1, nil},
		{"daily hotel", DailyHotelCheckinIssueSessionPolicy, CompletionModeUserControlled, 0, true, true, 1, nil},
		{"workplace", WorkplacePracticeSessionPolicy, CompletionModeUserControlled, 0, true, true, 1, nil},
		{"workplace risk", WorkplaceProgressRiskUpdateSessionPolicy, CompletionModeUserControlled, 0, true, true, 1, nil},
		{"turn-limited interview", InterviewPracticeSessionPolicy, CompletionModeTurnLimited, 6, false, true, 3, nil},
		{"interview", InterviewUserControlledSessionPolicy, CompletionModeUserControlled, 0, false, true, 3, nil},
		{"turn-limited interview deep dive", InterviewProjectDeepDiveSessionPolicy, CompletionModeTurnLimited, 6, false, true, 3, nil},
		{"interview deep dive", InterviewProjectDeepDiveUserControlledSessionPolicy, CompletionModeUserControlled, 0, false, true, 3, nil},
		{"exam", ExamPracticeSessionPolicy, CompletionModeTurnLimited, 6, false, false, 1, nil},
		{"unknown", "unknown.practice.session.v1", "", 0, false, false, 0, ErrExecutionPolicyNotFound},
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
			if err != nil || policy.CompletionMode != test.completion ||
				policy.MaxEffectiveTurns != test.maxTurns ||
				policy.RetryAllowed != test.retry ||
				policy.QuestionTranslationAllowed != test.readAids ||
				policy.QuestionTipsAllowed != test.readAids ||
				!policy.SpeechFeedbackAllowed ||
				policy.MaxFollowUpsPerQuestion != test.followUps ||
				!ValidSessionPolicy(
					test.reference,
					option.Mode,
					len(prompt.TurnBlueprints),
					option.SuggestedDurationSeconds,
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
		TurnBlueprints: []string{"one", "two", "three", "four"},
	}
	option := PracticeOption{
		Mode:                     PracticeModeFullSimulation,
		SuggestedDurationSeconds: 480,
	}
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
		option.Mode,
		len(prompt.TurnBlueprints),
		option.SuggestedDurationSeconds,
		policy,
	) {
		t.Fatal("daily policy accepted contradictory retry value")
	}
}

func TestEverySessionPolicyAllowsSpeechFeedback(t *testing.T) {
	t.Parallel()
	registrations := []struct {
		reference string
		mode      PracticeMode
	}{
		{GenericPracticeSessionPolicy, PracticeModeFullSimulation},
		{DailyPracticeSessionPolicy, PracticeModeFullSimulation},
		{WorkplacePracticeSessionPolicy, PracticeModeFullSimulation},
		{InterviewPracticeSessionPolicy, PracticeModeFullSimulation},
		{InterviewUserControlledSessionPolicy, PracticeModeFullSimulation},
		{ExamPracticeSessionPolicy, PracticeModeFullSimulation},
		{DailyHotelCheckinIssueSessionPolicy, PracticeModeFullSimulation},
		{WorkplaceProgressRiskUpdateSessionPolicy, PracticeModeFullSimulation},
		{InterviewProjectDeepDiveSessionPolicy, PracticeModeFullSimulation},
		{
			InterviewProjectDeepDiveUserControlledSessionPolicy,
			PracticeModeFullSimulation,
		},
		{IELTSSpeakingPart1SessionPolicy, PracticeModePart1},
		{IELTSSpeakingPart2SessionPolicy, PracticeModePart2},
		{IELTSSpeakingPart3SessionPolicy, PracticeModePart3},
		{IELTSSpeakingFullMockSessionPolicy, PracticeModeFullMock},
	}
	prompt := ScenePrompt{
		TurnBlueprints: []string{"one", "two", "three", "four"},
	}
	for _, registration := range registrations {
		registration := registration
		t.Run(registration.reference, func(t *testing.T) {
			policy, err := ResolveSessionPolicy(
				registration.reference,
				prompt,
				PracticeOption{
					Mode:                     registration.mode,
					SuggestedDurationSeconds: 600,
				},
				0,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !policy.SpeechFeedbackAllowed {
				t.Fatalf("speech feedback disabled: %#v", policy)
			}
		})
	}
}

func TestResolveSessionPolicyFreezesIELTSBlueprintCount(t *testing.T) {
	t.Parallel()
	blueprints := make([]string, 15)
	for index := range blueprints {
		blueprints[index] = "question"
	}
	prompt := ScenePrompt{
		TurnBlueprints: blueprints,
	}
	policy, err := ResolveSessionPolicy(
		IELTSSpeakingPart2SessionPolicy,
		prompt,
		PracticeOption{
			Mode: PracticeModePart2, SuggestedDurationSeconds: 900,
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if policy.MinEffectiveTurns != len(blueprints) ||
		policy.MaxEffectiveTurns != len(blueprints) ||
		policy.CoverageCheckpointTurn != len(blueprints) ||
		policy.MaxFollowUpsPerQuestion != 0 || policy.RetryAllowed {
		t.Fatalf("IELTS policy = %#v", policy)
	}
}

func TestResolveSessionPolicyAllowsReadAidsOnlyForIELTSSectionPractice(
	t *testing.T,
) {
	t.Parallel()
	prompt := ScenePrompt{TurnBlueprints: []string{"question"}}
	tests := []struct {
		name      string
		reference string
		mode      PracticeMode
		wantAids  bool
	}{
		{"part 1", IELTSSpeakingPart1SessionPolicy, PracticeModePart1, true},
		{"part 2", IELTSSpeakingPart2SessionPolicy, PracticeModePart2, true},
		{"part 3", IELTSSpeakingPart3SessionPolicy, PracticeModePart3, true},
		{"full mock", IELTSSpeakingFullMockSessionPolicy, PracticeModeFullMock, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy, err := ResolveSessionPolicy(
				test.reference,
				prompt,
				PracticeOption{
					Mode: test.mode, SuggestedDurationSeconds: 300,
				},
				0,
			)
			if err != nil {
				t.Fatal(err)
			}
			if policy.QuestionTranslationAllowed != test.wantAids ||
				policy.QuestionTipsAllowed != test.wantAids {
				t.Fatalf("IELTS read aids policy = %#v", policy)
			}
		})
	}
}
