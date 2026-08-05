package postgres

import (
	"crypto/sha256"
	"testing"
	"time"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestValidCreateSessionCommandUsesCanonicalPlanSnapshot(t *testing.T) {
	t.Parallel()
	actor := practice.Actor{UserID: "user-1", SessionID: "auth-1"}
	command := validSessionCommandFixture(actor.UserID)
	if !validCreateSessionCommand(actor, command) {
		t.Fatal("valid canonical Session command was rejected")
	}
	command.Snapshot.PracticeObjectives = nil
	if validCreateSessionCommand(actor, command) {
		t.Fatal("Session without frozen Practice objectives was accepted")
	}
}

func TestValidContextSnapshotRejectsLegacyParticipantRoles(t *testing.T) {
	t.Parallel()
	actor := practice.Actor{UserID: "user-1", SessionID: "auth-1"}
	for _, role := range []string{"INTERVIEWER", "CANDIDATE"} {
		command := validSessionCommandFixture(actor.UserID)
		command.Snapshot.Participants[0].Role = role
		if validCreateSessionCommand(actor, command) {
			t.Fatalf("legacy participant role %q was accepted", role)
		}
	}
}

func TestRetryExecutionPolicyIgnoresSceneClassificationMetadata(t *testing.T) {
	t.Parallel()
	snapshot := validSessionCommandFixture("user-1").Snapshot
	snapshot.SceneSelection.Scene.PracticeOptions[0].SessionPolicyRef =
		practice.DailyPracticeSessionPolicy
	option, err := snapshot.SceneSelection.PracticeOption()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := practice.ResolveSessionPolicy(
		option.SessionPolicyRef,
		snapshot.SceneSelection.Scene.Prompt,
		option,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SessionPolicy = policy
	snapshot.Experience = practice.PracticeExperienceIELTSSpeaking
	snapshot.Category = practice.SceneCategory("EXAM")
	snapshot.SceneSelection.Scene.Experience = snapshot.Experience
	snapshot.SceneSelection.Scene.Category = snapshot.Category
	if !validRetryExecutionPolicy(snapshot) {
		t.Fatal("retry behavior changed with Scene classification metadata")
	}
}

func validSessionCommandFixture(
	actorUserID string,
) practice.CreateSessionCommand {
	createdAt := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	definition := practice.SceneDefinition{
		ID:         "scene-1",
		Experience: practice.PracticeExperienceInterview,
		Category:   practice.SceneCategory("PROFESSIONAL"),
		Version:    1,
		Status:     practice.SceneStatusActive,
		Prompt: practice.ScenePrompt{
			FocusAreas:     []string{"clarity"},
			TurnBlueprints: []string{"question"},
		},
		Roles: []practice.RoleDefinition{{
			ID: "role-1", SceneID: "scene-1",
			PracticeObjectives: []practice.PracticeObjectiveDefinition{{
				ID: "clarity", Description: "Explain the answer clearly.",
			}},
		}},
		PracticeOptions: []practice.PracticeOption{{
			ID:                       "option-1",
			SceneID:                  "scene-1",
			Mode:                     practice.PracticeModeFullSimulation,
			SuggestedDurationSeconds: 600,
			TurnPolicyRef:            practice.GenericPracticeTurnPolicy,
			SessionPolicyRef:         practice.GenericPracticeSessionPolicy,
			EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
		}},
	}
	snapshot := practice.SessionSnapshot{
		ID:           "snapshot-1",
		SessionID:    "session-1",
		PlanRevision: 1,
		Experience:   definition.Experience,
		Category:     definition.Category,
		PracticeMode: practice.PracticeModeFullSimulation,
		SceneSelection: practice.SceneSelection{
			Scene: definition, SelectedRoleIDs: []string{"role-1"},
			PracticeOptionID: "option-1",
		},
		Preparation: practice.PreparationSnapshot{
			ID: "preparation-1", SourceProfileID: "profile-1", SourceVersion: 1,
			BackgroundSnapshot: "Backend engineer", CreatedAt: createdAt,
		},
		Participants: []practice.Participant{
			{
				ID: "participant-facilitator", SessionID: "session-1",
				Role:             "FACILITATOR",
				SubjectRef:       practice.SubjectRef{Namespace: "speakup.role", SubjectID: "role-1"},
				RoleDefinitionID: "role-1", RoleSnapshot: &definition.Roles[0], Order: 1,
			},
			{
				ID: "participant-learner", SessionID: "session-1", Role: "LEARNER",
				SubjectRef: practice.SubjectRef{Namespace: "speakup.user", SubjectID: actorUserID},
				Order:      2,
			},
		},
		SessionPolicy: practice.SessionPolicy{
			SuggestedDurationSeconds: 600, MinEffectiveTurns: 4,
			MaxEffectiveTurns: 6, CoverageCheckpointTurn: 4,
			MaxFollowUpsPerQuestion: 1,
			EarlyCompletionRule:     practice.EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		},
		PracticeObjectives: []practice.PracticeObjective{{ID: "clarity", Description: "clarity"}},
	}
	return practice.CreateSessionCommand{
		SessionID: "session-1", SnapshotID: "snapshot-1",
		PlanID: "plan-1", PlanRevision: 1, Snapshot: snapshot,
		Intent: practice.IdempotencyIntent{
			Method: "POST", CanonicalPath: "/v1/practice-plans/plan-1/practice-sessions",
			Key: "session-create-0001", PayloadFingerprint: sha256.Sum256([]byte("payload")),
		},
	}
}
