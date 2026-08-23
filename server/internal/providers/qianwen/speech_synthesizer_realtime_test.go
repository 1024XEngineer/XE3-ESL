package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	"github.com/gorilla/websocket"
)

var realtimeSpeechTaskIDPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestRealtimeSpeechTaskIDUsesRFC4122UUID(t *testing.T) {
	t.Parallel()
	taskID, err := newRealtimeSpeechTaskID()
	if err != nil {
		t.Fatalf("generate realtime speech task ID: %v", err)
	}
	if !realtimeSpeechTaskIDPattern.MatchString(taskID) {
		t.Fatalf("realtime speech task ID is not an RFC 4122 UUID: %q", taskID)
	}
}

func TestRealtimeSynthesisStreamsTextThroughOneDocumentedTask(t *testing.T) {
	t.Parallel()
	const apiKey = "test-realtime-tts-key"
	chunks := [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}}
	texts := []string{"Speak this ", "sentence."}
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get(authorizationHeaderName) != "Bearer "+apiKey {
			t.Errorf("authorization header was not forwarded")
		}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer connection.Close()
		_, runPayload, err := connection.ReadMessage()
		if err != nil {
			t.Errorf("read run-task: %v", err)
			return
		}
		var run struct {
			Header struct {
				Action string `json:"action"`
				TaskID string `json:"task_id"`
			} `json:"header"`
			Payload struct {
				Model      string `json:"model"`
				Parameters struct {
					Voice      string `json:"voice"`
					Format     string `json:"format"`
					SampleRate int    `json:"sample_rate"`
				} `json:"parameters"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(runPayload, &run); err != nil {
			t.Errorf("decode run-task: %v", err)
			return
		}
		if run.Header.Action != "run-task" ||
			run.Payload.Model != "qwen-audio-3.0-tts-flash" ||
			run.Payload.Parameters.Voice != "loongeva_v3.6" ||
			run.Payload.Parameters.Format != "pcm" ||
			run.Payload.Parameters.SampleRate != ttsOutputSampleRate {
			t.Errorf("unexpected run-task: %#v", run)
			return
		}
		if err := connection.WriteJSON(map[string]any{
			"header": map[string]any{
				"task_id": run.Header.TaskID, "event": "task-started",
			},
		}); err != nil {
			t.Errorf("write task-started: %v", err)
			return
		}
		for index, expectedText := range texts {
			_, continuePayload, err := connection.ReadMessage()
			if err != nil {
				t.Errorf("read continue-task: %v", err)
				return
			}
			var continued struct {
				Header struct {
					Action string `json:"action"`
					TaskID string `json:"task_id"`
				} `json:"header"`
				Payload struct {
					Input struct {
						Text string `json:"text"`
					} `json:"input"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(continuePayload, &continued); err != nil ||
				continued.Header.Action != "continue-task" ||
				continued.Header.TaskID != run.Header.TaskID ||
				continued.Payload.Input.Text != expectedText {
				t.Errorf("unexpected continue-task: %s", continuePayload)
				return
			}
			if err := connection.WriteMessage(
				websocket.BinaryMessage,
				chunks[index],
			); err != nil {
				t.Errorf("write audio: %v", err)
				return
			}
		}
		_, finishPayload, err := connection.ReadMessage()
		if err != nil {
			t.Errorf("read finish-task: %v", err)
			return
		}
		var finished struct {
			Header struct {
				Action string `json:"action"`
			} `json:"header"`
		}
		if err := json.Unmarshal(finishPayload, &finished); err != nil ||
			finished.Header.Action != "finish-task" {
			t.Errorf("unexpected finish-task: %s", finishPayload)
			return
		}
		if err := connection.WriteJSON(map[string]any{
			"header": map[string]any{
				"task_id": run.Header.TaskID, "event": "task-finished",
			},
		}); err != nil {
			t.Errorf("write task-finished: %v", err)
		}
	}))
	defer server.Close()

	synthesizer := mustSynthesizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("realtime synthesis must not use HTTP generation")
		return nil, nil
	}), apiKey, "")
	synthesizer.realtimeEndpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	received := make(chan []byte, len(chunks))
	session, err := synthesizer.openRealtimeSpeech(
		context.Background(),
		func(chunk []byte) error {
			received <- bytes.Clone(chunk)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("open realtime synthesis: %v", err)
	}
	defer session.Close()
	for index, text := range texts {
		if err := session.AppendText(text); err != nil {
			t.Fatalf("append realtime text: %v", err)
		}
		select {
		case chunk := <-received:
			if !bytes.Equal(chunk, chunks[index]) {
				t.Fatalf("received chunk = %#v", chunk)
			}
		case <-time.After(time.Second):
			t.Fatal("realtime audio did not arrive before task completion")
		}
	}
	if err := session.Finish(); err != nil {
		t.Fatalf("finish realtime synthesis: %v", err)
	}
}

func TestSingaporeSynthesizeUsesRealtimeWebSocketAndReturnsValidatedWAV(
	t *testing.T,
) {
	t.Parallel()

	pcm := make([]byte, ttsOutputSampleRate/10*2)
	for index := range pcm {
		pcm[index] = byte(index)
	}
	server := newRealtimeSynthesisServer(t, "Say hello.", realtimeSynthesisOutcome{
		pcm: pcm,
	})
	directory := t.TempDir()
	synthesizer := newSingaporeTestSynthesizer(
		t,
		server,
		directory,
		time.Second,
	)
	result, err := synthesizer.Synthesize(
		context.Background(),
		protocol.SynthesisRequest{Text: "  Say hello.  "},
	)
	if err != nil {
		t.Fatalf("synthesize Singapore WAV: %v", err)
	}
	if result.RequestID == "" || result.AudioID != result.RequestID ||
		result.Provider != providerName || result.Model != synthesizer.model ||
		result.Audio == nil {
		t.Fatalf("unexpected Singapore synthesis result: %#v", result)
	}
	if err := platformmedia.ValidateAudioSource(result.Audio); err != nil {
		t.Fatalf("validate Singapore WAV: %v", err)
	}
	if result.Audio.Size() != int64(pcmWAVHeaderBytes+len(pcm)) ||
		result.Audio.SampleRate() != ttsOutputSampleRate ||
		result.Audio.Duration() != 100*time.Millisecond {
		t.Fatalf(
			"unexpected Singapore WAV metadata: size=%d rate=%d duration=%s",
			result.Audio.Size(),
			result.Audio.SampleRate(),
			result.Audio.Duration(),
		)
	}
	reader, err := result.Audio.Open()
	if err != nil {
		t.Fatalf("open Singapore WAV: %v", err)
	}
	wav, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read Singapore WAV: %v", errors.Join(readErr, closeErr))
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" ||
		!bytes.Equal(wav[pcmWAVHeaderBytes:], pcm) {
		t.Fatal("Singapore synthesis returned an invalid WAV payload")
	}
	if err := result.Audio.Close(); err != nil {
		t.Fatalf("close Singapore WAV: %v", err)
	}
	assertDirectoryEmpty(t, directory)
}

func TestSingaporeSynthesizeFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		outcome realtimeSynthesisOutcome
		timeout time.Duration
		kind    protocol.ErrorKind
		code    string
	}{
		{
			name: "provider task failed",
			outcome: realtimeSynthesisOutcome{
				event:           "task-failed",
				providerCode:    "InvalidParameter",
				providerMessage: "must-not-appear",
			},
			timeout: time.Second,
			kind:    protocol.ErrorInvalidResponse,
			code:    "InvalidParameter",
		},
		{
			name: "provider task failed before start",
			outcome: realtimeSynthesisOutcome{
				event:           "task-failed",
				failBeforeStart: true,
				providerCode:    "ModelNotFound",
				providerMessage: "must-not-appear",
			},
			timeout: time.Second,
			kind:    protocol.ErrorInvalidResponse,
			code:    "ModelNotFound",
		},
		{
			name:    "provider returned no audio",
			outcome: realtimeSynthesisOutcome{},
			timeout: time.Second,
			kind:    protocol.ErrorInvalidResponse,
		},
		{
			name:    "provider returned incomplete PCM frame",
			outcome: realtimeSynthesisOutcome{pcm: []byte{1}},
			timeout: time.Second,
			kind:    protocol.ErrorInvalidResponse,
		},
		{
			name:    "provider start timed out",
			outcome: realtimeSynthesisOutcome{withholdStart: true},
			timeout: 50 * time.Millisecond,
			kind:    protocol.ErrorTimeout,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := newRealtimeSynthesisServer(t, "Say hello.", test.outcome)
			directory := t.TempDir()
			synthesizer := newSingaporeTestSynthesizer(
				t,
				server,
				directory,
				test.timeout,
			)
			result, err := synthesizer.Synthesize(
				context.Background(),
				protocol.SynthesisRequest{Text: "Say hello."},
			)
			if result.Audio != nil {
				_ = result.Audio.Close()
				t.Fatal("failed Singapore synthesis returned audio")
			}
			var speechError *protocol.SpeechError
			if !errors.As(err, &speechError) || speechError.Kind != test.kind ||
				speechError.ProviderCode != test.code {
				t.Fatalf("synthesis error = %v, want kind %s", err, test.kind)
			}
			if strings.Contains(safeLiveSpeechError(err), "must-not-appear") {
				t.Fatal("provider message leaked through live error")
			}
			assertDirectoryEmpty(t, directory)
		})
	}
}

type realtimeSynthesisOutcome struct {
	pcm             []byte
	event           string
	withholdStart   bool
	failBeforeStart bool
	providerCode    string
	providerMessage string
}

func newRealtimeSynthesisServer(
	t *testing.T,
	expectedText string,
	outcome realtimeSynthesisOutcome,
) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer connection.Close()

		_, payload, err := connection.ReadMessage()
		if err != nil {
			t.Errorf("read run-task: %v", err)
			return
		}
		var run struct {
			Header struct {
				Action string `json:"action"`
				TaskID string `json:"task_id"`
			} `json:"header"`
			Payload struct {
				Model      string `json:"model"`
				Parameters struct {
					Format     string `json:"format"`
					SampleRate int    `json:"sample_rate"`
				} `json:"parameters"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(payload, &run); err != nil ||
			run.Header.Action != "run-task" ||
			!realtimeSpeechTaskIDPattern.MatchString(run.Header.TaskID) ||
			run.Payload.Model != "qwen-audio-3.0-tts-flash" ||
			run.Payload.Parameters.Format != "pcm" ||
			run.Payload.Parameters.SampleRate != ttsOutputSampleRate {
			t.Errorf("unexpected run-task: %s", payload)
			return
		}
		if outcome.withholdStart {
			_, _, _ = connection.ReadMessage()
			return
		}
		if outcome.failBeforeStart {
			if err := writeRealtimeSynthesisEvent(connection, run.Header.TaskID, outcome); err != nil {
				t.Errorf("write task-failed: %v", err)
			}
			return
		}
		if err := connection.WriteJSON(map[string]any{
			"header": map[string]any{
				"task_id": run.Header.TaskID,
				"event":   "task-started",
			},
		}); err != nil {
			t.Errorf("write task-started: %v", err)
			return
		}

		_, payload, err = connection.ReadMessage()
		if err != nil {
			t.Errorf("read continue-task: %v", err)
			return
		}
		var continued struct {
			Header struct {
				Action string `json:"action"`
				TaskID string `json:"task_id"`
			} `json:"header"`
			Payload struct {
				Input struct {
					Text string `json:"text"`
				} `json:"input"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(payload, &continued); err != nil ||
			continued.Header.Action != "continue-task" ||
			continued.Header.TaskID != run.Header.TaskID ||
			continued.Payload.Input.Text != expectedText {
			t.Errorf("unexpected continue-task: %s", payload)
			return
		}
		if len(outcome.pcm) > 0 {
			if err := connection.WriteMessage(websocket.BinaryMessage, outcome.pcm); err != nil {
				t.Errorf("write PCM: %v", err)
				return
			}
		}
		_, payload, err = connection.ReadMessage()
		if err != nil {
			t.Errorf("read finish-task: %v", err)
			return
		}
		var finished struct {
			Header struct {
				Action string `json:"action"`
				TaskID string `json:"task_id"`
			} `json:"header"`
		}
		if err := json.Unmarshal(payload, &finished); err != nil ||
			finished.Header.Action != "finish-task" ||
			finished.Header.TaskID != run.Header.TaskID {
			t.Errorf("unexpected finish-task: %s", payload)
			return
		}
		event := outcome.event
		if event == "" {
			event = "task-finished"
		}
		outcome.event = event
		if err := writeRealtimeSynthesisEvent(connection, run.Header.TaskID, outcome); err != nil {
			t.Errorf("write %s: %v", event, err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func writeRealtimeSynthesisEvent(
	connection *websocket.Conn,
	taskID string,
	outcome realtimeSynthesisOutcome,
) error {
	return connection.WriteJSON(map[string]any{
		"header": map[string]any{
			"task_id":       taskID,
			"event":         outcome.event,
			"error_code":    outcome.providerCode,
			"error_message": outcome.providerMessage,
		},
	})
}

func newSingaporeTestSynthesizer(
	t *testing.T,
	server *httptest.Server,
	directory string,
	timeout time.Duration,
) *speechSynthesizer {
	t.Helper()
	synthesizer, err := newSynthesizerWithClient(TTSConfig{
		BaseURL:       "https://workspace-123.ap-southeast-1.maas.aliyuncs.com/api/v1",
		Model:         "qwen-audio-3.0-tts-flash",
		Voice:         "loongeva_v3.6",
		LanguageHint:  "en",
		Timeout:       timeout,
		TempDirectory: directory,
	}, "test-realtime-tts-key", doerFunc(func(*http.Request) (*http.Response, error) {
		t.Error("Singapore synthesis must not use HTTP generation")
		return nil, errors.New("unexpected HTTP generation")
	}))
	if err != nil {
		t.Fatalf("new Singapore synthesizer: %v", err)
	}
	synthesizer.realtimeEndpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	return synthesizer
}

func assertDirectoryEmpty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read temporary audio directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed synthesis left temporary audio: %v", entries)
	}
}
