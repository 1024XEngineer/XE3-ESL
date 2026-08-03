package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestImageAssetHTTPUploadContentAndDelete(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	images := &imageHTTPApplication{
		asset: agentimage.Asset{
			ID:          "30000000-0000-4000-8000-000000000001",
			OwnerID:     "user-a",
			ThreadID:    "thread-1",
			ContentType: "image/png",
			Size:        4,
			Width:       2,
			Height:      2,
			Status:      agentimage.StatusStaged,
			CreatedAt:   now,
		},
		content: objectstore.SignedGetResult{
			URL:       "https://objects.invalid/image.png?signature=safe",
			ExpiresAt: now.Add(2 * time.Minute),
		},
	}
	router := newImageHTTPRouter(t, images)

	upload := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-threads/thread-1/image-assets",
		bytes.NewReader([]byte{0x89, 'P', 'N', 'G'}),
	)
	upload.Header.Set("Authorization", "Bearer image-token-a")
	upload.Header.Set("Idempotency-Key", "image-request-1")
	upload.Header.Set("Content-Type", "image/png")
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf(
			"upload status = %d, body = %s",
			uploadResponse.Code,
			uploadResponse.Body.String(),
		)
	}
	if images.upload.ThreadID != "thread-1" ||
		images.upload.IdempotencyKey != "image-request-1" ||
		images.upload.ContentType != "image/png" ||
		!bytes.Equal(images.uploadBody, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf(
			"upload = %#v, body = %x",
			images.upload,
			images.uploadBody,
		)
	}
	var uploaded map[string]any
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded["image_asset_id"] != images.asset.ID ||
		uploaded["thread_id"] != "thread-1" {
		t.Fatalf("upload response = %#v", uploaded)
	}

	content := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-image-assets/"+images.asset.ID+"/content",
		nil,
	)
	content.Header.Set("Authorization", "Bearer image-token-a")
	contentResponse := httptest.NewRecorder()
	router.ServeHTTP(contentResponse, content)
	if contentResponse.Code != http.StatusOK ||
		contentResponse.Header().Get("Cache-Control") != "no-store" ||
		images.contentAssetID != images.asset.ID {
		t.Fatalf(
			"content status = %d, headers = %#v, asset = %q",
			contentResponse.Code,
			contentResponse.Header(),
			images.contentAssetID,
		)
	}

	deleteRequest := httptest.NewRequest(
		http.MethodDelete,
		"/v1/agent-image-assets/"+images.asset.ID,
		nil,
	)
	deleteRequest.Header.Set("Authorization", "Bearer image-token-a")
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent ||
		images.deletedAssetID != images.asset.ID {
		t.Fatalf(
			"delete status = %d, asset = %q",
			deleteResponse.Code,
			images.deletedAssetID,
		)
	}
}

func TestImageAssetHTTPRejectsInvalidEnvelopeBeforeApplication(t *testing.T) {
	images := &imageHTTPApplication{}
	router := newImageHTTPRouter(t, images)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-threads/thread-1/image-assets",
		bytes.NewReader([]byte("not-an-image")),
	)
	request.Header.Set("Authorization", "Bearer image-token-a")
	request.Header.Set("Idempotency-Key", "short")
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if images.uploadCalls != 0 {
		t.Fatalf("upload calls = %d", images.uploadCalls)
	}
}

func TestImageAssetHTTPUsesStableValidationErrors(t *testing.T) {
	tests := []struct {
		name          string
		contentType   string
		contentLength int64
		application   *imageHTTPApplication
		wantStatus    int
		wantCode      string
	}{
		{
			name:        "unsupported content type",
			contentType: "image/gif",
			application: &imageHTTPApplication{},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "unsupported_image_format",
		},
		{
			name:          "oversized content length",
			contentType:   "image/png",
			contentLength: agentimage.MaxBytes + 1,
			application:   &imageHTTPApplication{},
			wantStatus:    http.StatusRequestEntityTooLarge,
			wantCode:      "image_too_large",
		},
		{
			name:        "invalid decoded image",
			contentType: "image/png",
			application: &imageHTTPApplication{uploadErr: agentimage.ErrInvalid},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_image",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newImageHTTPRouter(t, test.application)
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/agent-threads/thread-1/image-assets",
				bytes.NewReader([]byte("fixture")),
			)
			request.Header.Set("Authorization", "Bearer image-token-a")
			request.Header.Set("Idempotency-Key", "image-request-1")
			request.Header.Set("Content-Type", test.contentType)
			if test.contentLength > 0 {
				request.ContentLength = test.contentLength
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus ||
				!strings.Contains(
					response.Body.String(),
					`"code":"`+test.wantCode+`"`,
				) {
				t.Fatalf(
					"status = %d, body = %s",
					response.Code,
					response.Body.String(),
				)
			}
		})
	}
}

func TestSubmitRunAcceptsValidatedImageAssetIDs(t *testing.T) {
	t.Parallel()

	const (
		threadID = "20000000-0000-4000-8000-000000000001"
		imageID  = "30000000-0000-4000-8000-000000000001"
	)
	runs := &imageHTTPRuns{}
	handler, err := NewHTTPHandlerWithRuns(
		imageHTTPThreads{},
		runs,
		imageHTTPMatters{},
		imageHTTPAuthenticator{},
		func() string { return "corr_multimodal_run" },
	)
	if err != nil {
		t.Fatalf("new run handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/runs",
		bytes.NewBufferString(`{
			"client_message_id": "multimodal-message-1",
			"content": "Review this image.",
			"image_asset_ids": ["`+imageID+`"]
		}`),
	)
	request.Header.Set("Authorization", "Bearer image-token-a")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if runs.threadID != threadID ||
		len(runs.imageAssetIDs) != 1 ||
		runs.imageAssetIDs[0] != imageID {
		t.Fatalf(
			"thread = %q, image IDs = %#v",
			runs.threadID,
			runs.imageAssetIDs,
		)
	}
}

func TestSubmitRunTreatsEmptyImageAssetIDsAsText(t *testing.T) {
	t.Parallel()

	runs := &imageHTTPRuns{}
	handler, err := NewHTTPHandlerWithRuns(
		imageHTTPThreads{},
		runs,
		imageHTTPMatters{},
		imageHTTPAuthenticator{},
		func() string { return "corr_text_run" },
	)
	if err != nil {
		t.Fatalf("new run handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-threads/20000000-0000-4000-8000-000000000001/runs",
		bytes.NewBufferString(`{
			"client_message_id": "text-message-1",
			"content": "Text only.",
			"image_asset_ids": []
		}`),
	)
	request.Header.Set("Authorization", "Bearer image-token-a")
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

func newImageHTTPRouter(
	t *testing.T,
	images agentimage.Application,
) http.Handler {
	t.Helper()
	handler, err := NewHTTPHandlerWithRunsVoiceAudioAndImages(
		imageHTTPThreads{},
		nil,
		nil,
		nil,
		images,
		imageHTTPMatters{},
		imageHTTPAuthenticator{},
		func() string { return "corr_agent_image" },
	)
	if err != nil {
		t.Fatalf("new image HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}

type imageHTTPApplication struct {
	asset          agentimage.Asset
	content        objectstore.SignedGetResult
	upload         agentimage.UploadRequest
	uploadBody     []byte
	uploadCalls    int
	contentAssetID string
	deletedAssetID string
	uploadErr      error
}

type imageHTTPRuns struct {
	agentrun.Application
	threadID      string
	imageAssetIDs []string
	textCalls     int
}

func (runs *imageHTTPRuns) SubmitText(
	_ context.Context,
	_ requestcontext.Actor,
	threadID string,
	_ string,
	_ string,
) (agentrun.Submission, error) {
	runs.textCalls++
	return imageHTTPRunSubmission(threadID), nil
}

func (runs *imageHTTPRuns) SubmitWithImages(
	_ context.Context,
	_ requestcontext.Actor,
	threadID string,
	_ string,
	_ string,
	imageAssetIDs []string,
) (agentrun.Submission, error) {
	runs.threadID = threadID
	runs.imageAssetIDs = append([]string(nil), imageAssetIDs...)
	return imageHTTPRunSubmission(threadID), nil
}

func imageHTTPRunSubmission(threadID string) agentrun.Submission {
	return agentrun.Submission{
		Run: agentrun.Run{
			ID:                "40000000-0000-4000-8000-000000000001",
			ThreadID:          threadID,
			InputMessageID:    "50000000-0000-4000-8000-000000000001",
			Attempt:           1,
			Status:            agentrun.StatusCompleted,
			RequestedProvider: "fake",
			RequestedModel:    "fake-multimodal",
			MaxOutputTokens:   256,
			CreatedAt: time.Date(
				2026,
				7,
				30,
				8,
				0,
				0,
				0,
				time.UTC,
			),
			UpdatedAt: time.Date(
				2026,
				7,
				30,
				8,
				0,
				1,
				0,
				time.UTC,
			),
			CompletedAt: time.Date(
				2026,
				7,
				30,
				8,
				0,
				1,
				0,
				time.UTC,
			),
			AssistantMessageID:   "60000000-0000-4000-8000-000000000001",
			ProviderCompletionID: "completion-1",
			ProviderModel:        "fake-multimodal",
			FinishReason:         "stop",
		},
		Created: true,
	}
}

func (application *imageHTTPApplication) Upload(
	_ context.Context,
	_ requestcontext.Actor,
	request agentimage.UploadRequest,
) (agentimage.Asset, error) {
	application.uploadCalls++
	application.upload = request
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return agentimage.Asset{}, err
	}
	application.uploadBody = body
	if application.uploadErr != nil {
		return agentimage.Asset{}, application.uploadErr
	}
	return application.asset, nil
}

func (application *imageHTTPApplication) Get(
	context.Context,
	requestcontext.Actor,
	string,
) (agentimage.Asset, error) {
	return application.asset, nil
}

func (application *imageHTTPApplication) Content(
	_ context.Context,
	_ requestcontext.Actor,
	assetID string,
) (objectstore.SignedGetResult, error) {
	application.contentAssetID = assetID
	return application.content, nil
}

func (application *imageHTTPApplication) Delete(
	_ context.Context,
	_ requestcontext.Actor,
	assetID string,
) error {
	application.deletedAssetID = assetID
	return nil
}

func (application *imageHTTPApplication) MessageAssets(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) ([]agentimage.Asset, error) {
	return []agentimage.Asset{application.asset}, nil
}

func (application *imageHTTPApplication) Attach(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	[]string,
) ([]agentimage.Asset, error) {
	return nil, nil
}

func (application *imageHTTPApplication) Reclaim(
	context.Context,
	int,
) (agentimage.CleanupResult, error) {
	return agentimage.CleanupResult{}, nil
}

type imageHTTPThreads struct {
	agentconversation.Application
}

func (imageHTTPThreads) GetThread(
	_ context.Context,
	actor requestcontext.Actor,
	threadID string,
) (agentconversation.Thread, error) {
	if actor.UserID != "user-a" || threadID != "thread-1" {
		return agentconversation.Thread{}, agentconversation.ErrNotFound
	}
	return agentconversation.Thread{ID: threadID, OwnerID: actor.UserID}, nil
}

type imageHTTPMatters struct {
	matter.Application
}

type imageHTTPAuthenticator struct{}

func (imageHTTPAuthenticator) AuthenticateSession(
	_ context.Context,
	token string,
) (requestcontext.Actor, error) {
	if token != "image-token-a" {
		return requestcontext.Actor{}, identity.ErrAuthenticationRequired
	}
	return requestcontext.Actor{
		UserID:    "user-a",
		SessionID: "image-session-a",
	}, nil
}
