package qianwen

import (
	"context"
	"errors"
	"testing"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestMapPracticeVoiceErrorPreservesStableProviderMetadata(t *testing.T) {
	for _, test := range []struct {
		name string
		kind protocol.ErrorKind
		want practicevoice.ProviderErrorKind
	}{
		{"invalid request", protocol.ErrorInvalidRequest, practicevoice.ProviderErrorInvalidRequest},
		{"configuration", protocol.ErrorConfiguration, practicevoice.ProviderErrorConfiguration},
		{"authentication", protocol.ErrorAuthentication, practicevoice.ProviderErrorAuthentication},
		{"authorization", protocol.ErrorAuthorization, practicevoice.ProviderErrorAuthorization},
		{"quota", protocol.ErrorQuotaExhausted, practicevoice.ProviderErrorQuotaExhausted},
		{"rate limited", protocol.ErrorRateLimited, practicevoice.ProviderErrorRateLimited},
		{"timeout", protocol.ErrorTimeout, practicevoice.ProviderErrorTimeout},
		{"unavailable", protocol.ErrorProviderUnavailable, practicevoice.ProviderErrorUnavailable},
		{"invalid response", protocol.ErrorInvalidResponse, practicevoice.ProviderErrorInvalidResponse},
		{"cancelled", protocol.ErrorCancelled, practicevoice.ProviderErrorCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("provider detail")
			mapped := mapPracticeVoiceError(
				protocol.NewSpeechError(
					protocol.SpeechOperationTranscription,
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
		protocol.NewGenerationError(
			protocol.ErrorRateLimited,
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
