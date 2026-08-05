package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPreparationResumeRevisionMigrationRejectsLegacyReferenceData(
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
	if err := runner.migrate.Steps(67); err != nil {
		t.Fatalf("apply migrations through version 67: %v", err)
	}

	scoped, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect to version 67 schema: %v", err)
	}
	t.Cleanup(func() {
		if err := scoped.Close(context.Background()); err != nil {
			t.Errorf("close version 67 schema connection: %v", err)
		}
	})
	const ownerID = "10000000-0000-4000-8000-000000000383"
	if _, err := scoped.Exec(context.Background(), `
		INSERT INTO identity_users (id, canonical_email)
		VALUES ($1, 'preparation-resume-migration@example.com')
	`, ownerID); err != nil {
		t.Fatalf("seed migration owner: %v", err)
	}
	if _, err := scoped.Exec(context.Background(), `
		INSERT INTO preparation_profiles (
			owner_user_id, profile_id, resume_ref, background_summary
		) VALUES ($1, 'legacy-profile', 'legacy-resume-body', 'Background')
	`, ownerID); err != nil {
		t.Fatalf("seed legacy Resume reference: %v", err)
	}

	err = runner.migrate.Steps(1)
	if err == nil || !strings.Contains(
		err.Error(),
		"recreate the development or test database",
	) {
		t.Fatalf("legacy Resume migration error = %v", err)
	}

	var oldColumn, newColumn bool
	if err := admin.QueryRow(context.Background(), `
		SELECT
			EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1
				  AND table_name = 'preparation_profiles'
				  AND column_name = 'resume_ref'
			),
			EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1
				  AND table_name = 'preparation_profiles'
				  AND column_name = 'resume_id'
			)
	`, schema).Scan(&oldColumn, &newColumn); err != nil {
		t.Fatalf("inspect rejected migration schema: %v", err)
	}
	if !oldColumn || newColumn {
		t.Fatalf(
			"schema after rejection: resume_ref=%t resume_id=%t",
			oldColumn,
			newColumn,
		)
	}
}
