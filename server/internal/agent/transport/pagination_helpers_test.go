package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentstore "github.com/1024XEngineer/XE3-ESL/server/internal/agent/store"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	agentTestUserA = "10000000-0000-4000-8000-000000000001"
	agentTestUserB = "10000000-0000-4000-8000-000000000002"
)

type agentTestDatabase struct {
	pool      *pgxpool.Pool
	scopedURL string
}

func newAgentTestDatabase(t *testing.T) agentTestDatabase {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	admin, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatal("connect to TEST_DATABASE_URL")
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "agent_data_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+identifier,
	); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})

	scopedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	query := scopedURL.Query()
	query.Set("search_path", schema)
	scopedURL.RawQuery = query.Encode()

	runner, err := migration.Open(scopedURL.String())
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})
	if _, err := runner.Up(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(scopedURL.String())
	if err != nil {
		t.Fatal("parse scoped pool config")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal("open scoped pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal("ping scoped pool")
	}
	for _, user := range []struct {
		id    string
		email string
	}{
		{id: agentTestUserA, email: "agent-a@example.com"},
		{id: agentTestUserB, email: "agent-b@example.com"},
	} {
		if _, err := pool.Exec(
			context.Background(),
			`INSERT INTO identity_users (id, canonical_email)
VALUES ($1, $2)`,
			user.id,
			user.email,
		); err != nil {
			t.Fatalf("insert identity user: %v", err)
		}
	}
	return agentTestDatabase{
		pool:      pool,
		scopedURL: scopedURL.String(),
	}
}

func (database agentTestDatabase) reopen(t *testing.T) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(database.scopedURL)
	if err != nil {
		t.Fatal("parse reopened pool config")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal("reopen scoped pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal("ping reopened pool")
	}
	return pool
}

func newAgentDataServices(
	t *testing.T,
	pool *pgxpool.Pool,
) (*matter.Service, *agentconversation.Service) {
	t.Helper()
	ids := identity.NewUUIDv4Generator(nil)
	matterRepository, err := matter.NewPostgresRepository(pool, ids)
	if err != nil {
		t.Fatalf("new Matter repository: %v", err)
	}
	matterService, err := matter.NewService(matterRepository)
	if err != nil {
		t.Fatalf("new Matter service: %v", err)
	}
	repository, err := agentstore.NewPostgresStore(pool, ids)
	if err != nil {
		t.Fatalf("new Agent repository: %v", err)
	}
	service, err := agentconversation.NewService(repository, matterService)
	if err != nil {
		t.Fatalf("new Agent service: %v", err)
	}
	return matterService, service
}

type authenticatorFunc func(
	context.Context,
	string,
) (requestcontext.Actor, error)

func (f authenticatorFunc) AuthenticateSession(
	ctx context.Context,
	token string,
) (requestcontext.Actor, error) {
	return f(ctx, token)
}

func performAgentRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func encodeMessagePageCursor(cursor agentconversation.MessagePageCursor) (string, error) {
	return agentconversation.EncodeMessagePageCursor(cursor)
}
