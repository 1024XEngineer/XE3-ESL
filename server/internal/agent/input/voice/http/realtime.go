package voicehttp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const voiceInputWebSocketProtocol = "speakup.voice-input.v1"
const maxRealtimePCMBytes = 16_000 * 2 * 60

type realtimeStartFrame struct {
	Type           string `json:"type"`
	IdempotencyKey string `json:"idempotency_key"`
	SampleRate     int    `json:"sample_rate"`
}

type realtimeControlFrame struct {
	Type string `json:"type"`
}

func (handler *Handler) uploadRealtime(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	thread, err := handler.threads.GetThread(
		c.Request.Context(), actor, c.Param("thread_id"),
	)
	if err != nil {
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
	start, err := readRealtimeStart(connection)
	if err != nil {
		writeRealtimeFailure(connection, "invalid_request", false)
		return
	}
	pcm, err := readRealtimePCM(connection, handler.readTimeout)
	if err != nil {
		writeRealtimeFailure(connection, "stream_interrupted", true)
		return
	}
	wav, err := pcm16MonoWAV(pcm, start.SampleRate)
	if err != nil {
		writeRealtimeFailure(connection, "invalid_audio", false)
		return
	}
	observer := &realtimeTranscriptionWriter{connection: connection}
	if err := observer.write("transcription.started", gin.H{}); err != nil {
		return
	}
	candidate, err := handler.application.UploadStream(
		c.Request.Context(),
		actor,
		agentvoice.UploadRequest{
			ThreadID:       thread.ID,
			IdempotencyKey: start.IdempotencyKey,
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(wav),
		},
		observer,
	)
	if err != nil {
		writeRealtimeFailure(connection, "stream_interrupted", true)
		return
	}
	event := "candidate.ready"
	if candidate.Status == agentvoice.StatusFailed {
		event = "candidate.failed"
	}
	_ = observer.write(event, gin.H{"candidate": CandidateResponse(candidate)})
}

func readRealtimeStart(connection *websocket.Conn) (realtimeStartFrame, error) {
	messageType, payload, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		return realtimeStartFrame{}, errors.New("voice realtime start frame is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var frame realtimeStartFrame
	if err := decoder.Decode(&frame); err != nil ||
		frame.Type != "start" ||
		!validRealtimeIdempotencyKey(frame.IdempotencyKey) ||
		frame.SampleRate != 16_000 {
		return realtimeStartFrame{}, errors.New("voice realtime start frame is invalid")
	}
	return frame, nil
}

func readRealtimePCM(
	connection *websocket.Conn,
	readTimeout time.Duration,
) ([]byte, error) {
	var pcm bytes.Buffer
	for {
		if err := connection.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return nil, err
		}
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return nil, err
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 ||
				pcm.Len()+len(payload) > maxRealtimePCMBytes {
				return nil, errors.New("voice realtime audio exceeds the accepted size")
			}
			_, _ = pcm.Write(payload)
		case websocket.TextMessage:
			var frame realtimeControlFrame
			decoder := json.NewDecoder(bytes.NewReader(payload))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&frame); err != nil {
				return nil, err
			}
			switch frame.Type {
			case "finish":
				if pcm.Len() == 0 || pcm.Len()%2 != 0 {
					return nil, errors.New("voice realtime audio is incomplete")
				}
				return pcm.Bytes(), nil
			case "cancel":
				return nil, context.Canceled
			default:
				return nil, errors.New("voice realtime control frame is invalid")
			}
		default:
			return nil, errors.New("voice realtime frame type is invalid")
		}
	}
}

func pcm16MonoWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if len(pcm) == 0 || len(pcm)%2 != 0 || sampleRate != 16_000 {
		return nil, errors.New("voice realtime PCM is invalid")
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

type realtimeTranscriptionWriter struct {
	connection *websocket.Conn
}

func (writer *realtimeTranscriptionWriter) OnTranscriptionUpdate(
	_ context.Context,
	update agentvoice.TranscriptionUpdate,
) error {
	return writer.write("transcription.updated", gin.H{
		"transcript": update.Transcript,
		"final":      update.Final,
	})
}

func (writer *realtimeTranscriptionWriter) write(
	event string,
	data any,
) error {
	return writer.connection.WriteJSON(gin.H{"type": event, "data": data})
}

func writeRealtimeFailure(
	connection *websocket.Conn,
	kind string,
	retryable bool,
) {
	_ = connection.WriteJSON(gin.H{
		"type": "candidate.failed",
		"data": gin.H{"kind": kind, "retryable": retryable},
	})
}

func containsWebSocketProtocol(protocols []string, required string) bool {
	for _, protocol := range protocols {
		if protocol == required {
			return true
		}
	}
	return false
}

func validRealtimeIdempotencyKey(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= 8 && len(value) <= 128
}
