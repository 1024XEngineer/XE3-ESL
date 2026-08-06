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
	OpenAssistantSpeech(context.Context) (AssistantSpeechSession, error)
}

type AssistantSpeechSession interface {
	StreamSegment(string, func([]byte) error) error
	Close() error
}
