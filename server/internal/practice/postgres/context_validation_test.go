package postgres

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestValidCreatePlanCommandAcceptsOptionalMatter(t *testing.T) {
	command := persistence.CreatePlanCommand{
		PlanID:                    "plan-1",
		AgentThreadID:             "thread-1",
		ScenarioDefinitionID:      "scenario-1",
		ScenarioDefinitionVersion: 1,
		ScenarioType:              persistence.ScenarioFamilyDaily,
		ScenarioModel: persistence.
			ScenarioModelHotelCheckinAndIssueHandling,
		ScenarioConfigID:      "config-1",
		ScenarioConfigVersion: 1,
		PreparationProfileID:  "profile-1",
		SelectedRoleIDs:       []string{"role-1"},
		Intent: persistence.ContextIdempotencyIntent{
			Method:        "POST",
			CanonicalPath: "/v1/practice-plans",
			Key:           "plan-key-1",
		},
	}
	if !validCreatePlanCommand(command) {
		t.Fatal("Matter-free scenario Plan command was rejected")
	}

	command.MatterID = "matter-1"
	if !validCreatePlanCommand(command) {
		t.Fatal("Matter-backed scenario Plan command was rejected")
	}
}

func TestValidContextPolicyScopesFourteenTurnsToIELTSFullMock(t *testing.T) {
	policy := persistence.ContextSessionPolicy{
		SuggestedDurationSeconds: 900,
		MinEffectiveTurns:        14,
		MaxEffectiveTurns:        14,
		CoverageCheckpointTurn:   14,
		MaxFollowUpsPerQuestion:  0,
		EarlyCompletionRule:      "Complete all frozen questions.",
		TargetObjectives: []persistence.PracticeObjective{
			{ID: "objective-1", Description: "Complete all three parts."},
		},
	}
	if !validContextPolicy(
		policy,
		"FULL_SIMULATION",
		persistence.ScenarioModelIELTSSpeakingFullMock,
	) {
		t.Fatal("IELTS full mock 14-turn policy was rejected")
	}
	if validContextPolicy(
		policy,
		"FULL_SIMULATION",
		persistence.ScenarioModelExamBasicDialogue,
	) {
		t.Fatal("generic exam accepted the IELTS-only 14-turn policy")
	}
	if validContextPolicy(
		policy,
		"FOCUS",
		persistence.ScenarioModelIELTSSpeakingFullMock,
	) {
		t.Fatal("IELTS focus option accepted the full-mock policy")
	}
}

func TestDynamicIELTSPreviewMatchesPlanAllowsFrozenQuestionSelection(t *testing.T) {
	objectives := []persistence.PracticeObjective{
		{ID: "objective-1", Description: "Discuss the selected topic."},
	}
	planConfig := persistence.ScenarioConfigSnapshot{
		ID:                   "config-1",
		ScenarioDefinitionID: "scenario-1",
		Type:                 persistence.ScenarioFamilyExam,
		Model:                persistence.ScenarioModelIELTSSpeakingFullMock,
		Version:              1,
		PromptModel: persistence.ScenarioPromptModel{
			PublicSceneBrief:         "Generic IELTS Part 3.",
			PracticeGoal:             "Answer every discussion question.",
			UserRole:                 "Candidate",
			AIRole:                   "Examiner",
			PersonaSummary:           "IELTS examiner",
			FocusAreas:               []string{"fluency"},
			TurnBlueprints:           []string{"q1", "q2", "q3", "q4", "q5"},
			SuggestedDurationSeconds: 300,
		},
		FocusAreas: []string{"fluency"},
	}
	planPolicy := persistence.ContextSessionPolicy{
		SuggestedDurationSeconds: 300,
		MinEffectiveTurns:        5,
		MaxEffectiveTurns:        5,
		CoverageCheckpointTurn:   5,
		MaxFollowUpsPerQuestion:  0,
		TargetObjectives:         objectives,
		EarlyCompletionRule:      "Complete all frozen questions.",
	}
	plan := persistence.Plan{
		CatalogSnapshot: &persistence.PlanCatalogSnapshot{
			ScenarioConfig: planConfig,
		},
		SessionPolicy: &planPolicy,
	}

	selectedQuestions := []string{"selected q1", "selected q2", "selected q3"}
	snapshotConfig := planConfig
	snapshotConfig.PromptModel.PublicSceneBrief = "Selected IELTS topic."
	snapshotConfig.PromptModel.TurnBlueprints = selectedQuestions
	snapshot := persistence.ContextSessionSnapshot{
		ScenarioConfig: snapshotConfig,
		SessionPolicy: persistence.ContextSessionPolicy{
			SuggestedDurationSeconds: 300,
			MinEffectiveTurns:        3,
			MaxEffectiveTurns:        3,
			CoverageCheckpointTurn:   3,
			MaxFollowUpsPerQuestion:  0,
			TargetObjectives:         objectives,
			EarlyCompletionRule:      "Complete all frozen questions.",
		},
		IELTSAssignment: &persistence.IELTSPracticeAssignment{
			TurnBlueprints: selectedQuestions,
		},
	}

	configMatches, policyMatches := dynamicIELTSPreviewMatchesPlan(
		snapshot,
		plan,
	)
	if !configMatches || !policyMatches {
		t.Fatalf(
			"selected IELTS questions were rejected: config=%t policy=%t",
			configMatches,
			policyMatches,
		)
	}

	changedConfig := snapshot
	changedConfig.ScenarioConfig.PromptModel.PracticeGoal = "Changed goal."
	configMatches, _ = dynamicIELTSPreviewMatchesPlan(changedConfig, plan)
	if configMatches {
		t.Fatal("static IELTS prompt changes were accepted")
	}

	changedPolicy := snapshot
	changedPolicy.SessionPolicy.EarlyCompletionRule = "Changed rule."
	_, policyMatches = dynamicIELTSPreviewMatchesPlan(changedPolicy, plan)
	if policyMatches {
		t.Fatal("static IELTS policy changes were accepted")
	}

	wrongTurnCount := snapshot
	wrongTurnCount.SessionPolicy.MaxEffectiveTurns = 4
	_, policyMatches = dynamicIELTSPreviewMatchesPlan(wrongTurnCount, plan)
	if policyMatches {
		t.Fatal("IELTS policy count mismatching its assignment was accepted")
	}
}
