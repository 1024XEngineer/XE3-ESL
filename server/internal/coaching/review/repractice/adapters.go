package repractice

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type sourceRepository interface {
	ReadSameQuestionRepracticeSource(
		context.Context,
		string,
		string,
	) (speechfeedback.SameQuestionRepracticeSource, error)
}

type SourceReader struct {
	repository sourceRepository
}

func NewSourceReader(
	repository sourceRepository,
) (*SourceReader, error) {
	if repository == nil {
		return nil, review.ErrRetryRequestInvalid
	}
	return &SourceReader{repository: repository}, nil
}

func (reader *SourceReader) ReadSameQuestionRepracticeSource(
	ctx context.Context,
	actor requestcontext.Actor,
	feedbackItemID string,
) (review.RepracticeSource, error) {
	if reader == nil || reader.repository == nil {
		return review.RepracticeSource{}, review.ErrRetryRequestInvalid
	}
	source, err := reader.repository.ReadSameQuestionRepracticeSource(
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

type practiceApplication interface {
	AuthorizeSameQuestionRetry(
		context.Context,
		requestcontext.Actor,
		practice.AuthorizeSameQuestionRetryCommand,
	) (practice.RetryTurnAuthorization, error)
}

type PracticeAuthorizer struct {
	application practiceApplication
}

func NewPracticeAuthorizer(
	application practiceApplication,
) (*PracticeAuthorizer, error) {
	if application == nil {
		return nil, review.ErrRetryRequestInvalid
	}
	return &PracticeAuthorizer{application: application}, nil
}

func (authorizer *PracticeAuthorizer) AuthorizeSameQuestionRetry(
	ctx context.Context,
	actor requestcontext.Actor,
	source review.SameQuestionRetrySource,
) error {
	if authorizer == nil || authorizer.application == nil {
		return review.ErrRetryRequestInvalid
	}
	_, err := authorizer.application.AuthorizeSameQuestionRetry(
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

type voiceService interface {
	Create(
		context.Context,
		requestcontext.Actor,
		practicevoice.CreateRetryTurnCommand,
	) (practicevoice.RetryTurnDraft, error)
}

type TurnCreator struct {
	service voiceService
}

func NewTurnCreator(
	service voiceService,
) (*TurnCreator, error) {
	if service == nil {
		return nil, review.ErrRetryRequestInvalid
	}
	return &TurnCreator{service: service}, nil
}

func (creator *TurnCreator) CreateSameQuestionRetryTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	source review.SameQuestionRetrySource,
) (string, error) {
	if creator == nil || creator.service == nil {
		return "", review.ErrRetryRequestInvalid
	}
	draft, err := creator.service.Create(
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

var (
	_ review.RepracticeSourceReader            = (*SourceReader)(nil)
	_ review.SameQuestionRetryPracticePort     = (*PracticeAuthorizer)(nil)
	_ review.SameQuestionRetryConversationPort = (*TurnCreator)(nil)
)
