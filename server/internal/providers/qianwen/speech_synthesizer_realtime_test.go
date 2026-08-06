package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRealtimeSynthesisUsesDocumentedWebSocketSequence(t *testing.T) {
	t.Parallel()
	const apiKey = "test-realtime-tts-key"
	chunks := [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}}
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
		_, continuePayload, err := connection.ReadMessage()
		if err != nil {
			t.Errorf("read continue-task: %v", err)
			return
		}
		var continued struct {
			Header struct {
				Action string `json:"action"`
			} `json:"header"`
			Payload struct {
				Input struct {
					Text string `json:"text"`
				} `json:"input"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(continuePayload, &continued); err != nil ||
			continued.Header.Action != "continue-task" ||
			continued.Payload.Input.Text != "Speak this sentence." {
			t.Errorf("unexpected continue-task: %s", continuePayload)
			return
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
		for _, chunk := range chunks {
			if err := connection.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				t.Errorf("write audio: %v", err)
				return
			}
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
	var received [][]byte
	err := synthesizer.streamRealtimePCM(
		context.Background(),
		" Speak this sentence. ",
		func(chunk []byte) error {
			received = append(received, bytes.Clone(chunk))
			return nil
		},
	)
	if err != nil {
		t.Fatalf("synthesize realtime: %v", err)
	}
	if len(received) != len(chunks) ||
		!bytes.Equal(received[0], chunks[0]) ||
		!bytes.Equal(received[1], chunks[1]) {
		t.Fatalf("received chunks = %#v", received)
	}
}
