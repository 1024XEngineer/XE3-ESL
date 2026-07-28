package preparation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type profileHTTPApplicationStub struct {
	createProfile func(
		context.Context,
		requestcontext.Actor,
		string,
		CreateProfileRequest,
	) (Profile, bool, error)
	createSnapshot func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		CreateSnapshotRequest,
	) (Snapshot, bool, error)
}

func (s profileHTTPApplicationStub) CreateProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	key string,
	request CreateProfileRequest,
) (Profile, bool, error) {
	if s.createProfile == nil {
		return Profile{}, false, errors.New("unexpected CreateProfile call")
	}
	return s.createProfile(ctx, actor, key, request)
}

func (s profileHTTPApplicationStub) CreateSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	profileID string,
	key string,
	request CreateSnapshotRequest,
) (Snapshot, bool, error) {
	if s.createSnapshot == nil {
		return Snapshot{}, false, errors.New("unexpected CreateSnapshot call")
	}
	return s.createSnapshot(ctx, actor, profileID, key, request)
}

func TestProfileHTTPCreateProfileUsesTrustedActor(t *testing.T) {
	actor := profileHTTPActor()
	updatedAt := time.Date(2026, 7, 26, 11, 12, 13, 0, time.UTC)
	want := Profile{
		ID:                "profile-1",
		UserID:            actor.UserID,
		ResumeRef:         "resume-1",
		JobDescriptionRef: "job-1",
		BackgroundSummary: "Confirmed background.",
		Version:           1,
		UpdatedAt:         updatedAt,
	}
	called := false
	router := newProfileHTTPTestRouter(t, profileHTTPApplicationStub{
		createProfile: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			key string,
			request CreateProfileRequest,
		) (Profile, bool, error) {
			called = true
			if gotActor != actor {
				t.Fatalf("actor = %#v, want %#v", gotActor, actor)
			}
			if key != "profile-key-0001" {
				t.Fatalf("idempotency key = %q", key)
			}
			if request != (CreateProfileRequest{
				ResumeRef:         "resume-1",
				JobDescriptionRef: "job-1",
				BackgroundSummary: "Confirmed background.",
			}) {
				t.Fatalf("request = %#v", request)
			}
			return want, false, nil
		},
	})

	response := serveProfileHTTPRequest(
		router,
		http.MethodPost,
		"/v1/preparation-profiles",
		`{"resume_ref":"resume-1","job_description_ref":"job-1",`+
			`"background_summary":"Confirmed background."}`,
		"application/json; charset=utf-8",
		"profile-key-0001",
		&actor,
	)

	if !called {
		t.Fatal("CreateProfile was not called")
	}
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	assertProfileHTTPNoStore(t, response)
	var got Profile
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %#v, want %#v", got, want)
	}
}

func TestProfileHTTPCreateSnapshotReplayKeepsCreatedSemantics(t *testing.T) {
	actor := profileHTTPActor()
	createdAt := time.Date(2026, 7, 26, 11, 13, 14, 0, time.UTC)
	want := Snapshot{
		ID:                     "snapshot-1",
		SourceProfileID:        "profile-1",
		SourceVersion:          3,
		ResumeSnapshot:         "Frozen resume.",
		JobDescriptionSnapshot: "Frozen job.",
		BackgroundSnapshot:     "Frozen background.",
		CreatedAt:              createdAt,
	}
	router := newProfileHTTPTestRouter(t, profileHTTPApplicationStub{
		createSnapshot: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			profileID string,
			key string,
			request CreateSnapshotRequest,
		) (Snapshot, bool, error) {
			if gotActor != actor {
				t.Fatalf("actor = %#v, want %#v", gotActor, actor)
			}
			if profileID != "profile-1" || key != "snapshot-key-0001" ||
				request.SourceVersion != 3 {
				t.Fatalf(
					"profile/key/request = %q/%q/%#v",
					profileID,
					key,
					request,
				)
			}
			return want, true, nil
		},
	})

	response := serveProfileHTTPRequest(
		router,
		http.MethodPost,
		"/v1/preparation-profiles/profile-1/snapshots",
		`{"source_version":3}`,
		"application/json",
		"snapshot-key-0001",
		&actor,
	)

	if response.Code != http.StatusCreated {
		t.Fatalf("replay status = %d, body = %s", response.Code, response.Body)
	}
	assertProfileHTTPNoStore(t, response)
	var got Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %#v, want %#v", got, want)
	}
}

func TestProfileHTTPRejectsInvalidTransportBeforeApplication(t *testing.T) {
	actor := profileHTTPActor()
	tests := []struct {
		name        string
		path        string
		body        string
		contentType string
		key         string
		actor       *requestcontext.Actor
		status      int
		code        string
	}{
		{
			name:        "missing actor",
			path:        "/v1/preparation-profiles",
			body:        `{"background_summary":"Confirmed."}`,
			contentType: "application/json",
			key:         "profile-key-0001",
			status:      http.StatusUnauthorized,
			code:        "authentication_required",
		},
		{
			name:        "missing idempotency key",
			path:        "/v1/preparation-profiles",
			body:        `{"background_summary":"Confirmed."}`,
			contentType: "application/json",
			actor:       &actor,
			status:      http.StatusBadRequest,
			code:        "invalid_request",
		},
		{
			name:        "bad content type",
			path:        "/v1/preparation-profiles",
			body:        `{"background_summary":"Confirmed."}`,
			contentType: "text/plain",
			key:         "profile-key-0001",
			actor:       &actor,
			status:      http.StatusBadRequest,
			code:        "invalid_request",
		},
		{
			name:        "unknown field",
			path:        "/v1/preparation-profiles",
			body:        `{"background_summary":"Confirmed.","user_id":"forged"}`,
			contentType: "application/json",
			key:         "profile-key-0001",
			actor:       &actor,
			status:      http.StatusBadRequest,
			code:        "invalid_request",
		},
		{
			name:        "trailing JSON",
			path:        "/v1/preparation-profiles",
			body:        `{"background_summary":"Confirmed."}{}`,
			contentType: "application/json",
			key:         "profile-key-0001",
			actor:       &actor,
			status:      http.StatusBadRequest,
			code:        "invalid_request",
		},
		{
			name:        "oversized body",
			path:        "/v1/preparation-profiles",
			body:        strings.Repeat("x", maxProfileHTTPRequestBody+1),
			contentType: "application/json",
			key:         "profile-key-0001",
			actor:       &actor,
			status:      http.StatusBadRequest,
			code:        "invalid_request",
		},
		{
			name: "invalid path resource ID",
			path: "/v1/preparation-profiles/" +
				strings.Repeat("p", 129) + "/snapshots",
			body:        `{"source_version":1}`,
			contentType: "application/json",
			key:         "snapshot-key-0001",
			actor:       &actor,
			status:      http.StatusBadRequest,
			code:        "invalid_request",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newProfileHTTPTestRouter(t, profileHTTPApplicationStub{})
			response := serveProfileHTTPRequest(
				router,
				http.MethodPost,
				test.path,
				test.body,
				test.contentType,
				test.key,
				test.actor,
			)
			assertProfileHTTPError(t, response, test.status, test.code)
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

func TestProfileHTTPMapsStableServiceErrors(t *testing.T) {
	actor := profileHTTPActor()
	sensitive := "postgres password=do-not-leak"
	tests := []struct {
		name   string
		path   string
		body   string
		key    string
		err    error
		status int
		code   string
	}{
		{
			name:   "invalid request",
			path:   "/v1/preparation-profiles",
			body:   `{"background_summary":"Confirmed."}`,
			key:    "profile-key-0001",
			err:    ErrProfileInvalid,
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name:   "profile not found",
			path:   "/v1/preparation-profiles/missing/snapshots",
			body:   `{"source_version":1}`,
			key:    "snapshot-key-0001",
			err:    ErrProfileNotFound,
			status: http.StatusNotFound,
			code:   "preparation_profile_not_found",
		},
		{
			name:   "version conflict",
			path:   "/v1/preparation-profiles/profile-1/snapshots",
			body:   `{"source_version":2}`,
			key:    "snapshot-key-0001",
			err:    ErrProfileConflict,
			status: http.StatusConflict,
			code:   "preparation_version_conflict",
		},
		{
			name:   "idempotency conflict",
			path:   "/v1/preparation-profiles",
			body:   `{"background_summary":"Changed."}`,
			key:    "profile-key-0001",
			err:    ErrProfileIdempotencyConflict,
			status: http.StatusConflict,
			code:   "idempotency_key_conflict",
		},
		{
			name:   "internal error is sanitized",
			path:   "/v1/preparation-profiles",
			body:   `{"background_summary":"Confirmed."}`,
			key:    "profile-key-0001",
			err:    errors.New(sensitive),
			status: http.StatusInternalServerError,
			code:   "internal_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := profileHTTPApplicationStub{
				createProfile: func(
					context.Context,
					requestcontext.Actor,
					string,
					CreateProfileRequest,
				) (Profile, bool, error) {
					return Profile{}, false, test.err
				},
				createSnapshot: func(
					context.Context,
					requestcontext.Actor,
					string,
					string,
					CreateSnapshotRequest,
				) (Snapshot, bool, error) {
					return Snapshot{}, false, test.err
				},
			}
			response := serveProfileHTTPRequest(
				newProfileHTTPTestRouter(t, stub),
				http.MethodPost,
				test.path,
				test.body,
				"application/json",
				test.key,
				&actor,
			)
			assertProfileHTTPError(t, response, test.status, test.code)
			if strings.Contains(response.Body.String(), sensitive) {
				t.Fatalf("sensitive error leaked: %s", response.Body)
			}
		})
	}
}

func newProfileHTTPTestRouter(
	t *testing.T,
	application ProfileHTTPApplication,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewProfileHTTPHandler(application)
	if err != nil {
		t.Fatalf("new profile HTTP handler: %v", err)
	}
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}

func serveProfileHTTPRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	contentType string,
	idempotencyKey string,
	actor *requestcontext.Actor,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if actor != nil {
		request = request.WithContext(
			requestcontext.WithActor(request.Context(), *actor),
		)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertProfileHTTPError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body)
	}
	assertProfileHTTPNoStore(t, response)
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

func assertProfileHTTPNoStore(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers = %#v", response.Header())
	}
}

func profileHTTPActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "11111111-1111-4111-8111-111111111111",
		SessionID: "session-1",
	}
}
