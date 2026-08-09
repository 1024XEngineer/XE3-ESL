package voicehttp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"time"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const practiceVoiceInputWebSocketProtocol = "speakup.voice-input.v1"
const maxPracticeRealtimePCMBytes = 16_000 * 2 * 60

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
		time.Now().Add(handler.readTimeout),
	); err != nil {
		return
	}
	start, err := readPracticeRealtimeStart(connection)
	if err != nil {
		writePracticeRealtimeFailure(connection, "invalid_request", false)
		return
	}
	pcm, err := readPracticeRealtimePCM(connection, handler.readTimeout)
	if err != nil {
		writePracticeRealtimeFailure(connection, "stream_interrupted", true)
		return
	}
	wav, err := practicePCM16MonoWAV(pcm, start.SampleRate)
	if err != nil {
		writePracticeRealtimeFailure(connection, "invalid_audio", false)
		return
	}
	observer := &practiceRealtimeTranscriptionWriter{connection: connection}
	if err := observer.write("transcription.started", gin.H{}); err != nil {
		return
	}
	candidate, err := handler.application.TranscribeStream(
		c.Request.Context(),
		actor,
		practicevoice.TranscribeVoiceCommand{
			SessionID:      sessionID,
			QuestionID:     questionID,
			IdempotencyKey: start.IdempotencyKey,
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(wav),
		},
		observer,
	)
	if err != nil {
		kind, retryable := practiceRealtimeFailure(err)
		writePracticeRealtimeFailure(connection, kind, retryable)
		return
	}
	_ = observer.write(
		"candidate.ready",
		gin.H{"candidate": TranscriptionCandidateResponse(candidate)},
	)
}

func readPracticeRealtimeStart(
	connection *websocket.Conn,
) (practiceRealtimeStartFrame, error) {
	messageType, payload, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		return practiceRealtimeStartFrame{},
			errors.New("practice voice realtime start frame is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var frame practiceRealtimeStartFrame
	if err := decoder.Decode(&frame); err != nil ||
		frame.Type != "start" ||
		!validPracticeRealtimeIdempotencyKey(frame.IdempotencyKey) ||
		frame.SampleRate != 16_000 {
		return practiceRealtimeStartFrame{},
			errors.New("practice voice realtime start frame is invalid")
	}
	return frame, nil
}

func readPracticeRealtimePCM(
	connection *websocket.Conn,
	readTimeout time.Duration,
) ([]byte, error) {
	var pcm bytes.Buffer
	for {
		if err := connection.SetReadDeadline(
			time.Now().Add(readTimeout),
		); err != nil {
			return nil, err
		}
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return nil, err
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 ||
				pcm.Len()+len(payload) > maxPracticeRealtimePCMBytes {
				return nil,
					errors.New("practice voice realtime audio exceeds the accepted size")
			}
			_, _ = pcm.Write(payload)
		case websocket.TextMessage:
			var frame practiceRealtimeControlFrame
			decoder := json.NewDecoder(bytes.NewReader(payload))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&frame); err != nil {
				return nil, err
			}
			switch frame.Type {
			case "finish":
				if pcm.Len() == 0 || pcm.Len()%2 != 0 {
					return nil,
						errors.New("practice voice realtime audio is incomplete")
				}
				return pcm.Bytes(), nil
			case "cancel":
				return nil, context.Canceled
			default:
				return nil,
					errors.New("practice voice realtime control frame is invalid")
			}
		default:
			return nil,
				errors.New("practice voice realtime frame type is invalid")
		}
	}
}

func practicePCM16MonoWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if len(pcm) == 0 || len(pcm)%2 != 0 || sampleRate != 16_000 {
		return nil, errors.New("practice voice realtime PCM is invalid")
	}
	result := make([]byte, 44+len(pcm))
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(result[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], uint32(len(pcm)))
	copy(result[44:], pcm)
	return result, nil
}

type practiceRealtimeTranscriptionWriter struct {
	connection *websocket.Conn
}

func (writer *practiceRealtimeTranscriptionWriter) OnTranscriptionUpdate(
	_ context.Context,
	update practicevoice.TranscriptionUpdate,
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
	var providerError *practicevoice.ProviderError
	if errors.As(err, &providerError) {
		return string(providerError.Kind), providerError.Retryable()
	}
	switch {
	case errors.Is(err, practicevoice.ErrVoiceRoundInvalid):
		return "invalid_audio", false
	case errors.Is(err, practicevoice.ErrVoiceRoundCapacity):
		return "audio_capacity", false
	case errors.Is(err, practicevoice.ErrVoiceRoundConflict):
		return "idempotency_conflict", false
	case errors.Is(err, practicevoice.ErrVoiceRoundProcessing):
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
