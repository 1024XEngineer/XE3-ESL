package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
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
	Acoustics     evaluation.SpeechFeedbackAcousticProvider
}

type SpeechFeedbackComposition struct {
	coordinator  *evaluation.SpeechFeedbackCoordinator
	handler      *evaluation.SpeechFeedbackHTTPHandler
	retryHandler *review.RetryRequestHTTPHandler
	worker       *evaluation.SpeechFeedbackWorker
}

func NewSpeechFeedbackComposition(
	database *pgxpool.Pool,
	generator ai.TextGenerator,
	configuration SpeechFeedbackConfiguration,
) (*SpeechFeedbackComposition, error) {
	if database == nil || generator == nil ||
		configuration.Provider == "" ||
		configuration.Model == "" {
		return nil, errors.New(
			"bootstrap: SpeechFeedback dependencies are required",
		)
	}
	evaluationRepository := evaluation.NewPostgresRepository(database)
	coordinator, err := evaluation.NewSpeechFeedbackCoordinator(
		evaluationRepository,
	)
	if err != nil {
		return nil, err
	}
	provider, err := evaluation.NewSpeechFeedbackTextProvider(generator)
	if err != nil {
		return nil, err
	}
	workerConfiguration := evaluation.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		RetryDelay:      configuration.RetryDelay,
		StrategyRef:     evaluation.SpeechFeedbackStrategyRef,
		PipelineVersion: evaluation.SpeechFeedbackPipelineVersion,
		PromptVersion:   evaluation.SpeechFeedbackPromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	var worker *evaluation.SpeechFeedbackWorker
	if configuration.Acoustics == nil {
		worker, err = evaluation.NewSpeechFeedbackWorker(
			evaluationRepository,
			provider,
			workerConfiguration,
		)
	} else {
		worker, err = evaluation.NewSpeechFeedbackWorkerWithAcoustics(
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
	handler, err := evaluation.NewSpeechFeedbackHTTPHandler(coordinator)
	if err != nil {
		return nil, err
	}
	practiceRepository, err := practicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	retryTurns, err := practiceinput.NewRetryTurnService(
		practiceRepository,
	)
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
		&speechFeedbackRetryConversationAdapter{
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

func (composition *SpeechFeedbackComposition) Coordinator() *evaluation.SpeechFeedbackCoordinator {
	if composition == nil {
		return nil
	}
	return composition.coordinator
}

func (composition *SpeechFeedbackComposition) HTTPHandler() *evaluation.SpeechFeedbackHTTPHandler {
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

func (composition *SpeechFeedbackComposition) Worker() *evaluation.SpeechFeedbackWorker {
	if composition == nil {
		return nil
	}
	return composition.worker
}

type speechFeedbackRepracticeSourceAdapter struct {
	repository *evaluation.PostgresRepository
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
	if errors.Is(err, evaluation.ErrSpeechFeedbackNotFound) {
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

type speechFeedbackRetryConversationAdapter struct {
	service *practiceinput.RetryTurnService
}

func (adapter *speechFeedbackRetryConversationAdapter) CreateSameQuestionRetryTurn(
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
		practiceinput.CreateRetryTurnCommand{
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
