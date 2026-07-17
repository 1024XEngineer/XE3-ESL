package practice_test

import (
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
)

func TestEvaluatePolicyProgression(t *testing.T) {
	tests := []struct {
		name           string
		mode           practice.PracticeMode
		effectiveTurns int
		coverage       practice.CoverageLevel
		followUpGap    bool
		followUpCount  int
		remainingTime  int
		want           practice.NextAction
	}{
		{name: "full completes at checkpoint when covered", mode: practice.PracticeModeFullSimulation, effectiveTurns: 4, coverage: practice.CoverageCovered, remainingTime: 1, want: practice.NextActionCompleteSession},
		{name: "full continues at checkpoint when uncovered", mode: practice.PracticeModeFullSimulation, effectiveTurns: 4, remainingTime: 1, want: practice.NextActionMoveToNextObjective},
		{name: "full continues at checkpoint when partial", mode: practice.PracticeModeFullSimulation, effectiveTurns: 4, coverage: practice.CoveragePartial, remainingTime: 1, want: practice.NextActionMoveToNextObjective},
		{name: "full completes at hard limit", mode: practice.PracticeModeFullSimulation, effectiveTurns: 6, followUpGap: true, want: practice.NextActionCompleteSession},
		{name: "focused completes at checkpoint when covered", mode: practice.PracticeModeFocusedPractice, effectiveTurns: 2, coverage: practice.CoverageCovered, remainingTime: 1, want: practice.NextActionCompleteSession},
		{name: "focused continues at checkpoint when uncovered", mode: practice.PracticeModeFocusedPractice, effectiveTurns: 2, remainingTime: 1, want: practice.NextActionMoveToNextObjective},
		{name: "focused completes at hard limit", mode: practice.PracticeModeFocusedPractice, effectiveTurns: 3, followUpGap: true, want: practice.NextActionCompleteSession},
		{name: "follows up below limit", mode: practice.PracticeModeFullSimulation, effectiveTurns: 2, followUpGap: true, followUpCount: 0, want: practice.NextActionFollowUpCurrent},
		{name: "moves on at follow up limit", mode: practice.PracticeModeFullSimulation, effectiveTurns: 2, followUpGap: true, followUpCount: 1, want: practice.NextActionMoveToNextObjective},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := practice.NewPracticeSessionPolicy(tt.mode, []string{"objective-1"})
			if err != nil {
				t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
			}
			got, err := practice.EvaluatePolicy(policy, practice.ProgressInput{
				EffectiveTurnCount: tt.effectiveTurns, RemainingTimeSeconds: tt.remainingTime,
				Outcome: practice.TurnOutcome{
					TurnID:            "turn-1",
					SessionID:         "session-1",
					AnswerValid:       true,
					FollowUpGap:       tt.followUpGap,
					FollowUpCount:     tt.followUpCount,
					ObjectiveCoverage: []practice.ObjectiveCoverage{{ObjectiveID: "objective-1", Level: tt.coverage}},
				},
			})
			if err != nil {
				t.Fatalf("EvaluatePolicy() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("EvaluatePolicy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluatePolicyRequiresAllTargetObjectives(t *testing.T) {
	policy, err := practice.NewPracticeSessionPolicy(practice.PracticeModeFullSimulation, []string{"objective-1", "objective-2"})
	if err != nil {
		t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
	}
	tests := []struct {
		name     string
		coverage []practice.ObjectiveCoverage
		want     practice.NextAction
	}{
		{name: "部分目标已覆盖", coverage: []practice.ObjectiveCoverage{{ObjectiveID: "objective-1", Level: practice.CoverageCovered}, {ObjectiveID: "objective-2", Level: practice.CoveragePartial}}, want: practice.NextActionMoveToNextObjective},
		{name: "全部目标已覆盖", coverage: []practice.ObjectiveCoverage{{ObjectiveID: "objective-1", Level: practice.CoverageCovered}, {ObjectiveID: "objective-2", Level: practice.CoverageCovered}}, want: practice.NextActionCompleteSession},
		{name: "无关目标不计入", coverage: []practice.ObjectiveCoverage{{ObjectiveID: "objective-other", Level: practice.CoverageCovered}}, want: practice.NextActionMoveToNextObjective},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := practice.EvaluatePolicy(policy, practice.ProgressInput{EffectiveTurnCount: 4, RemainingTimeSeconds: 1, Outcome: practice.TurnOutcome{TurnID: "turn-1", SessionID: "session-1", AnswerValid: true, ObjectiveCoverage: tt.coverage}})
			if err != nil || got != tt.want {
				t.Fatalf("EvaluatePolicy() = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestEvaluatePolicyCompletesAtCheckpointWhenTimeBudgetIsExhausted(t *testing.T) {
	policy, err := practice.NewPracticeSessionPolicy(practice.PracticeModeFullSimulation, []string{"objective-1"})
	if err != nil {
		t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
	}
	outcome := practice.TurnOutcome{TurnID: "turn-1", SessionID: "session-1", AnswerValid: true}
	for _, tt := range []struct {
		name             string
		turns, remaining int
		want             practice.NextAction
	}{
		{name: "检查点前不结束", turns: 3, remaining: 0, want: practice.NextActionMoveToNextObjective},
		{name: "检查点时间刚好耗尽", turns: 4, remaining: 0, want: practice.NextActionCompleteSession},
		{name: "检查点时间已超出", turns: 4, remaining: -1, want: practice.NextActionCompleteSession},
		{name: "检查点仍有时间", turns: 4, remaining: 1, want: practice.NextActionMoveToNextObjective},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := practice.EvaluatePolicy(policy, practice.ProgressInput{EffectiveTurnCount: tt.turns, RemainingTimeSeconds: tt.remaining, Outcome: outcome})
			if err != nil || got != tt.want {
				t.Fatalf("EvaluatePolicy() = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestEvaluatePolicyRejectsInvalidOutcome(t *testing.T) {
	policy, err := practice.NewPracticeSessionPolicy(practice.PracticeModeFullSimulation, []string{"objective-1"})
	if err != nil {
		t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
	}

	tests := []practice.TurnOutcome{
		{SessionID: "session-1", AnswerValid: true},
		{TurnID: "turn-1", AnswerValid: true},
		{TurnID: "turn-1", SessionID: "session-1", AnswerValid: false},
		{TurnID: "turn-1", SessionID: "session-1", AnswerValid: true, CompletedPrimaryQuestionCount: -1},
	}
	for _, outcome := range tests {
		_, err := practice.EvaluatePolicy(policy, practice.ProgressInput{EffectiveTurnCount: 1, Outcome: outcome})
		if !errors.Is(err, practice.ErrTurnOutcomeInvalid) {
			t.Fatalf("EvaluatePolicy() error = %v, want %v", err, practice.ErrTurnOutcomeInvalid)
		}
	}
}

func TestEvaluatePolicyIsDeterministic(t *testing.T) {
	policy, err := practice.NewPracticeSessionPolicy(practice.PracticeModeFullSimulation, []string{"objective-1"})
	if err != nil {
		t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
	}
	input := practice.ProgressInput{
		EffectiveTurnCount: 3,
		Outcome: practice.TurnOutcome{
			TurnID:            "turn-1",
			SessionID:         "session-1",
			AnswerValid:       true,
			ObjectiveCoverage: []practice.ObjectiveCoverage{{ObjectiveID: "objective-1", Level: practice.CoverageUncovered}},
		},
	}

	first, firstErr := practice.EvaluatePolicy(policy, input)
	second, secondErr := practice.EvaluatePolicy(policy, input)
	if firstErr != nil || secondErr != nil {
		t.Fatalf("EvaluatePolicy() errors = %v, %v", firstErr, secondErr)
	}
	if first != second {
		t.Fatalf("EvaluatePolicy() results = %q, %q", first, second)
	}
}

func TestPracticeSessionPolicyFields(t *testing.T) {
	tests := []struct {
		name                    string
		mode                    practice.PracticeMode
		wantDurationSeconds     int
		wantMinTurns            int
		wantMaxTurns            int
		wantCoverageCheckpoint  int
		wantEarlyCompletionRule practice.EarlyCompletionRule
	}{
		{name: "full simulation", mode: practice.PracticeModeFullSimulation, wantDurationSeconds: 600, wantMinTurns: 4, wantMaxTurns: 6, wantCoverageCheckpoint: 4, wantEarlyCompletionRule: practice.EarlyCompletionObjectivesCovered},
		{name: "focused practice", mode: practice.PracticeModeFocusedPractice, wantDurationSeconds: 300, wantMinTurns: 2, wantMaxTurns: 3, wantCoverageCheckpoint: 2, wantEarlyCompletionRule: practice.EarlyCompletionObjectivesCovered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectives := []string{"objective-1"}
			policy, err := practice.NewPracticeSessionPolicy(tt.mode, objectives)
			if err != nil {
				t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
			}
			objectives[0] = "changed"
			gotObjectives := policy.TargetObjectiveIDs()
			gotObjectives[0] = "changed-again"

			if policy.SuggestedDurationSeconds() != tt.wantDurationSeconds || policy.MinEffectiveTurns() != tt.wantMinTurns || policy.MaxEffectiveTurns() != tt.wantMaxTurns || policy.CoverageCheckpointTurn() != tt.wantCoverageCheckpoint || policy.EarlyCompletionRule() != tt.wantEarlyCompletionRule {
				t.Fatalf("policy fields do not match mode %q", tt.mode)
			}
			if got := policy.TargetObjectiveIDs()[0]; got != "objective-1" {
				t.Fatalf("TargetObjectiveIDs()[0] = %q, want objective-1", got)
			}
		})
	}
}
