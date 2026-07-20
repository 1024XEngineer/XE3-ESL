package practice_test

import (
	"context"
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
		practice.PracticePlan{ID: "plan-1", UserID: "user-1", Revision: 1, Status: practice.PracticePlanReady},
		practice.PracticeSession{ID: "session-1", PlanID: "plan-1", StartedAt: &createdAt},
		practice.PracticeParticipant{
			ID:               "participant-1",
			SessionID:        "session-1",
			ParticipantRole:  "CANDIDATE",
			SubjectRef:       practice.SubjectRef{Namespace: "user", SubjectID: "user-1"},
			RoleDefinitionID: "role-1",
		},
		practice.PracticeSessionPolicy{Mode: practice.PracticeModeFullSimulation, TargetObjectiveIDs: []string{"objective-1"}},
		practice.TurnOutcome{TurnID: "turn-1", SessionID: "session-1", AnswerValid: true},
		practice.PracticeSessionSnapshot{ID: "snapshot-1", SessionID: "session-1", CreatedAt: createdAt},
	}
	if len(contracts) != 6 {
		t.Fatalf("contracts = %d, want 6", len(contracts))
	}
}

var (
	_ func(practice.PlanService, context.Context, practice.CreatePracticePlanCommand) (practice.PracticePlan, error)                     = practice.PlanService.CreatePracticePlan
	_ func(practice.PlanService, context.Context, practice.RetryPracticePlanConfigurationCommand) (practice.PracticePlan, error)         = practice.PlanService.RetryPracticePlanConfiguration
	_ func(practice.PlanService, context.Context, practice.UpdatePracticePlanCommand) (practice.PracticePlan, error)                     = practice.PlanService.UpdatePracticePlan
	_ func(practice.PlanService, context.Context, practice.ArchivePracticePlanCommand) (practice.PracticePlan, error)                    = practice.PlanService.ArchivePracticePlan
	_ func(practice.PlanService, context.Context, practice.RestorePracticePlanCommand) (practice.PracticePlan, error)                    = practice.PlanService.RestorePracticePlan
	_ func(practice.PlanService, context.Context, practice.DeleteEmptyPracticePlanCommand) error                                         = practice.PlanService.DeleteEmptyPracticePlan
	_ func(practice.PlanService, context.Context, practice.GetPracticePlanQuery) (practice.PracticePlan, error)                          = practice.PlanService.GetPracticePlan
	_ func(practice.PlanService, context.Context, practice.ListPracticePlansQuery) ([]practice.PracticePlan, error)                      = practice.PlanService.ListPracticePlans
	_ func(practice.SessionService, context.Context, practice.CreatePracticeSessionCommand) (practice.PracticeSession, error)            = practice.SessionService.CreatePracticeSession
	_ func(practice.SessionService, context.Context, practice.StartPracticeSessionCommand) (practice.PracticeSession, error)             = practice.SessionService.StartPracticeSession
	_ func(practice.SessionService, context.Context, practice.PausePracticeSessionCommand) (practice.PracticeSession, error)             = practice.SessionService.PausePracticeSession
	_ func(practice.SessionService, context.Context, practice.ResumePracticeSessionCommand) (practice.PracticeSession, error)            = practice.SessionService.ResumePracticeSession
	_ func(practice.SessionService, context.Context, practice.EndPracticeSessionEarlyCommand) (practice.PracticeSession, error)          = practice.SessionService.EndPracticeSessionEarly
	_ func(practice.SessionService, context.Context, practice.GetActivePracticeSessionQuery) (practice.PracticeSession, error)           = practice.SessionService.GetActivePracticeSession
	_ func(practice.SessionService, context.Context, practice.GetPracticeSessionSnapshotQuery) (practice.PracticeSessionSnapshot, error) = practice.SessionService.GetPracticeSessionSnapshot
	_ func(practice.SessionService, context.Context, practice.ResolveActorParticipantQuery) (string, error)                              = practice.SessionService.ResolveActorParticipant
	_ func(practice.SessionService, context.Context, practice.ApplyTurnOutcomeCommand) (practice.NextAction, error)                      = practice.SessionService.ApplyTurnOutcome
	_ func(practice.PreparationReader, context.Context, string) (practice.ScenarioDefinitionSnapshot, error)                             = practice.PreparationReader.GetScenarioDefinition
	_ func(practice.PreparationReader, context.Context, string) (practice.ScenarioConfigSnapshot, error)                                 = practice.PreparationReader.GetScenarioConfig
	_ func(practice.PreparationReader, context.Context, string) (practice.PreparationSnapshot, error)                                    = practice.PreparationReader.GetPreparationSnapshot
	_ func(practice.PreparationReader, context.Context, string) (practice.RoleSnapshot, error)                                           = practice.PreparationReader.GetRoleDefinition
	_ func(practice.PreparationReader, context.Context, string) (practice.PracticeOptionSnapshot, error)                                 = practice.PreparationReader.GetPracticeOption
)

func TestModuleRegistration(t *testing.T) {
	if got := practice.New().Name(); got != "practice" {
		t.Fatalf("Name() = %q, want practice", got)
	}
}
