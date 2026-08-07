package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSessionHTTPRegistersOnlyPracticeSessionRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, err := NewHandler(contextHTTPApplicationStub{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
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
		"POST /v1/agent-threads/:thread_id/practice-start-confirmations",
	} {
		if _, exists := paths[forbidden]; exists {
			t.Fatalf("Practice registered Preparation route %q", forbidden)
		}
	}
	if _, exists := paths["POST /v1/practice-plans/:practice_plan_id/practice-sessions"]; !exists {
		t.Fatal("Practice Session create route missing")
	}
	if _, exists := paths["POST /v1/practice-sessions/:practice_session_id/complete"]; !exists {
		t.Fatal("Practice Session complete route missing")
	}
}

func TestSessionHTTPCompletesUserControlledSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotTransition practice.SessionTransition
	application := contextHTTPApplicationStub{
		transitionSession: func(
			_ context.Context,
			actor requestcontext.Actor,
			sessionID string,
			key string,
			expectedVersion int,
			transition practice.SessionTransition,
		) (practice.Session, bool, error) {
			if actor.UserID != "user-1" || sessionID != "session-1" ||
				key != "complete-session-0001" || expectedVersion != 8 {
				t.Fatal("TransitionSession received altered trusted input")
			}
			gotTransition = transition
			return practice.Session{
				ID: "session-1", Status: practice.SessionCompleted, Version: 9,
			}, false, nil
		},
	}
	response := serveContextHTTPRequest(
		t,
		contextHTTPRouter(t, application),
		http.MethodPost,
		"/v1/practice-sessions/session-1/complete",
		`{"expected_session_version":8}`,
		"complete-session-0001",
	)
	if response.Code != http.StatusOK ||
		gotTransition != practice.SessionComplete {
		t.Fatalf(
			"status=%d transition=%q body=%s",
			response.Code,
			gotTransition,
			response.Body.String(),
		)
	}
}

func TestSessionHTTPCreateSessionForwardsOnlyExecutablePlanInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var got practice.CreateSessionRequest
	application := contextHTTPApplicationStub{
		createSession: func(
			_ context.Context,
			actor requestcontext.Actor,
			planID string,
			key string,
			request practice.CreateSessionRequest,
		) (practice.SessionBootstrap, bool, error) {
			if actor.UserID != "user-1" || planID != "plan-1" ||
				key != "session-create-0001" {
				t.Fatal("CreateSession received altered trusted input")
			}
			got = request
			return practice.SessionBootstrap{
				Session: practice.Session{ID: "session-1"},
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

func TestSessionHTTPCreateSessionRejectsRemovedPlanSelectionFields(t *testing.T) {
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

func TestSessionHTTPMapsStaleExecutablePlanToVersionConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := contextHTTPApplicationStub{
		createSession: func(
			context.Context,
			requestcontext.Actor,
			string,
			string,
			practice.CreateSessionRequest,
		) (practice.SessionBootstrap, bool, error) {
			return practice.SessionBootstrap{}, false,
				practice.ErrConflict
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
	if code := decodeContextHTTPBody(t, response)["error"].(map[string]any)["code"]; code != "version_conflict" {
		t.Fatalf("error code = %v, want version_conflict", code)
	}
}

func TestSessionHTTPMapsExistingActiveSessionToDedicatedConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := contextHTTPApplicationStub{
		createSession: func(
			context.Context,
			requestcontext.Actor,
			string,
			string,
			practice.CreateSessionRequest,
		) (practice.SessionBootstrap, bool, error) {
			return practice.SessionBootstrap{}, false,
				practice.ErrActiveSessionConflict
		},
	}
	response := serveContextHTTPRequest(
		t,
		contextHTTPRouter(t, application),
		http.MethodPost,
		"/v1/practice-plans/plan-1/practice-sessions",
		`{"expected_plan_revision":2,"user_confirmed":true}`,
		"session-create-0005",
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if code := decodeContextHTTPBody(t, response)["error"].(map[string]any)["code"]; code != "active_session_conflict" {
		t.Fatalf("error code = %v, want active_session_conflict", code)
	}
}

type contextHTTPApplicationStub struct {
	createSession func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		practice.CreateSessionRequest,
	) (practice.SessionBootstrap, bool, error)
	transitionSession func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		int,
		practice.SessionTransition,
	) (practice.Session, bool, error)
}

func (s contextHTTPApplicationStub) CreateSession(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	key string,
	request practice.CreateSessionRequest,
) (practice.SessionBootstrap, bool, error) {
	if s.createSession == nil {
		return practice.SessionBootstrap{}, false,
			errors.New("unexpected CreateSession")
	}
	return s.createSession(ctx, actor, planID, key, request)
}

func (contextHTTPApplicationStub) GetSession(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Session, error) {
	return practice.Session{}, errors.New("unexpected GetSession")
}

func (contextHTTPApplicationStub) GetSessionSnapshot(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.SessionSnapshot, error) {
	return practice.SessionSnapshot{}, errors.New("unexpected GetSessionSnapshot")
}

func (s contextHTTPApplicationStub) TransitionSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	key string,
	expectedVersion int,
	transition practice.SessionTransition,
) (practice.Session, bool, error) {
	if s.transitionSession != nil {
		return s.transitionSession(
			ctx, actor, sessionID, key, expectedVersion, transition,
		)
	}
	return practice.Session{}, false,
		errors.New("unexpected TransitionSession")
}

func contextHTTPRouter(
	t *testing.T,
	application Application,
) *gin.Engine {
	t.Helper()
	handler, err := NewHandler(application)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
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
