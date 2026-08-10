package voicehttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const ephemeralTestThreadID = "10000000-0000-4000-8000-000000000001"

func TestEphemeralTranscriptionStreamsBeforeFinishWithoutVoicePersistence(
	t *testing.T,
) {
	recognizer := newEphemeralTestRecognizer()
	legacy := &voiceInputHTTPApplication{}
	server := httptest.NewServer(newEphemeralHTTPTestRouter(
		t,
		recognizer,
		&ephemeralThreadReader{},
		legacy,
		time.Second,
	))
	defer server.Close()

	connection := dialEphemeralTranscription(
		t,
		server.URL,
		ephemeralTestThreadID,
		"Bearer voice-token-a",
		voiceInputWebSocketProtocol,
	)
	defer connection.Close()
	writeEphemeralStart(t, connection, "ephemeral-voice-1", 16_000)
	started := readEphemeralEvent(t, connection)
	var startedData map[string]any
	if err := json.Unmarshal(started.Data, &startedData); err != nil ||
		started.Type != "transcription.started" || len(startedData) != 0 {
		t.Fatalf("started event = %#v", started)
	}

	pcm := make([]byte, 3_200)
	if err := connection.WriteMessage(websocket.BinaryMessage, pcm); err != nil {
		t.Fatalf("write PCM: %v", err)
	}
	// The client has deliberately not sent finish yet. Receiving this event
	// proves the provider consumes the live io.Pipe instead of a buffered WAV.
	updated := readEphemeralEvent(t, connection)
	assertEphemeralTranscriptEvent(
		t,
		updated,
		"transcription.updated",
		"partial transcript",
		false,
	)
	if err := connection.WriteJSON(map[string]string{"type": "finish"}); err != nil {
		t.Fatalf("write finish: %v", err)
	}
	completed := readEphemeralEvent(t, connection)
	assertEphemeralTranscriptEvent(
		t,
		completed,
		"transcription.completed",
		"completed transcript",
		true,
	)
	if recognizer.Calls() != 1 || recognizer.Bytes() != int64(len(pcm)) {
		t.Fatalf(
			"recognizer calls = %d bytes = %d",
			recognizer.Calls(),
			recognizer.Bytes(),
		)
	}
	if legacy.streamCalls != 0 || legacy.uploadBytes != 0 ||
		legacy.confirmCalls != 0 || legacy.deleteCandidateCalls != 0 {
		t.Fatalf("durable Voice application was called: %#v", legacy)
	}
}

func TestEphemeralTranscriptionRejectsAuthenticationOwnershipAndProtocol(
	t *testing.T,
) {
	recognizer := newEphemeralTestRecognizer()
	threads := &ephemeralThreadReader{}
	server := httptest.NewServer(newEphemeralHTTPTestRouter(
		t,
		recognizer,
		threads,
		nil,
		time.Second,
	))
	defer server.Close()
	endpoint := ephemeralWebSocketEndpoint(server.URL, ephemeralTestThreadID)

	tests := []struct {
		name       string
		token      string
		protocols  []string
		wantStatus int
	}{
		{"authentication", "", []string{voiceInputWebSocketProtocol}, http.StatusUnauthorized},
		{"ownership", "Bearer voice-token-b", []string{voiceInputWebSocketProtocol}, http.StatusNotFound},
		{"missing protocol", "Bearer voice-token-a", nil, http.StatusBadRequest},
		{"wrong protocol", "Bearer voice-token-a", []string{"speakup.other.v1"}, http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{}
			if test.token != "" {
				header.Set("Authorization", test.token)
			}
			connection, response, err := (&websocket.Dialer{
				Subprotocols: test.protocols,
			}).Dial(endpoint, header)
			if connection != nil {
				_ = connection.Close()
			}
			if err == nil || response == nil ||
				response.StatusCode != test.wantStatus {
				t.Fatalf(
					"dial error = %v status = %#v, want %d",
					err,
					response,
					test.wantStatus,
				)
			}
		})
	}
	if recognizer.Calls() != 0 {
		t.Fatalf("recognizer calls = %d", recognizer.Calls())
	}
}

func TestEphemeralTranscriptionValidatesStartAndAudioCapacity(t *testing.T) {
	for _, test := range []struct {
		name       string
		key        string
		sampleRate int
		raw        string
	}{
		{name: "idempotency key", key: "short", sampleRate: 16_000},
		{name: "unicode idempotency key", key: "🙂🙂", sampleRate: 16_000},
		{name: "padded idempotency key", key: " ephemeral-voice-2 ", sampleRate: 16_000},
		{name: "sample rate", key: "ephemeral-voice-2", sampleRate: 8_000},
		{
			name: "concatenated JSON",
			raw: `{"type":"start","idempotency_key":"ephemeral-voice-2",` +
				`"sample_rate":16000}{"type":"finish"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			recognizer := newEphemeralTestRecognizer()
			server := httptest.NewServer(newEphemeralHTTPTestRouter(
				t,
				recognizer,
				&ephemeralThreadReader{},
				nil,
				time.Second,
			))
			defer server.Close()
			connection := dialEphemeralTranscription(
				t,
				server.URL,
				ephemeralTestThreadID,
				"Bearer voice-token-a",
				voiceInputWebSocketProtocol,
			)
			defer connection.Close()
			if test.raw == "" {
				writeEphemeralStart(t, connection, test.key, test.sampleRate)
			} else if err := connection.WriteMessage(
				websocket.TextMessage,
				[]byte(test.raw),
			); err != nil {
				t.Fatal(err)
			}
			assertEphemeralFailure(
				t,
				readEphemeralEvent(t, connection),
				"invalid_request",
				false,
			)
			if recognizer.Calls() != 0 {
				t.Fatalf("recognizer calls = %d", recognizer.Calls())
			}
		})
	}

	recognizer := newEphemeralTestRecognizer()
	server := httptest.NewServer(newEphemeralHTTPTestRouter(
		t,
		recognizer,
		&ephemeralThreadReader{},
		nil,
		time.Second,
	))
	defer server.Close()
	connection := dialEphemeralTranscription(
		t,
		server.URL,
		ephemeralTestThreadID,
		"Bearer voice-token-a",
		voiceInputWebSocketProtocol,
	)
	defer connection.Close()
	writeEphemeralStart(t, connection, "ephemeral-voice-3", 16_000)
	if event := readEphemeralEvent(t, connection); event.Type != "transcription.started" {
		t.Fatalf("started event = %#v", event)
	}
	if err := connection.WriteMessage(
		websocket.BinaryMessage,
		make([]byte, maxRealtimePCMBytes+2),
	); err != nil {
		t.Fatalf("write oversized PCM: %v", err)
	}
	assertEphemeralFailure(
		t,
		readEphemeralEvent(t, connection),
		"audio_capacity",
		false,
	)
	waitForEphemeralRecognizerStop(t, recognizer)
}

func TestEphemeralTranscriptionReturnsProviderFailure(t *testing.T) {
	recognizer := failingEphemeralRecognizer{}
	server := httptest.NewServer(newEphemeralHTTPTestRouter(
		t,
		recognizer,
		&ephemeralThreadReader{},
		nil,
		time.Second,
	))
	defer server.Close()
	connection := dialEphemeralTranscription(
		t,
		server.URL,
		ephemeralTestThreadID,
		"Bearer voice-token-a",
		voiceInputWebSocketProtocol,
	)
	defer connection.Close()
	writeEphemeralStart(t, connection, "ephemeral-provider", 16_000)
	if event := readEphemeralEvent(t, connection); event.Type != "transcription.started" {
		t.Fatalf("started event = %#v", event)
	}
	// A provider can reject the request before the client sends its first PCM
	// frame. The failure must not be delayed until the client read timeout.
	assertEphemeralFailure(
		t,
		readEphemeralEvent(t, connection),
		"provider_unavailable",
		true,
	)
}

func TestEphemeralTranscriptionCancelsDisconnectsAndTimesOut(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		recognizer := newEphemeralTestRecognizer()
		server := httptest.NewServer(newEphemeralHTTPTestRouter(
			t,
			recognizer,
			&ephemeralThreadReader{},
			nil,
			time.Second,
		))
		defer server.Close()
		connection := dialEphemeralTranscription(
			t,
			server.URL,
			ephemeralTestThreadID,
			"Bearer voice-token-a",
			voiceInputWebSocketProtocol,
		)
		defer connection.Close()
		writeEphemeralStart(t, connection, "ephemeral-cancel", 16_000)
		_ = readEphemeralEvent(t, connection)
		if err := connection.WriteMessage(
			websocket.BinaryMessage,
			make([]byte, 320),
		); err != nil {
			t.Fatal(err)
		}
		_ = readEphemeralEvent(t, connection)
		if err := connection.WriteJSON(map[string]string{"type": "cancel"}); err != nil {
			t.Fatal(err)
		}
		assertEphemeralFailure(
			t,
			readEphemeralEvent(t, connection),
			"cancelled",
			false,
		)
		waitForEphemeralRecognizerStop(t, recognizer)
	})

	t.Run("disconnect", func(t *testing.T) {
		recognizer := newEphemeralTestRecognizer()
		server := httptest.NewServer(newEphemeralHTTPTestRouter(
			t,
			recognizer,
			&ephemeralThreadReader{},
			nil,
			time.Second,
		))
		defer server.Close()
		connection := dialEphemeralTranscription(
			t,
			server.URL,
			ephemeralTestThreadID,
			"Bearer voice-token-a",
			voiceInputWebSocketProtocol,
		)
		writeEphemeralStart(t, connection, "ephemeral-disconnect", 16_000)
		_ = readEphemeralEvent(t, connection)
		if err := connection.WriteMessage(
			websocket.BinaryMessage,
			make([]byte, 320),
		); err != nil {
			t.Fatal(err)
		}
		_ = readEphemeralEvent(t, connection)
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		waitForEphemeralRecognizerStop(t, recognizer)
	})

	t.Run("timeout", func(t *testing.T) {
		recognizer := newEphemeralTestRecognizer()
		server := httptest.NewServer(newEphemeralHTTPTestRouter(
			t,
			recognizer,
			&ephemeralThreadReader{},
			nil,
			50*time.Millisecond,
		))
		defer server.Close()
		connection := dialEphemeralTranscription(
			t,
			server.URL,
			ephemeralTestThreadID,
			"Bearer voice-token-a",
			voiceInputWebSocketProtocol,
		)
		defer connection.Close()
		writeEphemeralStart(t, connection, "ephemeral-timeout", 16_000)
		_ = readEphemeralEvent(t, connection)
		assertEphemeralFailure(
			t,
			readEphemeralEvent(t, connection),
			"timeout",
			true,
		)
		waitForEphemeralRecognizerStop(t, recognizer)
	})
}

type ephemeralEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func dialEphemeralTranscription(
	t *testing.T,
	baseURL string,
	threadID string,
	token string,
	protocol string,
) *websocket.Conn {
	t.Helper()
	header := http.Header{"Authorization": []string{token}}
	connection, response, err := (&websocket.Dialer{
		Subprotocols: []string{protocol},
	}).Dial(ephemeralWebSocketEndpoint(baseURL, threadID), header)
	if err != nil {
		if response != nil {
			t.Fatalf("dial status = %d: %v", response.StatusCode, err)
		}
		t.Fatalf("dial ephemeral transcription: %v", err)
	}
	if connection.Subprotocol() != voiceInputWebSocketProtocol {
		t.Fatalf("subprotocol = %q", connection.Subprotocol())
	}
	return connection
}

func ephemeralWebSocketEndpoint(baseURL string, threadID string) string {
	return "ws" + strings.TrimPrefix(baseURL, "http") +
		"/v1/agent-threads/" + threadID +
		"/voice-transcriptions/realtime"
}

func writeEphemeralStart(
	t *testing.T,
	connection *websocket.Conn,
	key string,
	sampleRate int,
) {
	t.Helper()
	if err := connection.WriteJSON(map[string]any{
		"type": "start", "idempotency_key": key, "sample_rate": sampleRate,
	}); err != nil {
		t.Fatalf("write start: %v", err)
	}
}

func readEphemeralEvent(
	t *testing.T,
	connection *websocket.Conn,
) ephemeralEvent {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var event ephemeralEvent
	if err := connection.ReadJSON(&event); err != nil {
		t.Fatalf("read ephemeral event: %v", err)
	}
	return event
}

func assertEphemeralTranscriptEvent(
	t *testing.T,
	event ephemeralEvent,
	eventType string,
	transcript string,
	final bool,
) {
	t.Helper()
	var data struct {
		Transcript string `json:"transcript"`
		Final      bool   `json:"final"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		event.Type != eventType || data.Transcript != transcript ||
		data.Final != final {
		t.Fatalf("event = %#v data = %#v err = %v", event, data, err)
	}
}

func assertEphemeralFailure(
	t *testing.T,
	event ephemeralEvent,
	kind string,
	retryable bool,
) {
	t.Helper()
	var data struct {
		Kind      string `json:"kind"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		event.Type != "transcription.failed" || data.Kind != kind ||
		data.Retryable != retryable {
		t.Fatalf("failure = %#v data = %#v err = %v", event, data, err)
	}
}

func newEphemeralHTTPTestRouter(
	t *testing.T,
	recognizer agentvoice.PCMStreamingSpeechRecognizer,
	threads ThreadReader,
	legacy Application,
	readTimeout time.Duration,
) http.Handler {
	t.Helper()
	renderer := httpresponse.NewRenderer(
		func() string { return "corr_agent_ephemeral_transcription" },
	)
	handler, err := NewEphemeralTranscriptionHandler(
		recognizer,
		threads,
		readTimeout,
		renderer,
	)
	if err != nil {
		t.Fatalf("new ephemeral transcription handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		var actor requestcontext.Actor
		switch c.GetHeader("Authorization") {
		case "Bearer voice-token-a":
			actor = requestcontext.Actor{UserID: "user-a", SessionID: "session-a"}
		case "Bearer voice-token-b":
			actor = requestcontext.Actor{UserID: "user-b", SessionID: "session-b"}
		}
		if actor.Valid() {
			c.Request = c.Request.WithContext(
				requestcontext.WithActor(c.Request.Context(), actor),
			)
		}
		c.Next()
	})
	handler.RegisterRoutes(router)
	if legacy != nil {
		legacyHandler, legacyErr := NewHandler(
			legacy,
			threads,
			readTimeout,
			renderer,
		)
		if legacyErr != nil {
			t.Fatalf("new legacy voice handler: %v", legacyErr)
		}
		legacyHandler.RegisterRoutes(router)
	}
	return router
}

type ephemeralThreadReader struct {
	calls atomic.Int64
}

func (reader *ephemeralThreadReader) GetThread(
	_ context.Context,
	actor requestcontext.Actor,
	threadID string,
) (agentconversation.Thread, error) {
	reader.calls.Add(1)
	if actor.UserID != "user-a" || threadID != ephemeralTestThreadID {
		return agentconversation.Thread{}, agentconversation.ErrNotFound
	}
	return agentconversation.Thread{
		ID: threadID, OwnerID: actor.UserID,
	}, nil
}

type ephemeralTestRecognizer struct {
	calls   atomic.Int64
	bytes   atomic.Int64
	stopped chan struct{}
	once    sync.Once
}

type failingEphemeralRecognizer struct{}

func (failingEphemeralRecognizer) TranscribePCMStream(
	context.Context,
	agentvoice.PCMTranscriptionRequest,
	agentvoice.TranscriptionObserver,
) (agentvoice.TranscriptionResult, error) {
	return agentvoice.TranscriptionResult{}, agentvoice.NewSpeechError(
		agentvoice.SpeechOperationTranscription,
		agentvoice.ErrorProviderUnavailable,
		0,
		"",
		"",
		errors.New("provider unavailable"),
	)
}

func newEphemeralTestRecognizer() *ephemeralTestRecognizer {
	return &ephemeralTestRecognizer{stopped: make(chan struct{})}
}

func (recognizer *ephemeralTestRecognizer) TranscribePCMStream(
	ctx context.Context,
	request agentvoice.PCMTranscriptionRequest,
	observer agentvoice.TranscriptionObserver,
) (agentvoice.TranscriptionResult, error) {
	recognizer.calls.Add(1)
	defer recognizer.once.Do(func() { close(recognizer.stopped) })
	if request.PCM == nil || request.SampleRate != 16_000 || observer == nil {
		return agentvoice.TranscriptionResult{}, errors.New(
			"ephemeral test recognizer received invalid input",
		)
	}
	first := make([]byte, 512)
	read, err := request.PCM.Read(first)
	if read > 0 {
		recognizer.bytes.Add(int64(read))
		if updateErr := observer.OnTranscriptionUpdate(
			ctx,
			agentvoice.TranscriptionUpdate{Transcript: "partial transcript"},
		); updateErr != nil {
			return agentvoice.TranscriptionResult{}, updateErr
		}
	}
	if err != nil {
		return agentvoice.TranscriptionResult{}, ephemeralRecognizerError(ctx, err)
	}
	copied, err := io.Copy(io.Discard, io.TeeReader(
		request.PCM,
		writerFunc(func(data []byte) (int, error) {
			recognizer.bytes.Add(int64(len(data)))
			return len(data), nil
		}),
	))
	_ = copied
	if err != nil {
		return agentvoice.TranscriptionResult{}, ephemeralRecognizerError(ctx, err)
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		agentvoice.TranscriptionUpdate{
			Transcript: "completed transcript", Final: true,
		},
	); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	return agentvoice.TranscriptionResult{
		ID: "ephemeral-result-1", Provider: "fake", Model: "fake-realtime",
		Transcript: "completed transcript",
	}, nil
}

func (recognizer *ephemeralTestRecognizer) Calls() int64 {
	return recognizer.calls.Load()
}

func (recognizer *ephemeralTestRecognizer) Bytes() int64 {
	return recognizer.bytes.Load()
}

func ephemeralRecognizerError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return agentvoice.NewSpeechError(
			agentvoice.SpeechOperationTranscription,
			agentvoice.ErrorCancelled,
			0,
			"",
			"",
			context.Canceled,
		)
	}
	return err
}

type writerFunc func([]byte) (int, error)

func (write writerFunc) Write(data []byte) (int, error) {
	return write(data)
}

func waitForEphemeralRecognizerStop(
	t *testing.T,
	recognizer *ephemeralTestRecognizer,
) {
	t.Helper()
	select {
	case <-recognizer.stopped:
	case <-time.After(time.Second):
		t.Fatal("ephemeral recognizer did not stop")
	}
}

var (
	_ agentvoice.PCMStreamingSpeechRecognizer = (*ephemeralTestRecognizer)(nil)
	_ agentvoice.PCMStreamingSpeechRecognizer = failingEphemeralRecognizer{}
	_ ThreadReader                            = (*ephemeralThreadReader)(nil)
)
