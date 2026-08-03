package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestAgentVoiceInputHTTPVerticalContract(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	application := &voiceInputHTTPApplication{
		candidate: agentvoice.Candidate{
			ID:               "30000000-0000-4000-8000-000000000001",
			OwnerID:          "user-a",
			ThreadID:         "thread-1",
			ObjectKey:        "audio/v1/agent/private-server-key.wav",
			ContentType:      platformmedia.ContentTypeWAV,
			Size:             3244,
			ChecksumSHA256:   strings.Repeat("a", 64),
			Duration:         100 * time.Millisecond,
			SampleRate:       16_000,
			Status:           agentvoice.StatusReady,
			ASRAttempt:       1,
			CandidateVersion: 1,
			ASRRequestID:     "fake-asr-request-1",
			ASRProvider:      "fake",
			ASRModel:         "fake-asr-model",
			ASRCandidateText: "Provider candidate text.",
			ASRLanguage:      "en",
			ExpiresAt:        now.Add(24 * time.Hour),
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		now: now,
	}
	router := newAgentVoiceInputHTTPRouter(t, application)

	upload := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/agent-threads/thread-1/voice-message-candidates",
		voiceTestWAV(0x55),
		map[string]string{
			"Content-Type":    platformmedia.ContentTypeWAV,
			"Idempotency-Key": "voice-http-upload-1",
		},
	)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body = %s", upload.Code, upload.Body)
	}
	candidate := decodeVoiceJSONObject(t, upload)
	if candidate["candidate_id"] != application.candidate.ID ||
		candidate["status"] != string(agentvoice.StatusReady) ||
		candidate["candidate_version"] != float64(1) ||
		!strings.Contains(upload.Body.String(), "Provider candidate text.") ||
		strings.Contains(upload.Body.String(), "private-server-key") ||
		strings.Contains(upload.Body.String(), "checksum") ||
		strings.Contains(upload.Body.String(), "owner") {
		t.Fatalf("unsafe candidate response = %#v", candidate)
	}
	if application.uploadThreadID != "thread-1" ||
		application.uploadKey != "voice-http-upload-1" ||
		application.uploadBytes == 0 {
		t.Fatalf("upload command = %#v", application)
	}

	streamed := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/agent-threads/thread-1/voice-message-candidates/stream",
		voiceTestWAV(0x56),
		map[string]string{
			"Content-Type":    platformmedia.ContentTypeWAV,
			"Idempotency-Key": "voice-http-stream-1",
		},
	)
	if streamed.Code != http.StatusOK ||
		!strings.HasPrefix(
			streamed.Header().Get("Content-Type"),
			"text/event-stream",
		) {
		t.Fatalf("stream status = %d headers = %#v body = %s",
			streamed.Code,
			streamed.Header(),
			streamed.Body,
		)
	}
	streamBody := streamed.Body.String()
	started := strings.Index(streamBody, "event: transcription.started")
	updated := strings.Index(streamBody, "event: transcription.updated")
	ready := strings.Index(streamBody, "event: candidate.ready")
	if started < 0 || updated <= started || ready <= updated ||
		strings.Count(streamBody, "event: transcription.updated") != 2 ||
		!strings.Contains(streamBody, `"transcript":"Provider candidate"`) ||
		!strings.Contains(streamBody, `"final":false`) ||
		!strings.Contains(streamBody, `"final":true`) ||
		application.streamCalls != 1 ||
		strings.Contains(streamBody, "private-server-key") {
		t.Fatalf("unsafe or unordered stream = %s", streamBody)
	}

	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{
			method: http.MethodGet,
			path: "/v1/agent-voice-message-candidates/" +
				application.candidate.ID,
			status: http.StatusOK,
		},
		{
			method: http.MethodPost,
			path: "/v1/agent-voice-message-candidates/" +
				application.candidate.ID + "/retries",
			status: http.StatusOK,
		},
	} {
		response := voiceHTTPRequest(
			t,
			router,
			test.method,
			test.path,
			nil,
			nil,
		)
		if response.Code != test.status {
			t.Fatalf(
				"%s %s status = %d body = %s",
				test.method,
				test.path,
				response.Code,
				response.Body,
			)
		}
	}

	confirm := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/agent-voice-message-candidates/"+
			application.candidate.ID+
			"/confirmations",
		[]byte(`{
			"candidate_version": 1,
			"client_message_id": "voice-client-message-1",
			"confirmed_text": "User confirmed canonical text."
		}`),
		map[string]string{"Content-Type": "application/json"},
	)
	if confirm.Code != http.StatusCreated {
		t.Fatalf("confirm status = %d body = %s", confirm.Code, confirm.Body)
	}
	confirmed := decodeVoiceJSONObject(t, confirm)
	message := confirmed["message"].(map[string]any)
	audio := message["audio"].(map[string]any)
	if message["modality"] != string(agentconversation.MessageModalityVoice) ||
		message["content"] != "User confirmed canonical text." ||
		audio["status"] != string(agentconversation.MessageAudioReadable) ||
		audio["playback_path"] !=
			"/v1/agent-message-audios/50000000-0000-4000-8000-000000000001/playback" ||
		strings.Contains(confirm.Body.String(), "object_key") ||
		strings.Contains(confirm.Body.String(), "checksum") {
		t.Fatalf("confirmation response = %#v", confirmed)
	}
	if application.confirmCommand.CandidateVersion != 1 ||
		application.confirmCommand.ClientMessageID !=
			"voice-client-message-1" ||
		application.confirmCommand.ConfirmedText !=
			"User confirmed canonical text." {
		t.Fatalf("confirmation command = %#v", application.confirmCommand)
	}

	playback := voiceHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/agent-message-audios/"+
			"50000000-0000-4000-8000-000000000001/playback",
		nil,
		nil,
	)
	if playback.Code != http.StatusOK ||
		playback.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(playback.Body.String(), "https://private.example/") {
		t.Fatalf("playback response = %d %#v %s",
			playback.Code,
			playback.Header(),
			playback.Body,
		)
	}

	speech := voiceHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/agent-messages/"+
			"70000000-0000-4000-8000-000000000001/speech",
		nil,
		nil,
	)
	if speech.Code != http.StatusOK ||
		speech.Header().Get("Content-Type") != platformmedia.ContentTypeWAV ||
		speech.Header().Get("Cache-Control") != "no-store" ||
		speech.Body.String() != "temporary-tts" {
		t.Fatalf("speech response = %d %#v %q",
			speech.Code,
			speech.Header(),
			speech.Body.String(),
		)
	}

	for _, path := range []string{
		"/v1/agent-voice-message-candidates/" +
			application.candidate.ID,
		"/v1/agent-message-audios/" +
			"50000000-0000-4000-8000-000000000001",
	} {
		response := voiceHTTPRequest(
			t,
			router,
			http.MethodDelete,
			path,
			nil,
			nil,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s status = %d body = %s",
				path,
				response.Code,
				response.Body,
			)
		}
	}
	if application.deleteCandidateCalls != 1 ||
		application.deleteAudioCalls != 1 {
		t.Fatalf(
			"delete calls = candidate %d audio %d",
			application.deleteCandidateCalls,
			application.deleteAudioCalls,
		)
	}
}

func TestAgentVoiceInputHTTPMapsStaleConfirmationAndRequiresAuth(
	t *testing.T,
) {
	application := &voiceInputHTTPApplication{
		confirmErr: agentvoice.ErrCandidateStale,
	}
	router := newAgentVoiceInputHTTPRouter(t, application)
	path := "/v1/agent-voice-message-candidates/" +
		"30000000-0000-4000-8000-000000000001/confirmations"
	body := []byte(`{
		"candidate_version": 1,
		"client_message_id": "voice-client-message-1",
		"confirmed_text": "Confirmed text."
	}`)
	stale := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		path,
		body,
		map[string]string{"Content-Type": "application/json"},
	)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status = %d body = %s", stale.Code, stale.Body)
	}
	failure := decodeVoiceJSONObject(t, stale)["error"].(map[string]any)
	if failure["code"] != "resource_conflict" ||
		failure["retryable"] != false {
		t.Fatalf("stale error = %#v", failure)
	}

	unauthenticated := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(unauthenticated, request)
	if unauthenticated.Code != http.StatusUnauthorized ||
		application.confirmCalls != 1 {
		t.Fatalf(
			"unauthenticated status = %d confirm calls = %d",
			unauthenticated.Code,
			application.confirmCalls,
		)
	}
}

func TestAgentMessageResponsePreservesPlainTextShapeAndProjectsVoice(
	t *testing.T,
) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	text := messageResponse(agentconversation.Message{
		ID:        "70000000-0000-4000-8000-000000000001",
		ThreadID:  "thread-1",
		Sequence:  1,
		Role:      agentconversation.MessageRoleUser,
		Modality:  agentconversation.MessageModalityText,
		Content:   "Plain text remains backward compatible.",
		CreatedAt: now,
	})
	if _, exists := text["modality"]; exists {
		t.Fatalf("plain text response added modality: %#v", text)
	}
	if _, exists := text["audio"]; exists {
		t.Fatalf("plain text response added audio: %#v", text)
	}

	audio := agentconversation.MessageAudio{
		ID:        "50000000-0000-4000-8000-000000000001",
		MessageID: "70000000-0000-4000-8000-000000000002",
		Status:    agentconversation.MessageAudioReadable,
		Duration:  100 * time.Millisecond,
	}
	voice := messageResponse(agentconversation.Message{
		ID:        audio.MessageID,
		ThreadID:  "thread-1",
		Sequence:  2,
		Role:      agentconversation.MessageRoleUser,
		Modality:  agentconversation.MessageModalityVoice,
		Content:   "Confirmed voice transcript.",
		Audio:     &audio,
		CreatedAt: now,
	})
	if voice["modality"] != agentconversation.MessageModalityVoice {
		t.Fatalf("voice modality = %#v", voice["modality"])
	}
	if _, exists := voice["audio"]; !exists {
		t.Fatalf("voice response omitted audio: %#v", voice)
	}
}

func newAgentVoiceInputHTTPRouter(
	t *testing.T,
	application agentvoice.Application,
) http.Handler {
	t.Helper()
	handler, err := NewHTTPHandlerWithRunsVoiceAndAudio(
		voiceHTTPApplication{},
		nil,
		nil,
		nil,
		voiceHTTPMatters{},
		voiceHTTPAuthenticator{},
		func() string { return "corr_agent_voice_message" },
		VoiceHTTPOptions{VoiceInput: application},
	)
	if err != nil {
		t.Fatalf("new Agent VoiceInput HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}

type voiceInputHTTPApplication struct {
	candidate            agentvoice.Candidate
	now                  time.Time
	uploadThreadID       string
	uploadKey            string
	uploadBytes          int
	confirmCommand       agentvoice.ConfirmCandidateCommand
	confirmErr           error
	confirmCalls         int
	deleteCandidateCalls int
	deleteAudioCalls     int
	streamCalls          int
}

func (application *voiceInputHTTPApplication) Upload(
	_ context.Context,
	actor requestcontext.Actor,
	request agentvoice.UploadRequest,
) (agentvoice.Candidate, error) {
	if actor.UserID != "user-a" {
		return agentvoice.Candidate{}, agentvoice.ErrNotFound
	}
	body, err := io.ReadAll(request.Audio)
	if err != nil {
		return agentvoice.Candidate{}, err
	}
	application.uploadThreadID = request.ThreadID
	application.uploadKey = request.IdempotencyKey
	application.uploadBytes = len(body)
	return application.candidate, nil
}

func (application *voiceInputHTTPApplication) UploadStream(
	ctx context.Context,
	actor requestcontext.Actor,
	request agentvoice.UploadRequest,
	observer ai.TranscriptionObserver,
) (agentvoice.Candidate, error) {
	application.streamCalls++
	if err := observer.OnTranscriptionUpdate(ctx, ai.TranscriptionUpdate{
		Transcript: "Provider candidate",
	}); err != nil {
		return agentvoice.Candidate{}, err
	}
	if err := observer.OnTranscriptionUpdate(ctx, ai.TranscriptionUpdate{
		Transcript: "Provider candidate",
		Final:      true,
	}); err != nil {
		return agentvoice.Candidate{}, err
	}
	return application.Upload(ctx, actor, request)
}

func (application *voiceInputHTTPApplication) GetCandidate(
	context.Context,
	requestcontext.Actor,
	string,
) (agentvoice.Candidate, error) {
	return application.candidate, nil
}

func (application *voiceInputHTTPApplication) Retry(
	context.Context,
	requestcontext.Actor,
	string,
) (agentvoice.Candidate, error) {
	return application.candidate, nil
}

func (application *voiceInputHTTPApplication) Confirm(
	_ context.Context,
	_ requestcontext.Actor,
	command agentvoice.ConfirmCandidateCommand,
) (agentvoice.Confirmation, error) {
	application.confirmCalls++
	application.confirmCommand = command
	if application.confirmErr != nil {
		return agentvoice.Confirmation{}, application.confirmErr
	}
	audio := agentconversation.MessageAudio{
		ID:          "50000000-0000-4000-8000-000000000001",
		ContentType: platformmedia.ContentTypeWAV,
		Size:        3244,
		Duration:    100 * time.Millisecond,
		Status:      agentconversation.MessageAudioReadable,
	}
	message := agentconversation.Message{
		ID:              "40000000-0000-4000-8000-000000000001",
		ThreadID:        "thread-1",
		Sequence:        1,
		Role:            agentconversation.MessageRoleUser,
		ClientMessageID: command.ClientMessageID,
		Modality:        agentconversation.MessageModalityVoice,
		Content:         command.ConfirmedText,
		Audio:           &audio,
		CreatedAt:       application.now,
	}
	candidate := application.candidate
	candidate.Status = agentvoice.StatusConfirmed
	candidate.ConfirmedMessageID = message.ID
	candidate.ConfirmedRunID = "60000000-0000-4000-8000-000000000001"
	candidate.MessageAudioID = audio.ID
	candidate.ConfirmedAt = application.now
	return agentvoice.Confirmation{
		Candidate: candidate,
		Message:   message,
		Audio:     audio,
		Run: agentrun.Run{
			ID:                   candidate.ConfirmedRunID,
			ThreadID:             "thread-1",
			InputMessageID:       message.ID,
			Attempt:              1,
			Status:               agentrun.StatusCompleted,
			RequestedProvider:    "fake",
			RequestedModel:       "fake-free-model",
			MaxOutputTokens:      128,
			AssistantMessageID:   "70000000-0000-4000-8000-000000000001",
			ProviderCompletionID: "fake-completion-1",
			ProviderModel:        "fake-free-model",
			FinishReason:         "stop",
			CreatedAt:            application.now,
			StartedAt:            application.now,
			CompletedAt:          application.now,
			UpdatedAt:            application.now,
		},
		Created: true,
	}, nil
}

func (application *voiceInputHTTPApplication) Playback(
	context.Context,
	requestcontext.Actor,
	string,
) (objectstore.SignedGetResult, error) {
	return objectstore.SignedGetResult{
		URL:       "https://private.example/audio.wav?signature=fake",
		ExpiresAt: application.now.Add(time.Minute),
	}, nil
}

func (application *voiceInputHTTPApplication) DeleteCandidate(
	context.Context,
	requestcontext.Actor,
	string,
) error {
	application.deleteCandidateCalls++
	return nil
}

func (application *voiceInputHTTPApplication) DeleteAudio(
	context.Context,
	requestcontext.Actor,
	string,
) error {
	application.deleteAudioCalls++
	return nil
}

func (*voiceInputHTTPApplication) SynthesizeMessage(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (ai.SynthesisResult, error) {
	return ai.SynthesisResult{
		Audio: &voiceInputHTTPAudio{body: []byte("temporary-tts")},
	}, nil
}

type voiceInputHTTPAudio struct {
	body   []byte
	closed bool
}

func (audio *voiceInputHTTPAudio) Open() (io.ReadCloser, error) {
	if audio.closed {
		return nil, platformmedia.ErrAudioClosed
	}
	return io.NopCloser(bytes.NewReader(audio.body)), nil
}

func (*voiceInputHTTPAudio) MediaType() string {
	return platformmedia.ContentTypeWAV
}

func (audio *voiceInputHTTPAudio) Size() int64 {
	return int64(len(audio.body))
}

func (*voiceInputHTTPAudio) Duration() time.Duration {
	return 100 * time.Millisecond
}

func (*voiceInputHTTPAudio) SampleRate() int {
	return 24_000
}

func (audio *voiceInputHTTPAudio) Close() error {
	if audio.closed {
		return errors.New("audio already closed")
	}
	audio.closed = true
	return nil
}

var _ agentvoice.Application = (*voiceInputHTTPApplication)(nil)
var _ platformmedia.ManagedAudioSource = (*voiceInputHTTPAudio)(nil)
