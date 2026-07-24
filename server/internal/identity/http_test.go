package identity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	expiresAt := time.Date(
		2026,
		8,
		23,
		12,
		0,
		0,
		123456000,
		time.UTC,
	)
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
		`{"expires_at":"2026-08-23T12:00:00.123456Z","session_token":"sess_secret","token_type":"Bearer","user":{"email":"learner@example.com","user_id":"user-1"}}`,
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
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	assertErrorCode(t, response, "invalid_request")
}

func TestIdentityRequestsRejectUnpairedJSONSurrogates(t *testing.T) {
	app := completeApplicationStub()
	app.register = func(context.Context, string, string) (User, error) {
		t.Fatal("register must not receive an unpaired surrogate")
		return User{}, nil
	}
	app.login = func(context.Context, string, string) (LoginResult, error) {
		t.Fatal("login must not receive an unpaired surrogate")
		return LoginResult{}, nil
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
	tests := []struct {
		name     string
		endpoint string
		body     string
	}{
		{
			name:     "register lone high surrogate",
			endpoint: "/v1/auth/register",
			body:     `{"email":"learner@example.com","password":"12345678901234\ud800"}`,
		},
		{
			name:     "login lone low surrogate",
			endpoint: "/v1/auth/login",
			body:     `{"email":"learner@example.com","password":"12345678901234\udc00"}`,
		},
		{
			name:     "login mismatched pair",
			endpoint: "/v1/auth/login",
			body:     `{"email":"learner@example.com","password":"12345678901234\ud800\u0041"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(
				router,
				http.MethodPost,
				test.endpoint,
				test.body,
				"",
			)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: %d", response.Code)
			}
			assertErrorCode(t, response, "invalid_request")
		})
	}
}

func TestIdentityRequestsPreserveValidJSONSurrogatePairs(t *testing.T) {
	const password = "12345678901234😀"
	app := completeApplicationStub()
	app.register = func(
		_ context.Context,
		_ string,
		gotPassword string,
	) (User, error) {
		if gotPassword != password {
			t.Fatalf("register password = %q, want %q", gotPassword, password)
		}
		return User{ID: "user-1", Email: "learner@example.com"}, nil
	}
	app.login = func(
		_ context.Context,
		_ string,
		gotPassword string,
	) (LoginResult, error) {
		if gotPassword != password {
			t.Fatalf("login password = %q, want %q", gotPassword, password)
		}
		return LoginResult{
			User:      User{ID: "user-1", Email: "learner@example.com"},
			Token:     "sess_secret",
			ExpiresAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
		}, nil
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
	body := `{"email":"learner@example.com","password":"12345678901234\ud83d\ude00"}`
	for _, test := range []struct {
		endpoint string
		status   int
	}{
		{"/v1/auth/register", http.StatusCreated},
		{"/v1/auth/login", http.StatusOK},
	} {
		response := performRequest(
			router,
			http.MethodPost,
			test.endpoint,
			body,
			"",
		)
		if response.Code != test.status {
			t.Fatalf(
				"%s status = %d, body = %s",
				test.endpoint,
				response.Code,
				response.Body,
			)
		}
	}
}

func TestJSONSurrogateValidationDoesNotRejectLiteralReplacementCharacter(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"password":"12345678901234\ufffd"}`),
		[]byte(`{"password":"12345678901234�"}`),
		[]byte(`{"password":"12345678901234\\ud800"}`),
	} {
		if !validJSONSurrogates(raw) {
			t.Fatalf("valid JSON string rejected: %s", raw)
		}
	}
}

func TestIdentityCredentialsRequireExactUnambiguousJSON(t *testing.T) {
	app := completeApplicationStub()
	app.register = func(context.Context, string, string) (User, error) {
		t.Fatal("ambiguous register request reached application")
		return User{}, nil
	}
	app.login = func(context.Context, string, string) (LoginResult, error) {
		t.Fatal("ambiguous login request reached application")
		return LoginResult{}, nil
	}
	router := newTestRouter(
		t,
		app,
		authenticatorStub(func(context.Context, string) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
	)
	bodies := []string{
		`{"email":"first@example.com","email":"second@example.com","password":"correct horse battery staple"}`,
		`{"Email":"learner@example.com","password":"correct horse battery staple"}`,
		`{"email":"learner@example.com","Password":"correct horse battery staple"}`,
	}
	for _, endpoint := range []string{"/v1/auth/register", "/v1/auth/login"} {
		for _, body := range bodies {
			response := performRequest(router, http.MethodPost, endpoint, body, "")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("%s accepted %s: %d", endpoint, body, response.Code)
			}
			assertErrorCode(t, response, "invalid_request")
		}
	}
}

func TestIdentityCredentialsRequireApplicationJSON(t *testing.T) {
	router := newTestRouter(
		t,
		completeApplicationStub(),
		authenticatorStub(func(context.Context, string) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
	)
	body := `{"email":"learner@example.com","password":"correct horse battery staple"}`
	tests := []struct {
		name        string
		contentType string
		want        int
	}{
		{name: "missing", contentType: "", want: http.StatusBadRequest},
		{name: "text", contentType: "text/plain", want: http.StatusBadRequest},
		{name: "invalid charset", contentType: "application/json; charset=iso-8859-1", want: http.StatusBadRequest},
		{name: "utf8 charset", contentType: "application/json; charset=UTF-8", want: http.StatusCreated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/auth/register",
				strings.NewReader(body),
			)
			request.RemoteAddr = "192.0.2.1:12345"
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body)
			}
		})
	}
}

func TestIdentityBodyReadDeadlineRejectsSlowDrip(t *testing.T) {
	app := completeApplicationStub()
	app.register = func(context.Context, string, string) (User, error) {
		t.Fatal("timed-out body reached application")
		return User{}, nil
	}
	router := newTestRouterWithOptions(
		t,
		app,
		authenticatorStub(func(context.Context, string) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
		WithBodyReadTimeout(30*time.Millisecond),
	)
	server := httptest.NewServer(router)
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "http://")
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(
		connection,
		"POST /v1/auth/register HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"email\":",
		address,
	); err != nil {
		t.Fatalf("write slow request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatalf("read timeout response: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("slow body status = %d", response.StatusCode)
	}
}

func TestPasswordAdmissionFailureHasStablePublicBoundary(t *testing.T) {
	app := completeApplicationStub()
	app.register = func(context.Context, string, string) (User, error) {
		return User{}, ErrPasswordUnavailable
	}
	app.login = func(context.Context, string, string) (LoginResult, error) {
		return LoginResult{}, ErrPasswordUnavailable
	}
	router := newTestRouter(
		t,
		app,
		authenticatorStub(func(context.Context, string) (requestcontext.Actor, error) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}),
		defaultTestRateLimits(),
	)
	for _, endpoint := range []string{"/v1/auth/register", "/v1/auth/login"} {
		response := performRequest(
			router,
			http.MethodPost,
			endpoint,
			`{"email":"learner@example.com","password":"correct horse battery staple"}`,
			"",
		)
		if response.Code != http.StatusTooManyRequests ||
			response.Header().Get("Retry-After") != "1" {
			t.Fatalf("%s unavailable response = %d %#v", endpoint, response.Code, response.Header())
		}
		assertErrorCode(t, response, "rate_limited")
	}
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
	return newTestRouterWithOptions(t, app, authenticator, limits)
}

func newTestRouterWithOptions(
	t *testing.T,
	app Application,
	authenticator Authenticator,
	limits RateLimiters,
	options ...HTTPOption,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewHTTPHandler(
		app,
		authenticator,
		limits,
		func() string { return "corr_test" },
		options...,
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
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
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
