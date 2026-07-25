package bootstrap

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/qianwen"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

// VoiceReviewGateway is the narrow Review capability consumed by the Agent
// voice Saga. Review implementations keep their Repository private.
type VoiceReviewGateway interface {
	agent.VoiceReviewPort
	agent.VoiceReviewReader
}

// VoicePorts are implemented by the owning Practice, Conversation, and Review
// modules. This Issue composes them but does not import their Repositories.
type VoicePorts struct {
	ConversationStore conversation.VoiceRoundStore
	Practice          agent.VoicePracticePort
	Sessions          agent.VoiceSessionPort
	Questions         agent.VoiceQuestionPort
	Checkpoints       agent.VoiceCheckpointPort
	Reviews           VoiceReviewGateway
}

type VoiceConfiguration struct {
	Recognizer     ai.SpeechRecognizer
	Synthesizer    ai.SpeechSynthesizer
	TemporaryAudio conversation.TemporaryAudioVault
	Ports          VoicePorts
}

// NewSpeechRecognizer is the server-side ASR registration boundary. Production
// never silently substitutes a Fake or another provider.
func NewSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (ai.SpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: speech recognition provider is not registered",
		)
	}
	return qianwen.NewRecognizer(
		qianwen.ASRConfig{
			BaseURL: configuration.BaseURL,
			Model:   configuration.Model,
			Timeout: configuration.Timeout,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewSpeechSynthesizer is the server-side TTS registration boundary.
func NewSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (ai.SpeechSynthesizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: speech synthesis provider is not registered",
		)
	}
	return qianwen.NewSynthesizer(
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

// buildVoiceApplication wires only application Ports. Durable adapters from
// Practice, Conversation, and Review are supplied by their owning Issues.
func buildVoiceApplication(
	matters matter.Reader,
	configuration VoiceConfiguration,
) (*agent.VoiceSessionApplication, error) {
	ports := configuration.Ports
	if matters == nil ||
		configuration.Recognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.TemporaryAudio == nil ||
		ports.ConversationStore == nil ||
		ports.Practice == nil ||
		ports.Sessions == nil ||
		ports.Questions == nil ||
		ports.Checkpoints == nil ||
		ports.Reviews == nil {
		return nil, errors.New("bootstrap: voice dependencies are required")
	}
	conversations, err := conversation.NewVoiceRoundService(
		ports.ConversationStore,
		configuration.TemporaryAudio,
		configuration.Recognizer,
		configuration.Synthesizer,
	)
	if err != nil {
		return nil, err
	}
	orchestrator, err := agent.NewVoiceRoundOrchestrator(
		conversations,
		ports.Practice,
		ports.Reviews,
	)
	if err != nil {
		return nil, err
	}
	return agent.NewVoiceSessionApplication(
		ports.Sessions,
		ports.Questions,
		ports.Checkpoints,
		orchestrator,
		ports.Reviews,
		matters,
	)
}
