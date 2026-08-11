package qianwen

import (
	"context"
	"errors"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
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
	request practicevoice.TranscriptionRequest,
) (practicevoice.TranscriptionResult, error) {
	if recognizer == nil || recognizer.recognizer == nil {
		return practicevoice.TranscriptionResult{},
			practicevoice.NewProviderError(
				practicevoice.ProviderOperationTranscription,
				practicevoice.ProviderErrorConfiguration,
				"",
				errors.New("Qianwen Practice Voice recognizer is required"),
			)
	}
	result, err := recognizer.recognizer.Transcribe(
		ctx,
		protocol.TranscriptionRequest{Audio: request.Audio},
	)
	if err != nil {
		return practicevoice.TranscriptionResult{},
			mapPracticeVoiceError(
				err,
				practicevoice.ProviderOperationTranscription,
			)
	}
	return practicevoice.TranscriptionResult{
		ID:         result.ID,
		Provider:   result.Provider,
		Model:      result.Model,
		Transcript: result.Transcript,
		Usage:      mapPracticeVoiceUsage(result.Usage),
	}, nil
}

func (recognizer *PracticeVoiceRecognizer) TranscribeStream(
	ctx context.Context,
	request practicevoice.StreamingTranscriptionRequest,
	observer practicevoice.TranscriptionObserver,
) (practicevoice.TranscriptionResult, error) {
	if recognizer == nil || recognizer.recognizer == nil || observer == nil ||
		recognizer.recognizer.model != "fun-asr-realtime" {
		return practicevoice.TranscriptionResult{},
			practicevoice.NewProviderError(
				practicevoice.ProviderOperationTranscription,
				practicevoice.ProviderErrorConfiguration,
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
		return practicevoice.TranscriptionResult{},
			mapPracticeVoiceError(
				err,
				practicevoice.ProviderOperationTranscription,
			)
	}
	return practicevoice.TranscriptionResult{
		ID:         result.ID,
		Provider:   result.Provider,
		Model:      result.Model,
		Transcript: result.Transcript,
		Usage:      mapPracticeVoiceUsage(result.Usage),
	}, nil
}

type practiceVoiceTranscriptionObserver struct {
	observer practicevoice.TranscriptionObserver
}

func (observer practiceVoiceTranscriptionObserver) OnTranscriptionUpdate(
	ctx context.Context,
	update protocol.TranscriptionUpdate,
) error {
	return observer.observer.OnTranscriptionUpdate(
		ctx,
		practicevoice.TranscriptionUpdate{
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
	request practicevoice.SynthesisRequest,
) (practicevoice.SynthesisResult, error) {
	if synthesizer == nil || synthesizer.synthesizer == nil {
		return practicevoice.SynthesisResult{},
			practicevoice.NewProviderError(
				practicevoice.ProviderOperationSynthesis,
				practicevoice.ProviderErrorConfiguration,
				"",
				errors.New("Qianwen Practice Voice synthesizer is required"),
			)
	}
	result, err := synthesizer.synthesizer.Synthesize(
		ctx,
		protocol.SynthesisRequest{Text: request.Text},
	)
	if err != nil {
		return practicevoice.SynthesisResult{},
			mapPracticeVoiceError(
				err,
				practicevoice.ProviderOperationSynthesis,
			)
	}
	return practicevoice.SynthesisResult{
		RequestID: result.RequestID,
		Provider:  result.Provider,
		Model:     result.Model,
		AudioID:   result.AudioID,
		Audio:     result.Audio,
		Usage:     mapPracticeVoiceUsage(result.Usage),
	}, nil
}

type PracticeVoiceQuestionGenerator struct {
	generator *textClient
}

func NewPracticeVoiceQuestionGenerator(
	configuration TextConfig,
	apiKey string,
) (*PracticeVoiceQuestionGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PracticeVoiceQuestionGenerator{generator: generator}, nil
}

func (generator *PracticeVoiceQuestionGenerator) GenerateQuestion(
	ctx context.Context,
	request practicevoice.QuestionGenerationRequest,
) (string, error) {
	if generator == nil || generator.generator == nil {
		return "", practicevoice.NewProviderError(
			practicevoice.ProviderOperationQuestionGeneration,
			practicevoice.ProviderErrorConfiguration,
			"",
			errors.New("Qianwen Practice Voice question generator is required"),
		)
	}
	result, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
	})
	if err != nil {
		return "", mapPracticeVoiceError(
			err,
			practicevoice.ProviderOperationQuestionGeneration,
		)
	}
	return result.Content, nil
}

type PracticeVoiceAnswerTipGenerator struct {
	generator *textClient
}

func NewPracticeVoiceAnswerTipGenerator(
	configuration TextConfig,
	apiKey string,
) (*PracticeVoiceAnswerTipGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PracticeVoiceAnswerTipGenerator{generator: generator}, nil
}

func (generator *PracticeVoiceAnswerTipGenerator) GenerateAnswerTip(
	ctx context.Context,
	request practicevoice.AnswerTipGenerationRequest,
) (practicevoice.AnswerTipGenerationResult, error) {
	if generator == nil || generator.generator == nil {
		return practicevoice.AnswerTipGenerationResult{},
			practicevoice.NewProviderError(
				practicevoice.ProviderOperationAnswerTipGeneration,
				practicevoice.ProviderErrorConfiguration,
				"",
				errors.New("Qianwen Practice Voice answer Tip generator is required"),
			)
	}
	result, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
	})
	if err != nil {
		return practicevoice.AnswerTipGenerationResult{},
			mapPracticeVoiceError(
				err,
				practicevoice.ProviderOperationAnswerTipGeneration,
			)
	}
	return practicevoice.AnswerTipGenerationResult{
		RequestID: result.ID,
		Provider:  result.Provider,
		Model:     result.Model,
		Content:   result.Content,
	}, nil
}

func mapPracticeVoiceUsage(usage protocol.SpeechUsage) practicevoice.SpeechUsage {
	return practicevoice.SpeechUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		AudioSeconds: usage.AudioSeconds,
		Characters:   usage.Characters,
	}
}

func mapPracticeVoiceError(
	err error,
	operation practicevoice.ProviderOperation,
) error {
	var speechError *protocol.SpeechError
	if errors.As(err, &speechError) {
		return practicevoice.NewProviderError(
			operation,
			mapPracticeVoiceErrorKind(speechError.Kind),
			speechError.RequestID,
			err,
		)
	}
	var generationError *protocol.GenerationError
	if errors.As(err, &generationError) {
		return practicevoice.NewProviderError(
			operation,
			mapPracticeVoiceErrorKind(generationError.Kind),
			generationError.RequestID,
			err,
		)
	}
	return practicevoice.NewProviderError(
		operation,
		practicevoice.ProviderErrorUnavailable,
		"",
		err,
	)
}

func mapPracticeVoiceErrorKind(kind protocol.ErrorKind) practicevoice.ProviderErrorKind {
	switch kind {
	case protocol.ErrorInvalidRequest:
		return practicevoice.ProviderErrorInvalidRequest
	case protocol.ErrorConfiguration:
		return practicevoice.ProviderErrorConfiguration
	case protocol.ErrorAuthentication:
		return practicevoice.ProviderErrorAuthentication
	case protocol.ErrorAuthorization:
		return practicevoice.ProviderErrorAuthorization
	case protocol.ErrorQuotaExhausted:
		return practicevoice.ProviderErrorQuotaExhausted
	case protocol.ErrorRateLimited:
		return practicevoice.ProviderErrorRateLimited
	case protocol.ErrorTimeout:
		return practicevoice.ProviderErrorTimeout
	case protocol.ErrorProviderUnavailable:
		return practicevoice.ProviderErrorUnavailable
	case protocol.ErrorInvalidResponse:
		return practicevoice.ProviderErrorInvalidResponse
	case protocol.ErrorCancelled:
		return practicevoice.ProviderErrorCancelled
	default:
		return practicevoice.ProviderErrorUnavailable
	}
}

var (
	_ practicevoice.StreamingSpeechRecognizer = (*PracticeVoiceRecognizer)(nil)
	_ practicevoice.SpeechSynthesizer         = (*PracticeVoiceSynthesizer)(nil)
	_ practicevoice.QuestionGenerator         = (*PracticeVoiceQuestionGenerator)(nil)
	_ practicevoice.AnswerTipGenerator        = (*PracticeVoiceAnswerTipGenerator)(nil)
)
