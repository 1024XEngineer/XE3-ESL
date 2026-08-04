package voice

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
)

func TestPostgresSceneCatalogReferencesRegisteredTurnPolicies(t *testing.T) {
	pool := newTurnPolicyCatalogPool(t)
	catalog, err := scene.NewPostgresCatalog(pool)
	if err != nil {
		t.Fatalf("NewPostgresCatalog: %v", err)
	}
	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes: %v", err)
	}
	if len(definitions) == 0 {
		t.Fatal("active Scene catalog is empty")
	}

	checked := make(map[string]struct{})
	for _, definition := range definitions {
		reference := definition.TurnPolicyRef
		if _, found := checked[reference]; found {
			continue
		}
		checked[reference] = struct{}{}
		if _, err := resolveTurnPolicy(reference); err != nil {
			t.Errorf(
				"Scene %q references unsupported turn policy %q: %v",
				definition.ID,
				reference,
				err,
			)
		}
	}
}

func newTurnPolicyCatalogPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	rawURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if rawURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	schema := fmt.Sprintf(
		"voice_turn_policy_%d_%d",
		os.Getpid(),
		time.Now().UnixNano(),
	)
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
	t.Cleanup(func() {
		admin, err := pgx.Connect(context.Background(), rawURL)
		if err != nil {
			return
		}
		defer admin.Close(context.Background())
		_, _ = admin.Exec(
			context.Background(),
			"DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE",
		)
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	databaseURL := parsed.String()
	runner, err := migration.Open(databaseURL)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	changed, err := runner.Up()
	if err != nil {
		_ = runner.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	if !changed {
		_ = runner.Close()
		t.Fatal("empty database reported no migration")
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("close migration runner: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
