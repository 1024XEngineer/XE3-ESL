package postgres

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestValidCreateContextSessionCommandUsesCanonicalPlanSnapshot(t *testing.T) {
	t.Parallel()
	actor := persistence.Actor{UserID: "user-1", SessionID: "auth-1"}
	command := validContextSessionCommandFixture(actor.UserID)
	if !validCreateContextSessionCommand(actor, command) {
		t.Fatal("valid canonical Session command was rejected")
	}
	command.Snapshot.PracticeObjectives = nil
	if validCreateContextSessionCommand(actor, command) {
		t.Fatal("Session without frozen Practice objectives was accepted")
	}
}

func TestValidContextSnapshotRejectsLegacyParticipantRoles(t *testing.T) {
	t.Parallel()
	actor := persistence.Actor{UserID: "user-1", SessionID: "auth-1"}
	for _, role := range []string{"INTERVIEWER", "CANDIDATE"} {
		command := validContextSessionCommandFixture(actor.UserID)
		command.Snapshot.Participants[0].Role = role
		if validCreateContextSessionCommand(actor, command) {
			t.Fatalf("legacy participant role %q was accepted", role)
		}
	}
}

func validContextSessionCommandFixture(
	actorUserID string,
) persistence.CreateContextSessionCommand {
	createdAt := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	definition := scene.SceneDefinition{
		ID:      "scene-1",
		Family:  scene.SceneFamilyInterview,
		Model:   scene.SceneModelInterviewBasicDialogue,
		Version: 1,
		Status:  scene.SceneStatusActive,
		Prompt: scene.ScenePrompt{
			FocusAreas:               []string{"clarity"},
			TurnBlueprints:           []string{"question"},
			SuggestedDurationSeconds: 600,
		},
		Roles: []scene.RoleDefinition{{
			ID: "role-1", SceneID: "scene-1",
			PracticeObjectives: []scene.PracticeObjectiveDefinition{{
				ID: "clarity", Description: "Explain the answer clearly.",
			}},
		}},
		PracticeOptions: []scene.PracticeOption{{
			ID: "option-1", SceneID: "scene-1",
			Type: scene.PracticeOptionFullSimulation,
		}},
	}
	snapshot := persistence.ContextSessionSnapshot{
		ID:           "snapshot-1",
		SessionID:    "session-1",
		PlanRevision: 1,
		SceneFamily:  definition.Family,
		SceneModel:   definition.Model,
		SceneSelection: scene.SelectionSnapshot{
			Scene: definition, SelectedRoleIDs: []string{"role-1"},
			PracticeOptionID: "option-1",
		},
		Preparation: preparation.Snapshot{
			ID: "preparation-1", SourceProfileID: "profile-1", SourceVersion: 1,
			BackgroundSnapshot: "Backend engineer", CreatedAt: createdAt,
		},
		Participants: []persistence.ContextParticipant{
			{
				ID: "participant-facilitator", SessionID: "session-1",
				Role:             "FACILITATOR",
				SubjectRef:       persistence.SubjectRef{Namespace: "speakup.role", SubjectID: "role-1"},
				RoleDefinitionID: "role-1", RoleSnapshot: &definition.Roles[0], Order: 1,
			},
			{
				ID: "participant-learner", SessionID: "session-1", Role: "LEARNER",
				SubjectRef: persistence.SubjectRef{Namespace: "speakup.user", SubjectID: actorUserID},
				Order:      2,
			},
		},
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: 600, MinEffectiveTurns: 1,
			MaxEffectiveTurns: 3, CoverageCheckpointTurn: 1,
			MaxFollowUpsPerQuestion: 1,
			EarlyCompletionRule:     preparation.EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		},
		PracticeObjectives: []preparation.PracticeObjective{{ID: "clarity", Description: "clarity"}},
	}
	return persistence.CreateContextSessionCommand{
		SessionID: "session-1", SnapshotID: "snapshot-1",
		PlanID: "plan-1", PlanRevision: 1, Snapshot: snapshot,
		Intent: persistence.ContextIdempotencyIntent{
			Method: "POST", CanonicalPath: "/v1/practice-plans/plan-1/practice-sessions",
			Key: "session-create-0001", PayloadFingerprint: sha256.Sum256([]byte("payload")),
		},
	}
}
