package voicehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const practiceVoiceInputWebSocketProtocol = "speakup.voice-input.v1"

// This is a transport-capacity boundary, not an IELTS answer timer. The
// Practice flow owns any exam-specific timing and stops capture accordingly.
const maxPracticeRealtimePCMBytes = int(platformmedia.MaxAudioBytes) - 44

type practiceRealtimeStartFrame struct {
	Type           string `json:"type"`
	IdempotencyKey string `json:"idempotency_key"`
	SampleRate     int    `json:"sample_rate"`
}

type practiceRealtimeControlFrame struct {
	Type string `json:"type"`
}

func (handler *Handler) transcribeCandidateRealtime(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	sessionID := c.Param("practice_session_id")
	questionID := c.Param("question_id")
	state, err := handler.application.Resume(
		c.Request.Context(),
		actor,
		sessionID,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	if state.Session.ID != sessionID || state.Question == nil ||
		state.Question.ID != questionID {
		handler.write(c, invalidRequest(nil))
		return
	}
	if !containsPracticeWebSocketProtocol(
		websocket.Subprotocols(c.Request),
		practiceVoiceInputWebSocketProtocol,
	) {
		handler.write(c, invalidRequest(nil))
		return
	}
	connection, err := (&websocket.Upgrader{
		Subprotocols: []string{practiceVoiceInputWebSocketProtocol},
	}).Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(platformmedia.MaxAudioBytes)
	if err := connection.SetReadDeadline(
		time.Now().Add(handler.realtimeReadTimeout),
	); err != nil {
		return
	}
	start, err := readPracticeRealtimeStart(connection)
	if err != nil {
		writePracticeRealtimeFailure(connection, "invalid_request", false)
		return
	}
	observer := &practiceRealtimeTranscriptionWriter{connection: connection}
	if err := observer.write("transcription.started", gin.H{}); err != nil {
		return
	}
	streamContext, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	reader, writer := io.Pipe()
	type transcriptionResult struct {
		candidate practiceinteraction.TranscriptionCandidate
		err       error
	}
	result := make(chan transcriptionResult, 1)
	go func() {
		defer reader.Close()
		candidate, transcribeErr := handler.application.TranscribeStream(
			streamContext,
			actor,
			practiceinteraction.TranscribeVoiceStreamCommand{
				SessionID:      sessionID,
				QuestionID:     questionID,
				IdempotencyKey: start.IdempotencyKey,
				PCM:            reader,
				SampleRate:     start.SampleRate,
			},
			observer,
		)
		result <- transcriptionResult{candidate: candidate, err: transcribeErr}
	}()
	streamErr := streamPracticeRealtimePCM(
		connection,
		writer,
		handler.realtimeReadTimeout,
	)
	if streamErr != nil {
		cancel()
		_ = writer.CloseWithError(streamErr)
	} else {
		_ = writer.Close()
	}
	completed := <-result
	if completed.err != nil {
		kind, retryable := practiceRealtimeFailure(completed.err)
		writePracticeRealtimeFailure(connection, kind, retryable)
		return
	}
	if streamErr != nil {
		writePracticeRealtimeFailure(connection, "stream_interrupted", true)
		return
	}
	_ = observer.write(
		"candidate.ready",
		gin.H{"candidate": TranscriptionCandidateResponse(completed.candidate)},
	)
}

func readPracticeRealtimeStart(
	connection *websocket.Conn,
) (practiceRealtimeStartFrame, error) {
	messageType, payload, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		return practiceRealtimeStartFrame{},
			errors.New("practice interaction realtime start frame is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var frame practiceRealtimeStartFrame
	if err := decoder.Decode(&frame); err != nil ||
		frame.Type != "start" ||
		!validPracticeRealtimeIdempotencyKey(frame.IdempotencyKey) ||
		frame.SampleRate != 16_000 {
		return practiceRealtimeStartFrame{},
			errors.New("practice interaction realtime start frame is invalid")
	}
	return frame, nil
}

func streamPracticeRealtimePCM(
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
			if len(payload) == 0 ||
				written+len(payload) > maxPracticeRealtimePCMBytes {
				return errors.New(
					"practice interaction realtime audio exceeds the accepted size",
				)
			}
			if _, err := destination.Write(payload); err != nil {
				return err
			}
			written += len(payload)
		case websocket.TextMessage:
			var frame practiceRealtimeControlFrame
			decoder := json.NewDecoder(bytes.NewReader(payload))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&frame); err != nil {
				return err
			}
			switch frame.Type {
			case "finish":
				if written == 0 || written%2 != 0 {
					return errors.New(
						"practice interaction realtime audio is incomplete",
					)
				}
				return nil
			case "cancel":
				return context.Canceled
			default:
				return errors.New(
					"practice interaction realtime control frame is invalid",
				)
			}
		default:
			return errors.New("practice interaction realtime frame type is invalid")
		}
	}
}

type practiceRealtimeTranscriptionWriter struct {
	connection *websocket.Conn
}

func (writer *practiceRealtimeTranscriptionWriter) OnTranscriptionUpdate(
	_ context.Context,
	update practiceinteraction.TranscriptionUpdate,
) error {
	return writer.write("transcription.updated", gin.H{
		"transcript": update.Transcript,
		"final":      update.Final,
	})
}

func (writer *practiceRealtimeTranscriptionWriter) write(
	event string,
	data any,
) error {
	return writer.connection.WriteJSON(gin.H{"type": event, "data": data})
}

func writePracticeRealtimeFailure(
	connection *websocket.Conn,
	kind string,
	retryable bool,
) {
	_ = connection.WriteJSON(gin.H{
		"type": "candidate.failed",
		"data": gin.H{"kind": kind, "retryable": retryable},
	})
}

func practiceRealtimeFailure(err error) (string, bool) {
	var providerError *practiceinteraction.ProviderError
	if errors.As(err, &providerError) {
		return string(providerError.Kind), providerError.Retryable()
	}
	switch {
	case errors.Is(err, practiceinteraction.ErrVoiceRoundInvalid):
		return "invalid_audio", false
	case errors.Is(err, practiceinteraction.ErrVoiceRoundCapacity):
		return "audio_capacity", false
	case errors.Is(err, practiceinteraction.ErrVoiceRoundConflict):
		return "idempotency_conflict", false
	case errors.Is(err, practiceinteraction.ErrVoiceRoundProcessing):
		return "processing", true
	default:
		return "stream_interrupted", true
	}
}

func containsPracticeWebSocketProtocol(
	protocols []string,
	required string,
) bool {
	for _, protocol := range protocols {
		if protocol == required {
			return true
		}
	}
	return false
}

func validPracticeRealtimeIdempotencyKey(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= 8 && len(value) <= 128
}
