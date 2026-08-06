package conversationhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	assistantSpeechWebSocketProtocol = "speakup.assistant-speech.v1"
	maxAssistantSpeechSegments       = 64
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
		"type": "stream.ready", "data": gin.H{},
	}); err != nil {
		return
	}
	speech, err := handler.assistantSpeech.OpenAssistantSpeech(c.Request.Context())
	if err != nil {
		writeAssistantSpeechFailure(connection, "synthesis_failed", true)
		return
	}
	defer speech.Close()
	expectedSequence := 1
	for {
		messageType, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			return
		}
		if messageType != websocket.TextMessage {
			writeAssistantSpeechFailure(connection, "invalid_request", false)
			return
		}
		frame, frameErr := decodeAssistantSpeechFrame(payload)
		if frameErr != nil {
			writeAssistantSpeechFailure(connection, "invalid_request", false)
			return
		}
		switch frame.Type {
		case "segment":
			if frame.Sequence != expectedSequence ||
				frame.Sequence > maxAssistantSpeechSegments {
				writeAssistantSpeechFailure(connection, "invalid_sequence", false)
				return
			}
			if err := connection.WriteJSON(gin.H{
				"type": "segment.started",
				"data": gin.H{
					"sequence":        frame.Sequence,
					"content_type":    agentconversation.AssistantSpeechContentTypePCM,
					"sample_rate":     agentconversation.AssistantSpeechSampleRate,
					"channel_count":   agentconversation.AssistantSpeechChannelCount,
					"bits_per_sample": agentconversation.AssistantSpeechBitsPerSample,
				},
			}); err != nil {
				return
			}
			chunkCount := 0
			synthesisErr := speech.StreamSegment(
				frame.Text,
				func(audio []byte) error {
					chunkCount++
					return connection.WriteMessage(websocket.BinaryMessage, audio)
				},
			)
			if synthesisErr != nil || chunkCount == 0 {
				writeAssistantSpeechFailure(connection, "synthesis_failed", true)
				return
			}
			if err := connection.WriteJSON(gin.H{
				"type": "segment.completed",
				"data": gin.H{
					"sequence": frame.Sequence,
				},
			}); err != nil {
				return
			}
			expectedSequence++
		case "finish":
			_ = connection.WriteJSON(gin.H{
				"type": "stream.completed", "data": gin.H{},
			})
			return
		case "cancel":
			return
		default:
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
		frame.Text = trimmed
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
