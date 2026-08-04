package qianwen

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
)

func TestMapPracticeVoiceErrorPreservesStableProviderMetadata(t *testing.T) {
	for _, test := range []struct {
		name string
		kind ai.ErrorKind
		want practicevoice.ProviderErrorKind
	}{
		{"invalid request", ai.ErrorInvalidRequest, practicevoice.ProviderErrorInvalidRequest},
		{"configuration", ai.ErrorConfiguration, practicevoice.ProviderErrorConfiguration},
		{"authentication", ai.ErrorAuthentication, practicevoice.ProviderErrorAuthentication},
		{"authorization", ai.ErrorAuthorization, practicevoice.ProviderErrorAuthorization},
		{"quota", ai.ErrorQuotaExhausted, practicevoice.ProviderErrorQuotaExhausted},
		{"rate limited", ai.ErrorRateLimited, practicevoice.ProviderErrorRateLimited},
		{"timeout", ai.ErrorTimeout, practicevoice.ProviderErrorTimeout},
		{"unavailable", ai.ErrorProviderUnavailable, practicevoice.ProviderErrorUnavailable},
		{"invalid response", ai.ErrorInvalidResponse, practicevoice.ProviderErrorInvalidResponse},
		{"cancelled", ai.ErrorCancelled, practicevoice.ProviderErrorCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("provider detail")
			mapped := mapPracticeVoiceError(
				ai.NewSpeechError(
					ai.SpeechOperationTranscription,
					test.kind,
					503,
					"ProviderCode",
					"request-1",
					cause,
				),
				practicevoice.ProviderOperationTranscription,
			)
			var providerError *practicevoice.ProviderError
			if !errors.As(mapped, &providerError) {
				t.Fatalf("mapped error = %T, want ProviderError", mapped)
			}
			if providerError.Operation != practicevoice.ProviderOperationTranscription ||
				providerError.Kind != test.want ||
				providerError.RequestID != "request-1" ||
				!errors.Is(providerError, cause) {
				t.Fatalf("ProviderError = %#v", providerError)
			}
		})
	}
}

func TestMapPracticeVoiceGenerationErrorUsesQuestionOperation(t *testing.T) {
	mapped := mapPracticeVoiceError(
		ai.NewGenerationError(
			ai.ErrorRateLimited,
			429,
			"Throttled",
			"question-request",
			errors.New("limited"),
		),
		practicevoice.ProviderOperationQuestionGeneration,
	)
	var providerError *practicevoice.ProviderError
	if !errors.As(mapped, &providerError) ||
		providerError.Operation != practicevoice.ProviderOperationQuestionGeneration ||
		providerError.Kind != practicevoice.ProviderErrorRateLimited ||
		providerError.RequestID != "question-request" ||
		!providerError.Retryable() {
		t.Fatalf("mapped error = %#v", mapped)
	}
}

func TestPracticeVoiceAdaptersRejectMissingProvider(t *testing.T) {
	var recognizer *PracticeVoiceRecognizer
	if _, err := recognizer.Transcribe(
		context.Background(),
		practicevoice.TranscriptionRequest{},
	); !isPracticeVoiceConfigurationError(err) {
		t.Fatalf("recognizer error = %v", err)
	}

	var synthesizer *PracticeVoiceSynthesizer
	if _, err := synthesizer.Synthesize(
		context.Background(),
		practicevoice.SynthesisRequest{},
	); !isPracticeVoiceConfigurationError(err) {
		t.Fatalf("synthesizer error = %v", err)
	}

	var generator *PracticeVoiceQuestionGenerator
	if _, err := generator.GenerateQuestion(
		context.Background(),
		practicevoice.QuestionGenerationRequest{},
	); !isPracticeVoiceConfigurationError(err) {
		t.Fatalf("generator error = %v", err)
	}
}

func isPracticeVoiceConfigurationError(err error) bool {
	var providerError *practicevoice.ProviderError
	return errors.As(err, &providerError) &&
		providerError.Kind == practicevoice.ProviderErrorConfiguration
}
