package conversationhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	assistantSpeechWebSocketProtocol = "speakup.assistant-speech.v1"
	maxAssistantSpeechSegments       = 1024
	maxAssistantSpeechRunes          = 4096
)

type assistantSpeechFrame struct {
	Type     string `json:"type"`
	Sequence int    `json:"sequence"`
	Text     string `json:"text"`
}

func (handler *Handler) streamAssistantSpeech(c *gin.Context) {
	trusted, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	if _, err := handler.application.GetThread(
		c.Request.Context(), trusted, c.Param("thread_id"),
	); err != nil {
		handler.write(c, mapError(err))
		return
	}
	if !containsProtocol(
		websocket.Subprotocols(c.Request),
		assistantSpeechWebSocketProtocol,
	) {
		handler.write(c, invalidRequest(nil))
		return
	}
	connection, err := (&websocket.Upgrader{
		Subprotocols: []string{assistantSpeechWebSocketProtocol},
	}).Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(4096)
	if err := connection.WriteJSON(gin.H{
		"type": "stream.ready",
		"data": gin.H{
			"content_type":    agentconversation.AssistantSpeechContentTypePCM,
			"sample_rate":     agentconversation.AssistantSpeechSampleRate,
			"channel_count":   agentconversation.AssistantSpeechChannelCount,
			"bits_per_sample": agentconversation.AssistantSpeechBitsPerSample,
		},
	}); err != nil {
		return
	}
	chunkCount := 0
	audioBytes := 0
	var firstAudio sync.Once
	speech, err := handler.assistantSpeech.OpenAssistantSpeech(
		c.Request.Context(),
		func(audio []byte) error {
			firstAudio.Do(func() {
				slog.InfoContext(
					c.Request.Context(),
					"agent.assistant_speech.first_audio",
				)
			})
			chunkCount++
			audioBytes += len(audio)
			return connection.WriteMessage(websocket.BinaryMessage, audio)
		},
	)
	if err != nil {
		slog.WarnContext(
			c.Request.Context(),
			"agent.assistant_speech.failed",
			slog.String("stage", "open"),
			slog.Any("error", err),
		)
		writeAssistantSpeechFailure(connection, "synthesis_failed", true)
		return
	}
	defer speech.Close()
	expectedSequence := 1
	totalRunes := 0
	for {
		messageType, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			return
		}
		if messageType != websocket.TextMessage {
			_ = speech.Close()
			writeAssistantSpeechFailure(connection, "invalid_request", false)
			return
		}
		frame, frameErr := decodeAssistantSpeechFrame(payload)
		if frameErr != nil {
			_ = speech.Close()
			writeAssistantSpeechFailure(connection, "invalid_request", false)
			return
		}
		switch frame.Type {
		case "segment":
			if frame.Sequence != expectedSequence ||
				frame.Sequence > maxAssistantSpeechSegments {
				_ = speech.Close()
				writeAssistantSpeechFailure(connection, "invalid_sequence", false)
				return
			}
			totalRunes += utf8.RuneCountInString(frame.Text)
			if totalRunes > maxAssistantSpeechRunes {
				_ = speech.Close()
				writeAssistantSpeechFailure(connection, "invalid_request", false)
				return
			}
			if err := speech.AppendText(frame.Text); err != nil {
				_ = speech.Close()
				slog.WarnContext(
					c.Request.Context(),
					"agent.assistant_speech.failed",
					slog.String("stage", "append"),
					slog.Int("text_chunks", expectedSequence-1),
					slog.Any("error", err),
				)
				writeAssistantSpeechFailure(connection, "synthesis_failed", true)
				return
			}
			expectedSequence++
		case "finish":
			if expectedSequence == 1 {
				_ = speech.Close()
				writeAssistantSpeechFailure(connection, "invalid_request", false)
				return
			}
			if err := speech.Finish(); err != nil {
				slog.WarnContext(
					c.Request.Context(),
					"agent.assistant_speech.failed",
					slog.String("stage", "finish"),
					slog.Int("text_chunks", expectedSequence-1),
					slog.Int("audio_chunks", chunkCount),
					slog.Any("error", err),
				)
				writeAssistantSpeechFailure(connection, "synthesis_failed", true)
				return
			}
			if chunkCount == 0 {
				slog.WarnContext(
					c.Request.Context(),
					"agent.assistant_speech.failed",
					slog.String("stage", "finish"),
					slog.Int("text_chunks", expectedSequence-1),
					slog.Int("audio_chunks", chunkCount),
					slog.String("error", "no audio returned"),
				)
				writeAssistantSpeechFailure(connection, "synthesis_failed", true)
				return
			}
			slog.InfoContext(
				c.Request.Context(),
				"agent.assistant_speech.completed",
				slog.Int("text_chunks", expectedSequence-1),
				slog.Int("text_runes", totalRunes),
				slog.Int("audio_chunks", chunkCount),
				slog.Int("audio_bytes", audioBytes),
			)
			_ = connection.WriteJSON(gin.H{
				"type": "stream.completed", "data": gin.H{},
			})
			return
		case "cancel":
			return
		default:
			_ = speech.Close()
			writeAssistantSpeechFailure(connection, "invalid_request", false)
			return
		}
	}
}

func decodeAssistantSpeechFrame(payload []byte) (assistantSpeechFrame, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var frame assistantSpeechFrame
	if err := decoder.Decode(&frame); err != nil {
		return assistantSpeechFrame{}, err
	}
	trimmed := strings.TrimSpace(frame.Text)
	switch frame.Type {
	case "segment":
		if frame.Sequence < 1 || trimmed == "" || !utf8.ValidString(trimmed) ||
			utf8.RuneCountInString(trimmed) >
				agentconversation.MaxAssistantSpeechSegmentRunes {
			return assistantSpeechFrame{}, errors.New("assistant speech segment is invalid")
		}
	case "finish", "cancel":
		if frame.Sequence != 0 || frame.Text != "" {
			return assistantSpeechFrame{}, errors.New("assistant speech control frame is invalid")
		}
	default:
		return assistantSpeechFrame{}, errors.New("assistant speech frame type is invalid")
	}
	return frame, nil
}

func writeAssistantSpeechFailure(
	connection *websocket.Conn,
	kind string,
	retryable bool,
) {
	_ = connection.WriteJSON(gin.H{
		"type": "stream.failed",
		"data": gin.H{"kind": kind, "retryable": retryable},
	})
}

func containsProtocol(protocols []string, required string) bool {
	for _, protocol := range protocols {
		if protocol == required {
			return true
		}
	}
	return false
}
