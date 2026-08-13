package voicehttp

import (
	"context"
	"log/slog"
	"sync"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const practiceQuestionSpeechWebSocketProtocol = "speakup.practice-question-speech.v1"

func (handler *Handler) questionSpeechRealtime(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	textApplication, ok := handler.application.(questionTextApplication)
	if !ok {
		handler.write(c, providerUnavailable(nil))
		return
	}
	text, err := textApplication.QuestionText(
		c.Request.Context(), actor, c.Param("question_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	if !containsWebSocketProtocol(
		websocket.Subprotocols(c.Request),
		practiceQuestionSpeechWebSocketProtocol,
	) {
		handler.write(c, invalidRequest(nil))
		return
	}
	connection, err := (&websocket.Upgrader{
		Subprotocols: []string{practiceQuestionSpeechWebSocketProtocol},
	}).Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
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

	streamContext, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	var firstAudio sync.Once
	chunks, bytes := 0, 0
	speech, err := handler.realtimeSpeech.OpenAssistantSpeech(
		streamContext,
		func(audio []byte) error {
			firstAudio.Do(func() {
				slog.InfoContext(streamContext, "practice.question_speech.first_audio")
			})
			chunks++
			bytes += len(audio)
			return connection.WriteMessage(websocket.BinaryMessage, audio)
		},
	)
	if err != nil {
		writePracticeSpeechFailure(connection, true)
		return
	}
	defer speech.Close()
	if err := speech.AppendText(text); err != nil {
		writePracticeSpeechFailure(connection, true)
		return
	}
	if err := speech.Finish(); err != nil || chunks == 0 {
		writePracticeSpeechFailure(connection, true)
		return
	}
	slog.InfoContext(
		streamContext,
		"practice.question_speech.completed",
		slog.Int("audio_chunks", chunks),
		slog.Int("audio_bytes", bytes),
	)
	_ = connection.WriteJSON(gin.H{"type": "stream.completed", "data": gin.H{}})
}

func writePracticeSpeechFailure(connection *websocket.Conn, retryable bool) {
	_ = connection.WriteJSON(gin.H{
		"type": "stream.failed",
		"data": gin.H{"kind": "synthesis_failed", "retryable": retryable},
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
