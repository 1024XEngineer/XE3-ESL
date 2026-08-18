package avatar

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestAvatarHTTPUploadContentAndDefault(t *testing.T) {
	now := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC)
	application := &avatarApplicationStub{profile: identity.UserProfile{
		UserID: "user-1", DisplayName: "小林", ProfileVersion: 2,
		Avatar:    &identity.ProfileAvatar{Width: 512, Height: 512, UpdatedAt: now},
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}, content: objectstore.SignedGetResult{
		URL:       "https://objects.invalid/avatar.png?signature=safe",
		ExpiresAt: now.Add(time.Minute),
	}}
	router := avatarRouter(t, application)

	upload := httptest.NewRequest(
		http.MethodPost, "/v1/me/avatar", bytes.NewReader([]byte{1, 2, 3}),
	)
	upload.Header.Set("Content-Type", "image/png")
	upload.Header.Set("Idempotency-Key", "avatar-request-1")
	upload.Header.Set("If-Match", `"1"`)
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusOK ||
		application.upload.ExpectedProfileVersion != 1 ||
		application.upload.IdempotencyKey != "avatar-request-1" ||
		!bytes.Equal(application.uploadBody, []byte{1, 2, 3}) {
		t.Fatalf("upload response/request = %d / %#v", uploadResponse.Code, application.upload)
	}

	content := httptest.NewRequest(http.MethodGet, "/v1/me/avatar/content", nil)
	contentResponse := httptest.NewRecorder()
	router.ServeHTTP(contentResponse, content)
	if contentResponse.Code != http.StatusOK ||
		contentResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("content response = %d / %#v", contentResponse.Code, contentResponse.Header())
	}

	useDefault := httptest.NewRequest(http.MethodDelete, "/v1/me/avatar", nil)
	useDefault.Header.Set("If-Match", `"2"`)
	defaultResponse := httptest.NewRecorder()
	router.ServeHTTP(defaultResponse, useDefault)
	if defaultResponse.Code != http.StatusOK || application.defaultVersion != 2 {
		t.Fatalf("default response/version = %d / %d", defaultResponse.Code, application.defaultVersion)
	}
}

func TestAvatarHTTPRejectsMissingConcurrencyAndIdempotencyHeaders(t *testing.T) {
	application := &avatarApplicationStub{}
	router := avatarRouter(t, application)
	request := httptest.NewRequest(
		http.MethodPost, "/v1/me/avatar", bytes.NewReader([]byte{1}),
	)
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || application.uploadCalls != 0 {
		t.Fatalf("response/calls = %d / %d", response.Code, application.uploadCalls)
	}
}

func avatarRouter(t *testing.T, application Application) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewHandler(application, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(requestcontext.WithActor(
			c.Request.Context(),
			requestcontext.Actor{UserID: "user-1", SessionID: "session-1"},
		))
		c.Next()
	})
	handler.RegisterRoutes(router)
	return router
}

type avatarApplicationStub struct {
	profile        identity.UserProfile
	content        objectstore.SignedGetResult
	upload         UploadRequest
	uploadBody     []byte
	uploadCalls    int
	defaultVersion int64
}

func (stub *avatarApplicationStub) Upload(
	_ context.Context,
	_ requestcontext.Actor,
	request UploadRequest,
) (identity.UserProfile, error) {
	stub.uploadCalls++
	stub.upload = request
	stub.uploadBody, _ = io.ReadAll(request.Body)
	return stub.profile, nil
}

func (stub *avatarApplicationStub) UseDefault(
	_ context.Context,
	_ requestcontext.Actor,
	version int64,
) (identity.UserProfile, error) {
	stub.defaultVersion = version
	stub.profile.Avatar = nil
	stub.profile.ProfileVersion = version + 1
	return stub.profile, nil
}

func (stub *avatarApplicationStub) Content(
	context.Context,
	requestcontext.Actor,
) (objectstore.SignedGetResult, error) {
	return stub.content, nil
}
