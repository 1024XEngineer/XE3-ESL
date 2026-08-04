package evaluationfeedback

import (
	"context"
	"errors"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// Adapter translates Evaluation's coordinator into the narrow Agent Voice
// feedback port.
type Adapter struct {
	coordinator *speechfeedback.SpeechFeedbackCoordinator
}

func New(
	coordinator *speechfeedback.SpeechFeedbackCoordinator,
) (*Adapter, error) {
	if coordinator == nil {
		return nil, agentvoice.ErrInvalidContext
	}
	return &Adapter{coordinator: coordinator}, nil
}

func (adapter *Adapter) EnsureMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) (agentvoice.FeedbackReference, error) {
	if adapter == nil || adapter.coordinator == nil {
		return agentvoice.FeedbackReference{}, agentvoice.ErrInvalidContext
	}
	reference, err := adapter.coordinator.EnsureAgentVoiceMessage(
		ctx,
		actor,
		threadID,
		messageID,
	)
	if errors.Is(err, speechfeedback.ErrSpeechFeedbackNotApplicable) {
		return agentvoice.FeedbackReference{}, nil
	}
	if err != nil {
		return agentvoice.FeedbackReference{}, err
	}
	return agentvoice.FeedbackReference{StatusURL: reference.StatusURL}, nil
}

var _ agentvoice.FeedbackPort = (*Adapter)(nil)
