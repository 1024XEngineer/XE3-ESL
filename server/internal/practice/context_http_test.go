package practice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

type contextHTTPApplicationStub struct {
	createPlan func(
		context.Context,
		requestcontext.Actor,
		string,
		CreatePlanRequest,
	) (persistence.Plan, bool, error)
	getPlan func(
		context.Context,
		requestcontext.Actor,
		string,
	) (persistence.Plan, error)
	createSession func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		CreateSessionRequest,
	) (persistence.ContextSessionBootstrap, bool, error)
	getSession func(
		context.Context,
		requestcontext.Actor,
		string,
	) (persistence.ContextSession, error)
	getSnapshot func(
		context.Context,
		requestcontext.Actor,
		string,
	) (persistence.ContextSessionSnapshot, error)
	transition func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		int,
		persistence.ContextSessionTransition,
	) (persistence.ContextSession, bool, error)
}

func (s contextHTTPApplicationStub) CreatePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	key string,
	request CreatePlanRequest,
) (persistence.Plan, bool, error) {
	if s.createPlan == nil {
		return persistence.Plan{}, false, errors.New("unexpected CreatePlan")
	}
	return s.createPlan(ctx, actor, key, request)
}

func (s contextHTTPApplicationStub) GetPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) (persistence.Plan, error) {
	if s.getPlan == nil {
		return persistence.Plan{}, errors.New("unexpected GetPlan")
	}
	return s.getPlan(ctx, actor, planID)
}

func (s contextHTTPApplicationStub) CreateSession(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	key string,
	request CreateSessionRequest,
) (persistence.ContextSessionBootstrap, bool, error) {
	if s.createSession == nil {
		return persistence.ContextSessionBootstrap{}, false,
			errors.New("unexpected CreateSession")
	}
	return s.createSession(ctx, actor, planID, key, request)
}

func (s contextHTTPApplicationStub) GetSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (persistence.ContextSession, error) {
	if s.getSession == nil {
		return persistence.ContextSession{}, errors.New("unexpected GetSession")
	}
	return s.getSession(ctx, actor, sessionID)
}

func (s contextHTTPApplicationStub) GetSessionSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (persistence.ContextSessionSnapshot, error) {
	if s.getSnapshot == nil {
		return persistence.ContextSessionSnapshot{},
			errors.New("unexpected GetSessionSnapshot")
	}
	return s.getSnapshot(ctx, actor, sessionID)
}

func (s contextHTTPApplicationStub) TransitionSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	key string,
	expectedVersion int,
	transition persistence.ContextSessionTransition,
) (persistence.ContextSession, bool, error) {
	if s.transition == nil {
		return persistence.ContextSession{}, false,
			errors.New("unexpected TransitionSession")
	}
	return s.transition(
		ctx,
		actor,
		sessionID,
		key,
		expectedVersion,
		transition,
	)
}

func TestContextHTTPRoutesUseTrustedActorAndCanonicalShapes(t *testing.T) {
	actor := contextHTTPActor()
	plan := contextHTTPPlan(actor.UserID)
	bootstrap := contextHTTPBootstrap()
	application := contextHTTPApplicationStub{
		createPlan: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			key string,
			request CreatePlanRequest,
		) (persistence.Plan, bool, error) {
			if gotActor != actor || key != "plan-intent-0001" ||
				!reflect.DeepEqual(request, contextHTTPPlanRequest()) {
				t.Fatal("CreatePlan received untrusted or altered input")
			}
			return plan, true, nil
		},
		getPlan: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			planID string,
		) (persistence.Plan, error) {
			if gotActor != actor || planID != plan.ID {
				t.Fatal("GetPlan received untrusted or altered input")
			}
			return plan, nil
		},
		createSession: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			planID string,
			key string,
			request CreateSessionRequest,
		) (persistence.ContextSessionBootstrap, bool, error) {
			if gotActor != actor || planID != plan.ID ||
				key != "session-intent-0001" ||
				!reflect.DeepEqual(request, contextHTTPSessionRequest()) {
				t.Fatal("CreateSession received untrusted or altered input")
			}
			return bootstrap, true, nil
		},
		getSession: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			sessionID string,
		) (persistence.ContextSession, error) {
			if gotActor != actor || sessionID != bootstrap.Session.ID {
				t.Fatal("GetSession received untrusted or altered input")
			}
			return bootstrap.Session, nil
		},
		getSnapshot: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			sessionID string,
		) (persistence.ContextSessionSnapshot, error) {
			if gotActor != actor || sessionID != bootstrap.Session.ID {
				t.Fatal("GetSessionSnapshot received untrusted or altered input")
			}
			return bootstrap.Snapshot, nil
		},
	}
	router := newContextHTTPTestRouter(t, application)

	createPlan := serveContextHTTPRequest(
		router,
		contextHTTPRequest{
			method:      http.MethodPost,
			path:        "/v1/practice-plans",
			body:        mustContextHTTPJSON(t, contextHTTPPlanRequest()),
			contentType: "application/json; charset=UTF-8",
			keyValues:   []string{"plan-intent-0001"},
			actor:       &actor,
		},
	)
	assertContextHTTPSuccess(t, createPlan, http.StatusCreated, plan)
	assertContextHTTPResponseKeys(t, createPlan, []string{
		"practice_plan_id",
		"user_id",
		"agent_thread_id",
		"matter_id",
		"scenario_definition_id",
		"scenario_definition_version",
		"scenario_type",
		"scenario_config_id",
		"scenario_config_version",
		"preparation_profile_id",
		"selected_role_ids",
		"plan_revision",
		"practice_plan_status",
		"created_at",
		"updated_at",
	})

	getPlan := serveContextHTTPRequest(
		router,
		contextHTTPRequest{
			method: http.MethodGet,
			path:   "/v1/practice-plans/" + plan.ID,
			actor:  &actor,
		},
	)
	assertContextHTTPSuccess(t, getPlan, http.StatusOK, plan)

	createSession := serveContextHTTPRequest(
		router,
		contextHTTPRequest{
			method: http.MethodPost,
			path: "/v1/practice-plans/" + plan.ID +
				"/practice-sessions",
			body:        mustContextHTTPJSON(t, contextHTTPSessionRequest()),
			contentType: "application/json",
			keyValues:   []string{"session-intent-0001"},
			actor:       &actor,
		},
	)
	assertContextHTTPSuccess(
		t,
		createSession,
		http.StatusCreated,
		bootstrap,
	)
	var bootstrapBody struct {
		Session map[string]any `json:"practice_session"`
	}
	if err := json.Unmarshal(
		createSession.Body.Bytes(),
		&bootstrapBody,
	); err != nil {
		t.Fatalf("decode bootstrap Session: %v", err)
	}
	assertContextHTTPResponseKeys(
		t,
		createSession,
		[]string{"practice_session", "snapshot"},
	)
	assertContextHTTPMapKeys(t, bootstrapBody.Session, []string{
		"practice_session_id",
		"practice_plan_id",
		"scenario_type",
		"snapshot_id",
		"practice_session_status",
		"session_version",
		"created_at",
	})
	for _, field := range []string{"started_at", "ended_at", "end_reason"} {
		assertContextHTTPFieldPresence(
			t,
			bootstrapBody.Session,
			field,
			false,
		)
	}

	getSession := serveContextHTTPRequest(
		router,
		contextHTTPRequest{
			method: http.MethodGet,
			path: "/v1/practice-sessions/" +
				bootstrap.Session.ID,
			actor: &actor,
		},
	)
	assertContextHTTPSuccess(
		t,
		getSession,
		http.StatusOK,
		bootstrap.Session,
	)
	assertContextHTTPSessionOptionalFields(
		t,
		getSession,
		false,
		false,
		false,
	)
	assertContextHTTPResponseKeys(t, getSession, []string{
		"practice_session_id",
		"practice_plan_id",
		"scenario_type",
		"snapshot_id",
		"practice_session_status",
		"session_version",
		"created_at",
	})

	getSnapshot := serveContextHTTPRequest(
		router,
		contextHTTPRequest{
			method: http.MethodGet,
			path: "/v1/practice-sessions/" +
				bootstrap.Session.ID + "/snapshot",
			actor: &actor,
		},
	)
	assertContextHTTPSuccess(
		t,
		getSnapshot,
		http.StatusOK,
		bootstrap.Snapshot,
	)
	assertContextHTTPResponseKeys(t, getSnapshot, []string{
		"snapshot_id",
		"practice_session_id",
		"plan_revision",
		"scenario_type",
		"scenario_definition_snapshot",
		"scenario_config_snapshot",
		"preparation_snapshot",
		"participants",
		"practice_option",
		"session_policy",
		"practice_focuses",
		"created_at",
	})
}

func TestContextHTTPLifecycleRoutesAndReplayStatus(t *testing.T) {
	actor := contextHTTPActor()
	sessionID := "session-lifecycle"
	startedAt := time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(10 * time.Minute)
	tests := []struct {
		name       string
		pathAction string
		transition persistence.ContextSessionTransition
		session    persistence.ContextSession
		started    bool
		ended      bool
		reason     bool
	}{
		{
			name:       "pause",
			pathAction: "pause",
			transition: persistence.ContextSessionPause,
			session: persistence.ContextSession{
				ID:           sessionID,
				PlanID:       "plan-1",
				ScenarioType: "INTERVIEW",
				SnapshotID:   "snapshot-1",
				Status:       persistence.ContextSessionPaused,
				Version:      3,
				StartedAt:    &startedAt,
				CreatedAt:    startedAt.Add(-time.Minute),
			},
			started: true,
		},
		{
			name:       "resume",
			pathAction: "resume",
			transition: persistence.ContextSessionResume,
			session: persistence.ContextSession{
				ID:           sessionID,
				PlanID:       "plan-1",
				ScenarioType: "INTERVIEW",
				SnapshotID:   "snapshot-1",
				Status:       persistence.ContextSessionProgress,
				Version:      3,
				StartedAt:    &startedAt,
				CreatedAt:    startedAt.Add(-time.Minute),
			},
			started: true,
		},
		{
			name:       "end early uses hyphenated HTTP path",
			pathAction: "end-early",
			transition: persistence.ContextSessionEndEarly,
			session: persistence.ContextSession{
				ID:           sessionID,
				PlanID:       "plan-1",
				ScenarioType: "INTERVIEW",
				SnapshotID:   "snapshot-1",
				Status:       persistence.ContextSessionEndedEarly,
				Version:      3,
				StartedAt:    &startedAt,
				EndedAt:      &endedAt,
				EndReason:    "USER_ENDED",
				CreatedAt:    startedAt.Add(-time.Minute),
			},
			started: true,
			ended:   true,
			reason:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := contextHTTPApplicationStub{
				transition: func(
					_ context.Context,
					gotActor requestcontext.Actor,
					gotSessionID string,
					key string,
					expectedVersion int,
					transition persistence.ContextSessionTransition,
				) (persistence.ContextSession, bool, error) {
					if gotActor != actor || gotSessionID != sessionID ||
						key != "transition-intent-0001" ||
						expectedVersion != 2 ||
						transition != test.transition {
						t.Fatal("TransitionSession received altered input")
					}
					return test.session, true, nil
				},
			}
			response := serveContextHTTPRequest(
				newContextHTTPTestRouter(t, application),
				contextHTTPRequest{
					method: http.MethodPost,
					path: "/v1/practice-sessions/" + sessionID +
						"/" + test.pathAction,
					body:        `{"expected_session_version":2}`,
					contentType: "application/json",
					keyValues:   []string{"transition-intent-0001"},
					actor:       &actor,
				},
			)
			assertContextHTTPSuccess(
				t,
				response,
				http.StatusOK,
				test.session,
			)
			assertContextHTTPSessionOptionalFields(
				t,
				response,
				test.started,
				test.ended,
				test.reason,
			)
		})
	}

	wrongPath := serveContextHTTPRequest(
		newContextHTTPTestRouter(t, contextHTTPApplicationStub{}),
		contextHTTPRequest{
			method:      http.MethodPost,
			path:        "/v1/practice-sessions/" + sessionID + "/end_early",
			body:        `{"expected_session_version":2}`,
			contentType: "application/json",
			keyValues:   []string{"transition-intent-0001"},
			actor:       &actor,
		},
	)
	if wrongPath.Code != http.StatusNotFound {
		t.Fatalf("underscore lifecycle path status = %d, want 404", wrongPath.Code)
	}
}

func TestContextHTTPRejectsInvalidTransportBeforeApplication(t *testing.T) {
	actor := contextHTTPActor()
	validPlanBody := mustContextHTTPJSON(t, contextHTTPPlanRequest())
	tests := []struct {
		name    string
		request contextHTTPRequest
		status  int
		code    string
	}{
		{
			name: "Authorization header is not a trusted Actor",
			request: contextHTTPRequest{
				method:        http.MethodPost,
				path:          "/v1/practice-plans",
				body:          validPlanBody,
				contentType:   "application/json",
				keyValues:     []string{"plan-intent-0001"},
				authorization: "Bearer untrusted-client-value",
			},
			status: http.StatusUnauthorized,
			code:   "authentication_required",
		},
		{
			name: "missing idempotency key",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        validPlanBody,
				contentType: "application/json",
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "invalid Session GET path ID is actor-scoped not found",
			request: contextHTTPRequest{
				method: http.MethodGet,
				path: "/v1/practice-sessions/" +
					strings.Repeat("s", 129),
				actor: &actor,
			},
			status: http.StatusNotFound,
			code:   "practice_session_not_found",
		},
		{
			name: "invalid Snapshot GET path ID is actor-scoped not found",
			request: contextHTTPRequest{
				method: http.MethodGet,
				path: "/v1/practice-sessions/" +
					strings.Repeat("s", 129) + "/snapshot",
				actor: &actor,
			},
			status: http.StatusNotFound,
			code:   "practice_session_not_found",
		},
		{
			name: "invalid create Session plan path ID",
			request: contextHTTPRequest{
				method: http.MethodPost,
				path: "/v1/practice-plans/" +
					strings.Repeat("p", 129) + "/practice-sessions",
				body:        mustContextHTTPJSON(t, contextHTTPSessionRequest()),
				contentType: "application/json",
				keyValues:   []string{"session-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "invalid lifecycle Session path ID",
			request: contextHTTPRequest{
				method: http.MethodPost,
				path: "/v1/practice-sessions/" +
					strings.Repeat("s", 129) + "/pause",
				body:        `{"expected_session_version":1}`,
				contentType: "application/json",
				keyValues:   []string{"transition-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "duplicate idempotency key",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        validPlanBody,
				contentType: "application/json",
				keyValues:   []string{"plan-key-0001", "plan-key-0002"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "short idempotency key",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        validPlanBody,
				contentType: "application/json",
				keyValues:   []string{"short"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "missing content type",
			request: contextHTTPRequest{
				method:    http.MethodPost,
				path:      "/v1/practice-plans",
				body:      validPlanBody,
				keyValues: []string{"plan-intent-0001"},
				actor:     &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "unsupported content type parameter",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        validPlanBody,
				contentType: "application/json; profile=custom",
				keyValues:   []string{"plan-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "JSON null is not an object",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        "null",
				contentType: "application/json",
				keyValues:   []string{"plan-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "JSON array is not an object",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        "[]",
				contentType: "application/json",
				keyValues:   []string{"plan-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "unknown owner field",
			request: contextHTTPRequest{
				method: http.MethodPost,
				path:   "/v1/practice-plans",
				body: strings.TrimSuffix(validPlanBody, "}") +
					`,"user_id":"forged"}`,
				contentType: "application/json",
				keyValues:   []string{"plan-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "trailing JSON value",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        validPlanBody + `{}`,
				contentType: "application/json",
				keyValues:   []string{"plan-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "missing required plan fields",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-plans",
				body:        `{}`,
				contentType: "application/json",
				keyValues:   []string{"plan-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "invalid plan path ID",
			request: contextHTTPRequest{
				method: http.MethodGet,
				path: "/v1/practice-plans/" +
					strings.Repeat("p", 129),
				actor: &actor,
			},
			status: http.StatusNotFound,
			code:   "practice_plan_not_found",
		},
		{
			name: "duplicate role selection",
			request: contextHTTPRequest{
				method: http.MethodPost,
				path:   "/v1/practice-plans/plan-1/practice-sessions",
				body: `{"expected_plan_revision":1,` +
					`"preparation_snapshot_id":"preparation-snapshot-1",` +
					`"practice_option_id":"option-1",` +
					`"role_definition_ids":["role-1","role-1"]}`,
				contentType: "application/json",
				keyValues:   []string{"session-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "invalid lifecycle version",
			request: contextHTTPRequest{
				method:      http.MethodPost,
				path:        "/v1/practice-sessions/session-1/pause",
				body:        `{"expected_session_version":0}`,
				contentType: "application/json",
				keyValues:   []string{"transition-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name: "oversized body",
			request: contextHTTPRequest{
				method: http.MethodPost,
				path:   "/v1/practice-plans",
				body: strings.Repeat(
					"x",
					maxPracticeContextHTTPRequestBody+1,
				),
				contentType: "application/json",
				keyValues:   []string{"plan-intent-0001"},
				actor:       &actor,
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveContextHTTPRequest(
				newContextHTTPTestRouter(t, contextHTTPApplicationStub{}),
				test.request,
			)
			assertContextHTTPError(t, response, test.status, test.code)
			if test.status == http.StatusUnauthorized &&
				response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf(
					"WWW-Authenticate = %q",
					response.Header().Get("WWW-Authenticate"),
				)
			}
		})
	}
}

func TestContextHTTPUnknownLengthBodyIsStillBounded(t *testing.T) {
	actor := contextHTTPActor()
	router := newContextHTTPTestRouter(t, contextHTTPApplicationStub{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/practice-plans",
		nil,
	)
	request.Body = io.NopCloser(strings.NewReader(strings.Repeat(
		"x",
		maxPracticeContextHTTPRequestBody+1,
	)))
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "plan-intent-0001")
	request = request.WithContext(
		requestcontext.WithActor(request.Context(), actor),
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertContextHTTPError(
		t,
		response,
		http.StatusBadRequest,
		"invalid_request",
	)
}

func TestContextHTTPMapsStableSanitizedErrors(t *testing.T) {
	actor := contextHTTPActor()
	sensitive := "database password must never be returned"
	tests := []struct {
		name   string
		route  string
		err    error
		status int
		code   string
	}{
		{
			name:   "invalid request",
			route:  "plan",
			err:    persistence.ErrInvalidArgument,
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name:   "invalid plan read remains actor-scoped not found",
			route:  "get-plan",
			err:    persistence.ErrInvalidArgument,
			status: http.StatusNotFound,
			code:   "practice_plan_not_found",
		},
		{
			name:   "invalid session read remains actor-scoped not found",
			route:  "session",
			err:    persistence.ErrInvalidArgument,
			status: http.StatusNotFound,
			code:   "practice_session_not_found",
		},
		{
			name:   "plan not found",
			route:  "plan",
			err:    persistence.ErrNotFound,
			status: http.StatusNotFound,
			code:   "practice_plan_not_found",
		},
		{
			name:   "session not found",
			route:  "session",
			err:    persistence.ErrNotFound,
			status: http.StatusNotFound,
			code:   "practice_session_not_found",
		},
		{
			name:   "idempotency conflict",
			route:  "plan",
			err:    persistence.ErrIdempotencyConflict,
			status: http.StatusConflict,
			code:   "idempotency_key_conflict",
		},
		{
			name:   "resource conflict",
			route:  "plan",
			err:    persistence.ErrConflict,
			status: http.StatusConflict,
			code:   "resource_conflict",
		},
		{
			name:   "terminal session",
			route:  "transition",
			err:    persistence.ErrSessionCompleted,
			status: http.StatusConflict,
			code:   "practice_session_already_terminal",
		},
		{
			name:   "internal error",
			route:  "plan",
			err:    errors.New(sensitive),
			status: http.StatusInternalServerError,
			code:   "internal_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := contextHTTPApplicationStub{
				createPlan: func(
					context.Context,
					requestcontext.Actor,
					string,
					CreatePlanRequest,
				) (persistence.Plan, bool, error) {
					return persistence.Plan{}, false, test.err
				},
				getPlan: func(
					context.Context,
					requestcontext.Actor,
					string,
				) (persistence.Plan, error) {
					return persistence.Plan{}, test.err
				},
				getSession: func(
					context.Context,
					requestcontext.Actor,
					string,
				) (persistence.ContextSession, error) {
					return persistence.ContextSession{}, test.err
				},
				transition: func(
					context.Context,
					requestcontext.Actor,
					string,
					string,
					int,
					persistence.ContextSessionTransition,
				) (persistence.ContextSession, bool, error) {
					return persistence.ContextSession{}, false, test.err
				},
			}
			request := contextHTTPRequest{
				actor: &actor,
			}
			switch test.route {
			case "plan":
				request.method = http.MethodPost
				request.path = "/v1/practice-plans"
				request.body = mustContextHTTPJSON(
					t,
					contextHTTPPlanRequest(),
				)
				request.contentType = "application/json"
				request.keyValues = []string{"plan-intent-0001"}
			case "get-plan":
				request.method = http.MethodGet
				request.path = "/v1/practice-plans/plan-1"
			case "session":
				request.method = http.MethodGet
				request.path = "/v1/practice-sessions/session-1"
			case "transition":
				request.method = http.MethodPost
				request.path = "/v1/practice-sessions/session-1/pause"
				request.body = `{"expected_session_version":1}`
				request.contentType = "application/json"
				request.keyValues = []string{"transition-intent-0001"}
			default:
				t.Fatalf("unknown test route %q", test.route)
			}
			response := serveContextHTTPRequest(
				newContextHTTPTestRouter(t, stub),
				request,
			)
			assertContextHTTPError(t, response, test.status, test.code)
			if strings.Contains(response.Body.String(), sensitive) {
				t.Fatal("internal error detail leaked")
			}
		})
	}
}

func TestNewContextHTTPHandlerRejectsNilApplication(t *testing.T) {
	if _, err := NewContextHTTPHandler(nil); err == nil {
		t.Fatal("NewContextHTTPHandler(nil) succeeded")
	}
}

type contextHTTPRequest struct {
	method        string
	path          string
	body          string
	contentType   string
	keyValues     []string
	authorization string
	actor         *requestcontext.Actor
}

func newContextHTTPTestRouter(
	t *testing.T,
	application ContextHTTPApplication,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewContextHTTPHandler(application)
	if err != nil {
		t.Fatalf("NewContextHTTPHandler: %v", err)
	}
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}

func serveContextHTTPRequest(
	handler http.Handler,
	input contextHTTPRequest,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		input.method,
		input.path,
		strings.NewReader(input.body),
	)
	if input.contentType != "" {
		request.Header.Set("Content-Type", input.contentType)
	}
	for _, key := range input.keyValues {
		request.Header.Add("Idempotency-Key", key)
	}
	if input.authorization != "" {
		request.Header.Set("Authorization", input.authorization)
	}
	if input.actor != nil {
		request = request.WithContext(
			requestcontext.WithActor(request.Context(), *input.actor),
		)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertContextHTTPSuccess(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	want any,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body)
	}
	assertContextHTTPPrivateJSON(t, response)
	wantJSON := mustContextHTTPJSON(t, want)
	var gotValue, wantValue any
	if err := json.Unmarshal(response.Body.Bytes(), &gotValue); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantValue); err != nil {
		t.Fatalf("decode expected response: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("response = %#v, want %#v", gotValue, wantValue)
	}
	assertContextHTTPNoNull(t, gotValue, "$")
}

func assertContextHTTPError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body)
	}
	assertContextHTTPPrivateJSON(t, response)
	var body struct {
		Error struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			Retryable     bool   `json:"retryable"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != code || body.Error.Message == "" ||
		body.Error.Retryable || body.Error.CorrelationID == "" {
		t.Fatalf("error response = %#v", body)
	}
}

func assertContextHTTPPrivateJSON(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("private cache headers = %#v", response.Header())
	}
	if !strings.HasPrefix(
		response.Header().Get("Content-Type"),
		"application/json",
	) {
		t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
	}
}

func assertContextHTTPSessionOptionalFields(
	t *testing.T,
	response *httptest.ResponseRecorder,
	started bool,
	ended bool,
	reason bool,
) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode Session response: %v", err)
	}
	assertContextHTTPFieldPresence(t, body, "started_at", started)
	assertContextHTTPFieldPresence(t, body, "ended_at", ended)
	assertContextHTTPFieldPresence(t, body, "end_reason", reason)
}

func assertContextHTTPFieldPresence(
	t *testing.T,
	body map[string]any,
	field string,
	want bool,
) {
	t.Helper()
	value, exists := body[field]
	if exists != want {
		t.Fatalf("%s presence = %v, want %v", field, exists, want)
	}
	if exists && value == nil {
		t.Fatalf("%s must be absent or non-null", field)
	}
}

func assertContextHTTPResponseKeys(
	t *testing.T,
	response *httptest.ResponseRecorder,
	want []string,
) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode object response: %v", err)
	}
	assertContextHTTPMapKeys(t, body, want)
}

func assertContextHTTPMapKeys(
	t *testing.T,
	body map[string]any,
	want []string,
) {
	t.Helper()
	got := make([]string, 0, len(body))
	for key := range body {
		got = append(got, key)
	}
	sort.Strings(got)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedWant)
	if !reflect.DeepEqual(got, sortedWant) {
		t.Fatalf("JSON keys = %v, want %v", got, sortedWant)
	}
}

func assertContextHTTPNoNull(t *testing.T, value any, path string) {
	t.Helper()
	switch typed := value.(type) {
	case nil:
		t.Fatalf("%s must not be null", path)
	case map[string]any:
		for key, child := range typed {
			assertContextHTTPNoNull(t, child, path+"."+key)
		}
	case []any:
		for index, child := range typed {
			assertContextHTTPNoNull(
				t,
				child,
				path+"["+mustContextHTTPJSON(t, index)+"]",
			)
		}
	}
}

func mustContextHTTPJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON fixture: %v", err)
	}
	return string(encoded)
}

func contextHTTPActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "11111111-1111-4111-8111-111111111111",
		SessionID: "22222222-2222-4222-8222-222222222222",
	}
}

func contextHTTPPlanRequest() CreatePlanRequest {
	return CreatePlanRequest{
		AgentThreadID:             "thread-1",
		MatterID:                  "matter-1",
		ScenarioDefinitionID:      "scenario-1",
		ScenarioDefinitionVersion: 1,
		ScenarioConfigID:          "config-1",
		ScenarioConfigVersion:     1,
		PreparationProfileID:      "profile-1",
		SelectedRoleIDs:           []string{"role-1"},
	}
}

func contextHTTPSessionRequest() CreateSessionRequest {
	return CreateSessionRequest{
		ExpectedPlanRevision:  1,
		PreparationSnapshotID: "preparation-snapshot-1",
		PracticeOptionID:      "option-1",
		RoleDefinitionIDs:     []string{"role-1"},
	}
}

func contextHTTPPlan(userID string) persistence.Plan {
	createdAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	request := contextHTTPPlanRequest()
	return persistence.Plan{
		ID:                        "plan-1",
		UserID:                    userID,
		AgentThreadID:             request.AgentThreadID,
		MatterID:                  request.MatterID,
		ScenarioDefinitionID:      request.ScenarioDefinitionID,
		ScenarioDefinitionVersion: request.ScenarioDefinitionVersion,
		ScenarioType:              "INTERVIEW",
		ScenarioConfigID:          request.ScenarioConfigID,
		ScenarioConfigVersion:     request.ScenarioConfigVersion,
		PreparationProfileID:      request.PreparationProfileID,
		SelectedRoleIDs:           append([]string(nil), request.SelectedRoleIDs...),
		Revision:                  1,
		Status:                    persistence.PlanStatusReady,
		CreatedAt:                 createdAt,
		UpdatedAt:                 createdAt,
	}
}

func contextHTTPBootstrap() persistence.ContextSessionBootstrap {
	createdAt := time.Date(2026, 7, 26, 12, 1, 0, 0, time.UTC)
	role := persistence.RoleSnapshot{
		ID:                   "role-1",
		ScenarioDefinitionID: "scenario-1",
		Type:                 "INTERVIEWER",
		DisplayName:          "Interviewer",
		Responsibilities:     "Ask evidence-based questions.",
		Style:                "Focused",
		FocusAreas:           []string{"system_design"},
		Version:              1,
	}
	snapshot := persistence.ContextSessionSnapshot{
		ID:           "snapshot-1",
		SessionID:    "session-1",
		PlanRevision: 1,
		ScenarioType: "INTERVIEW",
		ScenarioDefinition: persistence.ScenarioDefinitionSnapshot{
			ID:      "scenario-1",
			Type:    "INTERVIEW",
			Name:    "English interview",
			Version: 1,
			Status:  "active",
		},
		ScenarioConfig: persistence.ScenarioConfigSnapshot{
			ID:                   "config-1",
			ScenarioDefinitionID: "scenario-1",
			Type:                 "INTERVIEW",
			Version:              1,
			JobTitle:             "Backend engineer",
			JobDescription:       "Build reliable services.",
			FocusAreas:           []string{"system_design"},
		},
		Preparation: persistence.PreparationSnapshot{
			ID:                 "preparation-snapshot-1",
			SourceProfileID:    "profile-1",
			SourceVersion:      1,
			BackgroundSnapshot: "Confirmed background.",
			CreatedAt:          createdAt.Add(-time.Minute),
		},
		Participants: []persistence.ContextParticipant{
			{
				ID:               "participant-interviewer",
				SessionID:        "session-1",
				Role:             "INTERVIEWER",
				SubjectRef:       persistence.SubjectRef{Namespace: "speakup.role", SubjectID: "role-1"},
				RoleDefinitionID: "role-1",
				RoleSnapshot:     &role,
				Order:            1,
			},
			{
				ID:        "participant-candidate",
				SessionID: "session-1",
				Role:      "CANDIDATE",
				SubjectRef: persistence.SubjectRef{
					Namespace: "speakup.user",
					SubjectID: "11111111-1111-4111-8111-111111111111",
				},
				Order: 2,
			},
		},
		PracticeOption: persistence.PracticeOptionSnapshot{
			ID:                   "option-1",
			ScenarioDefinitionID: "scenario-1",
			Type:                 "FULL_SIMULATION",
			DisplayName:          "Full simulation",
			Version:              1,
		},
		SessionPolicy: persistence.ContextSessionPolicy{
			SuggestedDurationSeconds: 900,
			MinEffectiveTurns:        4,
			MaxEffectiveTurns:        6,
			CoverageCheckpointTurn:   4,
			MaxFollowUpsPerQuestion:  1,
			TargetObjectives: []persistence.PracticeObjective{
				{
					ID:          "system_design",
					Description: "Explain system design decisions.",
				},
			},
			EarlyCompletionRule: "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
		},
		PracticeFocuses: []persistence.PracticeObjective{
			{
				ID:          "system_design",
				Description: "Explain system design decisions.",
			},
		},
		CreatedAt: createdAt,
	}
	return persistence.ContextSessionBootstrap{
		Session: persistence.ContextSession{
			ID:           snapshot.SessionID,
			PlanID:       "plan-1",
			ScenarioType: snapshot.ScenarioType,
			SnapshotID:   snapshot.ID,
			Status:       persistence.ContextSessionStarting,
			Version:      1,
			CreatedAt:    createdAt,
		},
		Snapshot: snapshot,
	}
}
