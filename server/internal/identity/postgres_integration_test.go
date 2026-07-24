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
	"runtime"
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

const integrationPasswordHash = "$argon2id$v=19$m=8,t=1,p=1$MDEyMzQ1Njc4OWFiY2RlZg$MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"

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
	dummyHash, err := passwords.Hash(
		context.Background(),
		"unknown account timing password",
	)
	if err != nil {
		t.Fatalf("create dummy hash: %v", err)
	}
	tokens := NewOpaqueSessionTokens(nil)
	service, err := NewService(
		repository,
		passwords,
		tokens,
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
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE identity_auth_sessions
SET created_at = CURRENT_TIMESTAMP - INTERVAL '2 hours',
    expires_at = CURRENT_TIMESTAMP - INTERVAL '1 second'
WHERE token_digest = $1`,
		tokens.Digest(expiringLogin.Token),
	); err != nil {
		t.Fatalf("expire token with database time: %v", err)
	}
	if _, err := service.AuthenticateSession(
		context.Background(),
		expiringLogin.Token,
	); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("expired token authentication = %v", err)
	}

	testConcurrentPostgresRegistration(t, service)
	testConcurrentPostgresLogin(t, service)
	testPostgresHTTPVerticalSlice(t, service)
}

func TestPostgresCreateSessionRejectsSecurityEventInterleaving(t *testing.T) {
	pool := identityTestDatabase(t)
	repository, err := NewPostgresRepository(pool, NewUUIDv4Generator(nil))
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	user, err := repository.CreateUserWithCredential(
		context.Background(),
		"interleave@example.com",
		integrationPasswordHash,
	)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	credential, err := repository.FindCredentialByEmail(
		context.Background(),
		user.Email,
	)
	if err != nil {
		t.Fatalf("find credential: %v", err)
	}

	blocker, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(
		context.Background(),
		"SELECT id FROM identity_users WHERE id = $1 FOR UPDATE",
		user.ID,
	); err != nil {
		t.Fatalf("lock user: %v", err)
	}

	revokeDone := make(chan error, 1)
	go func() {
		revokeDone <- repository.RevokeAllSessionsForUser(
			context.Background(),
			user.ID,
			"security_event",
		)
	}()
	waitForBlockedPostgresQuery(t, pool, "SELECT id::text\nFROM identity_users")

	sessionDone := make(chan error, 1)
	go func() {
		_, err := repository.CreateSession(
			context.Background(),
			CreateSessionParams{
				UserID:              user.ID,
				TokenDigest:         bytes.Repeat([]byte{1}, 32),
				CredentialUpdatedAt: credential.UpdatedAt,
				Lifetime:            time.Hour,
				PreviousHash:        credential.PasswordHash,
			},
		)
		sessionDone <- err
	}()
	waitForBlockedPostgresQuery(t, pool, "SELECT account_status\nFROM identity_users")

	if err := blocker.Commit(context.Background()); err != nil {
		t.Fatalf("release blocker: %v", err)
	}
	if err := <-revokeDone; err != nil {
		t.Fatalf("revoke security event: %v", err)
	}
	if err := <-sessionDone; !errors.Is(err, ErrAuthenticationChanged) {
		t.Fatalf("stale login session result = %v", err)
	}
	var sessions int
	if err := pool.QueryRow(
		context.Background(),
		"SELECT count(*) FROM identity_auth_sessions WHERE user_id = $1",
		user.ID,
	).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("security event admitted %d stale sessions", sessions)
	}
}

func TestPostgresSessionUsesDatabaseTimeAndCannotRevive(t *testing.T) {
	pool := identityTestDatabase(t)
	repository, err := NewPostgresRepository(pool, NewUUIDv4Generator(nil))
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	user, err := repository.CreateUserWithCredential(
		context.Background(),
		"database-time@example.com",
		integrationPasswordHash,
	)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	credential, err := repository.FindCredentialByEmail(context.Background(), user.Email)
	if err != nil {
		t.Fatalf("find credential: %v", err)
	}
	const lifetime = 90 * time.Minute
	digest := bytes.Repeat([]byte{2}, 32)
	session, err := repository.CreateSession(
		context.Background(),
		CreateSessionParams{
			UserID:              user.ID,
			TokenDigest:         digest,
			CredentialUpdatedAt: credential.UpdatedAt,
			Lifetime:            lifetime,
			PreviousHash:        credential.PasswordHash,
		},
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	var createdAt, storedExpiresAt, databaseNow time.Time
	if err := pool.QueryRow(
		context.Background(),
		`SELECT created_at, expires_at, CURRENT_TIMESTAMP
FROM identity_auth_sessions
WHERE id = $1`,
		session.ID,
	).Scan(&createdAt, &storedExpiresAt, &databaseNow); err != nil {
		t.Fatalf("read database times: %v", err)
	}
	if !session.ExpiresAt.Equal(storedExpiresAt) ||
		storedExpiresAt.Sub(createdAt) != lifetime ||
		createdAt.After(databaseNow) {
		t.Fatalf(
			"inconsistent database time: created=%v returned=%v stored=%v now=%v",
			createdAt,
			session.ExpiresAt,
			storedExpiresAt,
			databaseNow,
		)
	}

	if _, err := pool.Exec(
		context.Background(),
		`UPDATE identity_auth_sessions
SET created_at = CURRENT_TIMESTAMP + INTERVAL '1 hour',
    expires_at = CURRENT_TIMESTAMP + INTERVAL '2 hours'
WHERE id = $1`,
		session.ID,
	); err != nil {
		t.Fatalf("simulate process clock skew: %v", err)
	}
	if _, err := repository.FindSessionByTokenDigest(
		context.Background(),
		digest,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("future-created session became valid: %v", err)
	}
	if err := repository.RevokeSession(
		context.Background(),
		user.ID,
		session.ID,
		"logout",
	); err != nil {
		t.Fatalf("revoke skewed session: %v", err)
	}
	var revokedAt time.Time
	if err := pool.QueryRow(
		context.Background(),
		"SELECT created_at, revoked_at FROM identity_auth_sessions WHERE id = $1",
		session.ID,
	).Scan(&createdAt, &revokedAt); err != nil {
		t.Fatalf("read revocation time: %v", err)
	}
	if revokedAt.Before(createdAt) {
		t.Fatalf("revoked_at %v precedes created_at %v", revokedAt, createdAt)
	}
}

func TestPostgresCreateSessionRequiresCurrentActiveCredential(t *testing.T) {
	tests := []struct {
		name   string
		mutate string
	}{
		{
			name:   "account deleting",
			mutate: "UPDATE identity_users SET account_status = 'deleting' WHERE id = $1 AND $2 <> ''",
		},
		{
			name: "password rotated",
			mutate: `UPDATE identity_credentials
SET password_hash = $2,
    updated_at = GREATEST(CURRENT_TIMESTAMP, updated_at + INTERVAL '1 microsecond')
WHERE user_id = $1`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := identityTestDatabase(t)
			repository, err := NewPostgresRepository(pool, NewUUIDv4Generator(nil))
			if err != nil {
				t.Fatalf("new repository: %v", err)
			}
			user, err := repository.CreateUserWithCredential(
				context.Background(),
				strings.ReplaceAll(test.name, " ", "-")+"@example.com",
				integrationPasswordHash,
			)
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			credential, err := repository.FindCredentialByEmail(
				context.Background(),
				user.Email,
			)
			if err != nil {
				t.Fatalf("find credential: %v", err)
			}
			if _, err := pool.Exec(
				context.Background(),
				test.mutate,
				user.ID,
				integrationPasswordHash+"A",
			); err != nil {
				t.Fatalf("mutate authentication state: %v", err)
			}
			if _, err := repository.CreateSession(
				context.Background(),
				CreateSessionParams{
					UserID:              user.ID,
					TokenDigest:         bytes.Repeat([]byte{3}, 32),
					CredentialUpdatedAt: credential.UpdatedAt,
					Lifetime:            time.Hour,
					PreviousHash:        credential.PasswordHash,
				},
			); !errors.Is(err, ErrAuthenticationChanged) {
				t.Fatalf("stale authentication state = %v", err)
			}
		})
	}
}

func TestPostgresCreateUserRollsBackAllFailurePaths(t *testing.T) {
	tests := []struct {
		name    string
		install string
	}{
		{
			name: "second write",
			install: `
CREATE FUNCTION reject_credential_insert() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'credential failure'; END $$;
CREATE TRIGGER reject_credential
BEFORE INSERT ON identity_credentials
FOR EACH ROW EXECUTE FUNCTION reject_credential_insert()`,
		},
		{
			name: "commit",
			install: `
CREATE FUNCTION reject_user_commit() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'commit failure'; END $$;
CREATE CONSTRAINT TRIGGER reject_user
AFTER INSERT ON identity_users
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION reject_user_commit()`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := identityTestDatabase(t)
			if _, err := pool.Exec(context.Background(), test.install); err != nil {
				t.Fatalf("install failure: %v", err)
			}
			repository, err := NewPostgresRepository(pool, NewUUIDv4Generator(nil))
			if err != nil {
				t.Fatalf("new repository: %v", err)
			}
			if _, err := repository.CreateUserWithCredential(
				context.Background(),
				test.name+"@example.com",
				"stored-hash",
			); !errors.Is(err, ErrRepository) {
				t.Fatalf("create failure = %v", err)
			}
			assertNoIdentityRows(t, pool)
		})
	}
}

func TestPostgresCreateUserHonorsCanceledContext(t *testing.T) {
	pool := identityTestDatabase(t)
	repository, err := NewPostgresRepository(pool, NewUUIDv4Generator(nil))
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.CreateUserWithCredential(
		ctx,
		"canceled@example.com",
		"stored-hash",
	); !errors.Is(err, ErrRepository) {
		t.Fatalf("canceled create = %v", err)
	}
	assertNoIdentityRows(t, pool)
}

func waitForBlockedPostgresQuery(
	t *testing.T,
	pool *pgxpool.Pool,
	queryFragment string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		err := pool.QueryRow(
			context.Background(),
			`SELECT EXISTS (
    SELECT 1
    FROM pg_stat_activity
    WHERE datname = current_database()
      AND wait_event_type = 'Lock'
      AND query LIKE '%' || $1 || '%'
)`,
			queryFragment,
		).Scan(&blocked)
		if err != nil {
			t.Fatalf("inspect blocked query: %v", err)
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("query did not block deterministically: %q", queryFragment)
		}
		runtime.Gosched()
	}
}

func assertNoIdentityRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var users, credentials int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
    (SELECT count(*) FROM identity_users),
    (SELECT count(*) FROM identity_credentials)`,
	).Scan(&users, &credentials); err != nil {
		t.Fatalf("count identity rows: %v", err)
	}
	if users != 0 || credentials != 0 {
		t.Fatalf("partial transaction persisted: users=%d credentials=%d", users, credentials)
	}
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
) {
	t.Helper()
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
