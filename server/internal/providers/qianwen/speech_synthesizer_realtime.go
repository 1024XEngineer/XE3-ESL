package qianwen

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	"github.com/gorilla/websocket"
)

const ttsRealtimePath = "/api-ws/v1/inference/"

type speechRealtimeEnvelope struct {
	Header speechRealtimeHeader `json:"header"`
}

type speechRealtimeHeader struct {
	Event        string `json:"event"`
	TaskID       string `json:"task_id"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

func realtimeTTSEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("Qianwen realtime TTS base URL is invalid")
	}
	parsed.Scheme = "wss"
	parsed.Path = ttsRealtimePath
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (synthesizer *speechSynthesizer) synthesizeRealtimeWAV(
	ctx context.Context,
	text string,
) ([]byte, error) {
	if synthesizer == nil || ctx == nil {
		return nil, errors.New("Qianwen realtime TTS is unavailable")
	}
	if err := protocol.ValidateSynthesisRequest(
		protocol.SynthesisRequest{Text: text},
	); err != nil {
		return nil, err
	}
	callContext, cancel := context.WithTimeout(ctx, synthesizer.timeout)
	defer cancel()
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+synthesizer.apiKey.reveal())
	header.Set("X-DashScope-DataInspection", "enable")
	connection, response, err := websocket.DefaultDialer.DialContext(
		callContext,
		synthesizer.realtimeEndpoint,
		header,
	)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return nil, speechTransportError(
			protocol.SpeechOperationSynthesis,
			callContext,
			err,
		)
	}
	defer connection.Close()
	if deadline, ok := callContext.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
		_ = connection.SetWriteDeadline(deadline)
	}
	taskID, err := newRealtimeTaskID()
	if err != nil {
		return nil, err
	}
	if err := connection.WriteJSON(map[string]any{
		"header": map[string]any{
			"action": "run-task", "task_id": taskID, "streaming": "duplex",
		},
		"payload": map[string]any{
			"task_group": "audio", "task": "tts",
			"function": "SpeechSynthesizer", "model": synthesizer.model,
			"parameters": map[string]any{
				"text_type": "PlainText", "voice": synthesizer.voice,
				"format": "wav", "sample_rate": ttsOutputSampleRate,
				"volume": 50, "rate": 1, "pitch": 1, "enable_ssml": false,
			},
			"input": map[string]any{},
		},
	}); err != nil {
		return nil, err
	}
	if err := waitForSpeechRealtimeEvent(connection, taskID, "task-started"); err != nil {
		return nil, err
	}
	if err := connection.WriteJSON(map[string]any{
		"header": map[string]any{
			"action": "continue-task", "task_id": taskID, "streaming": "duplex",
		},
		"payload": map[string]any{"input": map[string]any{"text": strings.TrimSpace(text)}},
	}); err != nil {
		return nil, err
	}
	if err := connection.WriteJSON(map[string]any{
		"header": map[string]any{
			"action": "finish-task", "task_id": taskID, "streaming": "duplex",
		},
		"payload": map[string]any{"input": map[string]any{}},
	}); err != nil {
		return nil, err
	}
	var audio bytes.Buffer
	for {
		messageType, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			return nil, readErr
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 ||
				int64(audio.Len()+len(payload)) > platformmedia.MaxAudioBytes {
				return nil, errors.New("Qianwen realtime TTS audio exceeds the accepted size")
			}
			_, _ = audio.Write(payload)
		case websocket.TextMessage:
			event, eventErr := decodeSpeechRealtimeEvent(payload, taskID)
			if eventErr != nil {
				return nil, eventErr
			}
			switch event {
			case "result-generated":
			case "task-finished":
				return normalizeRealtimeWAV(audio.Bytes())
			default:
				return nil, fmt.Errorf("Qianwen realtime TTS returned unexpected event %q", event)
			}
		default:
			return nil, errors.New("Qianwen realtime TTS returned an invalid frame")
		}
	}
}

func waitForSpeechRealtimeEvent(
	connection *websocket.Conn,
	taskID string,
	expected string,
) error {
	messageType, payload, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		return errors.New("Qianwen realtime TTS start event is unavailable")
	}
	event, err := decodeSpeechRealtimeEvent(payload, taskID)
	if err != nil {
		return err
	}
	if event != expected {
		return fmt.Errorf("Qianwen realtime TTS returned unexpected event %q", event)
	}
	return nil
}

func decodeSpeechRealtimeEvent(payload []byte, taskID string) (string, error) {
	var envelope speechRealtimeEnvelope
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&envelope); err != nil || envelope.Header.TaskID != taskID {
		return "", errors.New("Qianwen realtime TTS returned an invalid event")
	}
	if envelope.Header.Event == "task-failed" {
		return "", errors.New("Qianwen realtime TTS task failed")
	}
	if strings.TrimSpace(envelope.Header.Event) == "" {
		return "", errors.New("Qianwen realtime TTS event type is missing")
	}
	return envelope.Header.Event, nil
}

func normalizeRealtimeWAV(audio []byte) ([]byte, error) {
	if len(audio) < 44 || int64(len(audio)) > platformmedia.MaxAudioBytes {
		return nil, errors.New("Qianwen realtime TTS returned invalid WAV audio")
	}
	normalized, err := normalizeProviderWAVSizeMarkers(
		bytes.NewReader(audio),
		int64(len(audio)),
	)
	if err != nil {
		return nil, err
	}
	result, err := io.ReadAll(io.LimitReader(normalized, platformmedia.MaxAudioBytes+1))
	if err != nil || len(result) != len(audio) || string(result[:4]) != "RIFF" ||
		string(result[8:12]) != "WAVE" {
		return nil, errors.New("Qianwen realtime TTS returned malformed WAV audio")
	}
	return result, nil
}

func newRealtimeTaskID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
