package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestGeneralSceneMigrationExtendsEvaluationRuntimeAuthority(t *testing.T) {
	t.Parallel()
	up, err := migrations.Files.ReadFile(
		"000059_evaluation_general_scene_runtime.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.Files.ReadFile(
		"000059_evaluation_general_scene_runtime.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.ToLower(string(up))
	for _, required := range []string{
		"evaluation_general_scene_results",
		"general-scene-evaluation/v1",
		"overseas_daily_life",
		"overseas_workplace",
		"evaluation_interview_result_refs_are_consistent",
		"reject_evaluation_general_scene_result_mutation",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("general Scene migration is missing %q", required)
		}
	}
	if !strings.Contains(
		strings.ToLower(string(down)),
		"drop table if exists evaluation_general_scene_results",
	) {
		t.Fatal("general Scene migration does not restore the prior runtime")
	}
}

func TestGeneralSceneAtomicAttemptMigrationRoundTrips(t *testing.T) {
	pool := evaluationDatabaseThrough(
		t,
		"000087_ielts_part1_cue_card_types.up.sql",
	)
	up, err := migrations.Files.ReadFile(
		"000089_evaluation_general_scene_atomic_attempts.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.Files.ReadFile(
		"000089_evaluation_general_scene_atomic_attempts.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply atomic migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("roll back atomic migration: %v", err)
	}
	var absent bool
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass('evaluation_general_scene_atomic_attempts') IS NULL
	`).Scan(&absent); err != nil || !absent {
		t.Fatalf("atomic table absent=%t error=%v", absent, err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("reapply atomic migration: %v", err)
	}
}

func TestGeneralSceneAtomicAttemptMigrationIsBoundAndImmutable(t *testing.T) {
	t.Parallel()
	up, err := migrations.Files.ReadFile(
		"000089_evaluation_general_scene_atomic_attempts.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.Files.ReadFile(
		"000089_evaluation_general_scene_atomic_attempts.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.ToLower(string(up))
	for _, required := range []string{
		"evaluation_general_scene_atomic_attempts",
		"general-scene-evaluation-atomic-result/v1",
		"general-scene-evaluation-atomic-prompt/v1",
		"evaluation_assert_general_scene_atomic_attempt_binding",
		"evaluation_general_scene_atomic_attempts_immutable",
		"where status = 'ready'",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("general Scene atomic migration is missing %q", required)
		}
	}
	if !strings.Contains(
		strings.ToLower(string(down)),
		"drop table if exists evaluation_general_scene_atomic_attempts",
	) {
		t.Fatal("general Scene atomic migration does not drop its table")
	}
}
