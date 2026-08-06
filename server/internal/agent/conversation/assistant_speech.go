package conversation

import "context"

const (
	AssistantSpeechContentTypeWAV  = "audio/wav"
	MaxAssistantSpeechSegmentRunes = 800
)

type AssistantSpeechSegment struct {
	ContentType string
	Audio       []byte
}

type AssistantSpeechSynthesizer interface {
	SynthesizeAssistantSegment(
		context.Context,
		string,
	) (AssistantSpeechSegment, error)
}
