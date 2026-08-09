package voice

import (
	"context"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

type ProviderErrorKind string

const (
	ProviderErrorInvalidRequest  ProviderErrorKind = "invalid_request"
	ProviderErrorConfiguration   ProviderErrorKind = "configuration"
	ProviderErrorAuthentication  ProviderErrorKind = "authentication"
	ProviderErrorAuthorization   ProviderErrorKind = "authorization"
	ProviderErrorQuotaExhausted  ProviderErrorKind = "quota_exhausted"
	ProviderErrorRateLimited     ProviderErrorKind = "rate_limited"
	ProviderErrorTimeout         ProviderErrorKind = "timeout"
	ProviderErrorUnavailable     ProviderErrorKind = "provider_unavailable"
	ProviderErrorInvalidResponse ProviderErrorKind = "invalid_response"
	ProviderErrorCancelled       ProviderErrorKind = "cancelled"
)

func (kind ProviderErrorKind) Retryable() bool {
	switch kind {
	case ProviderErrorRateLimited,
		ProviderErrorTimeout,
		ProviderErrorUnavailable,
		ProviderErrorInvalidResponse,
		ProviderErrorCancelled:
		return true
	default:
		return false
	}
}

type ProviderOperation string

const (
	ProviderOperationQuestionGeneration  ProviderOperation = "question_generation"
	ProviderOperationAnswerTipGeneration ProviderOperation = "answer_tip_generation"
	ProviderOperationTranscription       ProviderOperation = "transcription"
	ProviderOperationSynthesis           ProviderOperation = "synthesis"
)

type ProviderError struct {
	Operation ProviderOperation
	Kind      ProviderErrorKind
	RequestID string
	cause     error
}

func NewProviderError(
	operation ProviderOperation,
	kind ProviderErrorKind,
	requestID string,
	cause error,
) *ProviderError {
	return &ProviderError{
		Operation: operation,
		Kind:      kind,
		RequestID: requestID,
		cause:     cause,
	}
}

func (err *ProviderError) Error() string {
	if err == nil {
		return "practice voice provider failed"
	}
	return "practice voice provider failed: " + string(err.Kind)
}

func (err *ProviderError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (err *ProviderError) Retryable() bool {
	return err != nil && err.Kind.Retryable()
}

type SpeechUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	AudioSeconds int
	Characters   int
}

type TranscriptionRequest struct {
	Audio platformmedia.AudioSource
}

type TranscriptionResult struct {
	ID         string
	Provider   string
	Model      string
	Transcript string
	Usage      SpeechUsage
}

type SpeechRecognizer interface {
	Transcribe(
		context.Context,
		TranscriptionRequest,
	) (TranscriptionResult, error)
}

// StreamingSpeechRecognizer exposes provider-authored transcript snapshots;
// the final TranscriptionResult remains the only durable result.
type StreamingSpeechRecognizer interface {
	SpeechRecognizer
	TranscribeStream(
		context.Context,
		TranscriptionRequest,
		TranscriptionObserver,
	) (TranscriptionResult, error)
}

type TranscriptionObserver interface {
	OnTranscriptionUpdate(context.Context, TranscriptionUpdate) error
}

type TranscriptionUpdate struct {
	Transcript string
	Final      bool
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

type SpeechSynthesizer interface {
	Synthesize(
		context.Context,
		SynthesisRequest,
	) (SynthesisResult, error)
}

type QuestionGenerationRequest struct {
	SystemPrompt string
	UserPrompt   string
}

type QuestionGenerator interface {
	GenerateQuestion(
		context.Context,
		QuestionGenerationRequest,
	) (string, error)
}

type AnswerTipGenerationRequest struct {
	SystemPrompt string
	UserPrompt   string
}

type AnswerTipGenerationResult struct {
	RequestID string
	Provider  string
	Model     string
	Content   string
}

type AnswerTipGenerator interface {
	GenerateAnswerTip(
		context.Context,
		AnswerTipGenerationRequest,
	) (AnswerTipGenerationResult, error)
}
