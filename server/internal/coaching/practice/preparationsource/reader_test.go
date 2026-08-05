package preparationsource

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestProjectConfirmedPlanFreezesRetryFromPolicyRefNotSceneMetadata(
	t *testing.T,
) {
	t.Parallel()
	plan := confirmedPlanFixture()
	plan.SceneSelection.Scene.Family = scene.SceneFamilyExam
	plan.SceneSelection.Scene.Model = scene.SceneModelIELTSSpeakingFullMock
	plan.SceneSelection.Scene.SessionPolicyRef =
		practice.DailyPracticeSessionPolicy

	projection, err := ProjectConfirmedPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.SessionPolicy.RetryAllowed {
		t.Fatalf("daily policy ref projection = %#v", projection.SessionPolicy)
	}

	plan.SceneSelection.Scene.Family = scene.SceneFamilyDaily
	plan.SceneSelection.Scene.Model = scene.SceneModelDailyBasicDialogue
	plan.SceneSelection.Scene.SessionPolicyRef =
		practice.InterviewPracticeSessionPolicy
	plan.SessionPolicy.RetryAllowed = false
	plan.SessionPolicy.QuestionTranslationAllowed = true
	projection, err = ProjectConfirmedPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if projection.SessionPolicy.RetryAllowed ||
		!projection.SessionPolicy.QuestionTranslationAllowed {
		t.Fatalf("interview policy ref projection = %#v", projection.SessionPolicy)
	}
}

func TestProjectConfirmedPlanCarriesOnlySelectedSceneValues(t *testing.T) {
	t.Parallel()
	plan := confirmedPlanFixture()
	projection, err := ProjectConfirmedPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.SceneSelection.Scene.Roles) != 1 ||
		projection.SceneSelection.Scene.Roles[0].ID != "role-selected" ||
		len(projection.SceneSelection.Scene.PracticeOptions) != 1 ||
		projection.SceneSelection.Scene.PracticeOptions[0].ID !=
			"option-selected" {
		t.Fatalf("Scene projection = %#v", projection.SceneSelection)
	}

	plan.SceneSelection.Scene.Roles[0].DisplayName = "changed"
	plan.SceneSelection.Scene.Prompt.TurnBlueprints[0] = "changed"
	if projection.SceneSelection.Scene.Roles[0].DisplayName == "changed" ||
		projection.SceneSelection.Scene.Prompt.TurnBlueprints[0] == "changed" {
		t.Fatal("projection retained mutable Plan values")
	}
}

func TestProjectConfirmedPlanRejectsPolicyThatContradictsRegistry(
	t *testing.T,
) {
	t.Parallel()
	plan := confirmedPlanFixture()
	plan.SessionPolicy.RetryAllowed = false
	if _, err := ProjectConfirmedPlan(plan); err == nil {
		t.Fatal("projection accepted Preparation-owned policy change")
	}
}

func confirmedPlanFixture() preparation.PracticePlan {
	prompt := scene.ScenePrompt{
		PublicSceneBrief:         "Brief",
		PracticeGoal:             "Goal",
		UserRole:                 "Learner",
		AIRole:                   "Counterpart",
		PersonaSummary:           "Direct counterpart",
		FocusAreas:               []string{"clarity"},
		TurnBlueprints:           []string{"one", "two", "three", "four"},
		SuggestedDurationSeconds: 600,
	}
	return preparation.PracticePlan{
		ID:     "plan-1",
		UserID: "user-1",
		PreparationSnapshot: preparation.Snapshot{
			ID:                 "preparation-1",
			SourceProfileID:    "profile-1",
			SourceVersion:      1,
			BackgroundSnapshot: "background",
			CreatedAt:          time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC),
		},
		SceneSelection: scene.SelectionSnapshot{
			Scene: scene.SceneDefinition{
				ID:               "scene-1",
				Family:           scene.SceneFamilyDaily,
				Model:            scene.SceneModelDailyBasicDialogue,
				Name:             "Scene",
				Version:          1,
				Status:           scene.SceneStatusActive,
				TurnPolicyRef:    practice.GenericPracticeTurnPolicy,
				SessionPolicyRef: practice.DailyPracticeSessionPolicy,
				Prompt:           prompt,
				Roles: []scene.RoleDefinition{
					{ID: "role-selected", SceneID: "scene-1", DisplayName: "selected"},
					{ID: "role-unselected", SceneID: "scene-1", DisplayName: "unselected"},
				},
				PracticeOptions: []scene.PracticeOption{
					{ID: "option-selected", SceneID: "scene-1", Type: scene.PracticeOptionFullSimulation},
					{ID: "option-unselected", SceneID: "scene-1", Type: scene.PracticeOptionFullSimulation},
				},
			},
			SelectedRoleIDs:  []string{"role-selected"},
			PracticeOptionID: "option-selected",
		},
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: 600,
			MinEffectiveTurns:        4,
			MaxEffectiveTurns:        6,
			CoverageCheckpointTurn:   4,
			MaxFollowUpsPerQuestion:  1,
			EarlyCompletionRule: preparation.
				EarlyCompletionCoverageSatisfiedAfterCheckpoint,
			RetryAllowed: true,
		},
		PracticeObjectives: []preparation.PracticeObjective{{
			ID: "clarity", Description: "Speak clearly.",
		}},
		Revision: 1,
		Status:   preparation.PlanStatusReady,
	}
}
