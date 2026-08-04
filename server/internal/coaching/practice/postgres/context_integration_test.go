package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestContextRepositoryRequiresCurrentReadyPlanRevision(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	planRepository := preparation.NewPostgresPlanRepository(pool)
	plan := createContextPlan(t, pool, planRepository, owner, "plan-current")

	revised, _, err := planRepository.RevisePlan(
		context.Background(),
		contextActor(owner.Actor),
		preparation.RevisePlanCommand{
			PlanID:               plan.ID,
			ExpectedPlanRevision: plan.Revision,
			SceneSelection:       plan.SceneSelection,
			SessionPolicy:        plan.SessionPolicy,
			PracticeObjectives:   plan.PracticeObjectives,
			Intent: preparationPlanIntent(
				"PUT",
				"/v1/practice-plans/"+plan.ID,
				"plan-revise-0001",
			),
		},
	)
	if err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}

	stale := contextSessionCommand(owner, plan, "session-stale", "snapshot-stale", "session-stale-key")
	if _, _, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		stale,
	); !errors.Is(err, practice.ErrConflict) {
		t.Fatalf("stale Plan revision error = %v", err)
	}
	current := contextSessionCommand(owner, revised, "session-current", "snapshot-current", "session-current-key")
	created, replayed, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		current,
	)
	if err != nil || replayed || created.Session.PlanRevision != revised.Revision {
		t.Fatalf("CreateSession current = (%#v,%t,%v)", created, replayed, err)
	}
	restarted, err := practicepostgres.New(pool)
	if err != nil {
		t.Fatalf("restart Practice repository: %v", err)
	}
	recovered, err := restarted.ResolveSessionByPlan(
		context.Background(),
		owner.Actor,
		plan.ID,
	)
	if err != nil || recovered.Session.ID != created.Session.ID ||
		recovered.Snapshot.Preparation.ID != plan.PreparationSnapshot.ID {
		t.Fatalf("ResolveSessionByPlan = (%#v,%v)", recovered, err)
	}
}

func TestContextRepositoryRejectsArchivedPlanAndConflictsByPlan(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	planRepository := preparation.NewPostgresPlanRepository(pool)
	firstPlan := createContextPlan(t, pool, planRepository, owner, "plan-active")
	first := contextSessionCommand(owner, firstPlan, "session-first", "snapshot-first", "session-first-key")
	if _, _, err := repository.CreateSession(context.Background(), owner.Actor, first); err != nil {
		t.Fatalf("create first Session: %v", err)
	}
	second := contextSessionCommand(owner, firstPlan, "session-second", "snapshot-second", "session-second-key")
	if _, _, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		second,
	); !errors.Is(err, practice.ErrActiveSessionConflict) {
		t.Fatalf("second effective Session error = %v", err)
	}

	archivedPlan := createContextPlan(t, pool, planRepository, owner, "plan-archived")
	if _, err := pool.Exec(context.Background(), `
		UPDATE preparation_practice_plans
		SET status = 'archived', updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND plan_id = $2
	`, owner.Actor.UserID, archivedPlan.ID); err != nil {
		t.Fatalf("archive Plan: %v", err)
	}
	archived := contextSessionCommand(owner, archivedPlan, "session-archived", "snapshot-archived", "session-archived-key")
	if _, _, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		archived,
	); !errors.Is(err, practice.ErrConflict) {
		t.Fatalf("archived Plan Session error = %v", err)
	}
}

func TestContextRepositoryPersistsCanonicalParticipantRoles(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	plan := createContextPlan(
		t,
		pool,
		preparation.NewPostgresPlanRepository(pool),
		owner,
		"plan-roles",
	)
	command := contextSessionCommand(owner, plan, "session-roles", "snapshot-roles", "session-roles-key")
	command.Snapshot.Participants[0].Role = "INTERVIEWER"
	if _, _, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		command,
	); !errors.Is(err, practice.ErrInvalidArgument) {
		t.Fatalf("legacy participant role error = %v", err)
	}
}

func TestContextRepositoryDeletesSessionAuthorityOnly(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	plan := createContextPlan(
		t,
		pool,
		preparation.NewPostgresPlanRepository(pool),
		owner,
		"plan-deletion",
	)
	command := contextSessionCommand(
		owner,
		plan,
		"session-deletion",
		"snapshot-deletion",
		"session-deletion-key",
	)
	if _, _, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		command,
	); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, owner.Actor.UserID); err != nil {
		t.Fatalf("mark identity deleting: %v", err)
	}

	deletion := practice.DeletionContext{
		UserID:     owner.Actor.UserID,
		Generation: 7,
	}
	if err := repository.DeleteUserData(context.Background(), deletion); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}
	if err := repository.DeleteUserData(context.Background(), deletion); err != nil {
		t.Fatalf("replayed DeleteUserData: %v", err)
	}
	if err := repository.DeleteUserData(
		context.Background(),
		practice.DeletionContext{
			UserID:     owner.Actor.UserID,
			Generation: 6,
		},
	); !errors.Is(err, practice.ErrDeletionGeneration) {
		t.Fatalf("stale deletion error = %v", err)
	}

	for table, want := range map[string]int{
		"practice_sessions":            0,
		"practice_idempotency_records": 0,
		"preparation_practice_plans":   1,
	} {
		var count int
		if err := pool.QueryRow(
			context.Background(),
			"SELECT count(*) FROM "+table+" WHERE owner_user_id = $1",
			owner.Actor.UserID,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != want {
			t.Fatalf("%s count = %d, want %d", table, count, want)
		}
	}
}

type contextOwnerFixture struct {
	Actor         practice.Actor
	ProfileID     string
	PreparationID string
	PreparationAt time.Time
}

func contextOwnerA() contextOwnerFixture {
	return contextOwnerFixture{
		Actor: practice.Actor{
			UserID:    "10000000-0000-4000-8000-000000000112",
			SessionID: "11000000-0000-4000-8000-000000000112",
		},
		ProfileID:     "profile-owner-a",
		PreparationID: "preparation-owner-a",
	}
}

func newContextRepository(
	t *testing.T,
) (*practicepostgres.Repository, *pgxpool.Pool) {
	t.Helper()
	databaseURL, cleanup := isolatedDatabaseURL(t)
	t.Cleanup(cleanup)
	runner, err := migration.Open(databaseURL)
	if err != nil {
		t.Fatalf("open context migration runner: %v", err)
	}
	changed, err := runner.Up()
	if err != nil {
		_ = runner.Close()
		t.Fatalf("apply context migrations: %v", err)
	}
	if !changed {
		_ = runner.Close()
		t.Fatal("empty context database reported no migration")
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("close context migration runner: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open context PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository, err := practicepostgres.New(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create Practice repository: %v", err)
	}
	return repository, pool
}

func seedContextOwner(
	t *testing.T,
	pool *pgxpool.Pool,
	owner *contextOwnerFixture,
) {
	t.Helper()
	ensureIdentityUsers(t, pool, owner.Actor)
	err := pool.QueryRow(context.Background(), `
		INSERT INTO preparation_profiles (
			owner_user_id, profile_id, background_summary
		) VALUES ($1, $2, 'Backend engineer')
		RETURNING updated_at
	`, owner.Actor.UserID, owner.ProfileID).Scan(new(time.Time))
	if err != nil {
		t.Fatalf("insert Preparation Profile: %v", err)
	}
	err = pool.QueryRow(context.Background(), `
		INSERT INTO preparation_snapshots (
			owner_user_id, snapshot_id, source_profile_id,
			source_version, background_snapshot
		) VALUES ($1, $2, $3, 1, 'Backend engineer')
		RETURNING created_at
	`, owner.Actor.UserID, owner.PreparationID, owner.ProfileID).Scan(
		&owner.PreparationAt,
	)
	if err != nil {
		t.Fatalf("insert Preparation Snapshot: %v", err)
	}
}

func createContextPlan(
	t *testing.T,
	pool *pgxpool.Pool,
	repository *preparation.PostgresPlanRepository,
	owner contextOwnerFixture,
	planID string,
) preparation.PracticePlan {
	t.Helper()
	catalog, err := scene.NewPostgresCatalog(pool)
	if err != nil {
		t.Fatalf("NewPostgresCatalog: %v", err)
	}
	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil || len(definitions) == 0 {
		t.Fatalf("ListActive Scenes = (%d,%v)", len(definitions), err)
	}
	definition := definitions[0]
	if len(definition.Roles) == 0 || len(definition.PracticeOptions) == 0 {
		t.Fatal("seeded Scene lacks selectable roles/options")
	}
	roleIDs := []string{definition.Roles[0].ID}
	optionID := ""
	for _, option := range definition.PracticeOptions {
		if option.Type == scene.PracticeOptionFullSimulation {
			roleIDs = make([]string, len(definition.Roles))
			for index, role := range definition.Roles {
				roleIDs[index] = role.ID
			}
			optionID = option.ID
			break
		}
		if option.Type == scene.PracticeOptionFocus &&
			option.RoleDefinitionID == roleIDs[0] {
			optionID = option.ID
		}
	}
	if optionID == "" {
		t.Fatal("seeded Scene lacks compatible Practice option")
	}
	selection, err := catalog.ResolveSelection(
		context.Background(),
		definition.ID,
		definition.Version,
		roleIDs,
		optionID,
	)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	snapshot := preparation.Snapshot{
		ID:                 owner.PreparationID,
		SourceProfileID:    owner.ProfileID,
		SourceVersion:      1,
		BackgroundSnapshot: "Backend engineer",
		CreatedAt:          owner.PreparationAt.UTC(),
	}
	policy := preparation.SessionPolicy{
		SuggestedDurationSeconds: 600,
		MinEffectiveTurns:        1,
		MaxEffectiveTurns:        3,
		CoverageCheckpointTurn:   1,
		MaxFollowUpsPerQuestion:  1,
		EarlyCompletionRule: preparation.
			EarlyCompletionCoverageSatisfiedAfterCheckpoint,
	}
	objectives := []preparation.PracticeObjective{{
		ID: "clarity", Description: "clarity",
	}}
	plan, replayed, err := repository.CreatePlan(
		context.Background(),
		contextActor(owner.Actor),
		preparation.CreatePlanCommand{
			PlanID:              planID,
			PreparationSnapshot: snapshot,
			SceneSelection:      selection,
			SessionPolicy:       policy,
			PracticeObjectives:  objectives,
			Intent: preparationPlanIntent(
				"POST",
				"/v1/practice-plans",
				planID+"-create-key",
			),
		},
	)
	if err != nil || replayed {
		t.Fatalf("CreatePlan = (%#v,%t,%v)", plan, replayed, err)
	}
	return plan
}

func contextSessionCommand(
	owner contextOwnerFixture,
	plan preparation.PracticePlan,
	sessionID string,
	snapshotID string,
	key string,
) practice.CreateSessionCommand {
	roles, _ := plan.SceneSelection.SelectedRoles()
	participants := make([]practice.Participant, 0, len(roles)+1)
	for _, role := range roles {
		roleCopy := role
		participants = append(participants, practice.Participant{
			ID: "participant-" + role.ID, SessionID: sessionID,
			Role:             "FACILITATOR",
			SubjectRef:       practice.SubjectRef{Namespace: "speakup.role", SubjectID: role.ID},
			RoleDefinitionID: role.ID, RoleSnapshot: &roleCopy,
			Order: len(participants) + 1,
		})
	}
	participants = append(participants, practice.Participant{
		ID: "participant-learner", SessionID: sessionID, Role: "LEARNER",
		SubjectRef: practice.SubjectRef{Namespace: "speakup.user", SubjectID: owner.Actor.UserID},
		Order:      len(participants) + 1,
	})
	snapshot := practice.SessionSnapshot{
		ID: snapshotID, SessionID: sessionID, PlanRevision: plan.Revision,
		SceneFamily:        plan.SceneSelection.Scene.Family,
		SceneModel:         plan.SceneSelection.Scene.Model,
		SceneSelection:     plan.SceneSelection,
		Preparation:        plan.PreparationSnapshot,
		Participants:       participants,
		SessionPolicy:      plan.SessionPolicy,
		PracticeObjectives: plan.PracticeObjectives,
	}
	return practice.CreateSessionCommand{
		SessionID: sessionID, SnapshotID: snapshotID,
		PlanID: plan.ID, PlanRevision: plan.Revision, Snapshot: snapshot,
		Intent: practice.IdempotencyIntent{
			Method:        "POST",
			CanonicalPath: "/v1/practice-plans/" + plan.ID + "/practice-sessions",
			Key:           key, PayloadFingerprint: sha256.Sum256([]byte(key)),
		},
	}
}

func preparationPlanIntent(
	method string,
	path string,
	key string,
) preparation.IdempotencyIntent {
	payload, _ := json.Marshal(map[string]string{"key": key})
	return preparation.IdempotencyIntent{
		Method: method, CanonicalPath: path, Key: key,
		PayloadFingerprint: sha256.Sum256(payload),
	}
}

func contextActor(actor practice.Actor) requestcontext.Actor {
	return requestcontext.Actor{UserID: actor.UserID, SessionID: actor.SessionID}
}
