package practice_test

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
)

func TestContractValues(t *testing.T) {
	tests := map[string]string{
		"scenario interview":        string(practice.ScenarioTypeInterview),
		"plan configuring":          string(practice.PracticePlanConfiguring),
		"plan configuration failed": string(practice.PracticePlanConfigurationFailed),
		"plan ready":                string(practice.PracticePlanReady),
		"plan archived":             string(practice.PracticePlanArchived),
		"session starting":          string(practice.PracticeSessionStarting),
		"session in progress":       string(practice.PracticeSessionInProgress),
		"session paused":            string(practice.PracticeSessionPaused),
		"session completed":         string(practice.PracticeSessionCompleted),
		"session ended early":       string(practice.PracticeSessionEndedEarly),
		"full simulation":           string(practice.PracticeModeFullSimulation),
		"focused practice":          string(practice.PracticeModeFocusedPractice),
		"coverage covered":          string(practice.CoverageCovered),
		"coverage partial":          string(practice.CoveragePartial),
		"coverage uncovered":        string(practice.CoverageUncovered),
		"objectives covered":        string(practice.EarlyCompletionObjectivesCovered),
		"coverage after checkpoint": string(practice.EarlyCompletionCoverageSatisfiedAfterCheckpoint),
		"coverage end reason":       string(practice.PracticeSessionEndCoverageSatisfiedAtCheckpoint),
		"follow up current":         string(practice.NextActionFollowUpCurrent),
		"move to next objective":    string(practice.NextActionMoveToNextObjective),
		"complete session":          string(practice.NextActionCompleteSession),
	}
	want := map[string]string{
		"scenario interview":        "INTERVIEW",
		"plan configuring":          "configuring",
		"plan configuration failed": "configuration_failed",
		"plan ready":                "ready",
		"plan archived":             "archived",
		"session starting":          "starting",
		"session in progress":       "in_progress",
		"session paused":            "paused",
		"session completed":         "completed",
		"session ended early":       "ended_early",
		"full simulation":           "full_simulation",
		"focused practice":          "focused_practice",
		"coverage covered":          "covered",
		"coverage partial":          "partial",
		"coverage uncovered":        "uncovered",
		"objectives covered":        "objectives_covered",
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
		practice.PracticePlan{
			ID:       "plan-1",
			UserID:   "user-1",
			Revision: 1,
			Status:   practice.PracticePlanReady,
		},
		practice.PracticeSession{
			ID:        "session-1",
			PlanID:    "plan-1",
			StartedAt: &createdAt,
		},
		practice.PracticeParticipant{
			ID:               "participant-1",
			SessionID:        "session-1",
			ParticipantRole:  "CANDIDATE",
			SubjectRef:       practice.SubjectRef{Namespace: "user", SubjectID: "user-1"},
			RoleDefinitionID: "role-1",
		},
		practice.PracticeSessionPolicy{
			Mode: practice.PracticeModeFullSimulation,
			TargetObjectives: []practice.PracticeObjective{{
				ID:          "objective-1",
				Description: "Explain the trade-off.",
			}},
		},
		practice.TurnOutcome{
			TurnID:      "turn-1",
			SessionID:   "session-1",
			AnswerValid: true,
		},
		practice.PracticeSessionSnapshot{
			ID:        "snapshot-1",
			SessionID: "session-1",
			CreatedAt: createdAt,
		},
	}
	if len(contracts) != 6 {
		t.Fatalf("contracts = %d, want 6", len(contracts))
	}
}

var (
	_ func(*practice.Service, practice.CreatePracticePlanCommand) (practice.PracticePlan, error)                   = (*practice.Service).CreatePracticePlan
	_ func(*practice.Service, practice.PracticePlanExistsQuery) bool                                               = (*practice.Service).PracticePlanExists
	_ func(*practice.Service, practice.CreatePracticeSessionCommand) (practice.CreatePracticeSessionResult, error) = (*practice.Service).CreatePracticeSession
	_ func(*practice.Service, practice.GetPracticeSessionQuery) (practice.PracticeSession, bool)                   = (*practice.Service).GetPracticeSession
	_ func(*practice.Service, practice.GetPracticeSessionSnapshotQuery) (practice.PracticeSessionSnapshot, bool)   = (*practice.Service).GetPracticeSessionSnapshot
	_ func(*practice.Service, practice.StartPracticeSessionCommand) (practice.StartPracticeSessionResult, error)   = (*practice.Service).StartPracticeSession
	_ func(*practice.Service, practice.AuthorizePracticeTurnCommand) error                                         = (*practice.Service).AuthorizePracticeTurn
	_ func(*practice.Service, practice.ApplyTurnOutcomeCommand) (practice.ApplyTurnOutcomeResult, error)           = (*practice.Service).ApplyTurnOutcome
)

func TestModuleRegistration(t *testing.T) {
	if got := practice.New().Name(); got != "practice" {
		t.Fatalf("Name() = %q, want practice", got)
	}
}
