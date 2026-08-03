package runhttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestSubmitRunAcceptsValidatedImageAssetIDs(t *testing.T) {
	const (
		threadID = "20000000-0000-4000-8000-000000000001"
		imageID  = "30000000-0000-4000-8000-000000000001"
	)
	runs := &imageRunHTTPApplication{}
	router := newImageRunHTTPRouter(t, runs)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/runs",
		bytes.NewBufferString(`{
			"client_message_id": "multimodal-message-1",
			"content": "Review this image.",
			"image_asset_ids": ["`+imageID+`"]
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if runs.actor.UserID != "user-a" ||
		runs.threadID != threadID ||
		len(runs.imageAssetIDs) != 1 ||
		runs.imageAssetIDs[0] != imageID {
		t.Fatalf(
			"actor = %#v, thread = %q, image IDs = %#v",
			runs.actor,
			runs.threadID,
			runs.imageAssetIDs,
		)
	}
}

func TestSubmitRunTreatsEmptyImageAssetIDsAsText(t *testing.T) {
	runs := &imageRunHTTPApplication{}
	router := newImageRunHTTPRouter(t, runs)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-threads/20000000-0000-4000-8000-000000000001/runs",
		bytes.NewBufferString(`{
			"client_message_id": "text-message-1",
			"content": "Text only.",
			"image_asset_ids": []
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || runs.textCalls != 1 {
		t.Fatalf(
			"status = %d, text calls = %d, body = %s",
			response.Code,
			runs.textCalls,
			response.Body.String(),
		)
	}
}

func newImageRunHTTPRouter(
	t *testing.T,
	application agentrun.Application,
) http.Handler {
	t.Helper()
	handler, err := NewHandler(
		application,
		httpresponse.NewRenderer(func() string { return "corr_agent_run" }),
	)
	if err != nil {
		t.Fatalf("new run HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	authenticated := router.Group("")
	authenticated.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(requestcontext.WithActor(
			c.Request.Context(),
			requestcontext.Actor{
				UserID:    "user-a",
				SessionID: "image-session-a",
			},
		))
		c.Next()
	})
	handler.RegisterRoutes(authenticated)
	return router
}

type imageRunHTTPApplication struct {
	agentrun.Application
	actor         requestcontext.Actor
	threadID      string
	imageAssetIDs []string
	textCalls     int
}

func (application *imageRunHTTPApplication) SubmitText(
	_ context.Context,
	actor requestcontext.Actor,
	threadID string,
	_ string,
	_ string,
) (agentrun.Submission, error) {
	application.actor = actor
	application.textCalls++
	return imageRunHTTPSubmission(threadID), nil
}

func (application *imageRunHTTPApplication) SubmitWithImages(
	_ context.Context,
	actor requestcontext.Actor,
	threadID string,
	_ string,
	_ string,
	imageAssetIDs []string,
) (agentrun.Submission, error) {
	application.actor = actor
	application.threadID = threadID
	application.imageAssetIDs = append([]string(nil), imageAssetIDs...)
	return imageRunHTTPSubmission(threadID), nil
}

func imageRunHTTPSubmission(threadID string) agentrun.Submission {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	return agentrun.Submission{
		Run: agentrun.Run{
			ID:                   "40000000-0000-4000-8000-000000000001",
			ThreadID:             threadID,
			InputMessageID:       "50000000-0000-4000-8000-000000000001",
			Attempt:              1,
			Status:               agentrun.StatusCompleted,
			RequestedProvider:    "fake",
			RequestedModel:       "fake-multimodal",
			MaxOutputTokens:      256,
			CreatedAt:            now,
			UpdatedAt:            now.Add(time.Second),
			CompletedAt:          now.Add(time.Second),
			AssistantMessageID:   "60000000-0000-4000-8000-000000000001",
			ProviderCompletionID: "completion-1",
			ProviderModel:        "fake-multimodal",
			FinishReason:         "stop",
		},
		Created: true,
	}
}
