package avatar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestHTTPHandlerReturnsStrictNoStoreSessionTokenContract(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	service := newTestService(
		t,
		contextSessionReaderStub{session: interactiveSession()},
		&tokenProviderStub{token: ProviderSessionToken{
			Value:     "provider-session-token",
			ExpiresAt: now.Add(10 * time.Minute),
		}},
	)
	service.now = func() time.Time { return now }
	router := avatarTestRouter(t, service, true)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/practice-sessions/practice-session-1/avatar-session-token",
		nil,
	)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf(
			"Cache-Control = %q",
			response.Header().Get("Cache-Control"),
		)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	expectedKeys := []string{
		"app_id",
		"avatar_id",
		"session_token",
		"region",
		"expires_at",
		"audio_format",
	}
	if len(document) != len(expectedKeys) {
		t.Fatalf("response keys = %v", document)
	}
	for _, key := range expectedKeys {
		if _, exists := document[key]; !exists {
			t.Fatalf("response is missing %q", key)
		}
	}
	var format AudioFormat
	if err := json.Unmarshal(document["audio_format"], &format); err != nil {
		t.Fatalf("decode audio format: %v", err)
	}
	if format != (AudioFormat{
		Encoding:     "PCM_S16LE",
		SampleRateHz: 24000,
		Channels:     1,
	}) {
		t.Fatalf("audio format = %#v", format)
	}
	if strings.Contains(response.Body.String(), "server-only") {
		t.Fatal("response exposed server-only material")
	}
}

func TestHTTPHandlerRequiresTrustedActorAndEmptyBody(t *testing.T) {
	service := newTestService(
		t,
		contextSessionReaderStub{session: interactiveSession()},
		&tokenProviderStub{},
	)
	tests := []struct {
		name      string
		withActor bool
		body      string
		status    int
		errorCode string
	}{
		{
			name:      "missing actor",
			status:    http.StatusUnauthorized,
			errorCode: "authentication_required",
		},
		{
			name:      "unexpected body",
			withActor: true,
			body:      `{}`,
			status:    http.StatusBadRequest,
			errorCode: "invalid_request",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := avatarTestRouter(t, service, test.withActor)
			var body *strings.Reader
			if test.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(test.body)
			}
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/practice-sessions/practice-session-1/avatar-session-token",
				body,
			)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf(
					"status = %d, body = %s",
					response.Code,
					response.Body,
				)
			}
			var document struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if document.Error.Code != test.errorCode {
				t.Fatalf("error code = %q", document.Error.Code)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf(
					"Cache-Control = %q",
					response.Header().Get("Cache-Control"),
				)
			}
		})
	}
}

func avatarTestRouter(
	t *testing.T,
	service *Service,
	withActor bool,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if withActor {
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(requestcontext.WithActor(
				context.Background(),
				testActor(),
			))
			c.Next()
		})
	}
	handler, err := NewHTTPHandler(service)
	if err != nil {
		t.Fatalf("NewHTTPHandler() error = %v", err)
	}
	handler.RegisterRoutes(router)
	return router
}
