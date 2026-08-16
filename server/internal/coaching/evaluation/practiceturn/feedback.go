package practiceturn

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type recordReader interface {
	GetRecordBySource(
		context.Context,
		string,
		evaluation.Kind,
		string,
	) (evaluation.Record, error)
}

type Feedback struct{ records recordReader }

func NewFeedback(records recordReader) (*Feedback, error) {
	if records == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &Feedback{records: records}, nil
}

func (feedback *Feedback) StatusURLForTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
) (string, bool, error) {
	if feedback == nil || feedback.records == nil || ctx == nil ||
		!actor.Valid() || turnID == "" {
		return "", false, evaluation.ErrInvalidRequest
	}
	_, err := feedback.records.GetRecordBySource(
		ctx, actor.UserID, evaluation.KindPracticeTurnFeedback, turnID,
	)
	if errors.Is(err, evaluation.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return "/v1/practice-turns/" + turnID + "/evaluation", true, nil
}

var _ practiceinteraction.TurnFeedbackStatusReader = (*Feedback)(nil)
