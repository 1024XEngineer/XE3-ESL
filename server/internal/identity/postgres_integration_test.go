package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIdentityVerticalSlice(t *testing.T) {
	pool := identityTestDatabase(t)
	repository, err := NewPostgresRepository(
		pool,
		NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	integrationParams := testArgon2idParams
	integrationParams.SaltLength = 16
	passwords, err := NewArgon2idHasher(
		integrationParams,
		nil,
		2,
	)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	dummyHash, err := passwords.Hash("unknown account timing password")
	if err != nil {
		t.Fatalf("create dummy hash: %v", err)
	}
	clock := &mutableClock{
		now: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	}
	tokens := NewOpaqueSessionTokens(nil)
	service, err := NewService(
		repository,
		passwords,
		tokens,
		clock,
		dummyHash,
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	const password = "correct horse battery staple"
	user, err := service.Register(
		context.Background(),
		" Learner@Example.COM ",
		password,
	)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.Email != "learner@example.com" || user.Status != AccountActive {
		t.Fatalf("unexpected user: %#v", user)
	}
	assertDatabaseStoresNoRawCredential(t, pool, user.ID, password, "")

	for _, email := range []string{
		"unknown@example.com",
		"learner@example.com",
	} {
		loginPassword := password
		if email == "learner@example.com" {
			loginPassword = "incorrect password value"
		}
		if _, err := service.Login(
			context.Background(),
			email,
			loginPassword,
		); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("login failure for %q = %v", email, err)
		}
	}

	firstLogin, err := service.Login(
		context.Background(),
		"learner@example.com",
		password,
	)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	secondLogin, err := service.Login(
		context.Background(),
		"learner@example.com",
		password,
	)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if firstLogin.Token == secondLogin.Token ||
		!strings.HasPrefix(firstLogin.Token, "sess_") {
		t.Fatalf("invalid session tokens")
	}
	assertDatabaseStoresNoRawCredential(
		t,
		pool,
		user.ID,
		password,
		firstLogin.Token,
	)
	assertStoredTokenDigest(t, pool, tokens, firstLogin.Token)

	actor, err := service.AuthenticateSession(
		context.Background(),
		firstLogin.Token,
	)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if actor.UserID != user.ID || actor.SessionID == "" {
		t.Fatalf("unexpected actor: %#v", actor)
	}
	current, err := service.CurrentUser(context.Background(), actor)
	if err != nil || current.ID != user.ID {
		t.Fatalf("current user = %#v, %v", current, err)
	}

	if err := service.RevokeAllSessionsForUser(
		context.Background(),
		user.ID,
		"security_event",
	); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	for _, token := range []string{firstLogin.Token, secondLogin.Token} {
		if _, err := service.AuthenticateSession(
			context.Background(),
			token,
		); !errors.Is(err, ErrAuthenticationRequired) {
			t.Fatalf("revoked token authentication = %v", err)
		}
	}

	logoutLogin, err := service.Login(
		context.Background(),
		"learner@example.com",
		password,
	)
	if err != nil {
		t.Fatalf("logout login: %v", err)
	}
	logoutActor, err := service.AuthenticateSession(
		context.Background(),
		logoutLogin.Token,
	)
	if err != nil {
		t.Fatalf("authenticate logout session: %v", err)
	}
	for range 2 {
		if err := service.Logout(
			context.Background(),
			logoutActor,
		); err != nil {
			t.Fatalf("idempotent logout: %v", err)
		}
	}
	if _, err := service.AuthenticateSession(
		context.Background(),
		logoutLogin.Token,
	); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("logged-out token authentication = %v", err)
	}

	expiringLogin, err := service.Login(
		context.Background(),
		"learner@example.com",
		password,
	)
	if err != nil {
		t.Fatalf("expiring login: %v", err)
	}
	clock.now = clock.now.Add(sessionLifetime)
	if _, err := service.AuthenticateSession(
		context.Background(),
		expiringLogin.Token,
	); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("expired token authentication = %v", err)
	}

	testConcurrentPostgresRegistration(t, service)
	testConcurrentPostgresLogin(t, service, clock)
	testPostgresHTTPVerticalSlice(t, service)
}

func testConcurrentPostgresRegistration(t *testing.T, service *Service) {
	t.Helper()
	const attempts = 6
	var successes atomic.Int32
	var conflicts atomic.Int32
	var wait sync.WaitGroup
	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := service.Register(
				context.Background(),
				"concurrent@example.com",
				"another correct password value",
			)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrRegistrationUnavailable):
				conflicts.Add(1)
			default:
				t.Errorf("concurrent registration: %v", err)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || conflicts.Load() != attempts-1 {
		t.Fatalf(
			"concurrent registration successes/conflicts = %d/%d",
			successes.Load(),
			conflicts.Load(),
		)
	}
}

func testConcurrentPostgresLogin(
	t *testing.T,
	service *Service,
	clock *mutableClock,
) {
	t.Helper()
	clock.now = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	const attempts = 6
	tokens := make(chan string, attempts)
	var wait sync.WaitGroup
	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.Login(
				context.Background(),
				"learner@example.com",
				"correct horse battery staple",
			)
			if err != nil {
				t.Errorf("concurrent login: %v", err)
				return
			}
			tokens <- result.Token
		}()
	}
	wait.Wait()
	close(tokens)
	unique := make(map[string]struct{})
	for token := range tokens {
		unique[token] = struct{}{}
	}
	if len(unique) != attempts {
		t.Fatalf("unique concurrent tokens = %d, want %d", len(unique), attempts)
	}
}

func testPostgresHTTPVerticalSlice(t *testing.T, service *Service) {
	t.Helper()
	handler, err := NewHTTPHandler(
		service,
		service,
		defaultTestRateLimits(),
		func() string { return "corr_postgres_integration" },
	)
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	module, err := NewModule(handler)
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	router := newIntegrationRouter(module)

	register := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/register",
		`{"email":"api@example.com","password":"correct horse battery staple"}`,
		"",
	)
	if register.Code != http.StatusCreated ||
		strings.Contains(register.Body.String(), "session_token") {
		t.Fatalf("unexpected register response: %d %s", register.Code, register.Body)
	}

	login := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/login",
		`{"email":"api@example.com","password":"correct horse battery staple"}`,
		"",
	)
	if login.Code != http.StatusOK {
		t.Fatalf("unexpected login response: %d %s", login.Code, login.Body)
	}
	var loginBody struct {
		Token string `json:"session_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if !strings.HasPrefix(loginBody.Token, "sess_") {
		t.Fatalf("unexpected token: %q", loginBody.Token)
	}

	me := performRequest(
		router,
		http.MethodGet,
		"/v1/me",
		"",
		"Bearer "+loginBody.Token,
	)
	if me.Code != http.StatusOK ||
		strings.Contains(me.Body.String(), loginBody.Token) {
		t.Fatalf("unexpected me response: %d %s", me.Code, me.Body)
	}
	logout := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/logout",
		"",
		"Bearer "+loginBody.Token,
	)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("unexpected logout response: %d %s", logout.Code, logout.Body)
	}
	rejected := performRequest(
		router,
		http.MethodGet,
		"/v1/me",
		"",
		"Bearer "+loginBody.Token,
	)
	if rejected.Code != http.StatusUnauthorized ||
		rejected.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf(
			"unexpected revoked response: %d %s",
			rejected.Code,
			rejected.Body,
		)
	}
}

func assertDatabaseStoresNoRawCredential(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	rawPassword string,
	rawToken string,
) {
	t.Helper()
	var storedHash string
	if err := pool.QueryRow(
		context.Background(),
		"SELECT password_hash FROM identity_credentials WHERE user_id = $1",
		userID,
	).Scan(&storedHash); err != nil {
		t.Fatalf("read password hash: %v", err)
	}
	if storedHash == rawPassword ||
		!strings.HasPrefix(storedHash, "$argon2id$") ||
		strings.Contains(storedHash, rawPassword) {
		t.Fatal("database did not store only an Argon2id PHC hash")
	}
	if rawToken == "" {
		return
	}
	var rawTokenMatches int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*)
FROM identity_auth_sessions
WHERE encode(token_digest, 'escape') = $1`,
		rawToken,
	).Scan(&rawTokenMatches); err != nil {
		t.Fatalf("check raw token persistence: %v", err)
	}
	if rawTokenMatches != 0 {
		t.Fatal("database persisted a raw session token")
	}
}

func assertStoredTokenDigest(
	t *testing.T,
	pool *pgxpool.Pool,
	tokens SessionTokens,
	rawToken string,
) {
	t.Helper()
	var stored []byte
	if err := pool.QueryRow(
		context.Background(),
		"SELECT token_digest FROM identity_auth_sessions WHERE token_digest = $1",
		tokens.Digest(rawToken),
	).Scan(&stored); err != nil {
		t.Fatalf("read token digest: %v", err)
	}
	if !bytes.Equal(stored, tokens.Digest(rawToken)) || len(stored) != 32 {
		t.Fatalf("unexpected stored digest: %x", stored)
	}
}

func identityTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
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
	schema := "identity_repository_" + hex.EncodeToString(random)
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize(),
	); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(
			context.Background(),
			"DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE",
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
	return pool
}

func newIntegrationRouter(module *Module) http.Handler {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	module.RegisterRoutes(router)
	return router
}
