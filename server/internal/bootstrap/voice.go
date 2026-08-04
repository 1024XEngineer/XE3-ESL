package bootstrap

import (
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	practiceevaluationfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/evaluationfeedback"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VoiceConfiguration contains concrete runtime dependencies selected by the
// composition root. Agent Voice and Practice Voice intentionally use their own
// ports even when both are backed by the same provider.
type VoiceConfiguration struct {
	Recognizer                ai.StreamingSpeechRecognizer
	Synthesizer               ai.SpeechSynthesizer
	PracticeRecognizer        practicevoice.SpeechRecognizer
	PracticeSynthesizer       practicevoice.SpeechSynthesizer
	QuestionGenerator         practicevoice.QuestionGenerator
	TemporaryAudio            practicevoice.TemporaryAudioVault
	ObjectStore               objectstore.Store
	AgentVoiceInputEnabled    bool
	ScratchDirectory          string
	ObjectReadAllowedHosts    []string
	AudioStagedTTL            time.Duration
	AudioUploadLease          time.Duration
	ASRLease                  time.Duration
	AudioReadTimeout          time.Duration
	ReviewHistoryCursorKey    []byte
	SpeechFeedbackCoordinator *speechfeedback.SpeechFeedbackCoordinator
}

type AgentImageConfiguration struct {
	ObjectStore objectstore.Store
	StagedTTL   time.Duration
	UploadLease time.Duration
}

// NewSpeechRecognizer selects the Agent Voice ASR implementation.
func NewSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (ai.StreamingSpeechRecognizer, error) {
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

// NewSpeechSynthesizer selects the Agent Voice TTS implementation.
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

// NewPracticeSpeechRecognizer selects the Practice Voice ASR adapter.
func NewPracticeSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (practicevoice.SpeechRecognizer, error) {
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

// NewPracticeSpeechSynthesizer selects the Practice Voice TTS adapter.
func NewPracticeSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (practicevoice.SpeechSynthesizer, error) {
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

// NewPracticeQuestionGenerator selects the Practice Voice question adapter.
func NewPracticeQuestionGenerator(
	configuration config.TextGenerationConfig,
) (practicevoice.QuestionGenerator, error) {
	if configuration.Provider != config.TextProviderQianwen {
		return nil, errors.New(
			"bootstrap: Practice question provider is not registered",
		)
	}
	return qianwen.NewPracticeVoiceQuestionGenerator(
		qianwen.Config{
			BaseURL:         configuration.BaseURL,
			Model:           configuration.Model,
			Timeout:         configuration.Timeout,
			MaxOutputTokens: configuration.MaxOutputTokens,
		},
		configuration.APIKey.Reveal(),
	)
}

// buildProductionVoiceApplication constructs infrastructure and delegates all
// Practice Voice business wiring to the owning package.
func buildProductionVoiceApplication(
	database *pgxpool.Pool,
	configuration VoiceConfiguration,
) (
	*practicevoice.SessionApplication,
	*practicevoice.SameQuestionRetryApplication,
	*practicevoice.AudioAssetService,
	error,
) {
	if database == nil ||
		configuration.PracticeRecognizer == nil ||
		configuration.PracticeSynthesizer == nil ||
		configuration.QuestionGenerator == nil ||
		configuration.TemporaryAudio == nil ||
		configuration.ASRLease <= 0 {
		return nil, nil, nil,
			errors.New("bootstrap: Practice Voice dependencies are required")
	}

	repository, err := practicevoicepostgres.New(database)
	if err != nil {
		return nil, nil, nil, err
	}
	var audioAssets *practicevoice.AudioAssetService
	if configuration.ObjectStore != nil {
		audioRepository, repositoryErr :=
			practicevoicepostgres.NewAudioAssetRepository(database)
		if repositoryErr != nil {
			return nil, nil, nil, repositoryErr
		}
		audioAssets, err = practicevoice.NewAudioAssetService(
			audioRepository,
			configuration.ObjectStore,
			practicevoice.SecureAudioAssetIDGenerator{},
			practicevoice.NewAudioAssetSystemClock(),
			audioRepository,
			configuration.AudioStagedTTL,
		)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	var feedback practicevoice.TurnFeedbackPort
	var feedbackReader practicevoice.TurnFeedbackStatusReader
	if configuration.SpeechFeedbackCoordinator != nil {
		feedbackAdapter, feedbackErr := practiceevaluationfeedback.New(
			configuration.SpeechFeedbackCoordinator,
		)
		if feedbackErr != nil {
			return nil, nil, nil, feedbackErr
		}
		feedback = feedbackAdapter
		feedbackReader = feedbackAdapter
	}
	var recordings practicevoice.VoiceRecordingLifecycle
	if audioAssets != nil {
		recordings = audioAssets
	}
	application, retryApplication, err :=
		practicevoice.NewRuntimeApplications(
			practicevoice.RuntimeConfiguration{
				Repository:        repository,
				TemporaryAudio:    configuration.TemporaryAudio,
				Recognizer:        configuration.PracticeRecognizer,
				Synthesizer:       configuration.PracticeSynthesizer,
				QuestionGenerator: configuration.QuestionGenerator,
				Recordings:        recordings,
				AudioAssets:       audioAssets,
				ASRLease:          configuration.ASRLease,
				Feedback:          feedback,
				FeedbackReader:    feedbackReader,
			},
		)
	if err != nil {
		return nil, nil, nil, err
	}
	return application, retryApplication, audioAssets, nil
}
