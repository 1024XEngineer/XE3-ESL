package audiohttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestMessageAudioRoutes(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	audio := &testAudio{body: []byte("temporary-tts")}
	application := &testApplication{
		playback: objectstore.SignedGetResult{
			URL:       "https://private.example/audio.wav?signature=fake",
			ExpiresAt: now.Add(time.Minute),
		},
		synthesis: ai.SynthesisResult{Audio: audio},
	}
	router := newTestRouter(t, application, true)

	playback := performRequest(
		router, http.MethodGet,
		"/v1/agent-message-audios/audio-1/playback", "", "",
	)
	if playback.Code != http.StatusOK ||
		playback.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(playback.Body.String(), application.playback.URL) {
		t.Fatalf("playback response = %d %#v %s",
			playback.Code, playback.Header(), playback.Body)
	}

	speech := performRequest(
		router, http.MethodGet,
		"/v1/agent-messages/message-1/speech", "", "",
	)
	if speech.Code != http.StatusOK ||
		speech.Header().Get("Content-Type") != platformmedia.ContentTypeWAV ||
		speech.Header().Get("Cache-Control") != "no-store" ||
		speech.Body.String() != "temporary-tts" || !audio.closed {
		t.Fatalf("speech response = %d %#v %q, closed=%t",
			speech.Code, speech.Header(), speech.Body.String(), audio.closed)
	}
	if application.messageID != "message-1" || application.previewText != "" {
		t.Fatalf("speech command = %q, %q",
			application.messageID, application.previewText)
	}

	application.synthesis.Audio = &testAudio{body: []byte("preview-tts")}
	preview := performRequest(
		router, http.MethodPost,
		"/v1/agent-messages/message-2/speech-previews",
		`{"text":"Preview text."}`, "application/json",
	)
	if preview.Code != http.StatusOK || preview.Body.String() != "preview-tts" ||
		application.messageID != "message-2" ||
		application.previewText != "Preview text." {
		t.Fatalf("preview response = %d %q, command=%q/%q",
			preview.Code, preview.Body.String(),
			application.messageID, application.previewText)
	}

	deleted := performRequest(
		router, http.MethodDelete,
		"/v1/agent-message-audios/audio-1", "", "",
	)
	if deleted.Code != http.StatusNoContent || application.deletedID != "audio-1" {
		t.Fatalf("delete response = %d, deleted=%q",
			deleted.Code, application.deletedID)
	}
}

func TestMessageAudioRoutesRejectUnauthenticatedAndInvalidPreview(t *testing.T) {
	application := &testApplication{}
	unauthenticated := newTestRouter(t, application, false)
	response := performRequest(
		unauthenticated, http.MethodGet,
		"/v1/agent-message-audios/audio-1/playback", "", "",
	)
	if response.Code != http.StatusUnauthorized ||
		response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("unauthenticated response = %d %#v %s",
			response.Code, response.Header(), response.Body)
	}

	authenticated := newTestRouter(t, application, true)
	invalid := performRequest(
		authenticated, http.MethodPost,
		"/v1/agent-messages/message-1/speech-previews",
		`{"text":" padded "}`, "application/json",
	)
	if invalid.Code != http.StatusBadRequest || application.synthesisCalls != 0 {
		t.Fatalf("invalid preview = %d %s, calls=%d",
			invalid.Code, invalid.Body, application.synthesisCalls)
	}
}

func TestMessageAudioMapsSpeechQuotaFailure(t *testing.T) {
	application := &testApplication{err: ai.NewSpeechError(
		ai.SpeechOperationSynthesis,
		ai.ErrorQuotaExhausted,
		429,
		"quota",
		"provider-request",
		errors.New("provider detail"),
	)}
	response := performRequest(
		newTestRouter(t, application, true),
		http.MethodGet,
		"/v1/agent-messages/message-1/speech",
		"",
		"",
	)
	if response.Code != http.StatusServiceUnavailable ||
		response.Header().Get("Retry-After") != "" {
		t.Fatalf("quota response = %d %#v %s",
			response.Code, response.Header(), response.Body)
	}
	var payload httpresponse.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code != "quota_exhausted" || payload.Error.Retryable {
		t.Fatalf("quota error = %#v", payload.Error)
	}
}

type testApplication struct {
	playback       objectstore.SignedGetResult
	synthesis      ai.SynthesisResult
	err            error
	messageID      string
	previewText    string
	deletedID      string
	synthesisCalls int
}

func (application *testApplication) Playback(
	context.Context,
	requestcontext.Actor,
	string,
) (objectstore.SignedGetResult, error) {
	return application.playback, application.err
}

func (application *testApplication) DeleteAudio(
	_ context.Context,
	_ requestcontext.Actor,
	audioID string,
) error {
	application.deletedID = audioID
	return application.err
}

func (application *testApplication) SynthesizeMessage(
	_ context.Context,
	_ requestcontext.Actor,
	messageID string,
	previewText string,
) (ai.SynthesisResult, error) {
	application.synthesisCalls++
	application.messageID = messageID
	application.previewText = previewText
	return application.synthesis, application.err
}

type testAudio struct {
	body   []byte
	closed bool
}

func (audio *testAudio) Open() (io.ReadCloser, error) {
	if audio.closed {
		return nil, platformmedia.ErrAudioClosed
	}
	return io.NopCloser(bytes.NewReader(audio.body)), nil
}

func (*testAudio) MediaType() string       { return platformmedia.ContentTypeWAV }
func (audio *testAudio) Size() int64       { return int64(len(audio.body)) }
func (*testAudio) Duration() time.Duration { return 100 * time.Millisecond }
func (*testAudio) SampleRate() int         { return 24_000 }
func (audio *testAudio) Close() error      { audio.closed = true; return nil }

func newTestRouter(
	t *testing.T,
	application Application,
	authenticated bool,
) http.Handler {
	t.Helper()
	handler, err := NewHandler(
		application,
		httpresponse.NewRenderer(func() string { return "corr_audio_test" }),
	)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes := router.Group("")
	if authenticated {
		routes.Use(func(c *gin.Context) {
			actor := requestcontext.Actor{UserID: "user-a", SessionID: "session-a"}
			c.Request = c.Request.WithContext(
				requestcontext.WithActor(c.Request.Context(), actor),
			)
			c.Next()
		})
	}
	handler.RegisterRoutes(routes)
	return router
}

func performRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	contentType string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

var _ Application = (*testApplication)(nil)
var _ platformmedia.ManagedAudioSource = (*testAudio)(nil)
