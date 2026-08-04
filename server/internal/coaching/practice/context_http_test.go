package practice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestContextHTTPRegistersOnlyPracticeSessionRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, err := NewContextHTTPHandler(contextHTTPApplicationStub{})
	if err != nil {
		t.Fatalf("NewContextHTTPHandler: %v", err)
	}
	router := gin.New()
	handler.RegisterRoutes(router)
	paths := make(map[string]struct{})
	for _, route := range router.Routes() {
		paths[route.Method+" "+route.Path] = struct{}{}
	}
	for _, forbidden := range []string{
		"POST /v1/practice-plans",
		"GET /v1/practice-plans/:practice_plan_id",
		"PUT /v1/practice-plans/:practice_plan_id",
	} {
		if _, exists := paths[forbidden]; exists {
			t.Fatalf("Practice registered Preparation route %q", forbidden)
		}
	}
	if _, exists := paths["POST /v1/practice-plans/:practice_plan_id/practice-sessions"]; !exists {
		t.Fatal("Practice Session create route missing")
	}
}

func TestContextHTTPCreateSessionForwardsOnlyExecutablePlanInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var got CreateSessionRequest
	application := contextHTTPApplicationStub{
		createSession: func(
			_ context.Context,
			actor requestcontext.Actor,
			planID string,
			key string,
			request CreateSessionRequest,
		) (SessionBootstrap, bool, error) {
			if actor.UserID != "user-1" || planID != "plan-1" ||
				key != "session-create-0001" {
				t.Fatal("CreateSession received altered trusted input")
			}
			got = request
			return SessionBootstrap{
				Session: Session{ID: "session-1"},
			}, false, nil
		},
	}
	router := contextHTTPRouter(t, application)
	response := serveContextHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/practice-plans/plan-1/practice-sessions",
		`{"expected_plan_revision":3,"user_confirmed":true}`,
		"session-create-0001",
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got.ExpectedPlanRevision != 3 || !got.UserConfirmed {
		t.Fatalf("CreateSession request = %#v", got)
	}
}

func TestContextHTTPCreateSessionRejectsRemovedPlanSelectionFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := contextHTTPRouter(t, contextHTTPApplicationStub{})
	response := serveContextHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/practice-plans/plan-1/practice-sessions",
		`{"expected_plan_revision":3,"user_confirmed":true,"ielts_selection":{"mode":"PART_1","part_1_set_id":"set-1"}}`,
		"session-create-0002",
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestContextHTTPConfirmAndStartRequiresTrustedUserConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := contextHTTPRouter(t, contextHTTPApplicationStub{})
	response := serveContextHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/agent-threads/thread-1/practice-start-confirmations",
		`{"practice_plan_id":"plan-1","expected_plan_revision":3,"user_confirmed":false}`,
		"session-create-0003",
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestContextHTTPMapsStaleExecutablePlanToVersionConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := contextHTTPApplicationStub{
		createSession: func(
			context.Context,
			requestcontext.Actor,
			string,
			string,
			CreateSessionRequest,
		) (SessionBootstrap, bool, error) {
			return SessionBootstrap{}, false,
				ErrConflict
		},
	}
	router := contextHTTPRouter(t, application)
	response := serveContextHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/practice-plans/plan-1/practice-sessions",
		`{"expected_plan_revision":2,"user_confirmed":true}`,
		"session-create-0004",
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

type contextHTTPApplicationStub struct {
	createSession func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		CreateSessionRequest,
	) (SessionBootstrap, bool, error)
}

func (s contextHTTPApplicationStub) CreateSession(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	key string,
	request CreateSessionRequest,
) (SessionBootstrap, bool, error) {
	if s.createSession == nil {
		return SessionBootstrap{}, false,
			errors.New("unexpected CreateSession")
	}
	return s.createSession(ctx, actor, planID, key, request)
}

func (contextHTTPApplicationStub) ConfirmAndStartPractice(
	context.Context,
	requestcontext.Actor,
	string,
	StartConfirmation,
) (ConfirmAndStartResult, error) {
	return ConfirmAndStartResult{}, errors.New("unexpected ConfirmAndStartPractice")
}

func (contextHTTPApplicationStub) GetSession(
	context.Context,
	requestcontext.Actor,
	string,
) (Session, error) {
	return Session{}, errors.New("unexpected GetSession")
}

func (contextHTTPApplicationStub) GetSessionSnapshot(
	context.Context,
	requestcontext.Actor,
	string,
) (SessionSnapshot, error) {
	return SessionSnapshot{}, errors.New("unexpected GetSessionSnapshot")
}

func (contextHTTPApplicationStub) TransitionSession(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	int,
	SessionTransition,
) (Session, bool, error) {
	return Session{}, false,
		errors.New("unexpected TransitionSession")
}

func contextHTTPRouter(
	t *testing.T,
	application ContextHTTPApplication,
) *gin.Engine {
	t.Helper()
	handler, err := NewContextHTTPHandler(application)
	if err != nil {
		t.Fatalf("NewContextHTTPHandler: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(requestcontext.WithActor(
			c.Request.Context(),
			requestcontext.Actor{
				UserID: "user-1", SessionID: "auth-session-1",
			},
		))
		c.Next()
	})
	handler.RegisterRoutes(router)
	return router
}

func serveContextHTTPRequest(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
	body string,
	idempotencyKey string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeContextHTTPBody(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}
