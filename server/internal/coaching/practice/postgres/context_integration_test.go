package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/planpolicy"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	preparationsource "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/preparationsource"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/repository/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestContextRepositoryRequiresCurrentReadyPlanRevision(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	planRepository := preparationpostgres.NewPostgresPlanRepository(pool)
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

func TestContextRepositoryFreezesTurnPolicyReferenceAcrossRestart(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	plan := createContextPlan(
		t,
		pool,
		preparationpostgres.NewPostgresPlanRepository(pool),
		owner,
		"plan-turn-policy",
	)
	originalOption, err := plan.SceneSelection.PracticeOption()
	if err != nil {
		t.Fatalf("PracticeOption: %v", err)
	}
	originalReference := originalOption.TurnPolicyRef
	command := contextSessionCommand(
		owner,
		plan,
		"session-turn-policy",
		"snapshot-turn-policy",
		"session-turn-policy-key",
	)
	created, replayed, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		command,
	)
	if err != nil || replayed {
		t.Fatalf("CreateSession = (%#v,%t,%v)", created, replayed, err)
	}
	createdOption, err := created.Snapshot.SceneSelection.PracticeOption()
	if err != nil || createdOption.TurnPolicyRef != originalReference {
		t.Fatalf(
			"created Practice option = (%#v,%v), want TurnPolicyRef %q",
			createdOption,
			err,
			originalReference,
		)
	}

	replacementReference := "generic.practice.turn.v1"
	if originalReference == replacementReference {
		replacementReference = "interview.project_deep_dive.turn.v1"
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO coaching_scene_versions (
			scene_id, scene_version, practice_experience, scene_category,
			name, status, prompt, roles, practice_options, display_order
		)
		SELECT
			scene_id, scene_version + 1, practice_experience, scene_category,
			name, status, prompt, roles,
			(
				SELECT jsonb_agg(
					CASE
						WHEN option_payload ->> 'practice_option_id' = $4
						THEN jsonb_set(
							option_payload,
							'{turn_policy_ref}',
							to_jsonb($3::text)
						)
						ELSE option_payload
					END
					ORDER BY option_order
				)
				FROM jsonb_array_elements(practice_options)
					WITH ORDINALITY AS option(option_payload, option_order)
			),
			display_order
		FROM coaching_scene_versions
		WHERE scene_id = $1 AND scene_version = $2
	`,
		plan.SceneSelection.Scene.ID,
		plan.SceneSelection.Scene.Version,
		replacementReference,
		plan.SceneSelection.PracticeOptionID,
	); err != nil {
		t.Fatalf("publish later Scene version: %v", err)
	}
	catalog, err := scene.NewPostgresCatalog(
		pool,
		scoring.NewEvaluationPolicyRegistry(),
	)
	if err != nil {
		t.Fatalf("NewPostgresCatalog: %v", err)
	}
	latest, err := catalog.GetScene(
		context.Background(),
		plan.SceneSelection.Scene.ID,
	)
	latestOption, optionErr := (scene.SelectionSnapshot{
		Scene:            latest,
		PracticeOptionID: plan.SceneSelection.PracticeOptionID,
	}).PracticeOption()
	if err != nil || optionErr != nil || latestOption.TurnPolicyRef != replacementReference {
		t.Fatalf("latest Scene = (%#v,%v)", latest, err)
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
	if err != nil {
		t.Fatalf("ResolveSessionByPlan: %v", err)
	}
	recoveredOption, err := recovered.Snapshot.SceneSelection.PracticeOption()
	if err != nil || recoveredOption.TurnPolicyRef != originalReference {
		t.Fatalf(
			"recovered Practice option = (%#v,%v), want frozen TurnPolicyRef %q",
			recoveredOption,
			err,
			originalReference,
		)
	}
}

func TestContextRepositoryRejectsArchivedPlanAndConflictsByPlan(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	planRepository := preparationpostgres.NewPostgresPlanRepository(pool)
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

func TestContextRepositoryCompletesUserControlledSessionIdempotently(
	t *testing.T,
) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	plan := createContextPlanForExperience(
		t,
		pool,
		preparationpostgres.NewPostgresPlanRepository(pool),
		owner,
		"plan-user-complete",
		scene.PracticeExperienceLifeAndTravel,
	)
	command := contextSessionCommand(
		owner,
		plan,
		"session-user-complete",
		"snapshot-user-complete",
		"session-user-complete-key",
	)
	created, replayed, err := repository.CreateSession(
		context.Background(),
		owner.Actor,
		command,
	)
	if err != nil || replayed {
		t.Fatalf("CreateSession = (%#v,%t,%v)", created, replayed, err)
	}
	if created.Snapshot.SessionPolicy.CompletionMode !=
		practice.CompletionModeUserControlled {
		t.Fatalf(
			"completion mode = %q, want %q",
			created.Snapshot.SessionPolicy.CompletionMode,
			practice.CompletionModeUserControlled,
		)
	}

	startedVersion := created.Session.Version + 1
	if _, err := pool.Exec(context.Background(), `
		UPDATE practice_sessions
		SET status = 'in_progress',
		    version = $3,
		    effective_turns = 1,
		    started_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND session_id = $2
	`, owner.Actor.UserID, created.Session.ID, startedVersion); err != nil {
		t.Fatalf("start user-controlled Session fixture: %v", err)
	}
	turnFingerprint := sha256.Sum256([]byte("turn-user-complete-payload"))
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO practice_turn_results (
			owner_user_id, session_id, turn_id, payload_fingerprint,
			round_number, effective_turns, session_version,
			completed, completion_token
		)
		VALUES ($1, $2, 'turn-user-complete', $3, 1, 1, $4, false, '')
	`, owner.Actor.UserID, created.Session.ID,
		turnFingerprint[:],
		startedVersion); err != nil {
		t.Fatalf("insert user-controlled final Turn fixture: %v", err)
	}
	completeIntent := practice.IdempotencyIntent{
		Method:        "POST",
		CanonicalPath: "/v1/practice-sessions/" + created.Session.ID + "/complete",
		Key:           "session-complete-key",
		PayloadFingerprint: sha256.Sum256(
			[]byte("session-complete-payload"),
		),
	}
	completeCommand := practice.TransitionSessionCommand{
		SessionID:              created.Session.ID,
		ExpectedSessionVersion: startedVersion,
		Transition:             practice.SessionComplete,
		Intent:                 completeIntent,
	}
	completed, replayed, err := repository.TransitionSession(
		context.Background(),
		owner.Actor,
		completeCommand,
	)
	if err != nil || replayed || completed.Status != practice.SessionCompleted ||
		completed.Version != startedVersion+1 ||
		completed.EndReason != "USER_COMPLETED" {
		t.Fatalf("complete Session = (%#v,%t,%v)", completed, replayed, err)
	}

	replayedSession, replayed, err := repository.TransitionSession(
		context.Background(),
		owner.Actor,
		completeCommand,
	)
	if err != nil || !replayed || replayedSession.ID != completed.ID ||
		replayedSession.Status != completed.Status ||
		replayedSession.Version != completed.Version ||
		replayedSession.EndReason != completed.EndReason {
		t.Fatalf(
			"replay complete Session = (%#v,%t,%v), want %#v",
			replayedSession,
			replayed,
			err,
			completed,
		)
	}
	var resourceKind string
	if err := pool.QueryRow(context.Background(), `
		SELECT resource_kind
		FROM practice_idempotency_records
		WHERE owner_user_id = $1
		  AND canonical_path = $2
		  AND idempotency_key = $3
	`, owner.Actor.UserID, completeIntent.CanonicalPath, completeIntent.Key).Scan(
		&resourceKind,
	); err != nil {
		t.Fatalf("read completion idempotency record: %v", err)
	}
	if resourceKind != string(practice.SessionComplete) {
		t.Fatalf("completion resource kind = %q", resourceKind)
	}
	var finalTurnID, completionToken, deliveryStatus string
	var completionVersion int
	if err := pool.QueryRow(context.Background(), `
		SELECT final_turn_id, session_version, completion_token, delivery_status
		FROM practice_completed
		WHERE owner_user_id = $1 AND session_id = $2
	`, owner.Actor.UserID, completed.ID).Scan(
		&finalTurnID,
		&completionVersion,
		&completionToken,
		&deliveryStatus,
	); err != nil {
		t.Fatalf("read completion handoff: %v", err)
	}
	if finalTurnID != "turn-user-complete" ||
		completionVersion != completed.Version || completionToken == "" ||
		deliveryStatus != "PENDING" {
		t.Fatalf(
			"completion handoff = (%q,%d,%q,%q)",
			finalTurnID,
			completionVersion,
			completionToken,
			deliveryStatus,
		)
	}
	var turnCompleted bool
	var turnCompletionToken string
	if err := pool.QueryRow(context.Background(), `
		SELECT completed, completion_token
		FROM practice_turn_results
		WHERE owner_user_id = $1 AND session_id = $2 AND turn_id = $3
	`, owner.Actor.UserID, completed.ID, finalTurnID).Scan(
		&turnCompleted,
		&turnCompletionToken,
	); err != nil {
		t.Fatalf("read completed final Turn result: %v", err)
	}
	if !turnCompleted || turnCompletionToken != completionToken {
		t.Fatalf(
			"final Turn completion = (%t,%q), want token %q",
			turnCompleted,
			turnCompletionToken,
			completionToken,
		)
	}
}

func TestContextRepositoryPersistsCanonicalParticipantRoles(t *testing.T) {
	repository, pool := newContextRepository(t)
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	plan := createContextPlan(
		t,
		pool,
		preparationpostgres.NewPostgresPlanRepository(pool),
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
		preparationpostgres.NewPostgresPlanRepository(pool),
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
	repository *preparationpostgres.PostgresPlanRepository,
	owner contextOwnerFixture,
	planID string,
) preparation.PracticePlan {
	t.Helper()
	return createContextPlanForExperience(
		t,
		pool,
		repository,
		owner,
		planID,
		"",
	)
}

func createContextPlanForExperience(
	t *testing.T,
	pool *pgxpool.Pool,
	repository *preparationpostgres.PostgresPlanRepository,
	owner contextOwnerFixture,
	planID string,
	experience scene.PracticeExperience,
) preparation.PracticePlan {
	t.Helper()
	catalog, err := scene.NewPostgresCatalog(
		pool,
		scoring.NewEvaluationPolicyRegistry(),
	)
	if err != nil {
		t.Fatalf("NewPostgresCatalog: %v", err)
	}
	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil || len(definitions) == 0 {
		t.Fatalf("ListActive Scenes = (%d,%v)", len(definitions), err)
	}
	definition := definitions[0]
	if experience != "" {
		found := false
		for _, candidate := range definitions {
			if candidate.Experience == experience {
				definition = candidate
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("seeded Scenes lack experience %q", experience)
		}
	}
	if len(definition.Roles) == 0 || len(definition.PracticeOptions) == 0 {
		t.Fatal("seeded Scene lacks selectable roles/options")
	}
	roleIDs := []string{definition.Roles[0].ID}
	optionID := ""
	for _, option := range definition.PracticeOptions {
		if option.Mode == scene.PracticeModeFullSimulation {
			roleIDs = make([]string, len(definition.Roles))
			for index, role := range definition.Roles {
				roleIDs[index] = role.ID
			}
			optionID = option.ID
			break
		}
		if option.Mode == scene.PracticeModeFocus &&
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
	option, err := selection.PracticeOption()
	if err != nil {
		t.Fatalf("PracticeOption: %v", err)
	}
	policy, err := planpolicy.NewResolver().ResolveSessionPolicy(
		selection.Scene,
		option,
		0,
	)
	if err != nil {
		t.Fatalf("ResolveSessionPolicy: %v", err)
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
	projection := mustProjectContextPlan(plan)
	roles, _ := projection.SceneSelection.SelectedRoles()
	option, _ := projection.SceneSelection.PracticeOption()
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
		Experience:         projection.SceneSelection.Scene.Experience,
		Category:           projection.SceneSelection.Scene.Category,
		PracticeMode:       option.Mode,
		SceneSelection:     projection.SceneSelection,
		Preparation:        projection.Preparation,
		Participants:       participants,
		SessionPolicy:      projection.SessionPolicy,
		PracticeObjectives: projection.PracticeObjectives,
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

func mustProjectContextPlan(
	plan preparation.PracticePlan,
) practice.PlanProjection {
	reader, err := preparationsource.New(contextPlanReader{plan: plan})
	if err != nil {
		panic(err)
	}
	projection, err := reader.ReadExecutablePlan(
		context.Background(),
		requestcontext.Actor{UserID: plan.UserID, SessionID: "fixture"},
		plan.ID,
		plan.Revision,
	)
	if err != nil {
		panic(err)
	}
	return projection
}

type contextPlanReader struct {
	plan preparation.PracticePlan
}

func (reader contextPlanReader) ReadExecutablePlan(
	context.Context,
	requestcontext.Actor,
	string,
	int,
) (preparation.PracticePlan, error) {
	return reader.plan, nil
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
