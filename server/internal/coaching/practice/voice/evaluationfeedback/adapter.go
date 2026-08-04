package evaluationfeedback

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// Adapter translates Evaluation's coordinator into the narrow Practice Voice
// feedback port. Evaluation remains the authority for evidence and status.
type Adapter struct {
	coordinator *evaluation.SpeechFeedbackCoordinator
}

func New(coordinator *evaluation.SpeechFeedbackCoordinator) (*Adapter, error) {
	if coordinator == nil {
		return nil, practicevoice.ErrInvalidContext
	}
	return &Adapter{coordinator: coordinator}, nil
}

func (adapter *Adapter) EnsureTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	turnID string,
) (practicevoice.TurnFeedbackReference, error) {
	if adapter == nil || adapter.coordinator == nil {
		return practicevoice.TurnFeedbackReference{},
			practicevoice.ErrInvalidContext
	}
	reference, err := adapter.coordinator.EnsureConversationTurn(
		ctx,
		actor,
		sessionID,
		turnID,
	)
	if errors.Is(err, evaluation.ErrSpeechFeedbackNotApplicable) {
		return practicevoice.TurnFeedbackReference{}, nil
	}
	if err != nil {
		return practicevoice.TurnFeedbackReference{}, err
	}
	return practicevoice.TurnFeedbackReference{
		StatusURL:  reference.StatusURL,
		Applicable: true,
	}, nil
}

func (adapter *Adapter) StatusURLForTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
) (string, bool, error) {
	if adapter == nil || adapter.coordinator == nil {
		return "", false, practicevoice.ErrInvalidContext
	}
	return adapter.coordinator.StatusURLForConversationTurn(
		ctx,
		actor,
		turnID,
	)
}

var (
	_ practicevoice.TurnFeedbackPort         = (*Adapter)(nil)
	_ practicevoice.TurnFeedbackStatusReader = (*Adapter)(nil)
)
