package postgres_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func ensureIdentityUsers(
	t *testing.T,
	pool *pgxpool.Pool,
	actors ...practice.Actor,
) {
	t.Helper()

	for index, actor := range actors {
		email := fmt.Sprintf(
			"practice-%s-%d@example.test",
			strings.ReplaceAll(actor.UserID, "-", ""),
			index,
		)
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO identity_users (id, canonical_email)
			VALUES ($1, $2)
			ON CONFLICT (id) DO NOTHING
		`, actor.UserID, email); err != nil {
			t.Fatalf("insert identity user %s: %v", actor.UserID, err)
		}
	}
}

func isolatedDatabaseURL(t *testing.T) (string, func()) {
	t.Helper()

	rawURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if rawURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	schema := fmt.Sprintf("practice_test_%d", os.Getpid())
	admin, err := pgx.Connect(context.Background(), rawURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize(),
	); err != nil {
		_ = admin.Close(context.Background())
		t.Fatalf("create isolated schema: %v", err)
	}
	if err := admin.Close(context.Background()); err != nil {
		t.Fatalf("close PostgreSQL admin connection: %v", err)
	}

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	cleanup := func() {
		admin, err := pgx.Connect(context.Background(), rawURL)
		if err != nil {
			return
		}
		defer admin.Close(context.Background())
		_, _ = admin.Exec(
			context.Background(),
			"DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE",
		)
	}
	return parsed.String(), cleanup
}
