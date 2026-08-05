package preparation_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/planpolicy"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

const (
	planThreadA = "30000000-0000-4000-8000-000000000111"
	planGoalA   = "40000000-0000-4000-8000-000000000111"
)

func TestPostgresPlanRepositoryPersistsImmutableRevisionsAndExactReplays(
	t *testing.T,
) {
	profileRepository, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA, preparationUserB)
	planRepository := preparation.NewPostgresPlanRepository(pool)
	actorA := preparationActor(preparationUserA, preparationSessionA)
	actorB := preparationActor(preparationUserB, preparationSessionB)
	createCommand := seedPlanCommand(
		t,
		pool,
		profileRepository,
		actorA.UserID,
		actorA.SessionID,
		"authority",
	)

	created, replayed, err := planRepository.CreatePlan(
		context.Background(),
		actorA,
		createCommand,
	)
	if err != nil || replayed {
		t.Fatalf("CreatePlan = (%#v, %t, %v)", created, replayed, err)
	}
	if created.Revision != 1 || created.Status != preparation.PlanStatusReady ||
		created.SourceThreadID != planThreadA || created.GoalSnapshot == nil ||
		created.GoalSnapshot.ID != planGoalA ||
		created.PreparationSnapshot.ID !=
			createCommand.PreparationSnapshot.ID {
		t.Fatalf("created Plan = %#v", created)
	}

	replayCommand := createCommand
	replayCommand.PlanID = "plan-create-replay-ignored"
	replayedCreate, replayed, err := planRepository.CreatePlan(
		context.Background(),
		actorA,
		replayCommand,
	)
	if err != nil || !replayed || !reflect.DeepEqual(replayedCreate, created) {
		t.Fatalf(
			"CreatePlan replay = (%#v, %t, %v), want %#v",
			replayedCreate,
			replayed,
			err,
			created,
		)
	}

	conflictingIntent := createCommand.Intent
	conflictingIntent.PayloadFingerprint = sha256.Sum256([]byte("changed"))
	if _, _, err := planRepository.ReplayPlan(
		context.Background(),
		actorA,
		conflictingIntent,
	); !errors.Is(err, preparation.ErrPlanIdempotencyConflict) {
		t.Fatalf("ReplayPlan conflicting payload error = %v", err)
	}

	current, err := planRepository.ReadCurrentPlan(
		context.Background(),
		actorA,
		created.ID,
	)
	if err != nil || !reflect.DeepEqual(current, created) {
		t.Fatalf("ReadCurrentPlan = (%#v, %v)", current, err)
	}
	if _, err := planRepository.ReadCurrentPlan(
		context.Background(),
		actorB,
		created.ID,
	); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("cross-actor ReadCurrentPlan error = %v", err)
	}
	if otherReplay, found, err := planRepository.ReplayPlan(
		context.Background(),
		actorB,
		createCommand.Intent,
	); err != nil || found || otherReplay.ID != "" {
		t.Fatalf("cross-actor ReplayPlan = (%#v, %t, %v)", otherReplay, found, err)
	}

	reviseCommand := revisePlanCommand(t, created)
	revised, replayed, err := planRepository.RevisePlan(
		context.Background(),
		actorA,
		reviseCommand,
	)
	if err != nil || replayed {
		t.Fatalf("RevisePlan = (%#v, %t, %v)", revised, replayed, err)
	}
	if revised.Revision != 2 ||
		revised.SceneSelection.PracticeOptionID != "option_full_simulation" ||
		revised.SourceThreadID != created.SourceThreadID ||
		!reflect.DeepEqual(revised.GoalSnapshot, created.GoalSnapshot) ||
		!reflect.DeepEqual(
			revised.PreparationSnapshot,
			created.PreparationSnapshot,
		) {
		t.Fatalf("revised Plan = %#v", revised)
	}

	replayedRevision, replayed, err := planRepository.RevisePlan(
		context.Background(),
		actorA,
		reviseCommand,
	)
	if err != nil || !replayed ||
		!reflect.DeepEqual(replayedRevision, revised) {
		t.Fatalf(
			"RevisePlan replay = (%#v, %t, %v)",
			replayedRevision,
			replayed,
			err,
		)
	}

	originalReplay, found, err := planRepository.ReplayPlan(
		context.Background(),
		actorA,
		createCommand.Intent,
	)
	if err != nil || !found || originalReplay.Revision != 1 ||
		originalReplay.SceneSelection.PracticeOptionID !=
			"option_technical_focus" {
		t.Fatalf("exact revision-1 replay = (%#v, %t, %v)", originalReplay, found, err)
	}

	staleCommand := reviseCommand
	staleCommand.Intent = planTestIntent(
		"PUT",
		"/v1/practice-plans/"+created.ID,
		"plan-stale-revision-key",
		map[string]any{"expected_plan_revision": 1, "attempt": "stale"},
	)
	if _, _, err := planRepository.RevisePlan(
		context.Background(),
		actorA,
		staleCommand,
	); !errors.Is(err, preparation.ErrPlanConflict) {
		t.Fatalf("stale RevisePlan error = %v", err)
	}

	assertStoredPlanRevisions(t, pool, actorA.UserID, created.ID)
	if _, err := planRepository.ReadExecutablePlan(
		context.Background(),
		actorA,
		created.ID,
		1,
	); !errors.Is(err, preparation.ErrPlanConflict) {
		t.Fatalf("old executable revision error = %v", err)
	}
	executable, err := planRepository.ReadExecutablePlan(
		context.Background(),
		actorA,
		created.ID,
		2,
	)
	if err != nil || executable.Revision != 2 {
		t.Fatalf("ReadExecutablePlan current = (%#v, %v)", executable, err)
	}

	if _, err := pool.Exec(context.Background(), `
		UPDATE preparation_practice_plans
		SET status = 'archived', updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND plan_id = $2
	`, actorA.UserID, created.ID); err != nil {
		t.Fatalf("archive Plan: %v", err)
	}
	if _, err := planRepository.ReadExecutablePlan(
		context.Background(),
		actorA,
		created.ID,
		2,
	); !errors.Is(err, preparation.ErrPlanConflict) {
		t.Fatalf("archived executable Plan error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE preparation_idempotency_records
		SET response_body = response_body || '{"unexpected": true}'::jsonb
		WHERE owner_user_id = $1
		  AND method = 'POST'
		  AND canonical_path = '/v1/practice-plans'
		  AND idempotency_key = $2
	`, actorA.UserID, createCommand.Intent.Key); err != nil {
		t.Fatalf("corrupt Plan replay fixture: %v", err)
	}
	if _, _, err := planRepository.ReplayPlan(
		context.Background(),
		actorA,
		createCommand.Intent,
	); !errors.Is(err, preparation.ErrPlanRepository) {
		t.Fatalf("strict Plan replay decode error = %v", err)
	}

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO preparation_deletion_fences (
			owner_user_id, deletion_generation
		) VALUES ($1, 1)
	`, actorA.UserID); err != nil {
		t.Fatalf("insert Preparation fence: %v", err)
	}
	if _, err := planRepository.ReadCurrentPlan(
		context.Background(),
		actorA,
		created.ID,
	); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("fenced ReadCurrentPlan error = %v", err)
	}
	if _, _, err := planRepository.ReplayPlan(
		context.Background(),
		actorA,
		createCommand.Intent,
	); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("fenced ReplayPlan error = %v", err)
	}
}

func TestPostgresPlanRepositoryPersistsFrozenIELTSAssignmentAcrossRevisions(
	t *testing.T,
) {
	profileRepository, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA)
	actor := preparationActor(preparationUserA, preparationSessionA)
	repository := preparation.NewPostgresPlanRepository(pool)
	command := seedPlanCommand(
		t,
		pool,
		profileRepository,
		actor.UserID,
		actor.SessionID,
		"ielts",
	)
	catalog, err := scene.NewPostgresCatalog(
		pool,
		scoring.NewEvaluationPolicyRegistry(),
	)
	if err != nil {
		t.Fatalf("NewPostgresCatalog: %v", err)
	}
	selection, err := catalog.ResolveSelection(
		context.Background(),
		"scn_ielts_speaking_part_2",
		1,
		[]string{"role_ielts_examiner"},
		"option_ielts_full_simulation",
	)
	if err != nil {
		t.Fatalf("ResolveSelection IELTS: %v", err)
	}
	bank, err := catalog.IELTSQuestionBank()
	if err != nil || len(bank.TopicGroups) == 0 {
		t.Fatalf("IELTSQuestionBank = (%d, %v)", len(bank.TopicGroups), err)
	}
	questionSelection := scene.IELTSQuestionSetSelection{
		Mode:         scene.IELTSPracticeModePart2,
		TopicGroupID: bank.TopicGroups[0].ID,
	}
	resolved, err := catalog.ResolveIELTSQuestionSet(questionSelection)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet: %v", err)
	}
	assignment := &preparation.IELTSAssignmentSnapshot{
		BankID:         resolved.BankID,
		Season:         resolved.Season,
		Mode:           resolved.Mode,
		TopicGroupID:   resolved.TopicGroupID,
		TopicTitle:     resolved.TopicTitle,
		Part2CueCard:   resolved.Part2CueCard,
		Part1Questions: resolved.Part1Questions,
		Part2Questions: resolved.Part2Questions,
		Part3Questions: resolved.Part3Questions,
		TurnBlueprints: append([]string(nil), resolved.TurnBlueprints...),
	}
	selection.Scene.Prompt.TurnBlueprints = append(
		[]string(nil),
		resolved.TurnBlueprints...,
	)
	selection.Scene.Prompt.PublicSceneBrief =
		"This IELTS Part 2 brief is frozen with the Plan revision."
	option, err := selection.PracticeOption()
	if err != nil {
		t.Fatalf("PracticeOption IELTS: %v", err)
	}
	policy, err := planpolicy.NewResolver().ResolveSessionPolicy(
		selection.Scene,
		option,
		0,
	)
	if err != nil {
		t.Fatalf("ResolveSessionPolicy IELTS: %v", err)
	}
	command.SceneSelection = selection
	command.SessionPolicy = policy
	command.PracticeObjectives = planObjectives(t, selection)
	command.IELTSAssignment = assignment
	command.Intent = planTestIntent(
		"POST",
		"/v1/practice-plans",
		"plan-create-key-ielts",
		preparation.CreatePlanRequest{
			SourceThreadID:        command.SourceThreadID,
			GoalID:                command.GoalSnapshot.ID,
			PreparationSnapshotID: command.PreparationSnapshot.ID,
			SceneID:               selection.Scene.ID,
			SceneVersion:          selection.Scene.Version,
			SelectedRoleIDs:       selection.SelectedRoleIDs,
			PracticeOptionID:      selection.PracticeOptionID,
			MaxEffectiveTurns:     policy.MaxEffectiveTurns,
			IELTSSelection: &preparation.IELTSQuestionSelection{
				Mode:         questionSelection.Mode,
				TopicGroupID: questionSelection.TopicGroupID,
			},
		},
	)

	created, replayed, err := repository.CreatePlan(
		context.Background(),
		actor,
		command,
	)
	if err != nil || replayed || created.IELTSAssignment == nil ||
		!reflect.DeepEqual(created.IELTSAssignment, assignment) {
		t.Fatalf("CreatePlan IELTS = (%#v, %t, %v)", created, replayed, err)
	}
	read, err := repository.ReadCurrentPlan(
		context.Background(),
		actor,
		created.ID,
	)
	if err != nil || !reflect.DeepEqual(read.IELTSAssignment, assignment) {
		t.Fatalf("ReadCurrentPlan IELTS = (%#v, %v)", read, err)
	}
	revise := preparation.RevisePlanCommand{
		PlanID:               created.ID,
		ExpectedPlanRevision: created.Revision,
		SceneSelection:       created.SceneSelection,
		SessionPolicy:        created.SessionPolicy,
		PracticeObjectives:   created.PracticeObjectives,
		IELTSAssignment:      created.IELTSAssignment,
		Intent: planTestIntent(
			"PUT",
			"/v1/practice-plans/"+created.ID,
			"plan-revise-key-ielts",
			preparation.RevisePlanRequest{
				ExpectedPlanRevision: created.Revision,
				SelectedRoleIDs:      created.SceneSelection.SelectedRoleIDs,
				PracticeOptionID:     created.SceneSelection.PracticeOptionID,
				MaxEffectiveTurns:    created.SessionPolicy.MaxEffectiveTurns,
			},
		),
	}
	revised, replayed, err := repository.RevisePlan(
		context.Background(),
		actor,
		revise,
	)
	if err != nil || replayed || revised.Revision != 2 ||
		!reflect.DeepEqual(revised.IELTSAssignment, assignment) {
		t.Fatalf("RevisePlan IELTS = (%#v, %t, %v)", revised, replayed, err)
	}

	changed := revise
	changed.ExpectedPlanRevision = revised.Revision
	changedAssignment := *assignment
	changedAssignment.TopicGroupID = "different-topic"
	changed.IELTSAssignment = &changedAssignment
	changed.Intent = planTestIntent(
		"PUT",
		"/v1/practice-plans/"+created.ID,
		"plan-revise-key-ielts-changed",
		map[string]any{"expected_plan_revision": revised.Revision},
	)
	if _, _, err := repository.RevisePlan(
		context.Background(),
		actor,
		changed,
	); !errors.Is(err, preparation.ErrPlanConflict) {
		t.Fatalf("changed frozen IELTS assignment error = %v", err)
	}
}

func TestDeleteProfileDataStopsAtPracticeSessionPlanReference(t *testing.T) {
	profileRepository, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA)
	actor := preparationActor(preparationUserA, preparationSessionA)
	planRepository := preparation.NewPostgresPlanRepository(pool)
	command := seedPlanCommand(
		t,
		pool,
		profileRepository,
		actor.UserID,
		actor.SessionID,
		"delete",
	)
	plan, _, err := planRepository.CreatePlan(
		context.Background(),
		actor,
		command,
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	seedBlockingPracticeSession(t, pool, plan)
	if _, err := pool.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = 'deleting', updated_at = transaction_timestamp()
		WHERE id = $1
	`, actor.UserID); err != nil {
		t.Fatalf("mark identity deleting: %v", err)
	}
	if _, err := planRepository.ReadCurrentPlan(
		context.Background(),
		actor,
		plan.ID,
	); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("deleting-account ReadCurrentPlan error = %v", err)
	}

	deletion := preparation.DeleteProfileDataCommand{
		UserID: actor.UserID, Generation: 1,
	}
	if err := profileRepository.DeleteProfileData(
		context.Background(),
		deletion,
	); !errors.Is(err, preparation.ErrProfileConflict) {
		t.Fatalf("blocked DeleteProfileData error = %v", err)
	}
	for _, table := range []string{
		"preparation_idempotency_records",
		"preparation_practice_plans",
		"preparation_practice_plan_revisions",
		"preparation_snapshots",
		"preparation_profiles",
	} {
		assertPlanOwnedRows(t, pool, table, actor.UserID, true)
	}
	var sessionExists bool
	if err := pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM practice_sessions
			WHERE owner_user_id = $1
			  AND session_id = 'session-blocking-delete'
		)
	`, actor.UserID).Scan(&sessionExists); err != nil || !sessionExists {
		t.Fatalf(
			"Practice Session after blocked deletion = (%t, %v)",
			sessionExists,
			err,
		)
	}
	if _, err := pool.Exec(context.Background(), `
		DELETE FROM practice_sessions
		WHERE owner_user_id = $1 AND session_id = 'session-blocking-delete'
	`, actor.UserID); err != nil {
		t.Fatalf("delete blocking Practice Session: %v", err)
	}
	if err := profileRepository.DeleteProfileData(
		context.Background(),
		deletion,
	); err != nil {
		t.Fatalf("DeleteProfileData after Session removal: %v", err)
	}
	for _, table := range []string{
		"preparation_idempotency_records",
		"preparation_practice_plans",
		"preparation_practice_plan_revisions",
		"preparation_snapshots",
		"preparation_profiles",
	} {
		assertPlanOwnedRows(t, pool, table, actor.UserID, false)
	}
}

func seedPlanCommand(
	t *testing.T,
	pool *pgxpool.Pool,
	profiles *preparation.PostgresProfileRepository,
	userID string,
	sessionID string,
	suffix string,
) preparation.CreatePlanCommand {
	t.Helper()
	ctx := context.Background()
	actor := preparationActor(userID, sessionID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_threads (id, owner_user_id)
		VALUES ($1, $2)
	`, planThreadA, userID); err != nil {
		t.Fatalf("seed Agent Thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO coaching_goals (
			goal_id, owner_user_id, title, status, version
		) VALUES ($1, $2, 'Prepare for platform interview', 'active', 3)
	`, planGoalA, userID); err != nil {
		t.Fatalf("seed Goal: %v", err)
	}

	profileRequest := preparation.CreateProfileRequest{
		BackgroundSummary: "Built reliable Go services.",
	}
	profile, replayed, err := profiles.CreateProfile(
		ctx,
		actor,
		preparation.CreateProfileCommand{
			ProfileID: "profile-plan-" + suffix,
			Request:   profileRequest,
			Intent: profileIntent(
				"profile-plan-key-"+suffix,
				profileRequest,
			),
		},
	)
	if err != nil || replayed {
		t.Fatalf("seed Plan Profile = (%#v, %t, %v)", profile, replayed, err)
	}
	snapshotRequest := preparation.CreateSnapshotRequest{SourceVersion: 1}
	snapshot, replayed, err := profiles.CreateSnapshot(
		ctx,
		actor,
		preparation.CreateSnapshotCommand{
			SnapshotID: "snapshot-plan-" + suffix,
			ProfileID:  profile.ID,
			Request:    snapshotRequest,
			Intent: snapshotIntent(
				profile.ID,
				"snapshot-plan-key-"+suffix,
				snapshotRequest,
			),
		},
	)
	if err != nil || replayed {
		t.Fatalf("seed Plan Snapshot = (%#v, %t, %v)", snapshot, replayed, err)
	}

	catalog, err := scene.NewPostgresCatalog(
		pool,
		scoring.NewEvaluationPolicyRegistry(),
	)
	if err != nil {
		t.Fatalf("NewPostgresCatalog: %v", err)
	}
	selection, err := catalog.ResolveSelection(
		ctx,
		"scn_programmer_interview",
		1,
		[]string{"role_technical_interviewer"},
		"option_technical_focus",
	)
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
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
	objectives := planObjectives(t, selection)
	request := preparation.CreatePlanRequest{
		SourceThreadID:        planThreadA,
		GoalID:                planGoalA,
		PreparationSnapshotID: snapshot.ID,
		SceneID:               selection.Scene.ID,
		SceneVersion:          selection.Scene.Version,
		SelectedRoleIDs:       append([]string(nil), selection.SelectedRoleIDs...),
		PracticeOptionID:      selection.PracticeOptionID,
		MaxEffectiveTurns:     policy.MaxEffectiveTurns,
	}
	return preparation.CreatePlanCommand{
		PlanID:         "plan-pg-" + suffix,
		SourceThreadID: planThreadA,
		GoalSnapshot: &preparation.GoalSnapshot{
			ID: planGoalA, Title: "Prepare for platform interview", Version: 3,
		},
		PreparationSnapshot: snapshot,
		SceneSelection:      selection,
		SessionPolicy:       policy,
		PracticeObjectives:  objectives,
		Intent: planTestIntent(
			"POST",
			"/v1/practice-plans",
			"plan-create-key-"+suffix,
			request,
		),
	}
}

func revisePlanCommand(
	t *testing.T,
	current preparation.PracticePlan,
) preparation.RevisePlanCommand {
	t.Helper()
	selection := current.SceneSelection
	selection.PracticeOptionID = "option_full_simulation"
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
	request := preparation.RevisePlanRequest{
		ExpectedPlanRevision: current.Revision,
		SelectedRoleIDs:      append([]string(nil), selection.SelectedRoleIDs...),
		PracticeOptionID:     selection.PracticeOptionID,
		MaxEffectiveTurns:    policy.MaxEffectiveTurns,
	}
	return preparation.RevisePlanCommand{
		PlanID:               current.ID,
		ExpectedPlanRevision: current.Revision,
		SceneSelection:       selection,
		SessionPolicy:        policy,
		PracticeObjectives:   planObjectives(t, selection),
		IELTSAssignment:      current.IELTSAssignment,
		Intent: planTestIntent(
			"PUT",
			"/v1/practice-plans/"+current.ID,
			"plan-revise-key-"+current.ID,
			request,
		),
	}
}

func planObjectives(
	t *testing.T,
	selection scene.SelectionSnapshot,
) []preparation.PracticeObjective {
	t.Helper()
	roles, err := selection.SelectedRoles()
	if err != nil {
		t.Fatalf("SelectedRoles: %v", err)
	}
	seen := make(map[string]string)
	var objectives []preparation.PracticeObjective
	for _, role := range roles {
		for _, objective := range role.PracticeObjectives {
			if description, exists := seen[objective.ID]; exists {
				if description != objective.Description {
					t.Fatalf("conflicting objective %q", objective.ID)
				}
				continue
			}
			seen[objective.ID] = objective.Description
			objectives = append(objectives, preparation.PracticeObjective{
				ID: objective.ID, Description: objective.Description,
			})
		}
	}
	return objectives
}

func planTestIntent(
	method string,
	path string,
	key string,
	payload any,
) preparation.IdempotencyIntent {
	document, err := json.Marshal(payload)
	if err != nil {
		panic("Plan test payload must be JSON encodable")
	}
	return preparation.IdempotencyIntent{
		Method: method, CanonicalPath: path, Key: key,
		PayloadFingerprint: sha256.Sum256(document),
	}
}

func assertStoredPlanRevisions(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	planID string,
) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT revision, scene_selection ->> 'practice_option_id'
		FROM preparation_practice_plan_revisions
		WHERE owner_user_id = $1 AND plan_id = $2
		ORDER BY revision
	`, userID, planID)
	if err != nil {
		t.Fatalf("read Plan revisions: %v", err)
	}
	defer rows.Close()
	var revisions []int
	var options []string
	for rows.Next() {
		var revision int
		var option string
		if err := rows.Scan(&revision, &option); err != nil {
			t.Fatalf("scan Plan revision: %v", err)
		}
		revisions = append(revisions, revision)
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate Plan revisions: %v", err)
	}
	if !reflect.DeepEqual(revisions, []int{1, 2}) ||
		!reflect.DeepEqual(
			options,
			[]string{"option_technical_focus", "option_full_simulation"},
		) {
		t.Fatalf("stored Plan revisions = (%v, %v)", revisions, options)
	}
	var idempotencyRevisions []int
	rows, err = pool.Query(context.Background(), `
		SELECT resource_revision
		FROM preparation_idempotency_records
		WHERE owner_user_id = $1
		  AND resource_kind = 'plan'
		  AND resource_id = $2
		ORDER BY resource_revision
	`, userID, planID)
	if err != nil {
		t.Fatalf("read Plan idempotency revisions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var revision int
		if err := rows.Scan(&revision); err != nil {
			t.Fatalf("scan Plan idempotency revision: %v", err)
		}
		idempotencyRevisions = append(idempotencyRevisions, revision)
	}
	if !reflect.DeepEqual(idempotencyRevisions, []int{1, 2}) {
		t.Fatalf("Plan idempotency revisions = %v", idempotencyRevisions)
	}
}

func seedBlockingPracticeSession(
	t *testing.T,
	pool *pgxpool.Pool,
	plan preparation.PracticePlan,
) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Practice Session fixture: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, status, version,
			effective_turns, started_at, snapshot_id,
			scene_family, scene_model, evaluation_policy_ref, plan_revision
		) VALUES (
			$1, 'session-blocking-delete', $2, 'in_progress', 1,
			0, transaction_timestamp(), 'session-blocking-snapshot',
			$3, $4, $5, $6
		)
	`,
		plan.UserID,
		plan.ID,
		plan.SceneSelection.Scene.Family,
		plan.SceneSelection.Scene.Model,
		plan.SceneSelection.Scene.EvaluationPolicyRef,
		plan.Revision,
	); err != nil {
		t.Fatalf("insert blocking Practice Session: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, mode, target_ids, participants,
			turn_limit, snapshot_id, snapshot_document
		) VALUES (
			$1, 'session-blocking-delete', 'formal', '[]'::jsonb,
			'[]'::jsonb, 1, 'session-blocking-snapshot', '{}'::jsonb
		)
	`, plan.UserID); err != nil {
		t.Fatalf("insert blocking Practice Session Snapshot: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit blocking Practice Session fixture: %v", err)
	}
}

func assertPlanOwnedRows(
	t *testing.T,
	pool *pgxpool.Pool,
	table string,
	userID string,
	wantRows bool,
) {
	t.Helper()
	allowed := map[string]bool{
		"preparation_idempotency_records":     true,
		"preparation_practice_plans":          true,
		"preparation_practice_plan_revisions": true,
		"preparation_snapshots":               true,
		"preparation_profiles":                true,
	}
	if !allowed[table] {
		t.Fatalf("unsupported Plan table %q", table)
	}
	query := "SELECT EXISTS (SELECT 1 FROM " +
		pgx.Identifier{table}.Sanitize() + " WHERE owner_user_id = $1)"
	var exists bool
	if err := pool.QueryRow(context.Background(), query, userID).Scan(
		&exists,
	); err != nil {
		t.Fatalf("read %s rows: %v", table, err)
	}
	if exists != wantRows {
		t.Fatalf("%s has rows = %t, want %t", table, exists, wantRows)
	}
}
