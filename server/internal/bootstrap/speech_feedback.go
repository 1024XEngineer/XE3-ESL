package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SpeechFeedbackConfiguration struct {
	Provider      string
	Model         string
	MaxAttempts   int
	LeaseDuration time.Duration
	RetryDelay    time.Duration
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
	workerConfiguration := speechfeedback.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		RetryDelay:      configuration.RetryDelay,
		StrategyRef:     speechfeedback.SpeechFeedbackStrategyRef,
		PipelineVersion: speechfeedback.SpeechFeedbackPipelineVersion,
		PromptVersion:   speechfeedback.SpeechFeedbackPromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
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
	retryRequests, err := review.NewRetryRequestService(
		review.NewPostgresRepository(database),
		&speechFeedbackRepracticeSourceAdapter{
			repository: evaluationRepository,
		},
		&speechFeedbackRetryPracticeAdapter{
			application: retryPractice,
		},
		&speechFeedbackRetryVoiceAdapter{
			service: retryTurns,
		},
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

type speechFeedbackRepracticeSourceAdapter struct {
	repository *speechfeedback.PostgresRepository
}

func (adapter *speechFeedbackRepracticeSourceAdapter) ReadSameQuestionRepracticeSource(
	ctx context.Context,
	actor requestcontext.Actor,
	feedbackItemID string,
) (review.RepracticeSource, error) {
	if adapter == nil || adapter.repository == nil {
		return review.RepracticeSource{}, review.ErrRetryRequestInvalid
	}
	source, err := adapter.repository.ReadSameQuestionRepracticeSource(
		ctx,
		actor.UserID,
		feedbackItemID,
	)
	if errors.Is(err, speechfeedback.ErrSpeechFeedbackNotFound) {
		return review.RepracticeSource{}, review.ErrRetryRequestNotFound
	}
	if err != nil {
		return review.RepracticeSource{}, err
	}
	return review.RepracticeSource{
		FeedbackItemID:    source.FeedbackItemID,
		SourceFeedbackID:  source.SpeechFeedbackID,
		PracticeSessionID: source.PracticeSessionID,
		OriginalTurnID:    source.OriginalTurnID,
		QuestionID:        source.QuestionID,
		SourceGeneration:  source.SourceGeneration,
	}, nil
}

type speechFeedbackRetryPracticeAdapter struct {
	application *practice.RetryTurnApplication
}

func (adapter *speechFeedbackRetryPracticeAdapter) AuthorizeSameQuestionRetry(
	ctx context.Context,
	actor requestcontext.Actor,
	source review.SameQuestionRetrySource,
) error {
	if adapter == nil || adapter.application == nil {
		return review.ErrRetryRequestInvalid
	}
	_, err := adapter.application.AuthorizeSameQuestionRetry(
		ctx,
		actor,
		practice.AuthorizeSameQuestionRetryCommand{
			RetryRequestID:    source.RetryRequestID,
			PracticeSessionID: source.PracticeSessionID,
			OriginalTurnID:    source.OriginalTurnID,
			QuestionID:        source.QuestionID,
		},
	)
	if errors.Is(err, practice.ErrRetryTurnNotAvailable) {
		return review.ErrRetryRequestSourceUnavailable
	}
	return err
}

type speechFeedbackRetryVoiceAdapter struct {
	service *practicevoice.RetryTurnService
}

func (adapter *speechFeedbackRetryVoiceAdapter) CreateSameQuestionRetryTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	source review.SameQuestionRetrySource,
) (string, error) {
	if adapter == nil || adapter.service == nil {
		return "", review.ErrRetryRequestInvalid
	}
	draft, err := adapter.service.Create(
		ctx,
		actor,
		practicevoice.CreateRetryTurnCommand{
			RetryRequestID:    source.RetryRequestID,
			PracticeSessionID: source.PracticeSessionID,
			OriginalTurnID:    source.OriginalTurnID,
			QuestionID:        source.QuestionID,
		},
	)
	if err != nil {
		return "", err
	}
	return draft.TurnID, nil
}
