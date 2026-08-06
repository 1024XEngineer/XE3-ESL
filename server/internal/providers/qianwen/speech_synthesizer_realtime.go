package qianwen

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
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

func (synthesizer *speechSynthesizer) streamRealtimePCM(
	ctx context.Context,
	text string,
	consume func([]byte) error,
) error {
	session, err := synthesizer.openRealtimeSpeech(ctx, consume)
	if err != nil {
		return err
	}
	defer session.Close()
	if err := session.AppendText(text); err != nil {
		return err
	}
	return session.Finish()
}

type speechRealtimeSession struct {
	context    context.Context
	cancel     context.CancelFunc
	connection *websocket.Conn
	taskID     string
	consume    func([]byte) error
	result     chan error
	done       chan struct{}
	closeOnce  sync.Once
	reading    bool
	finished   bool
}

func (synthesizer *speechSynthesizer) openRealtimeSpeech(
	ctx context.Context,
	consume func([]byte) error,
) (*speechRealtimeSession, error) {
	if synthesizer == nil || ctx == nil || consume == nil {
		return nil, errors.New("Qianwen realtime TTS is unavailable")
	}
	taskID, err := newRealtimeTaskID()
	if err != nil {
		return nil, err
	}
	callContext, cancel := context.WithTimeout(ctx, synthesizer.timeout)
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
		cancel()
		return nil, speechTransportError(
			protocol.SpeechOperationSynthesis,
			callContext,
			err,
		)
	}
	if deadline, ok := callContext.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
		_ = connection.SetWriteDeadline(deadline)
	}
	session := &speechRealtimeSession{
		context: callContext, cancel: cancel, connection: connection,
		taskID: taskID, consume: consume,
		result: make(chan error, 1), done: make(chan struct{}),
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
				"format": "pcm", "sample_rate": agentconversation.AssistantSpeechSampleRate,
				"volume": 50, "rate": 1, "pitch": 1, "enable_ssml": false,
			},
			"input": map[string]any{},
		},
	}); err != nil {
		cancel()
		_ = connection.Close()
		return nil, err
	}
	if err := waitForSpeechRealtimeEvent(
		connection,
		taskID,
		"task-started",
	); err != nil {
		cancel()
		_ = connection.Close()
		return nil, err
	}
	session.reading = true
	go session.readAudio()
	return session, nil
}

func (session *speechRealtimeSession) AppendText(text string) error {
	if session == nil || session.connection == nil || session.finished {
		return errors.New("Qianwen realtime TTS session is unavailable")
	}
	if err := protocol.ValidateSynthesisRequest(
		protocol.SynthesisRequest{Text: text},
	); err != nil {
		return err
	}
	if err := session.connection.WriteJSON(map[string]any{
		"header": map[string]any{
			"action": "continue-task", "task_id": session.taskID, "streaming": "duplex",
		},
		"payload": map[string]any{"input": map[string]any{"text": text}},
	}); err != nil {
		return err
	}
	return nil
}

func (session *speechRealtimeSession) Finish() error {
	if session == nil || session.connection == nil || session.finished {
		return errors.New("Qianwen realtime TTS session is unavailable")
	}
	session.finished = true
	if err := session.connection.WriteJSON(map[string]any{
		"header": map[string]any{
			"action": "finish-task", "task_id": session.taskID, "streaming": "duplex",
		},
		"payload": map[string]any{"input": map[string]any{}},
	}); err != nil {
		return err
	}
	select {
	case err := <-session.result:
		return err
	case <-session.context.Done():
		return speechTransportError(
			protocol.SpeechOperationSynthesis,
			session.context,
			session.context.Err(),
		)
	}
}

func (session *speechRealtimeSession) readAudio() {
	defer close(session.done)
	var audioBytes int64
	for {
		messageType, payload, readErr := session.connection.ReadMessage()
		if readErr != nil {
			session.result <- speechTransportError(
				protocol.SpeechOperationSynthesis,
				session.context,
				readErr,
			)
			return
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 || audioBytes+int64(len(payload)) > platformmedia.MaxAudioBytes {
				session.result <- errors.New("Qianwen realtime TTS audio exceeds the accepted size")
				return
			}
			audioBytes += int64(len(payload))
			if err := session.consume(payload); err != nil {
				session.result <- err
				return
			}
		case websocket.TextMessage:
			event, eventErr := decodeSpeechRealtimeEvent(payload, session.taskID)
			if eventErr != nil {
				session.result <- eventErr
				return
			}
			switch event {
			case "result-generated":
			case "task-finished":
				if audioBytes == 0 {
					session.result <- errors.New("Qianwen realtime TTS returned no audio")
					return
				}
				session.result <- nil
				return
			default:
				session.result <- fmt.Errorf("Qianwen realtime TTS returned unexpected event %q", event)
				return
			}
		default:
			session.result <- errors.New("Qianwen realtime TTS returned an invalid frame")
			return
		}
	}
}

func (session *speechRealtimeSession) Close() error {
	if session == nil {
		return nil
	}
	var err error
	session.closeOnce.Do(func() {
		session.cancel()
		err = session.connection.Close()
		if session.reading {
			<-session.done
		}
	})
	return err
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

func newRealtimeTaskID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
