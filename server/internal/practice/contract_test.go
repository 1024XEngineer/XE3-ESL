package practice_test

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	practicepersistence "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestContractValues(t *testing.T) {
	tests := map[string]string{
		"scene interview":           string(scene.SceneFamilyInterview),
		"plan ready":                string(preparation.PlanStatusReady),
		"plan archived":             string(preparation.PlanStatusArchived),
		"session starting":          string(practicepersistence.ContextSessionStarting),
		"session in progress":       string(practicepersistence.ContextSessionProgress),
		"session paused":            string(practicepersistence.ContextSessionPaused),
		"session completed":         string(practicepersistence.ContextSessionCompleted),
		"session ended early":       string(practicepersistence.ContextSessionEndedEarly),
		"coverage covered":          string(practice.CoverageCovered),
		"coverage partial":          string(practice.CoveragePartial),
		"coverage uncovered":        string(practice.CoverageUncovered),
		"coverage after checkpoint": string(preparation.EarlyCompletionCoverageSatisfiedAfterCheckpoint),
		"coverage end reason":       practice.EndReasonCoverageSatisfiedAtCheckpoint,
		"follow up current":         string(practice.NextActionFollowUpCurrent),
		"move to next objective":    string(practice.NextActionMoveToNextObjective),
		"complete session":          string(practice.NextActionCompleteSession),
	}
	want := map[string]string{
		"scene interview":           "INTERVIEW",
		"plan ready":                "ready",
		"plan archived":             "archived",
		"session starting":          "starting",
		"session in progress":       "in_progress",
		"session paused":            "paused",
		"session completed":         "completed",
		"session ended early":       "ended_early",
		"coverage covered":          "covered",
		"coverage partial":          "partial",
		"coverage uncovered":        "uncovered",
		"coverage after checkpoint": "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
		"coverage end reason":       "COVERAGE_SATISFIED_AT_CHECKPOINT",
		"follow up current":         "FOLLOW_UP_CURRENT",
		"move to next objective":    "MOVE_TO_NEXT_OBJECTIVE",
		"complete session":          "COMPLETE_SESSION",
	}
	for name, value := range tests {
		if value != want[name] {
			t.Errorf("%s = %q, want %q", name, value, want[name])
		}
	}
}

func TestCrossModuleContractsExposeRequiredFields(t *testing.T) {
	createdAt := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	contracts := []any{
		preparation.PracticePlan{
			ID:     "plan-1",
			UserID: "user-1",
			SessionPolicy: preparation.SessionPolicy{
				MaxEffectiveTurns: 6,
			},
			PracticeObjectives: []preparation.PracticeObjective{{
				ID:          "objective-1",
				Description: "Explain the trade-off.",
			}},
			Revision: 1,
			Status:   preparation.PlanStatusReady,
		},
		practicepersistence.ContextSession{
			ID:        "session-1",
			PlanID:    "plan-1",
			StartedAt: &createdAt,
		},
		practicepersistence.ContextParticipant{
			ID:               "participant-1",
			SessionID:        "session-1",
			Role:             "LEARNER",
			SubjectRef:       practicepersistence.SubjectRef{Namespace: "user", SubjectID: "user-1"},
			RoleDefinitionID: "role-1",
		},
		practice.TurnOutcome{
			TurnID:      "turn-1",
			SessionID:   "session-1",
			AnswerValid: true,
		},
		practicepersistence.ContextSessionSnapshot{
			ID:        "snapshot-1",
			SessionID: "session-1",
			CreatedAt: createdAt,
		},
	}
	if len(contracts) != 5 {
		t.Fatalf("contracts = %d, want 5", len(contracts))
	}
}

func TestModuleRegistration(t *testing.T) {
	if got := practice.New().Name(); got != "practice" {
		t.Fatalf("Name() = %q, want practice", got)
	}
}
