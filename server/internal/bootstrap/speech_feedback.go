package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SpeechFeedbackConfiguration struct {
	Provider      string
	Model         string
	MaxAttempts   int
	LeaseDuration time.Duration
	RetryDelay    time.Duration
	Acoustics     review.SpeechFeedbackAcousticProvider
}

type SpeechFeedbackComposition struct {
	coordinator  *review.SpeechFeedbackCoordinator
	handler      *review.SpeechFeedbackHTTPHandler
	retryHandler *review.RetryRequestHTTPHandler
	worker       *review.SpeechFeedbackWorker
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
	repository := review.NewPostgresRepository(database)
	coordinator, err := review.NewSpeechFeedbackCoordinator(repository)
	if err != nil {
		return nil, err
	}
	provider, err := review.NewSpeechFeedbackTextProvider(generator)
	if err != nil {
		return nil, err
	}
	workerConfiguration := review.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		RetryDelay:      configuration.RetryDelay,
		StrategyRef:     review.SpeechFeedbackStrategyRef,
		PipelineVersion: review.SpeechFeedbackPipelineVersion,
		PromptVersion:   review.SpeechFeedbackPromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	var worker *review.SpeechFeedbackWorker
	if configuration.Acoustics == nil {
		worker, err = review.NewSpeechFeedbackWorker(
			repository,
			provider,
			workerConfiguration,
		)
	} else {
		worker, err = review.NewSpeechFeedbackWorkerWithAcoustics(
			repository,
			provider,
			repository,
			configuration.Acoustics,
			workerConfiguration,
		)
	}
	if err != nil {
		return nil, err
	}
	handler, err := review.NewSpeechFeedbackHTTPHandler(coordinator)
	if err != nil {
		return nil, err
	}
	conversationRepository, err := conversationpostgres.New(database)
	if err != nil {
		return nil, err
	}
	retryTurns, err := conversation.NewRetryTurnService(
		conversationRepository,
	)
	if err != nil {
		return nil, err
	}
	retryPractice, err := practice.NewRetryTurnApplication(
		practicepostgres.New(database),
		"speakup.user",
	)
	if err != nil {
		return nil, err
	}
	retryRequests, err := review.NewRetryRequestService(
		repository,
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

func (composition *SpeechFeedbackComposition) Coordinator() *review.SpeechFeedbackCoordinator {
	if composition == nil {
		return nil
	}
	return composition.coordinator
}

func (composition *SpeechFeedbackComposition) HTTPHandler() *review.SpeechFeedbackHTTPHandler {
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

func (composition *SpeechFeedbackComposition) Worker() *review.SpeechFeedbackWorker {
	if composition == nil {
		return nil
	}
	return composition.worker
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
	service *conversation.RetryTurnService
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
		conversation.CreateRetryTurnCommand{
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
