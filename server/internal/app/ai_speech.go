package app

import (
	"errors"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
)

type AgentSpeechSynthesizer interface {
	agentvoice.SpeechSynthesizer
	agentconversation.AssistantSpeechSynthesizer
}

// NewAgentSpeechRecognizer selects the Agent Voice ASR implementation.
func NewAgentSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (agentvoice.StreamingSpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: speech recognition provider is not registered",
		)
	}
	return qianwen.NewAgentVoiceRecognizer(
		qianwen.ASRConfig{
			BaseURL: configuration.BaseURL,
			Model:   configuration.Model,
			Timeout: configuration.Timeout,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewAgentSpeechSynthesizer selects the Agent Voice TTS implementation.
func NewAgentSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (AgentSpeechSynthesizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: speech synthesis provider is not registered",
		)
	}
	return qianwen.NewAgentVoiceSynthesizer(
		qianwen.TTSConfig{
			BaseURL:       configuration.BaseURL,
			Model:         configuration.Model,
			Voice:         configuration.Voice,
			LanguageHint:  configuration.LanguageHint,
			Timeout:       configuration.Timeout,
			TempDirectory: configuration.TempDirectory,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewPracticeSpeechRecognizer selects the Practice Voice ASR adapter.
func NewPracticeSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (practiceinteraction.SpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: Practice speech recognition provider is not registered",
		)
	}
	return qianwen.NewPracticeVoiceRecognizer(
		qianwen.ASRConfig{
			BaseURL: configuration.BaseURL,
			Model:   configuration.Model,
			Timeout: configuration.Timeout,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewPracticeRecordedSpeechRecognizer selects the synchronous Practice Voice
// ASR adapter used only after a complete recording has been uploaded.
func NewPracticeRecordedSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (practiceinteraction.SpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: recorded Practice speech recognition provider is not registered",
		)
	}
	return qianwen.NewPracticeRecordedVoiceRecognizer(
		qianwen.ASRConfig{
			BaseURL: configuration.BaseURL,
			Model:   configuration.RecordedModel,
			Timeout: configuration.RecordedTimeout,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewPracticeSpeechSynthesizer selects the Practice Voice TTS adapter.
func NewPracticeSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (practiceinteraction.SpeechSynthesizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: Practice speech synthesis provider is not registered",
		)
	}
	return qianwen.NewPracticeVoiceSynthesizer(
		qianwen.TTSConfig{
			BaseURL:       configuration.BaseURL,
			Model:         configuration.Model,
			Voice:         configuration.Voice,
			LanguageHint:  configuration.LanguageHint,
			Timeout:       configuration.Timeout,
			TempDirectory: configuration.TempDirectory,
		},
		configuration.APIKey.Reveal(),
	)
}
