package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type applicationStub struct {
	register    func(context.Context, string, string) (User, error)
	login       func(context.Context, string, string) (LoginResult, error)
	logout      func(context.Context, requestcontext.Actor) error
	currentUser func(context.Context, requestcontext.Actor) (User, error)
	revokeAll   func(context.Context, string, string) error
}

func (a applicationStub) Register(
	ctx context.Context,
	email string,
	password string,
) (User, error) {
	return a.register(ctx, email, password)
}

func (a applicationStub) Login(
	ctx context.Context,
	email string,
	password string,
) (LoginResult, error) {
	return a.login(ctx, email, password)
}

func (a applicationStub) Logout(
	ctx context.Context,
	actor requestcontext.Actor,
) error {
	return a.logout(ctx, actor)
}

func (a applicationStub) CurrentUser(
	ctx context.Context,
	actor requestcontext.Actor,
) (User, error) {
	return a.currentUser(ctx, actor)
}

func (a applicationStub) RevokeAllSessionsForUser(
	ctx context.Context,
	userID string,
	reason string,
) error {
	return a.revokeAll(ctx, userID, reason)
}

type authenticatorStub func(
	context.Context,
	string,
) (requestcontext.Actor, error)

func (a authenticatorStub) AuthenticateSession(
	ctx context.Context,
	token string,
) (requestcontext.Actor, error) {
	return a(ctx, token)
}

type allowLimiter struct{}

func (allowLimiter) Allow(string) RateLimitDecision {
	return RateLimitDecision{Allowed: true}
}

type denyLimiter struct {
	retryAfter time.Duration
}

func (l denyLimiter) Allow(string) RateLimitDecision {
	return RateLimitDecision{RetryAfter: l.retryAfter}
}

func TestIdentityHTTPContract(t *testing.T) {
	expiresAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	app := completeApplicationStub()
	app.register = func(
		_ context.Context,
		email string,
		password string,
	) (User, error) {
		if email != "Learner@Example.com" ||
			password != "correct horse battery staple" {
			t.Fatalf("unexpected registration input")
		}
		return User{ID: "user-1", Email: "learner@example.com"}, nil
	}
	app.login = func(context.Context, string, string) (LoginResult, error) {
		return LoginResult{
			User:      User{ID: "user-1", Email: "learner@example.com"},
			Token:     "sess_secret",
			ExpiresAt: expiresAt,
		}, nil
	}
	app.currentUser = func(
		_ context.Context,
		got requestcontext.Actor,
	) (User, error) {
		if got != actor {
			t.Fatalf("unexpected actor: %#v", got)
		}
		return User{ID: actor.UserID, Email: "learner@example.com"}, nil
	}
	app.logout = func(
		_ context.Context,
		got requestcontext.Actor,
	) error {
		if got != actor {
			t.Fatalf("unexpected actor: %#v", got)
		}
		return nil
	}
	router := newTestRouter(t, app, authenticatorStub(func(
		_ context.Context,
		token string,
	) (requestcontext.Actor, error) {
		if token != "sess_secret" {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}
		return actor, nil
	}), defaultTestRateLimits())

	register := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/register",
		`{"email":"Learner@Example.com","password":"correct horse battery staple"}`,
		"",
	)
	assertStatusAndJSON(
		t,
		register,
		http.StatusCreated,
		`{"email":"learner@example.com","user_id":"user-1"}`,
	)

	login := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/login",
		`{"email":"Learner@Example.com","password":"correct horse battery staple"}`,
		"",
	)
	assertStatusAndJSON(
		t,
		login,
		http.StatusOK,
		`{"expires_at":"2026-08-23T12:00:00Z","session_token":"sess_secret","token_type":"Bearer","user":{"email":"learner@example.com","user_id":"user-1"}}`,
	)
	if login.Header().Get("Cache-Control") != "no-store" ||
		login.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("login response is cacheable: %#v", login.Header())
	}

	me := performRequest(
		router,
		http.MethodGet,
		"/v1/me",
		"",
		"Bearer sess_secret",
	)
	assertStatusAndJSON(
		t,
		me,
		http.StatusOK,
		`{"email":"learner@example.com","user_id":"user-1"}`,
	)
	if strings.Contains(me.Body.String(), "sess_secret") {
		t.Fatal("/v1/me leaked the session token")
	}

	logout := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/logout",
		"",
		"Bearer sess_secret",
	)
	if logout.Code != http.StatusNoContent || logout.Body.Len() != 0 {
		t.Fatalf("unexpected logout response: %d %q", logout.Code, logout.Body)
	}
}

func TestAuthenticationMiddlewareHasStableFailure(t *testing.T) {
	router := newTestRouter(
		t,
		completeApplicationStub(),
		authenticatorStub(func(
			context.Context,
			string,
		) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
	)

	for _, authorization := range []string{
		"",
		"Basic secret",
		"Bearer",
		"Bearer unknown",
	} {
		response := performRequest(
			router,
			http.MethodGet,
			"/v1/me",
			"",
			authorization,
		)
		if response.Code != http.StatusUnauthorized ||
			response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf(
				"unexpected auth response for %q: %d %#v",
				authorization,
				response.Code,
				response.Header(),
			)
		}
		assertErrorCode(t, response, "authentication_required")
	}
}

func TestIdentityRequestsRejectClientControlledActor(t *testing.T) {
	app := completeApplicationStub()
	app.register = func(context.Context, string, string) (User, error) {
		t.Fatal("application must not receive request with forged actor")
		return User{}, nil
	}
	router := newTestRouter(
		t,
		app,
		authenticatorStub(func(
			context.Context,
			string,
		) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
	)
	response := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/register",
		`{"email":"learner@example.com","password":"correct horse battery staple","user_id":"forged"}`,
		"",
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	assertErrorCode(t, response, "invalid_request")
}

func TestIdentityRequestsRejectInvalidUTF8WithoutChangingPassword(t *testing.T) {
	app := completeApplicationStub()
	app.register = func(context.Context, string, string) (User, error) {
		t.Fatal("application must not receive rewritten invalid UTF-8")
		return User{}, nil
	}
	router := newTestRouter(
		t,
		app,
		authenticatorStub(func(
			context.Context,
			string,
		) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
	)
	raw := append(
		[]byte(`{"email":"learner@example.com","password":"correct horse `),
		0xff,
	)
	raw = append(raw, []byte(` battery staple"}`)...)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/register",
		bytes.NewReader(raw),
	)
	request.RemoteAddr = "192.0.2.1:12345"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	assertErrorCode(t, response, "invalid_request")
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	app := completeApplicationStub()
	app.login = func(context.Context, string, string) (LoginResult, error) {
		return LoginResult{}, ErrInvalidCredentials
	}
	router := newTestRouter(
		t,
		app,
		authenticatorStub(func(
			context.Context,
			string,
		) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
	)
	response := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/login",
		`{"email":"unknown@example.com","password":"correct horse battery staple"}`,
		"",
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	assertErrorCode(t, response, "invalid_credentials")
	if response.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("invalid_credentials must not masquerade as missing authentication")
	}
}

func TestRateLimitIncludesDeterministicRetryAfter(t *testing.T) {
	limits := defaultTestRateLimits()
	limits.LoginIP = denyLimiter{retryAfter: 1500 * time.Millisecond}
	app := completeApplicationStub()
	app.login = func(context.Context, string, string) (LoginResult, error) {
		t.Fatal("limited request reached application")
		return LoginResult{}, nil
	}
	router := newTestRouter(
		t,
		app,
		authenticatorStub(func(
			context.Context,
			string,
		) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, nil
		}),
		limits,
	)
	response := performRequest(
		router,
		http.MethodPost,
		"/v1/auth/login",
		`{"email":"learner@example.com","password":"correct horse battery staple"}`,
		"",
	)
	if response.Code != http.StatusTooManyRequests ||
		response.Header().Get("Retry-After") != "2" {
		t.Fatalf("unexpected limited response: %d %#v", response.Code, response.Header())
	}
	assertErrorCode(t, response, "rate_limited")
}

func TestAuthenticationInternalErrorIsSanitized(t *testing.T) {
	const sensitive = "postgres://user:password@internal/database"
	router := newTestRouter(
		t,
		completeApplicationStub(),
		authenticatorStub(func(
			context.Context,
			string,
		) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, errors.New(sensitive)
		}),
		defaultTestRateLimits(),
	)
	response := performRequest(
		router,
		http.MethodGet,
		"/v1/me",
		"",
		"Bearer sess_secret",
	)
	if response.Code != http.StatusInternalServerError ||
		strings.Contains(response.Body.String(), sensitive) {
		t.Fatalf("unsafe response: %d %q", response.Code, response.Body)
	}
	assertErrorCode(t, response, "internal_error")
}

func newTestRouter(
	t *testing.T,
	app Application,
	authenticator Authenticator,
	limits RateLimiters,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewHTTPHandler(
		app,
		authenticator,
		limits,
		func() string { return "corr_test" },
	)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}

func defaultTestRateLimits() RateLimiters {
	return RateLimiters{
		RegistrationIP: allowLimiter{},
		LoginIP:        allowLimiter{},
		LoginAccount:   allowLimiter{},
	}
}

func performRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	authorization string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.1:12345"
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertStatusAndJSON(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	want string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(response.Body.Bytes(), &gotValue); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected response: %v", err)
	}
	gotJSON, _ := json.Marshal(gotValue)
	wantJSON, _ := json.Marshal(wantValue)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("response = %s, want %s", gotJSON, wantJSON)
	}
}

func assertErrorCode(
	t *testing.T,
	response *httptest.ResponseRecorder,
	want string,
) {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != want {
		t.Fatalf("error code = %q, want %q", body.Error.Code, want)
	}
}

func completeApplicationStub() applicationStub {
	return applicationStub{
		register: func(context.Context, string, string) (User, error) {
			return User{}, nil
		},
		login: func(context.Context, string, string) (LoginResult, error) {
			return LoginResult{}, nil
		},
		logout: func(context.Context, requestcontext.Actor) error {
			return nil
		},
		currentUser: func(
			context.Context,
			requestcontext.Actor,
		) (User, error) {
			return User{}, nil
		},
		revokeAll: func(context.Context, string, string) error {
			return nil
		},
	}
}
