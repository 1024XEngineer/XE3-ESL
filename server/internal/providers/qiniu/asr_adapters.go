package qiniu

import (
	"context"
	"errors"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
)

const qiniuProviderName = "qiniu"

type AgentVoiceRecognizer struct {
	client *asrClient
}

func NewAgentVoiceRecognizer(
	configuration ASRConfig,
	apiKey string,
) (*AgentVoiceRecognizer, error) {
	client, err := newASR(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &AgentVoiceRecognizer{client: client}, nil
}

func (recognizer *AgentVoiceRecognizer) Transcribe(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
) (agentvoice.TranscriptionResult, error) {
	return recognizer.transcribe(ctx, request, nil)
}

func (recognizer *AgentVoiceRecognizer) TranscribeStream(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
	observer agentvoice.TranscriptionObserver,
) (agentvoice.TranscriptionResult, error) {
	if observer == nil {
		return agentvoice.TranscriptionResult{}, agentvoice.NewSpeechError(
			agentvoice.SpeechOperationTranscription,
			agentvoice.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("Qiniu Agent Voice transcription observer is required"),
		)
	}
	return recognizer.transcribe(
		ctx,
		request,
		func(ctx context.Context, update asrUpdate) error {
			return observer.OnTranscriptionUpdate(
				ctx,
				agentvoice.TranscriptionUpdate{
					Transcript: update.transcript,
					Final:      update.final,
				},
			)
		},
	)
}

func (recognizer *AgentVoiceRecognizer) transcribe(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
	observer asrObserver,
) (agentvoice.TranscriptionResult, error) {
	if recognizer == nil || recognizer.client == nil {
		return agentvoice.TranscriptionResult{}, agentvoice.NewSpeechError(
			agentvoice.SpeechOperationTranscription,
			agentvoice.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("Qiniu Agent Voice recognizer is required"),
		)
	}
	if err := agentvoice.ValidateTranscriptionRequest(request); err != nil {
		return agentvoice.TranscriptionResult{}, agentvoice.NewSpeechError(
			agentvoice.SpeechOperationTranscription,
			agentvoice.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("Agent Voice transcription request is invalid"),
		)
	}
	result, err := recognizer.client.transcribeWAV(ctx, request.Audio, observer)
	if err != nil {
		return agentvoice.TranscriptionResult{}, mapAgentASRError(err)
	}
	return agentvoice.TranscriptionResult{
		ID:         result.id,
		Provider:   qiniuProviderName,
		Model:      recognizer.client.model,
		Transcript: result.transcript,
		Usage: agentvoice.SpeechUsage{
			AudioSeconds: result.audioSeconds,
		},
	}, nil
}

type PracticeVoiceRecognizer struct {
	client *asrClient
}

func NewPracticeVoiceRecognizer(
	configuration ASRConfig,
	apiKey string,
) (*PracticeVoiceRecognizer, error) {
	client, err := newASR(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PracticeVoiceRecognizer{client: client}, nil
}

func (recognizer *PracticeVoiceRecognizer) Transcribe(
	ctx context.Context,
	request practicevoice.TranscriptionRequest,
) (practicevoice.TranscriptionResult, error) {
	if recognizer == nil || recognizer.client == nil {
		return practicevoice.TranscriptionResult{}, practicevoice.NewProviderError(
			practicevoice.ProviderOperationTranscription,
			practicevoice.ProviderErrorConfiguration,
			"",
			errors.New("Qiniu Practice Voice recognizer is required"),
		)
	}
	result, err := recognizer.client.transcribeWAV(ctx, request.Audio, nil)
	if err != nil {
		return practicevoice.TranscriptionResult{}, mapPracticeASRError(err)
	}
	return practiceTranscriptionResult(recognizer.client.model, result), nil
}

func (recognizer *PracticeVoiceRecognizer) TranscribeStream(
	ctx context.Context,
	request practicevoice.StreamingTranscriptionRequest,
	observer practicevoice.TranscriptionObserver,
) (practicevoice.TranscriptionResult, error) {
	if recognizer == nil || recognizer.client == nil || observer == nil {
		return practicevoice.TranscriptionResult{}, practicevoice.NewProviderError(
			practicevoice.ProviderOperationTranscription,
			practicevoice.ProviderErrorConfiguration,
			"",
			errors.New("Qiniu streaming Practice Voice recognizer is required"),
		)
	}
	result, err := recognizer.client.transcribePCM(
		ctx,
		request.PCM,
		request.SampleRate,
		func(ctx context.Context, update asrUpdate) error {
			return observer.OnTranscriptionUpdate(
				ctx,
				practicevoice.TranscriptionUpdate{
					Transcript: update.transcript,
					Final:      update.final,
				},
			)
		},
	)
	if err != nil {
		return practicevoice.TranscriptionResult{}, mapPracticeASRError(err)
	}
	return practiceTranscriptionResult(recognizer.client.model, result), nil
}

func practiceTranscriptionResult(
	model string,
	result asrResult,
) practicevoice.TranscriptionResult {
	return practicevoice.TranscriptionResult{
		ID:         result.id,
		Provider:   qiniuProviderName,
		Model:      model,
		Transcript: result.transcript,
		Usage: practicevoice.SpeechUsage{
			AudioSeconds: result.audioSeconds,
		},
	}
}

func mapAgentASRError(err error) error {
	failure := asASRError(err)
	return agentvoice.NewSpeechError(
		agentvoice.SpeechOperationTranscription,
		mapAgentASRErrorKind(failure.kind),
		failure.statusCode,
		"",
		failure.requestID,
		failure,
	)
}

func mapAgentASRErrorKind(kind asrErrorKind) agentvoice.ErrorKind {
	switch kind {
	case asrErrorInvalidRequest:
		return agentvoice.ErrorInvalidRequest
	case asrErrorConfiguration:
		return agentvoice.ErrorConfiguration
	case asrErrorAuthentication:
		return agentvoice.ErrorAuthentication
	case asrErrorAuthorization:
		return agentvoice.ErrorAuthorization
	case asrErrorQuota:
		return agentvoice.ErrorQuotaExhausted
	case asrErrorRateLimited:
		return agentvoice.ErrorRateLimited
	case asrErrorTimeout:
		return agentvoice.ErrorTimeout
	case asrErrorInvalidResponse:
		return agentvoice.ErrorInvalidResponse
	case asrErrorCancelled:
		return agentvoice.ErrorCancelled
	default:
		return agentvoice.ErrorProviderUnavailable
	}
}

func mapPracticeASRError(err error) error {
	failure := asASRError(err)
	return practicevoice.NewProviderError(
		practicevoice.ProviderOperationTranscription,
		mapPracticeASRErrorKind(failure.kind),
		failure.requestID,
		failure,
	)
}

func mapPracticeASRErrorKind(kind asrErrorKind) practicevoice.ProviderErrorKind {
	switch kind {
	case asrErrorInvalidRequest:
		return practicevoice.ProviderErrorInvalidRequest
	case asrErrorConfiguration:
		return practicevoice.ProviderErrorConfiguration
	case asrErrorAuthentication:
		return practicevoice.ProviderErrorAuthentication
	case asrErrorAuthorization:
		return practicevoice.ProviderErrorAuthorization
	case asrErrorQuota:
		return practicevoice.ProviderErrorQuotaExhausted
	case asrErrorRateLimited:
		return practicevoice.ProviderErrorRateLimited
	case asrErrorTimeout:
		return practicevoice.ProviderErrorTimeout
	case asrErrorInvalidResponse:
		return practicevoice.ProviderErrorInvalidResponse
	case asrErrorCancelled:
		return practicevoice.ProviderErrorCancelled
	default:
		return practicevoice.ProviderErrorUnavailable
	}
}

func asASRError(err error) *asrError {
	var failure *asrError
	if errors.As(err, &failure) {
		return failure
	}
	return &asrError{
		kind:  asrErrorUnavailable,
		cause: errors.New("Qiniu ASR failed"),
	}
}

var (
	_ agentvoice.StreamingSpeechRecognizer    = (*AgentVoiceRecognizer)(nil)
	_ practicevoice.StreamingSpeechRecognizer = (*PracticeVoiceRecognizer)(nil)
)
