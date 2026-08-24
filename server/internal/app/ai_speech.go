package app

import (
	"errors"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
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
	return newAgentSpeechRecognizer(configuration, nil)
}

func (factory *ProviderFactory) AgentSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (agentvoice.StreamingSpeechRecognizer, error) {
	return newAgentSpeechRecognizer(configuration, factory.observer)
}

func newAgentSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
	observer providerobservability.Recorder,
) (agentvoice.StreamingSpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: speech recognition provider is not registered",
		)
	}
	return qianwen.NewAgentVoiceRecognizer(
		qianwen.ASRConfig{
			BaseURL:  configuration.BaseURL,
			Model:    configuration.Model,
			Timeout:  configuration.Timeout,
			Observer: observer,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewAgentSpeechSynthesizer selects the Agent Voice TTS implementation.
func NewAgentSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (AgentSpeechSynthesizer, error) {
	return newAgentSpeechSynthesizer(configuration, nil)
}

func (factory *ProviderFactory) AgentSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (AgentSpeechSynthesizer, error) {
	return newAgentSpeechSynthesizer(configuration, factory.observer)
}

func newAgentSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
	observer providerobservability.Recorder,
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
			Observer:      observer,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewPracticeSpeechRecognizer selects the Practice Voice ASR adapter.
func NewPracticeSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (practiceinteraction.SpeechRecognizer, error) {
	return newPracticeSpeechRecognizer(configuration, nil)
}

func (factory *ProviderFactory) PracticeSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (practiceinteraction.SpeechRecognizer, error) {
	return newPracticeSpeechRecognizer(configuration, factory.observer)
}

func newPracticeSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
	observer providerobservability.Recorder,
) (practiceinteraction.SpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: Practice speech recognition provider is not registered",
		)
	}
	return qianwen.NewPracticeVoiceRecognizer(
		qianwen.ASRConfig{
			BaseURL:  configuration.BaseURL,
			Model:    configuration.Model,
			Timeout:  configuration.Timeout,
			Observer: observer,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewPracticeRecordedSpeechRecognizer selects the synchronous Practice Voice
// ASR adapter used only after a complete recording has been uploaded.
func NewPracticeRecordedSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (practiceinteraction.SpeechRecognizer, error) {
	return newPracticeRecordedSpeechRecognizer(configuration, nil)
}

func (factory *ProviderFactory) PracticeRecordedSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (practiceinteraction.SpeechRecognizer, error) {
	return newPracticeRecordedSpeechRecognizer(configuration, factory.observer)
}

func newPracticeRecordedSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
	observer providerobservability.Recorder,
) (practiceinteraction.SpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: recorded Practice speech recognition provider is not registered",
		)
	}
	return qianwen.NewPracticeRecordedVoiceRecognizer(
		qianwen.ASRConfig{
			BaseURL:  configuration.BaseURL,
			Model:    configuration.RecordedModel,
			Timeout:  configuration.RecordedTimeout,
			Observer: observer,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewPracticeSpeechSynthesizer selects the Practice Voice TTS adapter.
func NewPracticeSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (practiceinteraction.SpeechSynthesizer, error) {
	return newPracticeSpeechSynthesizer(configuration, nil)
}

func (factory *ProviderFactory) PracticeSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (practiceinteraction.SpeechSynthesizer, error) {
	return newPracticeSpeechSynthesizer(configuration, factory.observer)
}

func newPracticeSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
	observer providerobservability.Recorder,
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
			Observer:      observer,
		},
		configuration.APIKey.Reveal(),
	)
}
