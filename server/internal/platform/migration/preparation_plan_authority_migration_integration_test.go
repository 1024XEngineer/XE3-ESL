package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	planAuthorityOwnerID             = "10000000-0000-4000-8000-000000000151"
	planAuthorityOtherOwnerID        = "10000000-0000-4000-8000-000000000152"
	planAuthorityGoalID              = "20000000-0000-4000-8000-000000000151"
	planAuthorityThreadID            = "30000000-0000-4000-8000-000000000151"
	planAuthorityOwnedPrivateSceneID = "scene-private-owned"
	planAuthorityOtherPrivateSceneID = "scene-private-other"
)

func TestPreparationPlanAuthorityMigrationSwitchesTheEmptyPlanSlice(
	t *testing.T,
) {
	migrationConfig, admin, schema := isolatedMigrationConfig(t)
	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	if err := runner.migrate.Steps(50); err != nil {
		t.Fatalf("apply migrations through version 50: %v", err)
	}
	assertMigrationStatus(t, runner, 50)
	assertPreparationPlanAuthoritySchema(t, admin, schema, false)

	if err := runner.migrate.Steps(1); err != nil {
		t.Fatalf("apply Preparation Plan authority migration: %v", err)
	}
	assertMigrationStatus(t, runner, 51)
	assertPreparationPlanAuthoritySchema(t, admin, schema, true)

	changed, err := runner.DownOne()
	if err != nil {
		t.Fatalf("revert Preparation Plan authority migration: %v", err)
	}
	if !changed {
		t.Fatal("Preparation Plan authority down migration reported no change")
	}
	assertMigrationStatus(t, runner, 50)
	assertPreparationPlanAuthoritySchema(t, admin, schema, false)

	if err := runner.migrate.Steps(1); err != nil {
		t.Fatalf("reapply Preparation Plan authority migration: %v", err)
	}
	assertMigrationStatus(t, runner, 51)
	assertPreparationPlanAuthoritySchema(t, admin, schema, true)
}

func TestPreparationPlanAuthorityMigrationRejectsNonemptyLegacySlices(
	t *testing.T,
) {
	for _, test := range []struct {
		name string
		seed func(*testing.T, *pgx.Conn)
	}{
		{name: "Plan", seed: seedLegacyPracticePlan},
		{name: "Session", seed: seedLegacyPracticeSession},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			migrationConfig, admin, schema := isolatedMigrationConfig(t)
			runner, err := openConfig(migrationConfig)
			if err != nil {
				t.Fatalf("open migration runner: %v", err)
			}
			t.Cleanup(func() {
				if err := runner.Close(); err != nil {
					t.Errorf("close migration runner: %v", err)
				}
			})
			if err := runner.migrate.Steps(50); err != nil {
				t.Fatalf("apply migrations through version 50: %v", err)
			}

			database, err := pgx.ConnectConfig(
				context.Background(),
				migrationConfig,
			)
			if err != nil {
				t.Fatalf("connect to version 50 schema: %v", err)
			}
			t.Cleanup(func() {
				if err := database.Close(context.Background()); err != nil {
					t.Errorf("close version 50 connection: %v", err)
				}
			})
			test.seed(t, database)

			err = runner.migrate.Steps(1)
			if err == nil || !strings.Contains(
				err.Error(),
				"recreate the development or test database",
			) {
				t.Fatalf("migration with legacy %s data error = %v", test.name, err)
			}
			assertPreparationPlanAuthoritySchema(t, admin, schema, false)
		})
	}
}

func TestPreparationPlanObjectiveValidatorUsesLowerSnakeIDs(t *testing.T) {
	migrationConfig, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})
	if err := runner.migrate.Steps(51); err != nil {
		t.Fatalf("apply migrations through version 51: %v", err)
	}
	database, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect to Preparation Plan authority schema: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close Preparation Plan authority connection: %v", err)
		}
	})

	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{
			name: "lower snake id",
			payload: `[{"objective_id":"system_design","description":` +
				`"Explain a system design with clear trade-offs."}]`,
			want: true,
		},
		{
			name: "uppercase id",
			payload: `[{"objective_id":"System_design","description":` +
				`"Explain a system design."}]`,
			want: false,
		},
		{
			name: "hyphenated id",
			payload: `[{"objective_id":"system-design","description":` +
				`"Explain a system design."}]`,
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got bool
			if err := database.QueryRow(context.Background(), `
				SELECT preparation_plan_objectives_are_valid_v1($1::jsonb)
			`, test.payload).Scan(&got); err != nil {
				t.Fatalf("validate Practice objectives: %v", err)
			}
			if got != test.want {
				t.Fatalf("objective validator = %t, want %t", got, test.want)
			}
		})
	}

	for _, test := range []struct {
		rule string
		want bool
	}{
		{rule: "COVERAGE_SATISFIED_AFTER_CHECKPOINT", want: true},
		{rule: "all_objectives_covered", want: false},
	} {
		var got bool
		if err := database.QueryRow(context.Background(), `
			SELECT preparation_plan_session_policy_is_valid_v1(
			    jsonb_build_object(
			        'suggested_duration_seconds', 600,
			        'min_effective_turns', 3,
			        'max_effective_turns', 6,
			        'coverage_checkpoint_turn', 4,
			        'max_follow_ups_per_question', 1,
				        'early_completion_rule', $1::text
				    )
				)
		`, test.rule).Scan(&got); err != nil {
			t.Fatalf("validate Session policy: %v", err)
		}
		if got != test.want {
			t.Fatalf("Session policy rule %q valid = %t, want %t", test.rule, got, test.want)
		}
	}
}

func TestPreparationPlanAuthorityEnforcesExactExecutableRevision(
	t *testing.T,
) {
	migrationConfig, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})
	if changed, err := runner.Up(); err != nil || !changed {
		t.Fatalf("apply migrations: changed=%t err=%v", changed, err)
	}

	database, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect to migrated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close migrated schema: %v", err)
		}
	})
	seedPreparationPlanSources(t, database)
	insertPreparationPlanRevision(t, database, "plan-1", 1, true)
	insertInactiveSceneVersion(t, database)
	insertPrivateSceneVersion(
		t,
		database,
		planAuthorityOwnedPrivateSceneID,
		planAuthorityOwnerID,
	)
	insertPrivateSceneVersion(
		t,
		database,
		planAuthorityOtherPrivateSceneID,
		planAuthorityOtherOwnerID,
	)

	_, err = database.Exec(context.Background(), `
		UPDATE preparation_practice_plan_revisions
		SET practice_objectives = practice_objectives
		WHERE owner_user_id = $1 AND plan_id = 'plan-1' AND revision = 1
	`, planAuthorityOwnerID)
	assertPGError(t, err, "55000", "")
	_, err = database.Exec(context.Background(), `
		DELETE FROM preparation_practice_plan_revisions
		WHERE owner_user_id = $1 AND plan_id = 'plan-1' AND revision = 1
	`, planAuthorityOwnerID)
	assertPGError(t, err, "55000", "")

	_, err = database.Exec(context.Background(), `
		INSERT INTO practice_sessions (
		    owner_user_id, session_id, plan_id, plan_revision,
		    snapshot_id, scene_family, scene_model,
		    status, version, effective_turns, started_at
		) VALUES (
		    $1, 'session-active-state', 'plan-1', 1,
		    'session-active-state-snapshot', 'INTERVIEW',
		    'PROJECT_EXPERIENCE_DEEP_DIVE', 'active', 1, 0,
		    transaction_timestamp()
		)
	`, planAuthorityOwnerID)
	assertPGError(t, err, "23514", "practice_sessions_lifecycle_check")

	appendPreparationPlanRevision(t, database, "plan-1", 2)
	assertInactiveSceneRejectedForNewPlan(t, database)
	assertPrivateSceneOwnershipBoundary(t, database)
	_, err = database.Exec(context.Background(), `
		UPDATE preparation_practice_plans
		SET current_revision = 1
		WHERE owner_user_id = $1 AND plan_id = 'plan-1'
	`, planAuthorityOwnerID)
	assertPGError(
		t,
		err,
		"23514",
		"preparation_practice_plans_current_revision_advance_check",
	)
	assertUnpublishedPlanRevisionRejected(t, database, "plan-1", 3)
	assertSessionInsertRejected(
		t,
		database,
		"session-stale-revision",
		1,
		"23514",
		"practice_sessions_executable_plan_revision_check",
	)

	if _, err := database.Exec(context.Background(), `
		UPDATE preparation_practice_plans
		SET status = 'archived', updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND plan_id = 'plan-1'
	`, planAuthorityOwnerID); err != nil {
		t.Fatalf("archive current Plan: %v", err)
	}
	assertSessionInsertRejected(
		t,
		database,
		"session-archived-plan",
		2,
		"23514",
		"practice_sessions_executable_plan_revision_check",
	)

	if _, err := database.Exec(context.Background(), `
		UPDATE preparation_practice_plans
		SET status = 'ready', updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND plan_id = 'plan-1'
	`, planAuthorityOwnerID); err != nil {
		t.Fatalf("restore ready Plan: %v", err)
	}
	insertPracticeSessionAtRevision(t, database, "session-current", 2)

	if _, err := database.Exec(context.Background(), `
		INSERT INTO preparation_idempotency_records (
		    owner_user_id, method, canonical_path, idempotency_key,
		    payload_fingerprint, resource_kind, resource_id,
		    resource_revision, response_status, response_body
		) VALUES (
		    $1, 'POST', '/v1/preparation-plans', 'plan-key-0001',
		    decode(repeat('11', 32), 'hex'), 'plan', 'plan-1', 2,
		    201, '{}'::jsonb
		)
	`, planAuthorityOwnerID); err != nil {
		t.Fatalf("persist Plan idempotency revision: %v", err)
	}
	_, err = database.Exec(context.Background(), `
		INSERT INTO practice_idempotency_records (
		    owner_user_id, method, canonical_path, idempotency_key,
		    payload_fingerprint, resource_kind, resource_id,
		    response_status, response_body
		) VALUES (
		    $1, 'POST', '/v1/preparation-plans', 'legacy-plan-key',
		    decode(repeat('22', 32), 'hex'), 'plan', 'plan-1',
		    201, '{}'::jsonb
		)
	`, planAuthorityOwnerID)
	assertPGError(
		t,
		err,
		"23514",
		"practice_idempotency_resource_check",
	)

	if _, err := database.Exec(context.Background(), `
		DELETE FROM agent_threads WHERE id = $1
	`, planAuthorityThreadID); err != nil {
		t.Fatalf("delete optional source Thread: %v", err)
	}
	var sourceThreadID *string
	if err := database.QueryRow(context.Background(), `
		SELECT source_thread_id::text
		FROM preparation_practice_plans
		WHERE owner_user_id = $1 AND plan_id = 'plan-1'
	`, planAuthorityOwnerID).Scan(&sourceThreadID); err != nil {
		t.Fatalf("read Plan source Thread after deletion: %v", err)
	}
	if sourceThreadID != nil {
		t.Fatalf("Plan source Thread after deletion = %v, want nil", sourceThreadID)
	}

	changed, err := runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("down Practice runtime authority = changed %t, error %v", changed, err)
	}
	changed, err = runner.DownOne()
	if err == nil || changed || !strings.Contains(
		err.Error(),
		"recreate the development or test database",
	) {
		t.Fatalf("down migration with Plan data = changed %t, error %v", changed, err)
	}
}

func assertPreparationPlanAuthoritySchema(
	t *testing.T,
	connection *pgx.Conn,
	schema string,
	wantAuthority bool,
) {
	t.Helper()

	for table, wantPresent := range map[string]bool{
		"practice_plans":                      !wantAuthority,
		"preparation_practice_plans":          wantAuthority,
		"preparation_practice_plan_revisions": wantAuthority,
	} {
		var relation *string
		if err := connection.QueryRow(
			context.Background(),
			"SELECT to_regclass($1)",
			schema+"."+table,
		).Scan(&relation); err != nil {
			t.Fatalf("inspect table %s: %v", table, err)
		}
		if (relation != nil) != wantPresent {
			t.Errorf("table %s present = %t, want %t", table, relation != nil, wantPresent)
		}
	}

	var ieltsAssignmentColumn bool
	if err := connection.QueryRow(context.Background(), `
		SELECT EXISTS (
		    SELECT 1
		    FROM information_schema.columns
		    WHERE table_schema = $1
		      AND table_name = 'preparation_practice_plan_revisions'
		      AND column_name = 'ielts_assignment'
		)
	`, schema).Scan(&ieltsAssignmentColumn); err != nil {
		t.Fatalf("inspect Plan IELTS assignment column: %v", err)
	}
	if ieltsAssignmentColumn != wantAuthority {
		t.Errorf(
			"Plan IELTS assignment column present = %t, want %t",
			ieltsAssignmentColumn,
			wantAuthority,
		)
	}

	var immutableTriggerDefinition *string
	if err := connection.QueryRow(context.Background(), `
		SELECT pg_get_triggerdef(trigger.oid)
		FROM pg_trigger AS trigger
		JOIN pg_class AS relation ON relation.oid = trigger.tgrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = $1
		  AND relation.relname = 'preparation_practice_plan_revisions'
		  AND trigger.tgname =
		      'preparation_practice_plan_revisions_are_immutable'
	`, schema).Scan(&immutableTriggerDefinition); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("inspect Plan revision immutable trigger: %v", err)
	}
	if wantAuthority {
		if immutableTriggerDefinition == nil ||
			!strings.Contains(*immutableTriggerDefinition, "UPDATE") ||
			!strings.Contains(*immutableTriggerDefinition, "DELETE") {
			t.Fatalf(
				"Plan revision immutable trigger = %v",
				immutableTriggerDefinition,
			)
		}
	} else if immutableTriggerDefinition != nil {
		t.Fatalf("Plan revision immutable trigger remains after down migration")
	}

	var reviewContextConstraint string
	if err := connection.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(constraint_record.oid)
		FROM pg_constraint AS constraint_record
		JOIN pg_class AS relation
		  ON relation.oid = constraint_record.conrelid
		JOIN pg_namespace AS namespace
		  ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = $1
		  AND relation.relname = 'reviews'
		  AND constraint_record.conname =
		      'reviews_evaluation_context_check'
	`, schema).Scan(&reviewContextConstraint); err != nil {
		t.Fatalf("inspect Review evaluation context constraint: %v", err)
	}
	if wantAuthority {
		if !strings.Contains(reviewContextConstraint, "scene_id") ||
			!strings.Contains(reviewContextConstraint, "scene_version") ||
			strings.Contains(reviewContextConstraint, "scenario_definition") {
			t.Fatalf(
				"Review context constraint after authority migration = %s",
				reviewContextConstraint,
			)
		}
	} else if !strings.Contains(
		reviewContextConstraint,
		"scenario_definition_id",
	) || !strings.Contains(
		reviewContextConstraint,
		"scenario_definition_version",
	) {
		t.Fatalf(
			"Review context constraint before authority migration = %s",
			reviewContextConstraint,
		)
	}

	for column, wantPresent := range map[string]bool{
		"plan_revision":   wantAuthority,
		"context_plan_id": !wantAuthority,
		"agent_thread_id": !wantAuthority,
		"goal_id":         !wantAuthority,
		"scene_family":    wantAuthority,
		"scenario_type":   !wantAuthority,
	} {
		var present bool
		if err := connection.QueryRow(context.Background(), `
			SELECT EXISTS (
			    SELECT 1
			    FROM information_schema.columns
			    WHERE table_schema = $1
			      AND table_name = 'practice_sessions'
			      AND column_name = $2
			)
		`, schema, column).Scan(&present); err != nil {
			t.Fatalf("inspect Practice Session column %s: %v", column, err)
		}
		if present != wantPresent {
			t.Errorf("Practice Session column %s present = %t, want %t", column, present, wantPresent)
		}
	}

	for column := range map[string]struct{}{
		"context_plan_id":         {},
		"preparation_snapshot_id": {},
	} {
		var present bool
		if err := connection.QueryRow(context.Background(), `
			SELECT EXISTS (
			    SELECT 1
			    FROM information_schema.columns
			    WHERE table_schema = $1
			      AND table_name = 'practice_session_snapshots'
			      AND column_name = $2
			)
		`, schema, column).Scan(&present); err != nil {
			t.Fatalf("inspect Practice Snapshot column %s: %v", column, err)
		}
		if present == wantAuthority {
			t.Errorf(
				"Practice Snapshot column %s present = %t, want %t",
				column,
				present,
				!wantAuthority,
			)
		}
	}
}

func seedLegacyPracticePlan(t *testing.T, database *pgx.Conn) {
	t.Helper()
	seedLegacyPlanOwnerAndThread(t, database)
	if _, err := database.Exec(context.Background(), `
		INSERT INTO preparation_profiles (
		    owner_user_id, profile_id, background_summary
		) VALUES ($1, 'legacy-profile', 'Legacy profile')
	`, planAuthorityOwnerID); err != nil {
		t.Fatalf("seed legacy Profile: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
		INSERT INTO practice_plans (
		    owner_user_id, plan_id, agent_thread_id,
		    scenario_definition_id, scenario_definition_version,
		    scenario_type, scenario_model,
		    scenario_config_id, scenario_config_version,
		    preparation_profile_id, selected_role_ids, status
		) VALUES (
		    $1, 'legacy-plan', $2,
		    'legacy-scene', 1, 'INTERVIEW',
		    'PROJECT_EXPERIENCE_DEEP_DIVE', 'legacy-config', 1,
		    'legacy-profile', '["legacy-role"]'::jsonb, 'ready'
		)
	`, planAuthorityOwnerID, planAuthorityThreadID); err != nil {
		t.Fatalf("seed legacy Plan: %v", err)
	}
}

func seedLegacyPracticeSession(t *testing.T, database *pgx.Conn) {
	t.Helper()
	seedLegacyPlanOwnerAndThread(t, database)
	if _, err := database.Exec(context.Background(), `
		INSERT INTO practice_sessions (
		    owner_user_id, session_id, plan_id, status, version,
		    effective_turns, started_at
		) VALUES (
		    $1, 'legacy-session', 'unbound-plan', 'active', 1, 0,
		    transaction_timestamp()
		)
	`, planAuthorityOwnerID); err != nil {
		t.Fatalf("seed legacy Session: %v", err)
	}
}

func seedLegacyPlanOwnerAndThread(t *testing.T, database *pgx.Conn) {
	t.Helper()
	if _, err := database.Exec(context.Background(), `
		INSERT INTO identity_users (id, canonical_email)
		VALUES ($1, 'legacy-plan-authority@example.test')
	`, planAuthorityOwnerID); err != nil {
		t.Fatalf("seed legacy Plan owner: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
		INSERT INTO agent_threads (id, owner_user_id) VALUES ($1, $2)
	`, planAuthorityThreadID, planAuthorityOwnerID); err != nil {
		t.Fatalf("seed legacy Plan Thread: %v", err)
	}
}

func seedPreparationPlanSources(t *testing.T, database *pgx.Conn) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO identity_users (id, canonical_email)
			 VALUES ($1, 'plan-authority@example.test')`,
			[]any{planAuthorityOwnerID},
		},
		{
			`INSERT INTO identity_users (id, canonical_email)
			 VALUES ($1, 'plan-authority-other@example.test')`,
			[]any{planAuthorityOtherOwnerID},
		},
		{
			`INSERT INTO agent_threads (id, owner_user_id) VALUES ($1, $2)`,
			[]any{planAuthorityThreadID, planAuthorityOwnerID},
		},
		{
			`INSERT INTO coaching_goals (
			     goal_id, owner_user_id, title, version
			 ) VALUES ($1, $2, 'Backend interview', 1)`,
			[]any{planAuthorityGoalID, planAuthorityOwnerID},
		},
		{
			`INSERT INTO preparation_profiles (
			     owner_user_id, profile_id, background_summary
			 ) VALUES ($1, 'profile-1', 'Backend engineer')`,
			[]any{planAuthorityOwnerID},
		},
		{
			`INSERT INTO preparation_snapshots (
			     owner_user_id, snapshot_id, source_profile_id,
			     source_version, background_snapshot
			 ) VALUES (
			     $1, 'preparation-snapshot-1', 'profile-1', 1,
			     'Backend engineer'
			 )`,
			[]any{planAuthorityOwnerID},
		},
	}
	for _, statement := range statements {
		if _, err := database.Exec(
			context.Background(),
			statement.query,
			statement.args...,
		); err != nil {
			t.Fatalf("seed Preparation Plan source: %v", err)
		}
	}
}

func insertPreparationPlanRevision(
	t *testing.T,
	database *pgx.Conn,
	planID string,
	revision int,
	includeSourceThread bool,
) {
	t.Helper()
	tx, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin initial Plan revision: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var sourceThread any
	if includeSourceThread {
		sourceThread = planAuthorityThreadID
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO preparation_practice_plans (
		    owner_user_id, plan_id, current_revision, status,
		    source_thread_id
		) VALUES ($1, $2, $3, 'ready', $4)
	`, planAuthorityOwnerID, planID, revision, sourceThread); err != nil {
		t.Fatalf("insert Plan identity: %v", err)
	}
	insertPlanRevisionRow(t, tx, planID, revision)
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit initial Plan revision: %v", err)
	}
}

func appendPreparationPlanRevision(
	t *testing.T,
	database *pgx.Conn,
	planID string,
	revision int,
) {
	t.Helper()
	tx, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin appended Plan revision: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO preparation_practice_plan_revisions (
		    owner_user_id, plan_id, revision,
		    goal_id, goal_version, goal_snapshot,
		    preparation_snapshot_id, preparation_snapshot,
		    scene_id, scene_version, scene_selection,
		    session_policy, practice_objectives, ielts_assignment
		)
		SELECT
		    owner_user_id, plan_id, $3,
		    goal_id, goal_version, goal_snapshot,
		    preparation_snapshot_id, preparation_snapshot,
		    scene_id, scene_version, scene_selection,
		    session_policy, practice_objectives, ielts_assignment
		FROM preparation_practice_plan_revisions
		WHERE owner_user_id = $1
		  AND plan_id = $2
		  AND revision = $3 - 1
	`, planAuthorityOwnerID, planID, revision); err != nil {
		t.Fatalf("append frozen Plan revision: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		UPDATE preparation_practice_plans
		SET current_revision = $3, updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND plan_id = $2
	`, planAuthorityOwnerID, planID, revision); err != nil {
		t.Fatalf("advance current Plan revision: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit appended Plan revision: %v", err)
	}
}

func assertUnpublishedPlanRevisionRejected(
	t *testing.T,
	database *pgx.Conn,
	planID string,
	revision int,
) {
	t.Helper()
	tx, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin unpublished Plan revision: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	insertPlanRevisionRow(t, tx, planID, revision)
	err = tx.Commit(context.Background())
	assertPGError(
		t,
		err,
		"23514",
		"preparation_practice_plans_current_revision_advance_check",
	)
}

func insertInactiveSceneVersion(t *testing.T, database *pgx.Conn) {
	t.Helper()
	if _, err := database.Exec(context.Background(), `
		INSERT INTO coaching_scene_versions (
		    scene_id, scene_version, scene_family, scene_model, name,
		    status, turn_policy_ref, session_policy_ref, prompt, roles,
		    practice_options, display_order
		)
		SELECT
		    scene_id, 2, scene_family, scene_model, name,
		    'inactive', turn_policy_ref, session_policy_ref, prompt, roles,
		    practice_options, display_order
		FROM coaching_scene_versions
		WHERE scene_id = 'scn_programmer_interview'
		  AND scene_version = 1
	`); err != nil {
		t.Fatalf("insert inactive Scene version: %v", err)
	}
}

func insertPrivateSceneVersion(
	t *testing.T,
	database *pgx.Conn,
	sceneID string,
	ownerUserID string,
) {
	t.Helper()
	if _, err := database.Exec(context.Background(), `
		INSERT INTO coaching_scenes (scene_id, owner_user_id)
		VALUES ($1, $2)
	`, sceneID, ownerUserID); err != nil {
		t.Fatalf("insert private Scene identity %q: %v", sceneID, err)
	}
	if _, err := database.Exec(context.Background(), `
		INSERT INTO coaching_scene_versions (
		    scene_id, scene_version, scene_family, scene_model, name,
		    status, turn_policy_ref, session_policy_ref, prompt, roles,
		    practice_options, display_order
		)
		SELECT
		    $1, 1, source.scene_family, source.scene_model, source.name,
		    'active', source.turn_policy_ref, source.session_policy_ref,
		    source.prompt,
		    (
		        SELECT jsonb_agg(
		            jsonb_set(role.value, '{scene_id}', to_jsonb($1::text))
		            ORDER BY role.ordinality
		        )
		        FROM jsonb_array_elements(source.roles)
		            WITH ORDINALITY AS role(value, ordinality)
		    ),
		    (
		        SELECT jsonb_agg(
		            jsonb_set(
		                practice_option.value,
		                '{scene_id}',
		                to_jsonb($1::text)
		            )
		            ORDER BY practice_option.ordinality
		        )
		        FROM jsonb_array_elements(source.practice_options)
		            WITH ORDINALITY AS practice_option(value, ordinality)
		    ),
		    source.display_order
		FROM coaching_scene_versions AS source
		WHERE source.scene_id = 'scn_programmer_interview'
		  AND source.scene_version = 1
	`, sceneID); err != nil {
		t.Fatalf("insert private Scene version %q: %v", sceneID, err)
	}
}

func assertInactiveSceneRejectedForNewPlan(
	t *testing.T,
	database *pgx.Conn,
) {
	t.Helper()
	for _, test := range []struct {
		name         string
		planID       string
		sceneVersion int
	}{
		{
			name:         "inactive latest version",
			planID:       "plan-inactive-scene",
			sceneVersion: 2,
		},
		{
			name:         "historical active version after deactivation",
			planID:       "plan-retired-scene-fallback",
			sceneVersion: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := database.Begin(context.Background())
			if err != nil {
				t.Fatalf("begin inactive Scene Plan: %v", err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(context.Background(), `
				INSERT INTO preparation_practice_plans (
				    owner_user_id, plan_id, current_revision, status
				) VALUES ($1, $2, 1, 'ready')
			`, planAuthorityOwnerID, test.planID); err != nil {
				t.Fatalf("insert inactive Scene Plan identity: %v", err)
			}
			err = insertPlanRevisionRowForScene(
				tx,
				test.planID,
				1,
				"scn_programmer_interview",
				test.sceneVersion,
			)
			assertPGError(
				t,
				err,
				"23503",
				"preparation_practice_plan_revisions_scene_fkey",
			)
		})
	}
}

func assertPrivateSceneOwnershipBoundary(
	t *testing.T,
	database *pgx.Conn,
) {
	t.Helper()

	t.Run("other owner's private Scene", func(t *testing.T) {
		tx, err := database.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin cross-owner private Scene Plan: %v", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO preparation_practice_plans (
			    owner_user_id, plan_id, current_revision, status
			) VALUES ($1, 'plan-other-private-scene', 1, 'ready')
		`, planAuthorityOwnerID); err != nil {
			t.Fatalf("insert cross-owner private Scene Plan identity: %v", err)
		}
		err = insertPlanRevisionRowForScene(
			tx,
			"plan-other-private-scene",
			1,
			planAuthorityOtherPrivateSceneID,
			1,
		)
		assertPGError(
			t,
			err,
			"23503",
			"preparation_practice_plan_revisions_scene_fkey",
		)
	})

	t.Run("actor-owned private Scene", func(t *testing.T) {
		tx, err := database.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin actor-owned private Scene Plan: %v", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO preparation_practice_plans (
			    owner_user_id, plan_id, current_revision, status
			) VALUES ($1, 'plan-owned-private-scene', 1, 'ready')
		`, planAuthorityOwnerID); err != nil {
			t.Fatalf("insert actor-owned private Scene Plan identity: %v", err)
		}
		if err := insertPlanRevisionRowForScene(
			tx,
			"plan-owned-private-scene",
			1,
			planAuthorityOwnedPrivateSceneID,
			1,
		); err != nil {
			t.Fatalf("insert actor-owned private Scene Plan revision: %v", err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatalf("commit actor-owned private Scene Plan: %v", err)
		}
	})
}

type planRevisionExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertPlanRevisionRow(
	t *testing.T,
	executor planRevisionExecutor,
	planID string,
	revision int,
) {
	t.Helper()
	if err := insertPlanRevisionRowForScene(
		executor,
		planID,
		revision,
		"scn_programmer_interview",
		1,
	); err != nil {
		t.Fatalf("insert Plan revision %d: %v", revision, err)
	}
}

func insertPlanRevisionRowForScene(
	executor planRevisionExecutor,
	planID string,
	revision int,
	sceneID string,
	sceneVersion int,
) error {
	_, err := executor.Exec(context.Background(), `
		INSERT INTO preparation_practice_plan_revisions (
		    owner_user_id, plan_id, revision,
		    goal_id, goal_version, goal_snapshot,
		    preparation_snapshot_id, preparation_snapshot,
		    scene_id, scene_version, scene_selection,
		    session_policy, practice_objectives
		)
		SELECT
		    $1, $2, $3,
		    $4::uuid, 1,
		    jsonb_build_object(
		        'goal_id', $4::text,
		        'title', 'Backend interview',
		        'version', 1
		    ),
		    'preparation-snapshot-1',
		    jsonb_build_object(
		        'preparation_snapshot_id', 'preparation-snapshot-1',
		        'source_profile_id', 'profile-1',
		        'source_version', 1,
		        'background_snapshot', 'Backend engineer',
		        'created_at', '2026-08-04T00:00:00Z'
		    ),
		    version.scene_id,
		    version.scene_version,
		    jsonb_build_object(
		        'scene', jsonb_build_object(
		            'scene_id', version.scene_id,
		            'scene_family', version.scene_family,
		            'scene_model', version.scene_model,
		            'name', version.name,
		            'scene_version', version.scene_version,
		            'status', version.status,
		            'turn_policy_ref', version.turn_policy_ref,
		            'session_policy_ref', version.session_policy_ref,
		            'prompt', version.prompt,
		            'roles', (
		                SELECT jsonb_agg(
		                    role.value - 'display_order'
		                    ORDER BY
		                        (role.value ->> 'display_order')::integer,
		                        role.value ->> 'role_definition_id'
		                )
		                FROM jsonb_array_elements(version.roles) AS role(value)
		            ),
		            'practice_options', (
		                SELECT jsonb_agg(
		                    practice_option.value - 'display_order'
		                    ORDER BY
		                        (
		                            practice_option.value ->> 'display_order'
		                        )::integer,
		                        practice_option.value ->> 'practice_option_id'
		                )
		                FROM jsonb_array_elements(version.practice_options)
		                    AS practice_option(value)
		            )
		        ),
		        'selected_role_ids', jsonb_build_array(
		            version.roles -> 0 ->> 'role_definition_id'
		        ),
		        'practice_option_id',
		            version.practice_options -> 1 ->> 'practice_option_id'
		    ),
		    jsonb_build_object(
		        'suggested_duration_seconds', 600,
		        'min_effective_turns', 3,
		        'max_effective_turns', 6,
		        'coverage_checkpoint_turn', 4,
		        'max_follow_ups_per_question', 1,
		        'early_completion_rule',
		            'COVERAGE_SATISFIED_AFTER_CHECKPOINT'
		    ),
		    '[{"objective_id":"objective_1","description":"Explain one project clearly"}]'::jsonb
		FROM coaching_scene_versions AS version
		WHERE version.scene_id = $5
		  AND version.scene_version = $6
	`,
		planAuthorityOwnerID,
		planID,
		revision,
		planAuthorityGoalID,
		sceneID,
		sceneVersion,
	)
	return err
}

func assertSessionInsertRejected(
	t *testing.T,
	database *pgx.Conn,
	sessionID string,
	revision int,
	code string,
	constraint string,
) {
	t.Helper()
	_, err := database.Exec(context.Background(), `
		INSERT INTO practice_sessions (
		    owner_user_id, session_id, plan_id, plan_revision,
		    snapshot_id, scene_family, scene_model,
		    status, version, effective_turns
		) VALUES (
		    $1, $2, 'plan-1', $3,
		    $2 || '-snapshot', 'INTERVIEW',
		    'PROJECT_EXPERIENCE_DEEP_DIVE', 'starting', 1, 0
		)
	`, planAuthorityOwnerID, sessionID, revision)
	assertPGError(t, err, code, constraint)
}

func insertPracticeSessionAtRevision(
	t *testing.T,
	database *pgx.Conn,
	sessionID string,
	revision int,
) {
	t.Helper()
	tx, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin exact Practice Session: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	snapshotID := sessionID + "-snapshot"
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO practice_sessions (
		    owner_user_id, session_id, plan_id, plan_revision,
		    snapshot_id, scene_family, scene_model,
		    status, version, effective_turns
		) VALUES (
		    $1, $2, 'plan-1', $3, $4, 'INTERVIEW',
		    'PROJECT_EXPERIENCE_DEEP_DIVE', 'starting', 1, 0
		)
	`, planAuthorityOwnerID, sessionID, revision, snapshotID); err != nil {
		t.Fatalf("insert exact Practice Session: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO practice_session_snapshots (
		    owner_user_id, session_id, mode, target_ids, participants,
		    turn_limit, snapshot_id, snapshot_document
		) VALUES (
		    $1, $2, 'INTERVIEW', '["goal"]'::jsonb,
		    '[{"participant_id":"candidate"}]'::jsonb,
		    6, $3, '{}'::jsonb
		)
	`, planAuthorityOwnerID, sessionID, snapshotID); err != nil {
		t.Fatalf("insert exact Practice Session Snapshot: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit exact Practice Session: %v", err)
	}
}

func assertPGError(
	t *testing.T,
	err error,
	code string,
	constraint string,
) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != code ||
		(constraint != "" && pgError.ConstraintName != constraint) {
		t.Fatalf(
			"PostgreSQL error = %v, want code %s constraint %q",
			err,
			code,
			constraint,
		)
	}
}
