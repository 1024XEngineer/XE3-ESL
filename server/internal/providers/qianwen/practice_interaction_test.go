package qianwen

import (
	"context"
	"errors"
	"testing"
	"time"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

func TestMapPracticeInteractionErrorPreservesStableProviderMetadata(t *testing.T) {
	for _, test := range []struct {
		name string
		kind protocol.ErrorKind
		want practiceinteraction.ProviderErrorKind
	}{
		{"invalid request", protocol.ErrorInvalidRequest, practiceinteraction.ProviderErrorInvalidRequest},
		{"configuration", protocol.ErrorConfiguration, practiceinteraction.ProviderErrorConfiguration},
		{"authentication", protocol.ErrorAuthentication, practiceinteraction.ProviderErrorAuthentication},
		{"authorization", protocol.ErrorAuthorization, practiceinteraction.ProviderErrorAuthorization},
		{"quota", protocol.ErrorQuotaExhausted, practiceinteraction.ProviderErrorQuotaExhausted},
		{"rate limited", protocol.ErrorRateLimited, practiceinteraction.ProviderErrorRateLimited},
		{"timeout", protocol.ErrorTimeout, practiceinteraction.ProviderErrorTimeout},
		{"unavailable", protocol.ErrorProviderUnavailable, practiceinteraction.ProviderErrorUnavailable},
		{"invalid response", protocol.ErrorInvalidResponse, practiceinteraction.ProviderErrorInvalidResponse},
		{"cancelled", protocol.ErrorCancelled, practiceinteraction.ProviderErrorCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("provider detail")
			mapped := mapPracticeInteractionError(
				protocol.NewSpeechError(
					protocol.SpeechOperationTranscription,
					test.kind,
					503,
					"ProviderCode",
					"request-1",
					cause,
				),
				practiceinteraction.ProviderOperationTranscription,
			)
			var providerError *practiceinteraction.ProviderError
			if !errors.As(mapped, &providerError) {
				t.Fatalf("mapped error = %T, want ProviderError", mapped)
			}
			if providerError.Operation != practiceinteraction.ProviderOperationTranscription ||
				providerError.Kind != test.want ||
				providerError.RequestID != "request-1" ||
				!errors.Is(providerError, cause) {
				t.Fatalf("ProviderError = %#v", providerError)
			}
		})
	}
}

func TestMapPracticeInteractionGenerationErrorUsesQuestionOperation(t *testing.T) {
	mapped := mapPracticeInteractionError(
		protocol.NewGenerationError(
			protocol.ErrorRateLimited,
			429,
			"Throttled",
			"question-request",
			errors.New("limited"),
		),
		practiceinteraction.ProviderOperationQuestionGeneration,
	)
	var providerError *practiceinteraction.ProviderError
	if !errors.As(mapped, &providerError) ||
		providerError.Operation != practiceinteraction.ProviderOperationQuestionGeneration ||
		providerError.Kind != practiceinteraction.ProviderErrorRateLimited ||
		providerError.RequestID != "question-request" ||
		!providerError.Retryable() {
		t.Fatalf("mapped error = %#v", mapped)
	}
}

func TestPracticeVoiceAdaptersRejectMissingProvider(t *testing.T) {
	var recognizer *PracticeVoiceRecognizer
	if _, err := recognizer.Transcribe(
		context.Background(),
		practiceinteraction.TranscriptionRequest{},
	); !isPracticeInteractionConfigurationError(err) {
		t.Fatalf("recognizer error = %v", err)
	}
	if _, err := recognizer.TranscribeStream(
		context.Background(),
		practiceinteraction.StreamingTranscriptionRequest{},
		&practiceVoiceObserverRecorder{},
	); !isPracticeInteractionConfigurationError(err) {
		t.Fatalf("streaming recognizer error = %v", err)
	}

	var synthesizer *PracticeVoiceSynthesizer
	if _, err := synthesizer.Synthesize(
		context.Background(),
		practiceinteraction.SynthesisRequest{},
	); !isPracticeInteractionConfigurationError(err) {
		t.Fatalf("synthesizer error = %v", err)
	}

	var generator *PracticeQuestionGenerator
	if _, err := generator.GenerateQuestion(
		context.Background(),
		practiceinteraction.QuestionGenerationRequest{},
	); !isPracticeInteractionConfigurationError(err) {
		t.Fatalf("generator error = %v", err)
	}

	var translator *Translator
	if _, err := translator.Translate(
		context.Background(),
		sharedtranslation.Request{},
	); !isTranslationConfigurationError(err) {
		t.Fatalf("translator error = %v", err)
	}

	var tipGenerator *PracticeAnswerTipGenerator
	if _, err := tipGenerator.GenerateAnswerTip(
		context.Background(),
		practiceinteraction.AnswerTipGenerationRequest{},
	); !isPracticeInteractionConfigurationError(err) {
		t.Fatalf("Tip generator error = %v", err)
	}
}

func TestPracticeVoiceStreamingObserverMapsProviderSnapshot(t *testing.T) {
	recorder := &practiceVoiceObserverRecorder{}
	adapter := practiceVoiceTranscriptionObserver{observer: recorder}
	if err := adapter.OnTranscriptionUpdate(
		context.Background(),
		protocol.TranscriptionUpdate{
			Transcript: "An answer in progress.",
			Final:      true,
		},
	); err != nil {
		t.Fatalf("observe update: %v", err)
	}
	if recorder.update.Transcript != "An answer in progress." ||
		!recorder.update.Final {
		t.Fatalf("mapped update = %#v", recorder.update)
	}
}

func TestPracticeVoiceStreamingRejectsNonRealtimeModel(t *testing.T) {
	recognizer := &PracticeVoiceRecognizer{
		recognizer: &speechRecognizer{model: "fun-asr-flash-2026-06-15"},
	}
	_, err := recognizer.TranscribeStream(
		context.Background(),
		practiceinteraction.StreamingTranscriptionRequest{},
		&practiceVoiceObserverRecorder{},
	)
	if !isPracticeInteractionConfigurationError(err) {
		t.Fatalf("non-realtime model error = %v", err)
	}
}

func TestPracticeRecordedVoiceRecognizerRequiresFlashModel(t *testing.T) {
	recorded, err := NewPracticeRecordedVoiceRecognizer(
		ASRConfig{
			BaseURL: "https://dashscope.aliyuncs.com/api/v1",
			Model:   "fun-asr-flash-2026-06-15",
			Timeout: time.Second,
		},
		"test-key",
	)
	if err != nil || recorded == nil || recorded.recognizer == nil ||
		recorded.recognizer.model != "fun-asr-flash-2026-06-15" {
		t.Fatalf("recorded recognizer = %#v, %v", recorded, err)
	}

	_, err = NewPracticeRecordedVoiceRecognizer(
		ASRConfig{
			BaseURL: "https://dashscope.aliyuncs.com/api/v1",
			Model:   "fun-asr-realtime",
			Timeout: time.Second,
		},
		"test-key",
	)
	if err == nil {
		t.Fatal("recorded recognizer accepted realtime model")
	}
}

type practiceVoiceObserverRecorder struct {
	update practiceinteraction.TranscriptionUpdate
}

func (recorder *practiceVoiceObserverRecorder) OnTranscriptionUpdate(
	_ context.Context,
	update practiceinteraction.TranscriptionUpdate,
) error {
	recorder.update = update
	return nil
}

func isTranslationConfigurationError(err error) bool {
	var providerError *sharedtranslation.ProviderError
	return errors.As(err, &providerError) &&
		providerError.Kind == sharedtranslation.ProviderErrorConfiguration
}

func isPracticeInteractionConfigurationError(err error) bool {
	var providerError *practiceinteraction.ProviderError
	return errors.As(err, &providerError) &&
		providerError.Kind == practiceinteraction.ProviderErrorConfiguration
}
