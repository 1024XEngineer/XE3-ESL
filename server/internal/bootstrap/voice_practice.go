package bootstrap

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

// PracticeVoiceApplication is the composition view of the Practice-owned
// application capability. Agent never receives or imports a Practice
// Repository.
type PracticeVoiceApplication interface {
	ResolveActorParticipant(
		context.Context,
		persistence.Actor,
		string,
	) (string, error)
	ApplyEffectiveTurn(
		context.Context,
		persistence.Actor,
		string,
		string,
	) (practice.VoiceTurnProgress, error)
}

type practiceVoicePort struct {
	application PracticeVoiceApplication
}

func NewPracticeVoicePort(
	application PracticeVoiceApplication,
) (agent.VoicePracticePort, error) {
	if application == nil {
		return nil, errors.New("bootstrap: Practice voice application is required")
	}
	return &practiceVoicePort{application: application}, nil
}

func (p *practiceVoicePort) ResolveActorParticipant(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (string, error) {
	if p == nil || p.application == nil || ctx == nil || !actor.Valid() {
		return "", persistence.ErrInvalidArgument
	}
	return p.application.ResolveActorParticipant(
		ctx,
		practiceVoiceActor(actor),
		sessionID,
	)
}

func (p *practiceVoicePort) ApplyEffectiveTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	turnID string,
) (agent.VoiceTurnProgress, error) {
	if p == nil || p.application == nil || ctx == nil || !actor.Valid() {
		return agent.VoiceTurnProgress{}, persistence.ErrInvalidArgument
	}
	progress, err := p.application.ApplyEffectiveTurn(
		ctx,
		practiceVoiceActor(actor),
		sessionID,
		turnID,
	)
	if err != nil {
		return agent.VoiceTurnProgress{}, err
	}
	if progress.EffectiveTurns < 1 ||
		progress.SessionVersion <= 1 ||
		progress.TurnLimit < 1 ||
		progress.TurnLimit > 6 ||
		progress.TurnLimit < progress.EffectiveTurns ||
		progress.SessionCompleted !=
			(progress.EffectiveTurns == progress.TurnLimit) {
		return agent.VoiceTurnProgress{}, persistence.ErrConflict
	}
	return agent.VoiceTurnProgress{
		EffectiveTurns:   progress.EffectiveTurns,
		SessionVersion:   progress.SessionVersion,
		TurnLimit:        progress.TurnLimit,
		SessionCompleted: progress.SessionCompleted,
	}, nil
}

func practiceVoiceActor(actor requestcontext.Actor) persistence.Actor {
	return persistence.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

var _ agent.VoicePracticePort = (*practiceVoicePort)(nil)
