package interaction

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

type RuntimeRepository interface {
	sessionRepository
	PersistenceStore
	questionRepository
	RecordingConfirmationStore
	RetryTurnStore
	QuestionTipStore
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
// needed by the complete Practice Interaction flow.
type RuntimeConfiguration struct {
	Repository         RuntimeRepository
	IDs                practice.PracticeResourceIDGenerator
	TemporaryAudio     TemporaryAudioVault
	Recognizer         SpeechRecognizer
	RecordedRecognizer SpeechRecognizer
	Synthesizer        SpeechSynthesizer
	QuestionGenerator  QuestionGenerator
	QuestionTranslator sharedtranslation.Translator
	AnswerTipGenerator AnswerTipGenerator
	Recordings         RecordingUploader
	ASRLease           time.Duration
	FeedbackReader     TurnFeedbackStatusReader
	DeferredContext    context.Context
	Logger             *slog.Logger
}

// NewRuntimeApplications assembles the authoritative Practice Interaction runtime.
// Bootstrap selects concrete infrastructure; the business wiring stays here.
func NewRuntimeApplications(
	configuration RuntimeConfiguration,
) (*SessionApplication, *SameQuestionRetryApplication, error) {
	if configuration.Repository == nil || configuration.IDs == nil ||
		configuration.TemporaryAudio == nil ||
		configuration.Recognizer == nil ||
		configuration.RecordedRecognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.QuestionGenerator == nil ||
		configuration.ASRLease <= 0 {
		return nil, nil, errors.New(
			"practice interaction: runtime dependencies are required",
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
	roundService, err := NewRoundServiceWithRecordings(
		roundStore,
		configuration.TemporaryAudio,
		configuration.Recognizer,
		configuration.RecordedRecognizer,
		configuration.Synthesizer,
		configuration.Recordings,
		roundStore,
	)
	if err != nil {
		return nil, nil, err
	}
	retryTurnService, err := NewRetryTurnService(configuration.Repository)
	if err != nil {
		return nil, nil, err
	}
	retryApplication, err := NewSameQuestionRetryApplication(
		&retryTurnAdapter{service: retryTurnService},
		roundService,
	)
	if err != nil {
		return nil, nil, err
	}

	feedbackReaders := make([]TurnFeedbackStatusReader, 0, 1)
	if configuration.FeedbackReader != nil {
		feedbackReaders = append(feedbackReaders, configuration.FeedbackReader)
	}
	orchestrator, err := NewRoundOrchestrator(
		roundService,
		participantResolver,
		feedbackReaders...,
	)
	if err != nil {
		return nil, nil, err
	}
	translationPorts := make([]sharedtranslation.Translator, 0, 1)
	if configuration.QuestionTranslator != nil {
		translationPorts = append(
			translationPorts,
			configuration.QuestionTranslator,
		)
	}
	var tipPort QuestionTipPort
	if configuration.AnswerTipGenerator != nil && configuration.QuestionTranslator != nil {
		tipService, tipErr := NewQuestionTipService(
			configuration.Repository,
			configuration.AnswerTipGenerator,
			configuration.QuestionTranslator,
		)
		if tipErr != nil {
			return nil, nil, tipErr
		}
		tipPort = tipService
	}
	application, err := NewSessionApplication(
		&sessionAdapter{repository: configuration.Repository},
		&questionAdapter{
			repository: configuration.Repository,
			generator:  configuration.QuestionGenerator,
			ids:        configuration.IDs,
		},
		&checkpointAdapter{
			repository: configuration.Repository,
			feedback:   configuration.FeedbackReader,
		},
		orchestrator,
		translationPorts...,
	)
	if err != nil {
		return nil, nil, err
	}
	application.tips = tipPort
	if configuration.Recordings != nil {
		deferredContext := configuration.DeferredContext
		if deferredContext == nil {
			deferredContext = context.Background()
		}
		logger := configuration.Logger
		if logger == nil {
			logger = slog.Default()
		}
		processor, processorErr := NewDeferredTranscriptionProcessor(
			deferredContext, orchestrator, logger,
		)
		if processorErr != nil {
			return nil, nil, processorErr
		}
		if err := application.EnableDeferredTranscription(processor); err != nil {
			return nil, nil, err
		}
	}
	return application, retryApplication, nil
}
