package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

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
