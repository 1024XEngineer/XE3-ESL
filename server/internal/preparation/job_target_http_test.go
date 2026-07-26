package preparation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestJobTargetHTTPCreateIsStrictPrivateAndActorDerived(t *testing.T) {
	t.Parallel()

	application := &jobTargetHTTPApplicationStub{}
	called := false
	application.create = func(
		_ context.Context,
		actor requestcontext.Actor,
		key string,
		request CreateJobTargetRequest,
	) (JobTarget, bool, error) {
		called = true
		if actor != jobTargetActor() ||
			key != "target-http-create" ||
			request.Source != JobTargetSourceQuickStart ||
			request.JobTitle != "Platform engineer" {
			t.Fatalf(
				"actor=%#v key=%q request=%#v",
				actor,
				key,
				request,
			)
		}
		return JobTarget{
			ID:           "target-1",
			UserID:       actor.UserID,
			Input:        request.input(),
			InputVersion: 1,
			Stage:        JobTargetStageDraft,
		}, false, nil
	}
	router := newJobTargetHTTPRouter(t, application, true)
	response := performJobTargetRequest(
		router,
		http.MethodPost,
		"/v1/job-targets",
		`{"source":"quick_start","job_title":"Platform engineer"}`,
		"target-http-create",
	)
	if response.Code != http.StatusCreated || !called {
		t.Fatalf(
			"status=%d called=%t body=%s",
			response.Code,
			called,
			response.Body.String(),
		)
	}
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("private headers = %#v", response.Header())
	}
}

func TestJobTargetHTTPRejectsNullUnknownAndTrailingJSON(t *testing.T) {
	t.Parallel()

	application := &jobTargetHTTPApplicationStub{
		create: func(
			context.Context,
			requestcontext.Actor,
			string,
			CreateJobTargetRequest,
		) (JobTarget, bool, error) {
			t.Fatal("invalid JSON reached application")
			return JobTarget{}, false, nil
		},
	}
	router := newJobTargetHTTPRouter(t, application, true)
	for _, body := range []string{
		`{"source":"quick_start","job_title":null}`,
		`{"source":"quick_start","job_title":"x","unknown":true}`,
		`{"source":"quick_start","job_title":"x"} {}`,
	} {
		response := performJobTargetRequest(
			router,
			http.MethodPost,
			"/v1/job-targets",
			body,
			"target-http-invalid",
		)
		if response.Code != http.StatusBadRequest ||
			!strings.Contains(
				response.Body.String(),
				`"code":"invalid_request"`,
			) {
			t.Fatalf(
				"body=%q status=%d response=%s",
				body,
				response.Code,
				response.Body.String(),
			)
		}
	}
}

func TestJobTargetHTTPAnalysisRecoveryAndSanitizedFailure(t *testing.T) {
	t.Parallel()

	application := &jobTargetHTTPApplicationStub{}
	application.analyze = func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		AnalyzeJobTargetRequest,
	) (JobTarget, bool, error) {
		return JobTarget{
			ID:           "target-1",
			UserID:       jobTargetActor().UserID,
			Input:        JobTargetInput{Source: JobTargetSourceQuickStart, JobTitle: "Engineer"},
			InputVersion: 1,
			Stage:        JobTargetStageParsing,
		}, false, nil
	}
	router := newJobTargetHTTPRouter(t, application, true)
	response := performJobTargetRequest(
		router,
		http.MethodPost,
		"/v1/job-targets/target-1/analyses",
		`{"expected_input_version":1}`,
		"target-http-analysis",
	)
	if response.Code != http.StatusAccepted {
		t.Fatalf(
			"analysis status=%d body=%s",
			response.Code,
			response.Body.String(),
		)
	}

	const sensitive = "SENSITIVE_JOB_DESCRIPTION_MUST_NOT_LEAK"
	application.analyze = func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		AnalyzeJobTargetRequest,
	) (JobTarget, bool, error) {
		return JobTarget{}, false, errors.New(
			sensitive,
		)
	}
	response = performJobTargetRequest(
		router,
		http.MethodPost,
		"/v1/job-targets/target-1/analyses",
		`{"expected_input_version":1}`,
		"target-http-analysis-error",
	)
	if response.Code != http.StatusInternalServerError ||
		strings.Contains(response.Body.String(), sensitive) {
		t.Fatalf(
			"failure status=%d body=%s",
			response.Code,
			response.Body.String(),
		)
	}
}

func TestJobTargetHTTPRequiresTrustedActor(t *testing.T) {
	t.Parallel()

	router := newJobTargetHTTPRouter(
		t,
		&jobTargetHTTPApplicationStub{},
		false,
	)
	response := performJobTargetRequest(
		router,
		http.MethodGet,
		"/v1/job-targets/target-1",
		"",
		"",
	)
	if response.Code != http.StatusUnauthorized ||
		response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf(
			"status=%d headers=%#v body=%s",
			response.Code,
			response.Header(),
			response.Body.String(),
		)
	}
}

func newJobTargetHTTPRouter(
	t *testing.T,
	application JobTargetHTTPApplication,
	withActor bool,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if withActor {
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(
				requestcontext.WithActor(
					c.Request.Context(),
					jobTargetActor(),
				),
			)
			c.Next()
		})
	}
	handler, err := NewJobTargetHTTPHandler(application)
	if err != nil {
		t.Fatalf("NewJobTargetHTTPHandler: %v", err)
	}
	handler.RegisterRoutes(router)
	return router
}

func performJobTargetRequest(
	router http.Handler,
	method string,
	path string,
	body string,
	key string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

type jobTargetHTTPApplicationStub struct {
	create  func(context.Context, requestcontext.Actor, string, CreateJobTargetRequest) (JobTarget, bool, error)
	get     func(context.Context, requestcontext.Actor, string) (JobTarget, error)
	update  func(context.Context, requestcontext.Actor, string, string, UpdateJobTargetRequest) (JobTarget, bool, error)
	analyze func(context.Context, requestcontext.Actor, string, string, AnalyzeJobTargetRequest) (JobTarget, bool, error)
	confirm func(context.Context, requestcontext.Actor, string, string, ConfirmJobTargetRequest) (JobTarget, bool, error)
	discard func(context.Context, requestcontext.Actor, string, string, DiscardJobTargetRequest) (JobTarget, bool, error)
}

func (s *jobTargetHTTPApplicationStub) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	key string,
	request CreateJobTargetRequest,
) (JobTarget, bool, error) {
	return s.create(ctx, actor, key, request)
}

func (s *jobTargetHTTPApplicationStub) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
) (JobTarget, error) {
	if s.get == nil {
		return JobTarget{}, ErrJobTargetNotFound
	}
	return s.get(ctx, actor, targetID)
}

func (s *jobTargetHTTPApplicationStub) Update(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	key string,
	request UpdateJobTargetRequest,
) (JobTarget, bool, error) {
	return s.update(ctx, actor, targetID, key, request)
}

func (s *jobTargetHTTPApplicationStub) Analyze(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	key string,
	request AnalyzeJobTargetRequest,
) (JobTarget, bool, error) {
	return s.analyze(ctx, actor, targetID, key, request)
}

func (s *jobTargetHTTPApplicationStub) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	key string,
	request ConfirmJobTargetRequest,
) (JobTarget, bool, error) {
	return s.confirm(ctx, actor, targetID, key, request)
}

func (s *jobTargetHTTPApplicationStub) Discard(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	key string,
	request DiscardJobTargetRequest,
) (JobTarget, bool, error) {
	return s.discard(ctx, actor, targetID, key, request)
}

var _ JobTargetHTTPApplication = (*jobTargetHTTPApplicationStub)(nil)
