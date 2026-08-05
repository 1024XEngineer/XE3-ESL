package voice

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type RuntimeRepository interface {
	sessionRepository
	PersistenceStore
	questionRepository
	RecordingConfirmationStore
	RetryTurnStore
	practice.RetryTurnRepository
}

// TurnFeedbackStatusReader is the read-only Evaluation boundary used when a
// persisted Turn is restored for the client.
type TurnFeedbackStatusReader interface {
	StatusURLForTurn(
		context.Context,
		requestcontext.Actor,
		string,
	) (string, bool, error)
}

// RuntimeConfiguration supplies the infrastructure and cross-module ports
// needed by the complete Practice Voice flow.
type RuntimeConfiguration struct {
	Repository        RuntimeRepository
	TemporaryAudio    TemporaryAudioVault
	Recognizer        SpeechRecognizer
	Synthesizer       SpeechSynthesizer
	QuestionGenerator QuestionGenerator
	Recordings        VoiceRecordingLifecycle
	AudioAssets       *AudioAssetService
	ASRLease          time.Duration
	Feedback          TurnFeedbackPort
	FeedbackReader    TurnFeedbackStatusReader
}

// NewRuntimeApplications assembles the authoritative Practice Voice runtime.
// Bootstrap selects concrete infrastructure; the business wiring stays here.
func NewRuntimeApplications(
	configuration RuntimeConfiguration,
) (*SessionApplication, *SameQuestionRetryApplication, error) {
	if configuration.Repository == nil ||
		configuration.TemporaryAudio == nil ||
		configuration.Recognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.QuestionGenerator == nil ||
		configuration.ASRLease <= 0 {
		return nil, nil, errors.New(
			"practice voice: runtime dependencies are required",
		)
	}

	participantResolver, err := NewParticipantResolver(
		configuration.Repository,
		"speakup.user",
	)
	if err != nil {
		return nil, nil, err
	}
	roundStore := &roundStoreAdapter{
		repository: configuration.Repository,
		recordings: configuration.Repository,
		asrLease:   configuration.ASRLease,
	}
	roundService, err := NewVoiceRoundServiceWithRecordings(
		roundStore,
		configuration.TemporaryAudio,
		configuration.Recognizer,
		configuration.Synthesizer,
		configuration.Recordings,
	)
	if err != nil {
		return nil, nil, err
	}
	retryTurnService, err := NewRetryTurnService(configuration.Repository)
	if err != nil {
		return nil, nil, err
	}
	retryPracticeApplication, err := practice.NewRetryTurnApplication(
		configuration.Repository,
	)
	if err != nil {
		return nil, nil, err
	}
	retryApplication, err := NewSameQuestionRetryApplication(
		&retryTurnAdapter{service: retryTurnService},
		&retryPracticeAdapter{application: retryPracticeApplication},
		roundService,
	)
	if err != nil {
		return nil, nil, err
	}

	feedbackPorts := make([]TurnFeedbackPort, 0, 1)
	if configuration.Feedback != nil {
		feedbackPorts = append(feedbackPorts, configuration.Feedback)
	}
	orchestrator, err := NewRoundOrchestrator(
		roundService,
		participantResolver,
		feedbackPorts...,
	)
	if err != nil {
		return nil, nil, err
	}
	application, err := NewSessionApplication(
		&sessionAdapter{repository: configuration.Repository},
		&questionAdapter{
			repository: configuration.Repository,
			generator:  configuration.QuestionGenerator,
		},
		&checkpointAdapter{
			repository:  configuration.Repository,
			audioAssets: configuration.AudioAssets,
			feedback:    configuration.FeedbackReader,
		},
		orchestrator,
	)
	if err != nil {
		return nil, nil, err
	}
	return application, retryApplication, nil
}
