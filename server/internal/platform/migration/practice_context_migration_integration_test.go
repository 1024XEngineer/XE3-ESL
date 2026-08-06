package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPracticeContextMigrationBindsSnapshotAndRestrictsPlanDeletion(
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
	if err := runner.migrate.Steps(50); err != nil {
		t.Fatalf("apply migrations through version 50: %v", err)
	}

	database, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect migrated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close migrated schema: %v", err)
		}
	})

	const (
		userID   = "10000000-0000-4000-8000-000000000112"
		goalID   = "20000000-0000-4000-8000-000000000112"
		threadID = "30000000-0000-4000-8000-000000000112"
	)
	seed, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin practice context seed: %v", err)
	}
	seedStatements := []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO identity_users (id, canonical_email)
			 VALUES ($1, 'practice-context@example.test')`,
			[]any{userID},
		},
		{
			`INSERT INTO coaching_goals (goal_id, owner_user_id, title)
			 VALUES ($1, $2, 'Accepted interview goal')`,
			[]any{goalID, userID},
		},
		{
			`INSERT INTO agent_threads (id, owner_user_id)
			 VALUES ($1, $2)`,
			[]any{threadID, userID},
		},
		{
			`INSERT INTO agent_thread_goal_links (
			     owner_user_id, thread_id, goal_id, is_active
			 ) VALUES ($1, $2, $3, true)`,
			[]any{userID, threadID, goalID},
		},
		{
			`INSERT INTO preparation_profiles (
			     owner_user_id, profile_id, background_summary
			 ) VALUES ($1, 'profile-1', 'Backend engineer')`,
			[]any{userID},
		},
		{
			`INSERT INTO preparation_snapshots (
			     owner_user_id, snapshot_id, source_profile_id,
			     source_version, background_snapshot
			 ) VALUES (
			     $1, 'preparation-snapshot-1', 'profile-1', 1,
			     'Backend engineer'
			 )`,
			[]any{userID},
		},
		{
			`INSERT INTO practice_plans (
			     owner_user_id, plan_id, agent_thread_id, goal_id,
			     scenario_definition_id, scenario_definition_version,
			     scenario_type, scenario_model,
			     scenario_config_id, scenario_config_version,
			     preparation_profile_id, selected_role_ids, status
			 ) VALUES (
			     $1, 'plan-1', $2, $3,
			     'scn_programmer_interview', 1, 'INTERVIEW',
			     'PROJECT_EXPERIENCE_DEEP_DIVE',
			     'scfg_backend_engineer', 1, 'profile-1',
			     '["role_technical_interviewer"]'::jsonb, 'ready'
			 )`,
			[]any{userID, threadID, goalID},
		},
	}
	for _, statement := range seedStatements {
		if _, err := seed.Exec(
			context.Background(),
			statement.query,
			statement.args...,
		); err != nil {
			_ = seed.Rollback(context.Background())
			t.Fatalf("seed practice context: %v", err)
		}
	}
	if err := seed.Commit(context.Background()); err != nil {
		t.Fatalf("commit practice context seed: %v", err)
	}

	tx, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin exact snapshot insert: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, context_plan_id,
			agent_thread_id, goal_id, snapshot_id, scenario_type,
			scenario_model, status, version, effective_turns
		) VALUES (
			$1, 'session-1', 'plan-1', 'plan-1',
			$2, $3, 'session-snapshot-1', 'INTERVIEW',
			'PROJECT_EXPERIENCE_DEEP_DIVE', 'starting', 1, 0
		)
	`, userID, threadID, goalID); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("insert exact Session binding: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, mode, target_ids, participants,
			turn_limit, snapshot_id, context_plan_id,
			preparation_snapshot_id, snapshot_document
		) VALUES (
			$1, 'session-1', 'INTERVIEW', '["goal"]'::jsonb,
			'[{"participant_id":"candidate"}]'::jsonb, 6,
			'session-snapshot-1', 'plan-1',
			'preparation-snapshot-1', '{}'::jsonb
		)
	`, userID); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("insert exact snapshot binding: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit exact snapshot binding: %v", err)
	}

	if _, err := database.Exec(context.Background(), `
		INSERT INTO practice_plans (
			owner_user_id, plan_id, agent_thread_id, goal_id,
			scenario_definition_id, scenario_definition_version,
			scenario_type, scenario_model,
			scenario_config_id, scenario_config_version,
			preparation_profile_id, selected_role_ids, status
		) VALUES (
			$1, 'plan-2', $2, $3,
			'scn_programmer_interview', 1, 'INTERVIEW',
			'PROJECT_EXPERIENCE_DEEP_DIVE',
			'scfg_backend_engineer', 1, 'profile-1',
			'["role_hr_interviewer"]'::jsonb, 'ready'
		)
	`, userID, threadID, goalID); err != nil {
		t.Fatalf("insert second Plan on Thread: %v", err)
	}
	_, err = database.Exec(context.Background(), `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, context_plan_id,
			agent_thread_id, goal_id, snapshot_id, scenario_type,
			scenario_model, status, version, effective_turns
		) VALUES (
			$1, 'session-thread-conflict', 'plan-2', 'plan-2',
			$2, $3, 'session-thread-conflict-snapshot',
			'INTERVIEW', 'PROJECT_EXPERIENCE_DEEP_DIVE', 'starting', 1, 0
		)
	`, userID, threadID, goalID)
	var constraintError *pgconn.PgError
	if !errors.As(err, &constraintError) ||
		constraintError.Code != "23505" ||
		constraintError.ConstraintName !=
			"practice_one_effective_session_per_agent_thread" {
		t.Fatalf(
			"second effective Thread Session error = %v, want Thread unique violation",
			err,
		)
	}

	mismatched, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin mismatched snapshot binding: %v", err)
	}
	if _, err := mismatched.Exec(context.Background(), `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, context_plan_id,
			agent_thread_id, goal_id, snapshot_id, scenario_type,
			scenario_model, status, version, effective_turns, started_at,
			completed_at, end_reason
		) VALUES (
			$1, 'session-2', 'plan-1', 'plan-1',
			$2, $3, 'session-snapshot-2', 'INTERVIEW',
			'PROJECT_EXPERIENCE_DEEP_DIVE', 'completed', 2, 1,
			transaction_timestamp(), transaction_timestamp(), 'TEST_COMPLETED'
		)
	`, userID, threadID, goalID); err != nil {
		_ = mismatched.Rollback(context.Background())
		t.Fatalf("stage mismatched Session binding: %v", err)
	}
	if _, err := mismatched.Exec(context.Background(), `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, mode, target_ids, participants,
			turn_limit, snapshot_id, context_plan_id,
			preparation_snapshot_id, snapshot_document
		) VALUES (
			$1, 'session-2', 'INTERVIEW', '["goal"]'::jsonb,
			'[{"participant_id":"candidate"}]'::jsonb, 6,
			'different-snapshot', 'plan-1',
			'preparation-snapshot-1', '{}'::jsonb
		)
	`, userID); err != nil {
		_ = mismatched.Rollback(context.Background())
		t.Fatalf("stage deferred mismatched binding: %v", err)
	}
	err = mismatched.Commit(context.Background())
	if !errors.As(err, &constraintError) ||
		constraintError.Code != "23503" {
		t.Fatalf("mismatched binding commit error = %v, want FK violation", err)
	}

	_, err = database.Exec(
		context.Background(),
		`DELETE FROM practice_plans
		 WHERE owner_user_id = $1 AND plan_id = 'plan-1'`,
		userID,
	)
	if !errors.As(err, &constraintError) ||
		(constraintError.Code != "23001" &&
			constraintError.Code != "23503") ||
		constraintError.ConstraintName !=
			"practice_sessions_context_plan_fkey" {
		t.Fatalf("delete referenced plan error = %v, want FK violation", err)
	}
}
