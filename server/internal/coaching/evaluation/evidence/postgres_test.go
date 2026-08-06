package evidence

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testOwnerA        = "10000000-0000-4000-8000-000000000001"
	integrationOwnerB = "20000000-0000-4000-8000-000000000002"
)

var evidenceSchemaSequence atomic.Uint64

func evaluationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL admin pool: %v", err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf(
		"evaluation_evidence_%d_%d",
		time.Now().UnixNano(),
		evidenceSchemaSequence.Add(1),
	)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create Evidence test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		if _, err := admin.Exec(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+schema+" CASCADE",
		); err != nil {
			t.Errorf("drop Evidence test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open isolated Evidence pool: %v", err)
	}
	t.Cleanup(pool.Close)
	upMigrations, err := fs.Glob(migrations.Files, "*.up.sql")
	if err != nil {
		t.Fatalf("enumerate migrations: %v", err)
	}
	for _, migration := range upMigrations {
		up, readErr := migrations.Files.ReadFile(migration)
		if readErr != nil {
			t.Fatalf("read %s: %v", migration, readErr)
		}
		if _, applyErr := pool.Exec(ctx, string(up)); applyErr != nil {
			var databaseError *pgconn.PgError
			if errors.As(applyErr, &databaseError) {
				t.Fatalf(
					"apply %s: %v position=%d internal_position=%d where=%s",
					migration,
					applyErr,
					databaseError.Position,
					databaseError.InternalPosition,
					databaseError.Where,
				)
			}
			t.Fatalf("apply %s: %v", migration, applyErr)
		}
	}
	return pool
}

func insertEvaluationUsers(
	t *testing.T,
	pool *pgxpool.Pool,
	userIDs ...string,
) {
	t.Helper()
	for _, userID := range userIDs {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO identity_users (id, canonical_email)
			VALUES ($1, $2)
		`, userID, "evaluation-"+userID+"@example.test"); err != nil {
			t.Fatalf("insert Identity user %s: %v", userID, err)
		}
	}
}

func postgresCode(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code
	}
	return ""
}
