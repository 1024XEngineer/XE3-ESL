package voicehttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/gorilla/websocket"
)

func TestRealtimeVoiceInputUsesAuthenticatedWebSocketFrames(t *testing.T) {
	now := time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)
	application := &voiceInputHTTPApplication{
		candidate: agentvoice.Candidate{
			ID:               "30000000-0000-4000-8000-000000000001",
			OwnerID:          "user-a",
			ThreadID:         "thread-1",
			ObjectKey:        "audio/v1/agent/private.wav",
			ContentType:      platformmedia.ContentTypeWAV,
			Size:             3244,
			ChecksumSHA256:   strings.Repeat("a", 64),
			Duration:         100 * time.Millisecond,
			SampleRate:       16_000,
			Status:           agentvoice.StatusReady,
			ASRAttempt:       1,
			CandidateVersion: 1,
			ASRRequestID:     "request-1",
			ASRProvider:      "fake",
			ASRModel:         "fake-asr",
			ASRCandidateText: "Provider candidate",
			ExpiresAt:        now.Add(24 * time.Hour),
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		now: now,
	}
	server := httptest.NewServer(newAgentVoiceInputHTTPRouter(t, application))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/v1/agent-threads/thread-1/voice-message-candidates/realtime"
	header := http.Header{"Authorization": []string{"Bearer voice-token-a"}}
	connection, response, err := (&websocket.Dialer{
		Subprotocols: []string{voiceInputWebSocketProtocol},
	}).Dial(endpoint, header)
	if err != nil {
		if response != nil {
			t.Fatalf("dial realtime voice status = %d: %v", response.StatusCode, err)
		}
		t.Fatalf("dial realtime voice: %v", err)
	}
	defer connection.Close()
	if connection.Subprotocol() != voiceInputWebSocketProtocol {
		t.Fatalf("subprotocol = %q", connection.Subprotocol())
	}
	if err := connection.WriteJSON(map[string]any{
		"type": "start", "idempotency_key": "voice-realtime-1",
		"sample_rate": 16_000,
	}); err != nil {
		t.Fatalf("write start: %v", err)
	}
	if err := connection.WriteMessage(
		websocket.BinaryMessage,
		make([]byte, 3_200),
	); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if err := connection.WriteJSON(map[string]string{"type": "finish"}); err != nil {
		t.Fatalf("write finish: %v", err)
	}
	var eventTypes []string
	for len(eventTypes) < 4 {
		_, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			t.Fatalf("read event %d: %v", len(eventTypes), readErr)
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		eventTypes = append(eventTypes, envelope.Type)
	}
	want := []string{
		"transcription.started",
		"transcription.updated",
		"transcription.updated",
		"candidate.ready",
	}
	for index := range want {
		if eventTypes[index] != want[index] {
			t.Fatalf("events = %#v", eventTypes)
		}
	}
	if application.uploadThreadID != "thread-1" ||
		application.uploadKey != "voice-realtime-1" ||
		application.uploadBytes != 3_244 {
		t.Fatalf("upload = %#v", application)
	}
}
