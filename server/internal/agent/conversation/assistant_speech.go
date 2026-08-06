package conversation

import "context"

const (
	AssistantSpeechContentTypePCM  = "audio/pcm"
	AssistantSpeechSampleRate      = 24_000
	AssistantSpeechChannelCount    = 1
	AssistantSpeechBitsPerSample   = 16
	MaxAssistantSpeechSegmentRunes = 800
)

type AssistantSpeechSynthesizer interface {
	StreamAssistantSegment(
		context.Context,
		string,
		func([]byte) error,
	) error
}
