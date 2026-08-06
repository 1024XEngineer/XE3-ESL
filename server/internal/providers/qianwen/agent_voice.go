package qianwen

import (
	"context"
	"errors"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type AgentVoiceRecognizer struct {
	recognizer *speechRecognizer
}

func (synthesizer *AgentVoiceSynthesizer) OpenAssistantSpeech(
	ctx context.Context,
) (agentconversation.AssistantSpeechSession, error) {
	if synthesizer == nil || synthesizer.synthesizer == nil {
		return nil, errors.New(
			"qianwen: Agent assistant speech synthesizer is required",
		)
	}
	return synthesizer.synthesizer.openRealtimeSpeech(ctx)
}

func NewAgentVoiceRecognizer(
	configuration ASRConfig,
	apiKey string,
) (*AgentVoiceRecognizer, error) {
	recognizer, err := newSpeechRecognizer(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &AgentVoiceRecognizer{recognizer: recognizer}, nil
}

func (recognizer *AgentVoiceRecognizer) Transcribe(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
) (agentvoice.TranscriptionResult, error) {
	if recognizer == nil || recognizer.recognizer == nil {
		return agentvoice.TranscriptionResult{}, agentvoice.NewSpeechError(
			agentvoice.SpeechOperationTranscription,
			agentvoice.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("qianwen: Agent Voice recognizer is required"),
		)
	}
	result, err := recognizer.recognizer.Transcribe(
		ctx,
		protocol.TranscriptionRequest{Audio: request.Audio},
	)
	if err != nil {
		return agentvoice.TranscriptionResult{}, mapAgentVoiceError(
			err,
			agentvoice.SpeechOperationTranscription,
		)
	}
	return agentVoiceTranscriptionResult(result), nil
}

func (recognizer *AgentVoiceRecognizer) TranscribeStream(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
	observer agentvoice.TranscriptionObserver,
) (agentvoice.TranscriptionResult, error) {
	if recognizer == nil || recognizer.recognizer == nil || observer == nil {
		return agentvoice.TranscriptionResult{}, agentvoice.NewSpeechError(
			agentvoice.SpeechOperationTranscription,
			agentvoice.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("qianwen: streaming Agent Voice recognizer is required"),
		)
	}
	result, err := recognizer.recognizer.TranscribeStream(
		ctx,
		protocol.TranscriptionRequest{Audio: request.Audio},
		agentVoiceTranscriptionObserver{observer: observer},
	)
	if err != nil {
		return agentvoice.TranscriptionResult{}, mapAgentVoiceError(
			err,
			agentvoice.SpeechOperationTranscription,
		)
	}
	return agentVoiceTranscriptionResult(result), nil
}

type agentVoiceTranscriptionObserver struct {
	observer agentvoice.TranscriptionObserver
}

func (observer agentVoiceTranscriptionObserver) OnTranscriptionUpdate(
	ctx context.Context,
	update protocol.TranscriptionUpdate,
) error {
	return observer.observer.OnTranscriptionUpdate(
		ctx,
		agentvoice.TranscriptionUpdate{
			Transcript: update.Transcript,
			Final:      update.Final,
		},
	)
}

type AgentVoiceSynthesizer struct {
	synthesizer *speechSynthesizer
}

func NewAgentVoiceSynthesizer(
	configuration TTSConfig,
	apiKey string,
) (*AgentVoiceSynthesizer, error) {
	synthesizer, err := newSpeechSynthesizer(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &AgentVoiceSynthesizer{synthesizer: synthesizer}, nil
}

func (synthesizer *AgentVoiceSynthesizer) Synthesize(
	ctx context.Context,
	request agentvoice.SynthesisRequest,
) (agentvoice.SynthesisResult, error) {
	if synthesizer == nil || synthesizer.synthesizer == nil {
		return agentvoice.SynthesisResult{}, agentvoice.NewSpeechError(
			agentvoice.SpeechOperationSynthesis,
			agentvoice.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("qianwen: Agent Voice synthesizer is required"),
		)
	}
	result, err := synthesizer.synthesizer.Synthesize(
		ctx,
		protocol.SynthesisRequest{Text: request.Text},
	)
	if err != nil {
		return agentvoice.SynthesisResult{}, mapAgentVoiceError(
			err,
			agentvoice.SpeechOperationSynthesis,
		)
	}
	return agentvoice.SynthesisResult{
		RequestID: result.RequestID,
		Provider:  result.Provider,
		Model:     result.Model,
		AudioID:   result.AudioID,
		Audio:     result.Audio,
		Usage:     agentVoiceSpeechUsage(result.Usage),
	}, nil
}

func agentVoiceTranscriptionResult(
	result protocol.TranscriptionResult,
) agentvoice.TranscriptionResult {
	return agentvoice.TranscriptionResult{
		ID:           result.ID,
		Provider:     result.Provider,
		Model:        result.Model,
		Transcript:   result.Transcript,
		Language:     result.Language,
		Emotion:      result.Emotion,
		FinishReason: result.FinishReason,
		Usage:        agentVoiceSpeechUsage(result.Usage),
	}
}

func agentVoiceSpeechUsage(usage protocol.SpeechUsage) agentvoice.SpeechUsage {
	return agentvoice.SpeechUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		AudioSeconds: usage.AudioSeconds,
		Characters:   usage.Characters,
	}
}

func mapAgentVoiceError(
	err error,
	operation agentvoice.SpeechOperation,
) error {
	var providerError *protocol.SpeechError
	if !errors.As(err, &providerError) {
		return agentvoice.NewSpeechError(
			operation,
			agentvoice.ErrorProviderUnavailable,
			0,
			"",
			"",
			err,
		)
	}
	return agentvoice.NewSpeechError(
		operation,
		mapAgentVoiceErrorKind(providerError.Kind),
		providerError.StatusCode,
		providerError.ProviderCode,
		providerError.RequestID,
		err,
	)
}

func mapAgentVoiceErrorKind(kind protocol.ErrorKind) agentvoice.ErrorKind {
	switch kind {
	case protocol.ErrorInvalidRequest:
		return agentvoice.ErrorInvalidRequest
	case protocol.ErrorConfiguration:
		return agentvoice.ErrorConfiguration
	case protocol.ErrorAuthentication:
		return agentvoice.ErrorAuthentication
	case protocol.ErrorAuthorization:
		return agentvoice.ErrorAuthorization
	case protocol.ErrorQuotaExhausted:
		return agentvoice.ErrorQuotaExhausted
	case protocol.ErrorRateLimited:
		return agentvoice.ErrorRateLimited
	case protocol.ErrorTimeout:
		return agentvoice.ErrorTimeout
	case protocol.ErrorInvalidResponse:
		return agentvoice.ErrorInvalidResponse
	case protocol.ErrorCancelled:
		return agentvoice.ErrorCancelled
	default:
		return agentvoice.ErrorProviderUnavailable
	}
}

var (
	_ agentvoice.StreamingSpeechRecognizer         = (*AgentVoiceRecognizer)(nil)
	_ agentvoice.SpeechSynthesizer                 = (*AgentVoiceSynthesizer)(nil)
	_ agentconversation.AssistantSpeechSynthesizer = (*AgentVoiceSynthesizer)(nil)
)
