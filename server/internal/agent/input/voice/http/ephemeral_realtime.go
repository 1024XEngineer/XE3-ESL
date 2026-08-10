package voicehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	errEphemeralAudioCapacity = errors.New(
		"agent ephemeral transcription audio exceeds the accepted size",
	)
	errEphemeralInvalidAudio = errors.New(
		"agent ephemeral transcription audio is invalid",
	)
	errEphemeralInvalidFrame = errors.New(
		"agent ephemeral transcription frame is invalid",
	)
)

type EphemeralTranscriptionHandler struct {
	recognizer  agentvoice.PCMStreamingSpeechRecognizer
	threads     ThreadReader
	readTimeout time.Duration
	errors      *httpresponse.Renderer
}

func NewEphemeralTranscriptionHandler(
	recognizer agentvoice.PCMStreamingSpeechRecognizer,
	threads ThreadReader,
	readTimeout time.Duration,
	errorRenderer *httpresponse.Renderer,
) (*EphemeralTranscriptionHandler, error) {
	if recognizer == nil || threads == nil || readTimeout < 0 {
		return nil, errors.New(
			"agent ephemeral transcription: HTTP dependencies are required",
		)
	}
	if readTimeout == 0 {
		readTimeout = defaultReadTimeout
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &EphemeralTranscriptionHandler{
		recognizer:  recognizer,
		threads:     threads,
		readTimeout: readTimeout,
		errors:      errorRenderer,
	}, nil
}

func (handler *EphemeralTranscriptionHandler) RegisterRoutes(
	routes gin.IRoutes,
) {
	routes.GET(
		"/v1/agent-threads/:thread_id/voice-transcriptions/realtime",
		handler.transcribeRealtime,
	)
}

func (handler *EphemeralTranscriptionHandler) transcribeRealtime(
	c *gin.Context,
) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	if _, err := handler.threads.GetThread(
		c.Request.Context(), actor, c.Param("thread_id"),
	); err != nil {
		handler.write(c, mapThreadError(err))
		return
	}
	if !containsWebSocketProtocol(
		websocket.Subprotocols(c.Request),
		voiceInputWebSocketProtocol,
	) {
		handler.write(c, invalidRequest(nil))
		return
	}
	connection, err := (&websocket.Upgrader{
		Subprotocols: []string{voiceInputWebSocketProtocol},
	}).Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(platformmedia.MaxAudioBytes)
	if err := connection.SetReadDeadline(
		time.Now().Add(handler.readTimeout),
	); err != nil {
		return
	}
	events := &ephemeralTranscriptionWriter{
		connection: connection,
		timeout:    handler.readTimeout,
	}
	start, err := readEphemeralStart(connection)
	if err != nil {
		_ = events.fail("invalid_request", false)
		return
	}
	if err := events.write("transcription.started", gin.H{}); err != nil {
		return
	}

	streamContext, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	reader, writer := io.Pipe()
	type transcriptionResult struct {
		result agentvoice.TranscriptionResult
		err    error
	}
	completed := make(chan transcriptionResult, 1)
	go func() {
		defer reader.Close()
		result, transcribeErr := handler.recognizer.TranscribePCMStream(
			streamContext,
			agentvoice.PCMTranscriptionRequest{
				PCM: reader, SampleRate: start.SampleRate,
			},
			events,
		)
		completed <- transcriptionResult{result: result, err: transcribeErr}
	}()

	streamed := make(chan error, 1)
	go func() {
		streamed <- streamEphemeralPCM(
			connection,
			writer,
			handler.readTimeout,
		)
	}()
	var streamErr error
	var transcription transcriptionResult
	select {
	case streamErr = <-streamed:
		if streamErr != nil {
			cancel()
			_ = writer.CloseWithError(streamErr)
		} else {
			_ = writer.Close()
		}
		transcription = <-completed
	case transcription = <-completed:
		cancel()
		if transcription.err != nil {
			_ = writer.CloseWithError(transcription.err)
		} else {
			_ = writer.Close()
		}
	}
	if streamErr != nil {
		kind, retryable, localFailure := ephemeralStreamFailure(streamErr)
		if !localFailure && transcription.err != nil {
			kind, retryable = ephemeralProviderFailure(transcription.err)
		}
		_ = events.fail(kind, retryable)
		return
	}
	if transcription.err != nil {
		kind, retryable := ephemeralProviderFailure(transcription.err)
		_ = events.fail(kind, retryable)
		return
	}
	transcript := strings.TrimSpace(transcription.result.Transcript)
	if transcript == "" {
		_ = events.fail("invalid_response", true)
		return
	}
	_ = events.complete(gin.H{
		"transcript": transcript,
		"final":      true,
	})
}

func readEphemeralStart(
	connection *websocket.Conn,
) (realtimeStartFrame, error) {
	messageType, payload, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		return realtimeStartFrame{}, errEphemeralInvalidFrame
	}
	var frame realtimeStartFrame
	if err := decodeEphemeralTextFrame(payload, &frame); err != nil ||
		frame.Type != "start" ||
		frame.IdempotencyKey != strings.TrimSpace(frame.IdempotencyKey) ||
		utf8.RuneCountInString(frame.IdempotencyKey) < 8 ||
		utf8.RuneCountInString(frame.IdempotencyKey) > 128 ||
		frame.SampleRate != 16_000 {
		return realtimeStartFrame{}, errEphemeralInvalidFrame
	}
	return frame, nil
}

func streamEphemeralPCM(
	connection *websocket.Conn,
	destination io.Writer,
	readTimeout time.Duration,
) error {
	written := 0
	for {
		if err := connection.SetReadDeadline(
			time.Now().Add(readTimeout),
		); err != nil {
			return err
		}
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 {
				return errEphemeralInvalidAudio
			}
			if written+len(payload) > maxRealtimePCMBytes {
				return errEphemeralAudioCapacity
			}
			if _, err := destination.Write(payload); err != nil {
				return err
			}
			written += len(payload)
		case websocket.TextMessage:
			var frame realtimeControlFrame
			if err := decodeEphemeralTextFrame(payload, &frame); err != nil {
				return errEphemeralInvalidFrame
			}
			switch frame.Type {
			case "finish":
				if written == 0 || written%2 != 0 {
					return errEphemeralInvalidAudio
				}
				return nil
			case "cancel":
				return context.Canceled
			default:
				return errEphemeralInvalidFrame
			}
		default:
			return errEphemeralInvalidFrame
		}
	}
}

func decodeEphemeralTextFrame(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errEphemeralInvalidFrame
	}
	return nil
}

type ephemeralTranscriptionWriter struct {
	connection *websocket.Conn
	timeout    time.Duration
	mutex      sync.Mutex
	terminal   bool
}

func (writer *ephemeralTranscriptionWriter) OnTranscriptionUpdate(
	_ context.Context,
	update agentvoice.TranscriptionUpdate,
) error {
	transcript := strings.TrimSpace(update.Transcript)
	if update.Final || transcript == "" {
		return nil
	}
	return writer.write("transcription.updated", gin.H{
		"transcript": transcript,
		"final":      false,
	})
}

func (writer *ephemeralTranscriptionWriter) write(
	event string,
	data any,
) error {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.terminal {
		return nil
	}
	return writer.writeLocked(event, data)
}

func (writer *ephemeralTranscriptionWriter) complete(data any) error {
	return writer.writeTerminal("transcription.completed", data)
}

func (writer *ephemeralTranscriptionWriter) fail(
	kind string,
	retryable bool,
) error {
	return writer.writeTerminal("transcription.failed", gin.H{
		"kind": kind, "retryable": retryable,
	})
}

func (writer *ephemeralTranscriptionWriter) writeTerminal(
	event string,
	data any,
) error {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.terminal {
		return nil
	}
	writer.terminal = true
	return writer.writeLocked(event, data)
}

func (writer *ephemeralTranscriptionWriter) writeLocked(
	event string,
	data any,
) error {
	if err := writer.connection.SetWriteDeadline(
		time.Now().Add(writer.timeout),
	); err != nil {
		return err
	}
	return writer.connection.WriteJSON(gin.H{"type": event, "data": data})
}

func ephemeralStreamFailure(err error) (string, bool, bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled", false, true
	case errors.Is(err, errEphemeralAudioCapacity):
		return "audio_capacity", false, true
	case errors.Is(err, errEphemeralInvalidAudio):
		return "invalid_audio", false, true
	case errors.Is(err, errEphemeralInvalidFrame):
		return "invalid_request", false, true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "timeout", true, true
	}
	return "stream_interrupted", true, false
}

func ephemeralProviderFailure(err error) (string, bool) {
	var speechError *agentvoice.SpeechError
	if errors.As(err, &speechError) {
		return string(speechError.Kind), speechError.Retryable()
	}
	return "stream_interrupted", true
}

func (handler *EphemeralTranscriptionHandler) write(
	c *gin.Context,
	err error,
) {
	writeHTTPError(c, handler.errors, err)
}
