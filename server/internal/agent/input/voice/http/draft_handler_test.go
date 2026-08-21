package voicehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const (
	draftHTTPUserID    = "10000000-0000-4000-8000-000000000001"
	draftHTTPThreadID  = "20000000-0000-4000-8000-000000000001"
	draftHTTPDraftID   = "30000000-0000-4000-8000-000000000001"
	draftHTTPMessageID = "40000000-0000-4000-8000-000000000001"
	draftHTTPRunID     = "50000000-0000-4000-8000-000000000001"
)

func TestAgentVoiceDraftHTTPUsesDraftContractAndFourStateProjection(
	t *testing.T,
) {
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	application := &draftHTTPApplication{draft: agentvoice.Draft{
		ID: draftHTTPDraftID, OwnerID: draftHTTPUserID,
		ThreadID: draftHTTPThreadID, ContentType: "audio/wav",
		Size: 3244, Duration: 100 * time.Millisecond, SampleRate: 16_000,
		Status: agentvoice.StatusReady, ASRAttempt: 1, Version: 1,
		ASRRequestID: "asr-request-1", ASRProvider: "fake",
		ASRModel: "fake-asr-v1", Transcript: "Provider transcript.",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}}
	router := newDraftHTTPRouter(t, application)

	get := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-voice-drafts/"+draftHTTPDraftID,
		nil,
	)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get Draft status = %d: %s", getResponse.Code, getResponse.Body)
	}
	ready := decodeDraftHTTPObject(t, getResponse.Body.Bytes())
	transcript, ok := ready["transcript"].(map[string]any)
	if ready["draft_id"] != draftHTTPDraftID || ready["status"] != "ready" ||
		ready["draft_version"] != float64(1) || ready["expires_at"] == nil ||
		!ok || transcript["text"] != "Provider transcript." {
		t.Fatalf("ready Draft response = %#v", ready)
	}
	for _, forbidden := range []string{
		"candidate_id", "candidate_version", "candidate_text", "deleted_at",
	} {
		if _, found := ready[forbidden]; found {
			t.Fatalf("ready Draft exposed %q: %#v", forbidden, ready)
		}
	}

	confirm := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent-voice-drafts/"+draftHTTPDraftID+"/confirmations",
		bytes.NewBufferString(`{
            "draft_version":1,
            "client_message_id":"voice-message-1",
            "confirmed_text":"Edited transcript."
        }`),
	)
	confirm.Header.Set("Content-Type", "application/json")
	confirmResponse := httptest.NewRecorder()
	router.ServeHTTP(confirmResponse, confirm)
	if confirmResponse.Code != http.StatusAccepted {
		t.Fatalf(
			"confirm Draft status = %d: %s",
			confirmResponse.Code,
			confirmResponse.Body,
		)
	}
	var confirmedEnvelope map[string]any
	if err := json.Unmarshal(confirmResponse.Body.Bytes(), &confirmedEnvelope); err != nil {
		t.Fatalf("decode confirmation: %v", err)
	}
	confirmed, ok := confirmedEnvelope["draft"].(map[string]any)
	if !ok || confirmed["status"] != "confirmed" ||
		confirmed["message_audio_id"] != draftHTTPDraftID {
		t.Fatalf("confirmed Draft response = %#v", confirmedEnvelope)
	}
	if _, found := confirmed["expires_at"]; found {
		t.Fatalf("confirmed Draft retained expiry: %#v", confirmed)
	}
	if application.confirmed.DraftID != draftHTTPDraftID ||
		application.confirmed.Version != 1 ||
		application.confirmed.ClientMessageID != "voice-message-1" ||
		application.confirmed.ConfirmedText != "Edited transcript." {
		t.Fatalf("confirmation command = %#v", application.confirmed)
	}

	oldRoute := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-voice-message-candidates/"+draftHTTPDraftID,
		nil,
	)
	oldResponse := httptest.NewRecorder()
	router.ServeHTTP(oldResponse, oldRoute)
	if oldResponse.Code != http.StatusNotFound {
		t.Fatalf("removed Candidate route status = %d", oldResponse.Code)
	}
}

func newDraftHTTPRouter(t *testing.T, application *draftHTTPApplication) http.Handler {
	t.Helper()
	handler, err := NewHandler(
		application,
		draftHTTPThreadReader{},
		nil,
		time.Second,
		httpresponse.NewRenderer(func() string { return "corr_voice_draft" }),
	)
	if err != nil {
		t.Fatalf("new Draft handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(requestcontext.WithActor(
			c.Request.Context(),
			requestcontext.Actor{UserID: draftHTTPUserID, SessionID: "session-1"},
		))
		c.Next()
	})
	handler.RegisterRoutes(router)
	return router
}

func decodeDraftHTTPObject(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode Draft response: %v", err)
	}
	return object
}

type draftHTTPApplication struct {
	draft     agentvoice.Draft
	confirmed agentvoice.ConfirmDraftCommand
}

func (application *draftHTTPApplication) UploadStream(
	context.Context,
	requestcontext.Actor,
	agentvoice.UploadRequest,
	agentvoice.TranscriptionObserver,
) (agentvoice.Draft, error) {
	return application.draft, nil
}

func (application *draftHTTPApplication) UploadRecognized(
	context.Context,
	requestcontext.Actor,
	agentvoice.UploadRequest,
	agentvoice.TranscriptionResult,
) (agentvoice.Draft, error) {
	return application.draft, nil
}

func (application *draftHTTPApplication) GetDraft(
	context.Context,
	requestcontext.Actor,
	string,
) (agentvoice.Draft, error) {
	return application.draft, nil
}

func (application *draftHTTPApplication) Retry(
	context.Context,
	requestcontext.Actor,
	string,
) (agentvoice.Draft, error) {
	return application.draft, nil
}

func (application *draftHTTPApplication) Confirm(
	_ context.Context,
	_ requestcontext.Actor,
	command agentvoice.ConfirmDraftCommand,
) (agentvoice.Confirmation, error) {
	application.confirmed = command
	now := application.draft.UpdatedAt.Add(time.Second)
	confirmed := application.draft
	confirmed.Status = agentvoice.StatusConfirmed
	confirmed.ExpiresAt = time.Time{}
	confirmed.ConfirmedMessageID = draftHTTPMessageID
	confirmed.ConfirmedRunID = draftHTTPRunID
	confirmed.ConfirmedAt = now
	confirmed.UpdatedAt = now
	return agentvoice.Confirmation{
		Draft: confirmed,
		Message: conversation.Message{
			ID: draftHTTPMessageID, ThreadID: draftHTTPThreadID,
			Role: conversation.MessageRoleUser, Sequence: 1,
			ClientMessageID: command.ClientMessageID,
			Modality:        conversation.MessageModalityVoice,
			Content:         command.ConfirmedText, CreatedAt: now,
		},
		Attachment: conversation.AudioAttachment{
			ID: draftHTTPDraftID, MessageID: draftHTTPMessageID,
			ContentType: "audio/wav", Size: 3244,
			Duration: 100 * time.Millisecond, CreatedAt: now,
		},
		Run: agentrun.Run{
			ID: draftHTTPRunID, ThreadID: draftHTTPThreadID,
			InputMessageID: draftHTTPMessageID, Attempt: 1,
			Status: agentrun.StatusPending, RequestedProvider: "fake",
			RequestedModel: "fake-text-v1", MaxOutputTokens: 256,
			CreatedAt: now, UpdatedAt: now,
		},
		Created: true,
	}, nil
}

func (application *draftHTTPApplication) ConfirmStream(
	ctx context.Context,
	actor requestcontext.Actor,
	command agentvoice.ConfirmDraftCommand,
	_ agentvoice.ConfirmationStreamObserver,
) (agentvoice.Confirmation, error) {
	return application.Confirm(ctx, actor, command)
}

func (*draftHTTPApplication) DeleteDraft(
	context.Context,
	requestcontext.Actor,
	string,
) error {
	return nil
}

type draftHTTPThreadReader struct{}

func (draftHTTPThreadReader) GetThread(
	context.Context,
	requestcontext.Actor,
	string,
) (conversation.Thread, error) {
	return conversation.Thread{ID: draftHTTPThreadID, OwnerID: draftHTTPUserID}, nil
}
