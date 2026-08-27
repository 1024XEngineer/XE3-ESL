package voicehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const practiceQuestionSpeechWebSocketProtocol = "speakup.practice-question-speech.v1"

const maxPracticePromptSpeechRunes = 4096

type practicePromptSpeechFrame struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (handler *Handler) questionSpeechRealtime(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	synthesisApplication, ok := handler.application.(questionSynthesisApplication)
	if !ok {
		handler.write(c, providerUnavailable(nil))
		return
	}
	input, err := synthesisApplication.QuestionSynthesis(
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
	connection, err := upgradePracticeSpeech(c)
	if err != nil {
		return
	}
	defer connection.Close()
	if writePracticeSpeechReady(connection) != nil {
		return
	}
	handler.streamPracticeSpeech(c.Request.Context(), connection, input.Text, input.Profile)
}

func (handler *Handler) promptSpeechRealtime(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	profileApplication, ok := handler.application.(sessionSynthesisApplication)
	if !ok {
		handler.write(c, providerUnavailable(nil))
		return
	}
	profile, err := profileApplication.SessionSynthesisProfile(
		c.Request.Context(), actor, c.Param("practice_session_id"),
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
	connection, err := upgradePracticeSpeech(c)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(16 * 1024)
	if writePracticeSpeechReady(connection) != nil {
		return
	}
	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		return
	}
	if messageType != websocket.TextMessage {
		writePracticeSpeechFailure(connection, false)
		return
	}
	text, err := decodePracticePromptSpeech(payload)
	if err != nil {
		writePracticeSpeechFailure(connection, false)
		return
	}
	handler.streamPracticeSpeech(c.Request.Context(), connection, text, profile)
}

func upgradePracticeSpeech(c *gin.Context) (*websocket.Conn, error) {
	return (&websocket.Upgrader{
		Subprotocols: []string{practiceQuestionSpeechWebSocketProtocol},
	}).Upgrade(c.Writer, c.Request, nil)
}

func writePracticeSpeechReady(connection *websocket.Conn) error {
	return connection.WriteJSON(gin.H{
		"type": "stream.ready",
		"data": gin.H{
			"content_type":    agentconversation.AssistantSpeechContentTypePCM,
			"sample_rate":     agentconversation.AssistantSpeechSampleRate,
			"channel_count":   agentconversation.AssistantSpeechChannelCount,
			"bits_per_sample": agentconversation.AssistantSpeechBitsPerSample,
		},
	})
}

func decodePracticePromptSpeech(payload []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var frame practicePromptSpeechFrame
	if err := decoder.Decode(&frame); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", context.Canceled
	}
	text := strings.TrimSpace(frame.Text)
	if frame.Type != "speak" || text == "" || !utf8.ValidString(text) ||
		utf8.RuneCountInString(text) > maxPracticePromptSpeechRunes {
		return "", context.Canceled
	}
	return text, nil
}

func (handler *Handler) streamPracticeSpeech(
	requestContext context.Context,
	connection *websocket.Conn,
	text string,
	profile practiceinteraction.SynthesisProfile,
) {
	if handler.realtimeSpeech == nil && handler.legacyRealtimeSpeech != nil {
		handler.streamLegacyPracticeSpeech(requestContext, connection, text)
		return
	}

	streamContext, cancel := context.WithCancel(requestContext)
	defer cancel()
	var firstAudio sync.Once
	chunks, bytes := 0, 0
	speech, err := handler.realtimeSpeech.OpenPracticeSpeech(
		streamContext,
		profile,
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

func (handler *Handler) promptSpeechRealtimeLegacy(c *gin.Context) {
	if _, ok := requestcontext.ActorFromContext(c.Request.Context()); !ok {
		handler.write(c, authenticationRequired())
		return
	}
	if !containsWebSocketProtocol(websocket.Subprotocols(c.Request), practiceQuestionSpeechWebSocketProtocol) {
		handler.write(c, invalidRequest(nil))
		return
	}
	connection, err := upgradePracticeSpeech(c)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(16 * 1024)
	if writePracticeSpeechReady(connection) != nil {
		return
	}
	messageType, payload, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		writePracticeSpeechFailure(connection, false)
		return
	}
	text, err := decodePracticePromptSpeech(payload)
	if err != nil {
		writePracticeSpeechFailure(connection, false)
		return
	}
	handler.streamLegacyPracticeSpeech(c.Request.Context(), connection, text)
}

func (handler *Handler) streamLegacyPracticeSpeech(
	requestContext context.Context,
	connection *websocket.Conn,
	text string,
) {
	streamContext, cancel := context.WithCancel(requestContext)
	defer cancel()
	chunks := 0
	speech, err := handler.legacyRealtimeSpeech.OpenAssistantSpeech(
		streamContext,
		func(audio []byte) error {
			chunks++
			return connection.WriteMessage(websocket.BinaryMessage, audio)
		},
	)
	if err != nil {
		writePracticeSpeechFailure(connection, true)
		return
	}
	defer speech.Close()
	if speech.AppendText(text) != nil || speech.Finish() != nil || chunks == 0 {
		writePracticeSpeechFailure(connection, true)
		return
	}
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
