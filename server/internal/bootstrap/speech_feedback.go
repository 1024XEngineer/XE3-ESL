package bootstrap

import (
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	reviewrepractice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/repractice"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SpeechFeedbackConfiguration struct {
	Provider      string
	Model         string
	LeaseDuration time.Duration
	Acoustics     speechfeedback.SpeechFeedbackAcousticProvider
}

type SpeechFeedbackComposition struct {
	coordinator  *speechfeedback.SpeechFeedbackCoordinator
	handler      *speechfeedback.SpeechFeedbackHTTPHandler
	retryHandler *review.RetryRequestHTTPHandler
	worker       *speechfeedback.SpeechFeedbackWorker
}

func NewSpeechFeedbackComposition(
	database *pgxpool.Pool,
	generator speechfeedback.TextGenerator,
	configuration SpeechFeedbackConfiguration,
) (*SpeechFeedbackComposition, error) {
	if database == nil || generator == nil ||
		configuration.Provider == "" ||
		configuration.Model == "" {
		return nil, errors.New(
			"bootstrap: SpeechFeedback dependencies are required",
		)
	}
	evaluationRepository := speechfeedback.NewPostgresRepository(database)
	coordinator, err := speechfeedback.NewSpeechFeedbackCoordinator(
		evaluationRepository,
	)
	if err != nil {
		return nil, err
	}
	provider, err := speechfeedback.NewSpeechFeedbackTextProvider(generator)
	if err != nil {
		return nil, err
	}
	workerConfiguration, err := speechfeedback.NewWorkerConfiguration(
		configuration.Provider,
		configuration.Model,
		configuration.LeaseDuration,
	)
	if err != nil {
		return nil, err
	}
	var worker *speechfeedback.SpeechFeedbackWorker
	if configuration.Acoustics == nil {
		worker, err = speechfeedback.NewSpeechFeedbackWorker(
			evaluationRepository,
			provider,
			workerConfiguration,
		)
	} else {
		worker, err = speechfeedback.NewSpeechFeedbackWorkerWithAcoustics(
			evaluationRepository,
			provider,
			evaluationRepository,
			configuration.Acoustics,
			workerConfiguration,
		)
	}
	if err != nil {
		return nil, err
	}
	handler, err := speechfeedback.NewSpeechFeedbackHTTPHandler(coordinator)
	if err != nil {
		return nil, err
	}
	practiceRepository, err := practicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	voiceRepository, err := practicevoicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	retryTurns, err := practicevoice.NewRetryTurnService(voiceRepository)
	if err != nil {
		return nil, err
	}
	retryPractice, err := practice.NewRetryTurnApplication(
		practiceRepository,
		"speakup.user",
	)
	if err != nil {
		return nil, err
	}
	sourceReader, err := reviewrepractice.NewSourceReader(evaluationRepository)
	if err != nil {
		return nil, err
	}
	practiceAuthorizer, err := reviewrepractice.NewPracticeAuthorizer(retryPractice)
	if err != nil {
		return nil, err
	}
	turnCreator, err := reviewrepractice.NewTurnCreator(retryTurns)
	if err != nil {
		return nil, err
	}
	retryRequests, err := review.NewRetryRequestService(
		review.NewPostgresRepository(database),
		sourceReader,
		practiceAuthorizer,
		turnCreator,
	)
	if err != nil {
		return nil, err
	}
	retryHandler, err := review.NewRetryRequestHTTPHandler(
		retryRequests,
	)
	if err != nil {
		return nil, err
	}
	return &SpeechFeedbackComposition{
		coordinator:  coordinator,
		handler:      handler,
		retryHandler: retryHandler,
		worker:       worker,
	}, nil
}

func (composition *SpeechFeedbackComposition) Coordinator() *speechfeedback.SpeechFeedbackCoordinator {
	if composition == nil {
		return nil
	}
	return composition.coordinator
}

func (composition *SpeechFeedbackComposition) HTTPHandler() *speechfeedback.SpeechFeedbackHTTPHandler {
	if composition == nil {
		return nil
	}
	return composition.handler
}

func (composition *SpeechFeedbackComposition) RetryHTTPHandler() *review.RetryRequestHTTPHandler {
	if composition == nil {
		return nil
	}
	return composition.retryHandler
}

func (composition *SpeechFeedbackComposition) Worker() *speechfeedback.SpeechFeedbackWorker {
	if composition == nil {
		return nil
	}
	return composition.worker
}
