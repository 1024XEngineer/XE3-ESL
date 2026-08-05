package agentvoice

import (
	"context"
	"errors"

	voice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// Feedback translates Evaluation's coordinator into Agent Voice's feedback
// port. Evaluation remains authoritative for evidence and feedback status.
type Feedback struct {
	coordinator *speechfeedback.SpeechFeedbackCoordinator
}

func NewFeedback(
	coordinator *speechfeedback.SpeechFeedbackCoordinator,
) (*Feedback, error) {
	if coordinator == nil {
		return nil, voice.ErrInvalidContext
	}
	return &Feedback{coordinator: coordinator}, nil
}

func (feedback *Feedback) EnsureMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) (voice.FeedbackReference, error) {
	if feedback == nil || feedback.coordinator == nil {
		return voice.FeedbackReference{}, voice.ErrInvalidContext
	}
	reference, err := feedback.coordinator.EnsureAgentVoiceMessage(
		ctx,
		actor,
		threadID,
		messageID,
	)
	if errors.Is(err, speechfeedback.ErrSpeechFeedbackNotApplicable) {
		return voice.FeedbackReference{}, nil
	}
	if err != nil {
		return voice.FeedbackReference{}, err
	}
	return voice.FeedbackReference{StatusURL: reference.StatusURL}, nil
}

var _ voice.FeedbackPort = (*Feedback)(nil)
