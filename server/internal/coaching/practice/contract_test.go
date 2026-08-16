package practice_test

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestContractValues(t *testing.T) {
	tests := map[string]string{
		"scene interview":           string(scene.PracticeExperienceInterview),
		"plan ready":                string(preparation.PlanStatusReady),
		"session starting":          string(practice.SessionStarting),
		"session in progress":       string(practice.SessionInProgress),
		"session paused":            string(practice.SessionPaused),
		"session completed":         string(practice.SessionCompleted),
		"session ended early":       string(practice.SessionEndedEarly),
		"coverage after checkpoint": string(preparation.EarlyCompletionCoverageSatisfiedAfterCheckpoint),
	}
	want := map[string]string{
		"scene interview":           "INTERVIEW",
		"plan ready":                "ready",
		"session starting":          "starting",
		"session in progress":       "in_progress",
		"session paused":            "paused",
		"session completed":         "completed",
		"session ended early":       "ended_early",
		"coverage after checkpoint": "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
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
			Version: 1,
			Status:  preparation.PlanStatusReady,
		},
		practice.Session{
			ID:        "session-1",
			PlanID:    "plan-1",
			StartedAt: &createdAt,
		},
		practice.Participant{
			ID:               "participant-1",
			SessionID:        "session-1",
			Role:             "LEARNER",
			SubjectRef:       practice.SubjectRef{Namespace: "user", SubjectID: "user-1"},
			RoleDefinitionID: "role-1",
		},
		practice.SessionSnapshot{SessionID: "session-1"},
	}
	if len(contracts) != 4 {
		t.Fatalf("contracts = %d, want 4", len(contracts))
	}
}

func TestModuleRegistration(t *testing.T) {
	if got := practice.New().Name(); got != "practice" {
		t.Fatalf("Name() = %q, want practice", got)
	}
}
