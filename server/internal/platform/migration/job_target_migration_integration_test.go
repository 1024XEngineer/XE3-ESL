package migration

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"

	migrationfiles "github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestJobTargetMigrationPreservesPreparationDataAcrossUpAndDown(
	t *testing.T,
) {
	migrationConfig, _, schema := isolatedMigrationConfig(t)
	runner, err := openConfigWithFS(
		migrationConfig,
		jobTargetMigrationHistory(t),
	)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	if err := runner.migrate.Steps(14); err != nil {
		t.Fatalf("migrate to version 14: %v", err)
	}
	assertMigrationVersion(t, runner, 14)

	database, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect migrated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close migrated schema: %v", err)
		}
	})

	const userID = "10000000-0000-4000-8000-000000000119"
	seedStatements := []string{
		`INSERT INTO identity_users (id, canonical_email)
		 VALUES ($1, 'job-target-migration@example.test')`,
		`INSERT INTO preparation_profiles (
		     owner_user_id, profile_id, background_summary
		 ) VALUES ($1, 'profile-before-v15', 'Existing candidate background')`,
		`INSERT INTO preparation_snapshots (
		     owner_user_id, snapshot_id, source_profile_id,
		     source_version, background_snapshot
		 ) VALUES (
		     $1, 'snapshot-before-v15', 'profile-before-v15',
		     1, 'Existing candidate background'
		 )`,
	}
	for _, statement := range seedStatements {
		if _, err := database.Exec(
			context.Background(),
			statement,
			userID,
		); err != nil {
			t.Fatalf("seed version 14 preparation data: %v", err)
		}
	}

	if err := runner.migrate.Steps(1); err != nil {
		t.Fatalf("migrate from version 14 to 15: %v", err)
	}
	assertMigrationVersion(t, runner, 15)
	assertPreparationRows(t, database, userID)

	if changed, err := runner.DownOne(); err != nil || !changed {
		t.Fatalf(
			"migrate from version 15 to 14: changed=%t err=%v",
			changed,
			err,
		)
	}
	assertMigrationVersion(t, runner, 14)
	assertPreparationRows(t, database, userID)

	for _, table := range []string{
		"preparation_job_targets",
		"preparation_job_target_analysis_attempts",
		"preparation_job_target_confirmations",
		"preparation_job_target_idempotency_records",
	} {
		var relation *string
		if err := database.QueryRow(
			context.Background(),
			"SELECT to_regclass($1)",
			schema+"."+table,
		).Scan(&relation); err != nil {
			t.Fatalf("inspect %s after down migration: %v", table, err)
		}
		if relation != nil {
			t.Fatalf("%s still exists after v15 down as %q", table, *relation)
		}
	}
}

func jobTargetMigrationHistory(t *testing.T) fstest.MapFS {
	t.Helper()

	history := make(fstest.MapFS)
	for version := 1; version <= 13; version++ {
		for _, direction := range []string{"up", "down"} {
			name := fmt.Sprintf("%06d_*.%s.sql", version, direction)
			matches, err := fs.Glob(migrationfiles.Files, name)
			if err != nil || len(matches) != 1 {
				t.Fatalf(
					"find migration %d %s: matches=%v err=%v",
					version,
					direction,
					matches,
					err,
				)
			}
			content, err := fs.ReadFile(migrationfiles.Files, matches[0])
			if err != nil {
				t.Fatalf("read migration %s: %v", matches[0], err)
			}
			history[matches[0]] = &fstest.MapFile{Data: content}
		}
	}
	history["000014_existing_preparation_boundary.up.sql"] =
		&fstest.MapFile{Data: []byte("BEGIN; COMMIT;")}
	history["000014_existing_preparation_boundary.down.sql"] =
		&fstest.MapFile{Data: []byte("BEGIN; COMMIT;")}
	for _, direction := range []string{"up", "down"} {
		name := "000015_job_targets." + direction + ".sql"
		content, err := fs.ReadFile(migrationfiles.Files, name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		history[name] = &fstest.MapFile{Data: content}
	}
	return history
}

func assertMigrationVersion(t *testing.T, runner *Runner, want int) {
	t.Helper()

	status, err := runner.Version()
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if !status.Present || status.Dirty || status.Version != want {
		t.Fatalf("migration status = %+v, want clean version %d", status, want)
	}
}

func assertPreparationRows(
	t *testing.T,
	database *pgx.Conn,
	userID string,
) {
	t.Helper()

	var profileBackground string
	if err := database.QueryRow(
		context.Background(),
		`SELECT background_summary
		   FROM preparation_profiles
		  WHERE owner_user_id = $1
		    AND profile_id = 'profile-before-v15'`,
		userID,
	).Scan(&profileBackground); err != nil {
		t.Fatalf("read preserved preparation profile: %v", err)
	}
	if profileBackground != "Existing candidate background" {
		t.Fatalf("profile background = %q", profileBackground)
	}

	var snapshotBackground string
	if err := database.QueryRow(
		context.Background(),
		`SELECT background_snapshot
		   FROM preparation_snapshots
		  WHERE owner_user_id = $1
		    AND snapshot_id = 'snapshot-before-v15'`,
		userID,
	).Scan(&snapshotBackground); err != nil {
		t.Fatalf("read preserved preparation snapshot: %v", err)
	}
	if snapshotBackground != "Existing candidate background" {
		t.Fatalf("snapshot background = %q", snapshotBackground)
	}
}
