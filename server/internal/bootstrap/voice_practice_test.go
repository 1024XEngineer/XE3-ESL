package bootstrap

import (
	"context"
	"errors"
	"testing"

	agent "github.com/1024XEngineer/XE3-ESL/server/internal/agent/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestPracticeVoicePortMapsTrustedActorAndProgress(t *testing.T) {
	application := &practiceVoiceApplicationStub{
		participantID: "candidate-1",
		progress: practice.VoiceTurnProgress{
			EffectiveTurns:   3,
			SessionVersion:   4,
			TurnLimit:        3,
			SessionCompleted: true,
		},
	}
	port, err := NewPracticeVoicePort(application)
	if err != nil {
		t.Fatalf("NewPracticeVoicePort: %v", err)
	}
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000001",
		SessionID: "20000000-0000-4000-8000-000000000001",
	}

	participantID, err := port.ResolveActorParticipant(
		context.Background(),
		actor,
		"practice-session",
	)
	if err != nil || participantID != "candidate-1" {
		t.Fatalf("ResolveActorParticipant = %q, %v", participantID, err)
	}
	progress, err := port.ApplyEffectiveTurn(
		context.Background(),
		actor,
		"practice-session",
		"confirmed-turn",
		true,
	)
	if err != nil {
		t.Fatalf("ApplyEffectiveTurn: %v", err)
	}
	if progress != (agent.VoiceTurnProgress{
		EffectiveTurns:   3,
		SessionVersion:   4,
		TurnLimit:        3,
		SessionCompleted: true,
	}) {
		t.Fatalf("progress = %#v", progress)
	}
	if application.actor != (persistence.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}) {
		t.Fatalf("mapped actor = %#v", application.actor)
	}
	required, err := port.RequiresSessionReview(
		context.Background(),
		actor,
		"practice-session",
	)
	if err != nil || !required {
		t.Fatalf("RequiresSessionReview = %t, %v", required, err)
	}
}

func TestPracticeVoicePortPropagatesPracticeFailure(t *testing.T) {
	want := errors.New("practice unavailable")
	port, err := NewPracticeVoicePort(&practiceVoiceApplicationStub{err: want})
	if err != nil {
		t.Fatalf("NewPracticeVoicePort: %v", err)
	}

	_, err = port.ApplyEffectiveTurn(
		context.Background(),
		requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000002",
			SessionID: "20000000-0000-4000-8000-000000000002",
		},
		"practice-session",
		"confirmed-turn",
		true,
	)
	if !errors.Is(err, want) {
		t.Fatalf("ApplyEffectiveTurn error = %v", err)
	}
}

func TestNewPracticeVoicePortRejectsNilApplication(t *testing.T) {
	if _, err := NewPracticeVoicePort(nil); err == nil {
		t.Fatal("NewPracticeVoicePort(nil) succeeded")
	}
}

func TestPracticeVoicePortRejectsInvalidTrustedActor(t *testing.T) {
	application := &practiceVoiceApplicationStub{}
	port, err := NewPracticeVoicePort(application)
	if err != nil {
		t.Fatalf("NewPracticeVoicePort: %v", err)
	}

	if _, err := port.ResolveActorParticipant(
		context.Background(),
		requestcontext.Actor{},
		"practice-session",
	); !errors.Is(err, persistence.ErrInvalidArgument) {
		t.Fatalf("ResolveActorParticipant error = %v", err)
	}
	if application.actor != (persistence.Actor{}) {
		t.Fatalf("invalid actor reached Practice: %#v", application.actor)
	}
}

func TestPracticeVoicePortRejectsIncompleteProgressEvidence(t *testing.T) {
	port, err := NewPracticeVoicePort(&practiceVoiceApplicationStub{
		progress: practice.VoiceTurnProgress{
			EffectiveTurns: 1,
			SessionVersion: 2,
		},
	})
	if err != nil {
		t.Fatalf("NewPracticeVoicePort: %v", err)
	}

	if _, err := port.ApplyEffectiveTurn(
		context.Background(),
		requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000003",
			SessionID: "20000000-0000-4000-8000-000000000003",
		},
		"practice-session",
		"confirmed-turn",
		true,
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("ApplyEffectiveTurn error = %v", err)
	}
}

type practiceVoiceApplicationStub struct {
	participantID string
	progress      practice.VoiceTurnProgress
	err           error
	actor         persistence.Actor
}

func (s *practiceVoiceApplicationStub) ResolveActorParticipant(
	_ context.Context,
	actor persistence.Actor,
	_ string,
) (string, error) {
	s.actor = actor
	return s.participantID, s.err
}

func (s *practiceVoiceApplicationStub) ApplyEffectiveTurn(
	_ context.Context,
	actor persistence.Actor,
	_ string,
	_ string,
	_ bool,
) (practice.VoiceTurnProgress, error) {
	s.actor = actor
	return s.progress, s.err
}

func (s *practiceVoiceApplicationStub) RequiresSessionReview(
	_ context.Context,
	actor persistence.Actor,
	_ string,
) (bool, error) {
	s.actor = actor
	return true, s.err
}

var _ PracticeVoiceApplication = (*practiceVoiceApplicationStub)(nil)
