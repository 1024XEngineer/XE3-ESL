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
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestAgentVoiceRecognizerExposesRealtimePCMCapabilityOnlyForRealtime(
	t *testing.T,
) {
	t.Parallel()
	for _, test := range []struct {
		model string
		want  bool
	}{
		{model: "fun-asr-realtime", want: true},
		{model: "fun-asr-flash-2026-06-15", want: false},
	} {
		t.Run(test.model, func(t *testing.T) {
			recognizer, err := NewAgentVoiceRecognizer(ASRConfig{
				BaseURL: "https://dashscope.aliyuncs.com/api/v1",
				Model:   test.model,
				Timeout: time.Second,
			}, "test-api-key")
			if err != nil {
				t.Fatal(err)
			}
			_, got := recognizer.(agentvoice.PCMStreamingSpeechRecognizer)
			if got != test.want {
				t.Fatalf("PCM capability = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRealtimeTranscribeUsesDocumentedWebSocketSequence(t *testing.T) {
	t.Parallel()
	const apiKey = "test-realtime-key"
	audio := bytes.Repeat([]byte{1, 2, 3, 4}, realtimeASRChunkBytes)
	formats := make(chan string, 2)
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
			run.Payload.Parameters.SampleRate != 16_000 {
			t.Errorf("unexpected run-task: %#v", run)
			return
		}
		formats <- run.Payload.Parameters.Format
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
		protocol.TranscriptionRequest{Audio: &asrTestAudio{data: audio}},
		observer,
	)
	if err != nil {
		t.Fatalf("transcribe realtime: %v", err)
	}
	if format := <-formats; format != "wav" {
		t.Fatalf("validated source format = %q, want wav", format)
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

	pcmObserver := &recordingTranscriptionObserver{}
	pcmResult, err := recognizer.transcribeRealtimePCM(
		context.Background(),
		bytes.NewReader(audio),
		16_000,
		pcmObserver,
	)
	if err != nil {
		t.Fatalf("transcribe realtime PCM: %v", err)
	}
	if format := <-formats; format != "pcm" {
		t.Fatalf("live capture format = %q, want pcm", format)
	}
	pcmUpdates := pcmObserver.Updates()
	if pcmResult.Transcript != result.Transcript || len(pcmUpdates) == 0 ||
		!pcmUpdates[len(pcmUpdates)-1].Final {
		t.Fatalf("PCM result = %#v, updates = %#v", pcmResult, pcmUpdates)
	}
}

type recordingTranscriptionObserver struct {
	mutex   sync.Mutex
	updates []protocol.TranscriptionUpdate
}

func (observer *recordingTranscriptionObserver) OnTranscriptionUpdate(
	_ context.Context,
	update protocol.TranscriptionUpdate,
) error {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	observer.updates = append(observer.updates, update)
	return nil
}

func (observer *recordingTranscriptionObserver) Updates() []protocol.TranscriptionUpdate {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	return append([]protocol.TranscriptionUpdate(nil), observer.updates...)
}

func TestRealtimeEndpointDerivesFromDashScopeHTTPBase(t *testing.T) {
	t.Parallel()
	got := realtimeASREndpoint("https://dashscope.aliyuncs.com/api/v1")
	if got != "wss://dashscope.aliyuncs.com/api-ws/v1/inference/?heartbeat=true" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestRealtimeTranscribeClosesProviderConnectionOnCancellation(t *testing.T) {
	providerConnected := make(chan struct{})
	providerClosed := make(chan struct{})
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
		if _, _, err := connection.ReadMessage(); err != nil {
			t.Errorf("read run-task: %v", err)
			return
		}
		close(providerConnected)
		if _, _, err := connection.ReadMessage(); err == nil {
			t.Error("provider connection remained open after cancellation")
		}
		close(providerClosed)
	}))
	defer server.Close()

	recognizer := mustRecognizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("realtime recognition must not use HTTP generation")
		return nil, nil
	}), "test-realtime-key")
	recognizer.model = "fun-asr-realtime"
	recognizer.wsEndpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := recognizer.transcribeRealtimePCM(
			ctx,
			bytes.NewReader([]byte{1, 2}),
			16_000,
			nil,
		)
		result <- err
	}()
	select {
	case <-providerConnected:
	case <-time.After(time.Second):
		t.Fatal("provider did not accept the realtime task")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled realtime transcription succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled realtime transcription did not return")
	}
	select {
	case <-providerClosed:
	case <-time.After(time.Second):
		t.Fatal("cancelled realtime transcription did not close provider")
	}
}
