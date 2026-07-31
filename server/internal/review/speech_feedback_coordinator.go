package review

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// SpeechFeedbackReader is the owner-isolated read projection used by HTTP and
// parent-resource decorators. It never creates work.
type SpeechFeedbackReader interface {
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (SpeechFeedback, error)
	StatusURLForConversationTurn(
		context.Context,
		requestcontext.Actor,
		string,
	) (string, bool, error)
	StatusURLForAgentVoiceMessage(
		context.Context,
		requestcontext.Actor,
		string,
	) (string, bool, error)
}

type SpeechFeedbackCoordinator struct {
	repository SpeechFeedbackRepository
}

func NewSpeechFeedbackCoordinator(
	repository SpeechFeedbackRepository,
) (*SpeechFeedbackCoordinator, error) {
	if repository == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &SpeechFeedbackCoordinator{repository: repository}, nil
}

// EnsureConversationTurn is the composition entry point for a confirmed
// practice voice Turn. Review derives and freezes the source revision;
// callers cannot supply an EvidenceSnapshot identity.
func (coordinator *SpeechFeedbackCoordinator) EnsureConversationTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
	turnID string,
) (SpeechFeedbackReference, error) {
	if !trustedSpeechFeedbackActor(ctx, actor) ||
		strings.TrimSpace(practiceSessionID) != practiceSessionID ||
		strings.TrimSpace(turnID) != turnID {
		return SpeechFeedbackReference{}, ErrInvalidSpeechFeedback
	}
	reference, err := coordinator.repository.
		EnsureConfirmedConversationTurn(
			ctx,
			actor.UserID,
			practiceSessionID,
			turnID,
		)
	if err != nil {
		return SpeechFeedbackReference{}, err
	}
	if !reference.valid() {
		return SpeechFeedbackReference{}, ErrInvalidSpeechFeedback
	}
	return reference, nil
}

// EnsureAgentVoiceMessage is the composition entry point for a confirmed
// ordinary Agent voice Message. Review derives the immutable transcript
// evidence identity and never synthesizes Practice identifiers.
func (coordinator *SpeechFeedbackCoordinator) EnsureAgentVoiceMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) (SpeechFeedbackReference, error) {
	if !trustedSpeechFeedbackActor(ctx, actor) ||
		strings.TrimSpace(threadID) != threadID ||
		strings.TrimSpace(messageID) != messageID {
		return SpeechFeedbackReference{}, ErrInvalidSpeechFeedback
	}
	reference, err := coordinator.repository.
		EnsureConfirmedAgentVoiceMessage(
			ctx,
			actor.UserID,
			threadID,
			messageID,
		)
	if err != nil {
		return SpeechFeedbackReference{}, err
	}
	if !reference.valid() {
		return SpeechFeedbackReference{}, ErrInvalidSpeechFeedback
	}
	return reference, nil
}

func (coordinator *SpeechFeedbackCoordinator) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	speechFeedbackID string,
) (SpeechFeedback, error) {
	if coordinator == nil || coordinator.repository == nil ||
		!trustedSpeechFeedbackActor(ctx, actor) {
		return SpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	feedback, err := coordinator.repository.GetSpeechFeedback(
		ctx,
		actor.UserID,
		speechFeedbackID,
	)
	if errors.Is(err, ErrSpeechFeedbackNotFound) {
		return SpeechFeedback{}, ErrSpeechFeedbackNotFound
	}
	return feedback, err
}

func (coordinator *SpeechFeedbackCoordinator) StatusURLForConversationTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
) (string, bool, error) {
	if coordinator == nil || coordinator.repository == nil ||
		!trustedSpeechFeedbackActor(ctx, actor) ||
		strings.TrimSpace(turnID) != turnID {
		return "", false, ErrInvalidSpeechFeedback
	}
	reference, found, err := coordinator.repository.
		FindSpeechFeedbackByConversationTurn(
			ctx,
			actor.UserID,
			turnID,
		)
	if err != nil || !found {
		return "", false, err
	}
	if !reference.valid() {
		return "", false, ErrInvalidSpeechFeedback
	}
	return reference.StatusURL, true, nil
}

func (coordinator *SpeechFeedbackCoordinator) StatusURLForAgentVoiceMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	messageID string,
) (string, bool, error) {
	if coordinator == nil || coordinator.repository == nil ||
		!trustedSpeechFeedbackActor(ctx, actor) ||
		strings.TrimSpace(messageID) != messageID {
		return "", false, ErrInvalidSpeechFeedback
	}
	reference, found, err := coordinator.repository.
		FindSpeechFeedbackByAgentMessage(
			ctx,
			actor.UserID,
			messageID,
		)
	if err != nil || !found {
		return "", false, err
	}
	if !reference.valid() {
		return "", false, ErrInvalidSpeechFeedback
	}
	return reference.StatusURL, true, nil
}

func trustedSpeechFeedbackActor(
	ctx context.Context,
	actor requestcontext.Actor,
) bool {
	if ctx == nil || !actor.Valid() {
		return false
	}
	trusted, ok := requestcontext.ActorFromContext(ctx)
	return ok && trusted == actor
}

var _ SpeechFeedbackReader = (*SpeechFeedbackCoordinator)(nil)
