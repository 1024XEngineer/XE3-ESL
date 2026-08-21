package qianwen

import (
	"context"
	"errors"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type PracticeVoiceRecognizer struct {
	recognizer *speechRecognizer
}

func NewPracticeVoiceRecognizer(
	configuration ASRConfig,
	apiKey string,
) (*PracticeVoiceRecognizer, error) {
	recognizer, err := newSpeechRecognizer(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PracticeVoiceRecognizer{recognizer: recognizer}, nil
}

func NewPracticeRecordedVoiceRecognizer(
	configuration ASRConfig,
	apiKey string,
) (*PracticeVoiceRecognizer, error) {
	model, err := normalizeASRModel(configuration.Model)
	if err != nil {
		return nil, err
	}
	if model != "fun-asr-flash-2026-06-15" {
		return nil, errors.New(
			"Qianwen recorded Practice Voice requires fun-asr-flash-2026-06-15",
		)
	}
	configuration.Model = model
	return NewPracticeVoiceRecognizer(configuration, apiKey)
}

func (recognizer *PracticeVoiceRecognizer) Transcribe(
	ctx context.Context,
	request practiceinteraction.TranscriptionRequest,
) (practiceinteraction.TranscriptionResult, error) {
	if recognizer == nil || recognizer.recognizer == nil {
		return practiceinteraction.TranscriptionResult{},
			practiceinteraction.NewProviderError(
				practiceinteraction.ProviderOperationTranscription,
				practiceinteraction.ProviderErrorConfiguration,
				"",
				errors.New("Qianwen Practice Voice recognizer is required"),
			)
	}
	result, err := recognizer.recognizer.Transcribe(
		ctx,
		protocol.TranscriptionRequest{Audio: request.Audio},
	)
	if err != nil {
		return practiceinteraction.TranscriptionResult{},
			mapPracticeInteractionError(
				err,
				practiceinteraction.ProviderOperationTranscription,
			)
	}
	return practiceinteraction.TranscriptionResult{
		ID:         result.ID,
		Provider:   result.Provider,
		Model:      result.Model,
		Transcript: result.Transcript,
		Usage:      mapPracticeVoiceUsage(result.Usage),
	}, nil
}

func (recognizer *PracticeVoiceRecognizer) TranscribeStream(
	ctx context.Context,
	request practiceinteraction.StreamingTranscriptionRequest,
	observer practiceinteraction.TranscriptionObserver,
) (practiceinteraction.TranscriptionResult, error) {
	if recognizer == nil || recognizer.recognizer == nil || observer == nil ||
		recognizer.recognizer.model != "fun-asr-realtime" {
		return practiceinteraction.TranscriptionResult{},
			practiceinteraction.NewProviderError(
				practiceinteraction.ProviderOperationTranscription,
				practiceinteraction.ProviderErrorConfiguration,
				"",
				errors.New("Qianwen streaming Practice Voice recognizer is required"),
			)
	}
	result, err := recognizer.recognizer.transcribeRealtimePCM(
		ctx,
		request.PCM,
		request.SampleRate,
		practiceVoiceTranscriptionObserver{observer: observer},
	)
	if err != nil {
		return practiceinteraction.TranscriptionResult{},
			mapPracticeInteractionError(
				err,
				practiceinteraction.ProviderOperationTranscription,
			)
	}
	return practiceinteraction.TranscriptionResult{
		ID:         result.ID,
		Provider:   result.Provider,
		Model:      result.Model,
		Transcript: result.Transcript,
		Usage:      mapPracticeVoiceUsage(result.Usage),
	}, nil
}

type practiceVoiceTranscriptionObserver struct {
	observer practiceinteraction.TranscriptionObserver
}

func (observer practiceVoiceTranscriptionObserver) OnTranscriptionUpdate(
	ctx context.Context,
	update protocol.TranscriptionUpdate,
) error {
	return observer.observer.OnTranscriptionUpdate(
		ctx,
		practiceinteraction.TranscriptionUpdate{
			Transcript: update.Transcript,
			Final:      update.Final,
		},
	)
}

type PracticeVoiceSynthesizer struct {
	synthesizer *speechSynthesizer
}

func NewPracticeVoiceSynthesizer(
	configuration TTSConfig,
	apiKey string,
) (*PracticeVoiceSynthesizer, error) {
	synthesizer, err := newSpeechSynthesizer(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PracticeVoiceSynthesizer{synthesizer: synthesizer}, nil
}

func (synthesizer *PracticeVoiceSynthesizer) Synthesize(
	ctx context.Context,
	request practiceinteraction.SynthesisRequest,
) (practiceinteraction.SynthesisResult, error) {
	if synthesizer == nil || synthesizer.synthesizer == nil {
		return practiceinteraction.SynthesisResult{},
			practiceinteraction.NewProviderError(
				practiceinteraction.ProviderOperationSynthesis,
				practiceinteraction.ProviderErrorConfiguration,
				"",
				errors.New("Qianwen Practice Voice synthesizer is required"),
			)
	}
	result, err := synthesizer.synthesizer.Synthesize(
		ctx,
		protocol.SynthesisRequest{Text: request.Text},
	)
	if err != nil {
		return practiceinteraction.SynthesisResult{},
			mapPracticeInteractionError(
				err,
				practiceinteraction.ProviderOperationSynthesis,
			)
	}
	return practiceinteraction.SynthesisResult{
		RequestID: result.RequestID,
		Provider:  result.Provider,
		Model:     result.Model,
		AudioID:   result.AudioID,
		Audio:     result.Audio,
		Usage:     mapPracticeVoiceUsage(result.Usage),
	}, nil
}

type PracticeQuestionGenerator struct {
	generator *textClient
}

func NewPracticeQuestionGenerator(
	configuration TextConfig,
	apiKey string,
) (*PracticeQuestionGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PracticeQuestionGenerator{generator: generator}, nil
}

func (generator *PracticeQuestionGenerator) GenerateQuestion(
	ctx context.Context,
	request practiceinteraction.QuestionGenerationRequest,
) (string, error) {
	if generator == nil || generator.generator == nil {
		return "", practiceinteraction.NewProviderError(
			practiceinteraction.ProviderOperationQuestionGeneration,
			practiceinteraction.ProviderErrorConfiguration,
			"",
			errors.New("Qianwen Practice Interaction question generator is required"),
		)
	}
	result, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
	})
	if err != nil {
		return "", mapPracticeInteractionError(
			err,
			practiceinteraction.ProviderOperationQuestionGeneration,
		)
	}
	return result.Content, nil
}

type PracticeAnswerTipGenerator struct {
	generator *textClient
}

func NewPracticeAnswerTipGenerator(
	configuration TextConfig,
	apiKey string,
) (*PracticeAnswerTipGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PracticeAnswerTipGenerator{generator: generator}, nil
}

func (generator *PracticeAnswerTipGenerator) GenerateAnswerTip(
	ctx context.Context,
	request practiceinteraction.AnswerTipGenerationRequest,
) (practiceinteraction.AnswerTipGenerationResult, error) {
	if generator == nil || generator.generator == nil {
		return practiceinteraction.AnswerTipGenerationResult{},
			practiceinteraction.NewProviderError(
				practiceinteraction.ProviderOperationAnswerTipGeneration,
				practiceinteraction.ProviderErrorConfiguration,
				"",
				errors.New("Qianwen Practice Interaction answer Tip generator is required"),
			)
	}
	result, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
	})
	if err != nil {
		return practiceinteraction.AnswerTipGenerationResult{},
			mapPracticeInteractionError(
				err,
				practiceinteraction.ProviderOperationAnswerTipGeneration,
			)
	}
	return practiceinteraction.AnswerTipGenerationResult{
		RequestID: result.ID,
		Provider:  result.Provider,
		Model:     result.Model,
		Content:   result.Content,
	}, nil
}

func mapPracticeVoiceUsage(usage protocol.SpeechUsage) practiceinteraction.SpeechUsage {
	return practiceinteraction.SpeechUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		AudioSeconds: usage.AudioSeconds,
		Characters:   usage.Characters,
	}
}

func mapPracticeInteractionError(
	err error,
	operation practiceinteraction.ProviderOperation,
) error {
	var speechError *protocol.SpeechError
	if errors.As(err, &speechError) {
		return practiceinteraction.NewProviderError(
			operation,
			mapPracticeInteractionErrorKind(speechError.Kind),
			speechError.RequestID,
			err,
		)
	}
	var generationError *protocol.GenerationError
	if errors.As(err, &generationError) {
		return practiceinteraction.NewProviderError(
			operation,
			mapPracticeInteractionErrorKind(generationError.Kind),
			generationError.RequestID,
			err,
		)
	}
	return practiceinteraction.NewProviderError(
		operation,
		practiceinteraction.ProviderErrorUnavailable,
		"",
		err,
	)
}

func mapPracticeInteractionErrorKind(kind protocol.ErrorKind) practiceinteraction.ProviderErrorKind {
	switch kind {
	case protocol.ErrorInvalidRequest:
		return practiceinteraction.ProviderErrorInvalidRequest
	case protocol.ErrorConfiguration:
		return practiceinteraction.ProviderErrorConfiguration
	case protocol.ErrorAuthentication:
		return practiceinteraction.ProviderErrorAuthentication
	case protocol.ErrorAuthorization:
		return practiceinteraction.ProviderErrorAuthorization
	case protocol.ErrorQuotaExhausted:
		return practiceinteraction.ProviderErrorQuotaExhausted
	case protocol.ErrorRateLimited:
		return practiceinteraction.ProviderErrorRateLimited
	case protocol.ErrorTimeout:
		return practiceinteraction.ProviderErrorTimeout
	case protocol.ErrorProviderUnavailable:
		return practiceinteraction.ProviderErrorUnavailable
	case protocol.ErrorInvalidResponse:
		return practiceinteraction.ProviderErrorInvalidResponse
	case protocol.ErrorCancelled:
		return practiceinteraction.ProviderErrorCancelled
	default:
		return practiceinteraction.ProviderErrorUnavailable
	}
}

var (
	_ practiceinteraction.StreamingSpeechRecognizer = (*PracticeVoiceRecognizer)(nil)
	_ practiceinteraction.SpeechSynthesizer         = (*PracticeVoiceSynthesizer)(nil)
	_ practiceinteraction.QuestionGenerator         = (*PracticeQuestionGenerator)(nil)
	_ practiceinteraction.AnswerTipGenerator        = (*PracticeAnswerTipGenerator)(nil)
)
