package avatar

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestAvatarHTTPHandlerRequiresApplication(t *testing.T) {
	if _, err := NewHTTPHandler(nil); err == nil {
		t.Fatal("expected nil application to be rejected")
	}
}

func TestAvatarSessionTokenRoute(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	service := avatarHTTPService(t, now)
	handler, err := NewHTTPHandler(service)
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	gin.SetMode(gin.TestMode)

	t.Run("issues token for authenticated actor", func(t *testing.T) {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(requestcontext.WithActor(
				c.Request.Context(),
				requestcontext.Actor{UserID: "user-1", SessionID: "auth-1"},
			))
			c.Next()
		})
		handler.RegisterRoutes(router)

		response := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/practice-sessions/session-1/avatar-session-token",
			nil,
		)
		router.ServeHTTP(response, request)

		if response.Code != http.StatusOK ||
			response.Header().Get("Cache-Control") != "no-store" ||
			!strings.Contains(response.Body.String(), "short-lived-token") {
			t.Fatalf("response = %d / %s", response.Code, response.Body.String())
		}
	})

	t.Run("rejects request body", func(t *testing.T) {
		router := gin.New()
		handler.RegisterRoutes(router)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/practice-sessions/session-1/avatar-session-token",
			strings.NewReader("{}"),
		)
		router.ServeHTTP(response, request)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("response = %d / %s", response.Code, response.Body.String())
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		router := gin.New()
		handler.RegisterRoutes(router)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/practice-sessions/session-1/avatar-session-token",
			nil,
		)
		router.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized ||
			response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("response = %d / %#v", response.Code, response.Header())
		}
	})
}

func avatarHTTPService(t *testing.T, now time.Time) *Service {
	t.Helper()
	service, err := NewService(
		ServiceConfiguration{
			Enabled: true, AppID: "app", ProviderProfile: "spatialreal_default",
			Region: "ap-northeast", TokenTTL: 5 * time.Minute,
		},
		avatarSessionReaderStub{session: practice.Session{
			ID: "session-1", Status: practice.SessionInProgress,
		}, snapshot: practice.SessionSnapshot{Presentation: practice.PresentationSnapshot{
			SchemaVersion: practice.PresentationSnapshotSchemaVersion,
			Avatar: practice.AvatarPresentationSnapshot{
				OptionID: "avatar_lisa", Provider: "spatialreal",
				ProviderProfile: "spatialreal_default", ProviderAvatarID: "avatar",
				BindingVersion: 1,
			},
			Voice: practice.VoicePresentationSnapshot{
				OptionID: "voice_ava", Provider: "qianwen",
				ProviderProfile: "qianwen_default", ProviderModel: "model",
				ProviderVoiceID: "voice", Locale: "en-US", BindingVersion: 1,
			},
		}}},
		avatarProviderStub{token: ProviderSessionToken{
			Value: "short-lived-token", ExpiresAt: now.Add(5 * time.Minute),
		}},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.now = func() time.Time { return now }
	return service
}
