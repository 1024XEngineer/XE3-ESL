package http

import (
	"bytes"
	"context"
	"encoding/json"
	basehttp "net/http"
	"net/http/httptest"
	"testing"

	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const httpUserID = "10000000-0000-4000-8000-000000000001"

var httpActor = requestcontext.Actor{
	UserID: httpUserID, SessionID: "session-1",
}

func TestProfileRoutesRequireAuthentication(t *testing.T) {
	application := &applicationStub{profile: coachingprofile.Empty(httpUserID)}
	router := profileRouter(t, application, false)
	for _, method := range []string{basehttp.MethodGet, basehttp.MethodPatch} {
		request := httptest.NewRequest(method, "/v1/me/coaching-profile", nil)
		if method == basehttp.MethodPatch {
			request = httptest.NewRequest(
				method,
				"/v1/me/coaching-profile",
				bytes.NewBufferString(`{"expected_version":0,"memory_enabled":false}`),
			)
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != basehttp.StatusUnauthorized {
			t.Fatalf("%s status = %d", method, response.Code)
		}
	}
}

func TestGetReturnsLogicalEmptyEnabledProfile(t *testing.T) {
	application := &applicationStub{profile: coachingprofile.Empty(httpUserID)}
	response := performProfileRequest(
		t,
		profileRouter(t, application, true),
		basehttp.MethodGet,
		nil,
	)
	if response.Code != basehttp.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		MemoryEnabled bool                          `json:"memory_enabled"`
		Profile       coachingprofile.Data          `json:"profile"`
		FieldSources  map[coachingprofile.Field]any `json:"field_sources"`
		Version       int64                         `json:"version"`
	}
	if json.Unmarshal(response.Body.Bytes(), &body) != nil ||
		!body.MemoryEnabled || body.Version != 0 || !body.Profile.Empty() ||
		len(body.FieldSources) != 0 {
		t.Fatalf("logical profile response = %s", response.Body.String())
	}
}

func TestPatchRejectsUnknownAndNullFields(t *testing.T) {
	tests := []string{
		`{"expected_version":0,"unknown":true}`,
		`{"expected_version":0,"memory_enabled":null}`,
		`{"expected_version":0,"updates":{"occupation":null}}`,
	}
	for _, body := range tests {
		application := &applicationStub{profile: coachingprofile.Empty(httpUserID)}
		response := performProfileRequest(
			t,
			profileRouter(t, application, true),
			basehttp.MethodPatch,
			[]byte(body),
		)
		if response.Code != basehttp.StatusBadRequest || application.updateCalls != 0 {
			t.Fatalf(
				"body=%s status=%d updates=%d response=%s",
				body,
				response.Code,
				application.updateCalls,
				response.Body.String(),
			)
		}
	}
}

func TestPatchReturnsVersionConflict(t *testing.T) {
	application := &applicationStub{
		profile:   coachingprofile.Empty(httpUserID),
		updateErr: coachingprofile.ErrVersionConflict,
	}
	response := performProfileRequest(
		t,
		profileRouter(t, application, true),
		basehttp.MethodPatch,
		[]byte(`{"expected_version":0,"memory_enabled":false}`),
	)
	if response.Code != basehttp.StatusConflict ||
		!bytes.Contains(response.Body.Bytes(), []byte("profile_version_conflict")) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPatchAllowsSettingsUpdateWhileDisabled(t *testing.T) {
	disabled := coachingprofile.Empty(httpUserID)
	disabled.MemoryEnabled = false
	disabled.Version = 2
	application := &applicationStub{profile: disabled}
	response := performProfileRequest(
		t,
		profileRouter(t, application, true),
		basehttp.MethodPatch,
		[]byte(`{"expected_version":2,"updates":{"occupation":"设计师"}}`),
	)
	if response.Code != basehttp.StatusOK || application.updateCalls != 1 ||
		application.command.SourceType != coachingprofile.SourceUserSetting ||
		application.command.Patch.Occupation == nil ||
		*application.command.Patch.Occupation != "设计师" {
		t.Fatalf(
			"status=%d command=%#v body=%s",
			response.Code,
			application.command,
			response.Body.String(),
		)
	}
}

func profileRouter(
	t *testing.T,
	application Application,
	authenticated bool,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := New(application, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(
				requestcontext.WithActor(c.Request.Context(), httpActor),
			)
			c.Next()
		})
	}
	handler.RegisterRoutes(router)
	return router
}

func performProfileRequest(
	t *testing.T,
	router basehttp.Handler,
	method string,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/v1/me/coaching-profile", bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

type applicationStub struct {
	profile     coachingprofile.Profile
	getErr      error
	updateErr   error
	command     coachingprofile.UpdateCommand
	updateCalls int
}

func (application *applicationStub) Get(
	context.Context,
	requestcontext.Actor,
) (coachingprofile.Profile, error) {
	return application.profile, application.getErr
}

func (application *applicationStub) Update(
	_ context.Context,
	_ requestcontext.Actor,
	command coachingprofile.UpdateCommand,
) (coachingprofile.Profile, error) {
	application.updateCalls++
	application.command = command
	if application.updateErr != nil {
		return coachingprofile.Profile{}, application.updateErr
	}
	application.profile.Version++
	return application.profile, nil
}
