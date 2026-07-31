package qianwen

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/gorilla/websocket"
)

const realtimeASRChunkBytes = 16 * 1024

type realtimeASREvent struct {
	Header struct {
		TaskID       string `json:"task_id"`
		Event        string `json:"event"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"header"`
	Payload struct {
		Output struct {
			Sentence struct {
				Text        string `json:"text"`
				SentenceEnd bool   `json:"sentence_end"`
				SentenceID  int    `json:"sentence_id"`
			} `json:"sentence"`
		} `json:"output"`
		Usage *struct {
			Duration int `json:"duration"`
		} `json:"usage"`
	} `json:"payload"`
}

func (recognizer *Recognizer) transcribeRealtime(
	ctx context.Context,
	request ai.TranscriptionRequest,
) (ai.TranscriptionResult, error) {
	if ctx == nil {
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("speech transcription context is required"),
		)
	}
	if err := ai.ValidateTranscriptionRequest(request); err != nil {
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	callContext, cancel := context.WithTimeout(ctx, recognizer.timeout)
	defer cancel()
	header := http.Header{}
	header.Set(authorizationHeaderName, "Bearer "+recognizer.apiKey.reveal())
	connection, response, err := websocket.DefaultDialer.DialContext(
		callContext,
		recognizer.wsEndpoint,
		header,
	)
	if err != nil {
		statusCode := 0
		requestID := ""
		if response != nil {
			statusCode = response.StatusCode
			requestID = response.Header.Get("X-Request-Id")
		}
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorProviderUnavailable,
			statusCode,
			"",
			sanitizeIdentifier(requestID),
			err,
		)
	}
	defer connection.Close()
	if deadline, ok := callContext.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
		_ = connection.SetWriteDeadline(deadline)
	}
	taskID, err := newRealtimeASRTaskID()
	if err != nil {
		return ai.TranscriptionResult{}, err
	}
	runTask := map[string]any{
		"header": map[string]any{
			"action": "run-task", "task_id": taskID, "streaming": "duplex",
		},
		"payload": map[string]any{
			"task_group": "audio", "task": "asr", "function": "recognition",
			"model": recognizer.model,
			"parameters": map[string]any{
				"format": "wav", "sample_rate": request.Audio.SampleRate(),
				"max_sentence_silence": 400,
			},
			"input": map[string]any{},
		},
	}
	if err := connection.WriteJSON(runTask); err != nil {
		return ai.TranscriptionResult{}, realtimeASRTransportError(callContext, err)
	}
	if err := waitForRealtimeASRStart(connection, taskID); err != nil {
		return ai.TranscriptionResult{}, err
	}
	if err := streamRealtimeASRAudio(connection, request.Audio); err != nil {
		return ai.TranscriptionResult{}, realtimeASRTransportError(callContext, err)
	}
	finishTask := map[string]any{
		"header": map[string]any{
			"action": "finish-task", "task_id": taskID, "streaming": "duplex",
		},
		"payload": map[string]any{"input": map[string]any{}},
	}
	if err := connection.WriteJSON(finishTask); err != nil {
		return ai.TranscriptionResult{}, realtimeASRTransportError(callContext, err)
	}
	transcript, duration, err := collectRealtimeASRResult(connection, taskID)
	if err != nil {
		return ai.TranscriptionResult{}, err
	}
	return ai.TranscriptionResult{
		ID: taskID, Provider: providerName, Model: recognizer.model,
		Transcript: transcript,
		Usage:      ai.SpeechUsage{AudioSeconds: duration},
	}, nil
}

func waitForRealtimeASRStart(connection *websocket.Conn, taskID string) error {
	for {
		event, err := readRealtimeASREvent(connection, taskID)
		if err != nil {
			return err
		}
		switch event.Header.Event {
		case "task-started":
			return nil
		case "task-failed":
			return realtimeASRProviderError(event)
		}
	}
}

func streamRealtimeASRAudio(
	connection *websocket.Conn,
	source interface{ Open() (io.ReadCloser, error) },
) error {
	reader, err := source.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	buffer := make([]byte, realtimeASRChunkBytes)
	for {
		read, readErr := reader.Read(buffer)
		if read > 0 {
			if err := connection.WriteMessage(websocket.BinaryMessage, buffer[:read]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func collectRealtimeASRResult(
	connection *websocket.Conn,
	taskID string,
) (string, int, error) {
	sentences := map[int]string{}
	duration := 0
	for {
		event, err := readRealtimeASREvent(connection, taskID)
		if err != nil {
			return "", 0, err
		}
		if event.Payload.Usage != nil && event.Payload.Usage.Duration > duration {
			duration = event.Payload.Usage.Duration
		}
		switch event.Header.Event {
		case "result-generated":
			sentence := event.Payload.Output.Sentence
			if sentence.SentenceEnd && sentence.SentenceID > 0 {
				sentences[sentence.SentenceID] = strings.TrimSpace(sentence.Text)
			}
		case "task-finished":
			ids := make([]int, 0, len(sentences))
			for id := range sentences {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			parts := make([]string, 0, len(ids))
			for _, id := range ids {
				if sentences[id] != "" {
					parts = append(parts, sentences[id])
				}
			}
			transcript := strings.TrimSpace(strings.Join(parts, " "))
			if transcript == "" {
				return "", 0, invalidSpeechResponse(
					ai.SpeechOperationTranscription, 0, taskID,
					"Fun-ASR realtime response has no transcript",
				)
			}
			return transcript, duration, nil
		case "task-failed":
			return "", 0, realtimeASRProviderError(event)
		}
	}
}

func readRealtimeASREvent(
	connection *websocket.Conn,
	taskID string,
) (realtimeASREvent, error) {
	_, payload, err := connection.ReadMessage()
	if err != nil {
		return realtimeASREvent{}, err
	}
	var event realtimeASREvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return realtimeASREvent{}, invalidSpeechResponse(
			ai.SpeechOperationTranscription, 0, taskID,
			"decode Fun-ASR realtime event",
		)
	}
	if event.Header.TaskID != taskID || event.Header.Event == "" {
		return realtimeASREvent{}, invalidSpeechResponse(
			ai.SpeechOperationTranscription, 0, taskID,
			"Fun-ASR realtime event has invalid task identity",
		)
	}
	return event, nil
}

func realtimeASRProviderError(event realtimeASREvent) error {
	return ai.NewSpeechError(
		ai.SpeechOperationTranscription,
		ai.ErrorProviderUnavailable,
		0,
		sanitizeIdentifier(event.Header.ErrorCode),
		sanitizeIdentifier(event.Header.TaskID),
		errors.New(strings.TrimSpace(event.Header.ErrorMessage)),
	)
}

func realtimeASRTransportError(ctx context.Context, err error) error {
	return speechTransportError(ai.SpeechOperationTranscription, ctx, err)
}

func realtimeASREndpoint(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path = "/api-ws/v1/inference/"
	parsed.RawQuery = "heartbeat=true"
	return parsed.String()
}

func newRealtimeASRTaskID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded), nil
}
