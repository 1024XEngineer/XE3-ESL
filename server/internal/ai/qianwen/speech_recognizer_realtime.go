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
	observer ai.TranscriptionObserver,
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
	type readResult struct {
		transcript string
		duration   int
		err        error
	}
	readResults := make(chan readResult, 1)
	go func() {
		transcript, duration, readErr := collectRealtimeASRResult(
			callContext,
			connection,
			taskID,
			observer,
		)
		readResults <- readResult{
			transcript: transcript,
			duration:   duration,
			err:        readErr,
		}
	}()
	if err := streamRealtimeASRAudio(connection, request.Audio); err != nil {
		_ = connection.Close()
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
	var completed readResult
	select {
	case completed = <-readResults:
	case <-callContext.Done():
		return ai.TranscriptionResult{}, realtimeASRTransportError(
			callContext,
			callContext.Err(),
		)
	}
	if completed.err != nil {
		return ai.TranscriptionResult{}, completed.err
	}
	return ai.TranscriptionResult{
		ID: taskID, Provider: providerName, Model: recognizer.model,
		Transcript: completed.transcript,
		Usage:      ai.SpeechUsage{AudioSeconds: completed.duration},
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
	ctx context.Context,
	connection *websocket.Conn,
	taskID string,
	observer ai.TranscriptionObserver,
) (string, int, error) {
	accumulator := realtimeTranscriptAccumulator{}
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
			transcript := accumulator.Apply(
				sentence.Text,
				sentence.SentenceEnd,
				sentence.SentenceID,
			)
			if transcript != "" && observer != nil {
				if err := observer.OnTranscriptionUpdate(
					ctx,
					ai.TranscriptionUpdate{Transcript: transcript},
				); err != nil {
					return "", 0, err
				}
			}
		case "task-finished":
			transcript := accumulator.Transcript()
			if transcript == "" {
				return "", 0, invalidSpeechResponse(
					ai.SpeechOperationTranscription, 0, taskID,
					"Fun-ASR realtime response has no transcript",
				)
			}
			if observer != nil {
				if err := observer.OnTranscriptionUpdate(
					ctx,
					ai.TranscriptionUpdate{
						Transcript: transcript,
						Final:      true,
					},
				); err != nil {
					return "", 0, err
				}
			}
			return transcript, duration, nil
		case "task-failed":
			return "", 0, realtimeASRProviderError(event)
		}
	}
}

type realtimeTranscriptAccumulator struct {
	committedByID map[int]string
	committed     []string
	partial       string
}

func (accumulator *realtimeTranscriptAccumulator) Apply(
	raw string,
	final bool,
	sentenceID int,
) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return accumulator.Transcript()
	}
	if !final {
		accumulator.partial = text
		return accumulator.Transcript()
	}
	if sentenceID > 0 {
		if accumulator.committedByID == nil {
			accumulator.committedByID = make(map[int]string)
		}
		accumulator.committedByID[sentenceID] = text
	} else if len(accumulator.committed) == 0 ||
		accumulator.committed[len(accumulator.committed)-1] != text {
		accumulator.committed = append(accumulator.committed, text)
	}
	accumulator.partial = ""
	return accumulator.Transcript()
}

func (accumulator realtimeTranscriptAccumulator) Transcript() string {
	parts := make([]string, 0, len(accumulator.committedByID)+
		len(accumulator.committed)+1)
	if len(accumulator.committedByID) > 0 {
		ids := make([]int, 0, len(accumulator.committedByID))
		for id := range accumulator.committedByID {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		for _, id := range ids {
			parts = append(parts, accumulator.committedByID[id])
		}
	} else {
		parts = append(parts, accumulator.committed...)
	}
	if accumulator.partial != "" {
		parts = append(parts, accumulator.partial)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
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
