package persistence

import (
	"strings"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

func validUUID(value string) bool {
	return ValidUUID(value)
}

func validMessageContent(value string) bool {
	return ValidMessageContent(value)
}

func validRunConfiguration(configuration RunConfiguration) bool {
	return ValidRunConfiguration(configuration)
}

func validVoiceIdempotencyKey(value string) bool {
	return value == strings.TrimSpace(value) &&
		len(value) >= 8 &&
		len(value) <= 128 &&
		!strings.ContainsAny(value, "\r\n\x00")
}

func validVoiceTranscription(result ai.TranscriptionResult) bool {
	return ValidModelID(result.ID) &&
		ValidProviderID(result.Provider) &&
		ValidModelID(result.Model) &&
		ValidMessageContent(result.Transcript) &&
		len(result.Language) <= 64 &&
		len(result.Emotion) <= 64 &&
		len(result.FinishReason) <= 64
}
