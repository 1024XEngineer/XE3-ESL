package ai

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

const MaxSynthesisTextRunes = 2_000

type SpeechRecognizer interface {
	Transcribe(context.Context, TranscriptionRequest) (TranscriptionResult, error)
}

type SpeechSynthesizer interface {
	Synthesize(context.Context, SynthesisRequest) (SynthesisResult, error)
}

type TranscriptionRequest struct {
	Audio platformmedia.AudioSource
}

type SpeechUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	AudioSeconds int
	Characters   int
}

type TranscriptionResult struct {
	ID           string
	Provider     string
	Model        string
	Transcript   string
	Language     string
	Emotion      string
	FinishReason string
	Usage        SpeechUsage
}

type SynthesisRequest struct {
	Text string
}

type SynthesisResult struct {
	RequestID string
	Provider  string
	Model     string
	AudioID   string
	Audio     platformmedia.ManagedAudioSource
	Usage     SpeechUsage
}

func ValidateTranscriptionRequest(request TranscriptionRequest) error {
	return platformmedia.ValidateAudioSource(request.Audio)
}

func ValidateSynthesisRequest(request SynthesisRequest) error {
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return errors.New("speech synthesis text is required")
	}
	if !utf8.ValidString(text) {
		return errors.New("speech synthesis text must be valid UTF-8")
	}
	if utf8.RuneCountInString(text) > MaxSynthesisTextRunes {
		return errors.New("speech synthesis text exceeds the accepted length")
	}
	return nil
}

type SpeechOperation string

const (
	SpeechOperationTranscription SpeechOperation = "transcription"
	SpeechOperationSynthesis     SpeechOperation = "synthesis"
)

// SpeechError carries stable failure metadata without retaining provider
// messages, request bodies, audio bytes, synthesized text, or credentials.
type SpeechError struct {
	Operation    SpeechOperation
	Kind         ErrorKind
	StatusCode   int
	ProviderCode string
	RequestID    string
	cause        error
}

func NewSpeechError(
	operation SpeechOperation,
	kind ErrorKind,
	statusCode int,
	providerCode string,
	requestID string,
	cause error,
) *SpeechError {
	return &SpeechError{
		Operation:    operation,
		Kind:         kind,
		StatusCode:   statusCode,
		ProviderCode: providerCode,
		RequestID:    requestID,
		cause:        cause,
	}
}

func (e *SpeechError) Error() string {
	if e == nil {
		return "speech operation failed"
	}
	switch e.Operation {
	case SpeechOperationTranscription:
		return "speech transcription failed: " + string(e.Kind)
	case SpeechOperationSynthesis:
		return "speech synthesis failed: " + string(e.Kind)
	default:
		return "speech operation failed: " + string(e.Kind)
	}
}

func (e *SpeechError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *SpeechError) Retryable() bool {
	return e != nil && e.Kind.Retryable()
}
