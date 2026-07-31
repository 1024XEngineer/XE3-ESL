package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/gorilla/websocket"
)

func TestRealtimeTranscribeUsesDocumentedWebSocketSequence(t *testing.T) {
	t.Parallel()
	const apiKey = "test-realtime-key"
	audio := bytes.Repeat([]byte{1, 2, 3, 4}, realtimeASRChunkBytes)
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
			run.Payload.Model != "fun-asr-realtime" ||
			run.Payload.Parameters.Format != "wav" ||
			run.Payload.Parameters.SampleRate != 16_000 {
			t.Errorf("unexpected run-task: %#v", run)
			return
		}
		if err := connection.WriteJSON(map[string]any{
			"header": map[string]any{
				"task_id": run.Header.TaskID, "event": "task-started",
			},
			"payload": map[string]any{},
		}); err != nil {
			t.Errorf("write task-started: %v", err)
			return
		}
		var received bytes.Buffer
		partialSent := false
		for {
			messageType, payload, readErr := connection.ReadMessage()
			if readErr != nil {
				t.Errorf("read audio or finish-task: %v", readErr)
				return
			}
			if messageType == websocket.BinaryMessage {
				received.Write(payload)
				if !partialSent {
					partialSent = true
					if err := connection.WriteJSON(map[string]any{
						"header": map[string]any{
							"task_id": run.Header.TaskID,
							"event":   "result-generated",
						},
						"payload": map[string]any{
							"output": map[string]any{"sentence": map[string]any{
								"text": "I practice", "sentence_end": false,
							}},
						},
					}); err != nil {
						t.Errorf("write intermediate result: %v", err)
						return
					}
				}
				continue
			}
			var finish struct {
				Header struct {
					Action string `json:"action"`
				} `json:"header"`
			}
			if err := json.Unmarshal(payload, &finish); err != nil ||
				finish.Header.Action != "finish-task" {
				t.Errorf("unexpected finish-task: %s", payload)
				return
			}
			break
		}
		if !bytes.Equal(received.Bytes(), audio) {
			t.Errorf("streamed audio differs from source")
			return
		}
		for id, text := range map[int]string{2: "for interviews.", 1: "I practice English"} {
			if err := connection.WriteJSON(map[string]any{
				"header": map[string]any{
					"task_id": run.Header.TaskID, "event": "result-generated",
				},
				"payload": map[string]any{
					"output": map[string]any{"sentence": map[string]any{
						"text": text, "sentence_end": true, "sentence_id": id,
					}},
					"usage": map[string]any{"duration": 4},
				},
			}); err != nil {
				t.Errorf("write result: %v", err)
				return
			}
		}
		if err := connection.WriteJSON(map[string]any{
			"header": map[string]any{
				"task_id": run.Header.TaskID, "event": "task-finished",
			},
			"payload": map[string]any{},
		}); err != nil {
			t.Errorf("write task-finished: %v", err)
		}
	}))
	defer server.Close()

	recognizer := mustRecognizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("realtime recognition must not use HTTP generation")
		return nil, nil
	}), apiKey)
	recognizer.model = "fun-asr-realtime"
	recognizer.wsEndpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	observer := &recordingTranscriptionObserver{}
	result, err := recognizer.TranscribeStream(
		context.Background(),
		ai.TranscriptionRequest{Audio: &asrTestAudio{data: audio}},
		observer,
	)
	if err != nil {
		t.Fatalf("transcribe realtime: %v", err)
	}
	if result.Transcript != "I practice English for interviews." ||
		result.Model != "fun-asr-realtime" ||
		result.Usage.AudioSeconds != 4 {
		t.Fatalf("result = %#v", result)
	}
	updates := observer.Updates()
	if len(updates) < 2 || updates[0].Transcript != "I practice" ||
		!updates[len(updates)-1].Final ||
		updates[len(updates)-1].Transcript != result.Transcript {
		t.Fatalf("stream updates = %#v", updates)
	}
}

type recordingTranscriptionObserver struct {
	mutex   sync.Mutex
	updates []ai.TranscriptionUpdate
}

func (observer *recordingTranscriptionObserver) OnTranscriptionUpdate(
	_ context.Context,
	update ai.TranscriptionUpdate,
) error {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	observer.updates = append(observer.updates, update)
	return nil
}

func (observer *recordingTranscriptionObserver) Updates() []ai.TranscriptionUpdate {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	return append([]ai.TranscriptionUpdate(nil), observer.updates...)
}

func TestRealtimeEndpointDerivesFromDashScopeHTTPBase(t *testing.T) {
	t.Parallel()
	got := realtimeASREndpoint("https://dashscope.aliyuncs.com/api/v1")
	if got != "wss://dashscope.aliyuncs.com/api-ws/v1/inference/?heartbeat=true" {
		t.Fatalf("endpoint = %q", got)
	}
}
