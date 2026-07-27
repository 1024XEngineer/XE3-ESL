package practice

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestVoiceApplicationResolvesActorFromFrozenSnapshot(t *testing.T) {
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000001",
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	repository := &voiceRepositoryStub{
		session: persistence.Session{
			ID:          "practice-session",
			OwnerUserID: actor.UserID,
			Snapshot: persistence.SessionSnapshot{
				Participants: []persistence.ParticipantSnapshot{
					{
						ParticipantID: "agent-participant",
						SubjectRef: persistence.SubjectRef{
							Namespace: "speakup.agent",
							SubjectID: actor.UserID,
						},
					},
					{
						ParticipantID: "actor-participant",
						SubjectRef: persistence.SubjectRef{
							Namespace: "speakup.user",
							SubjectID: actor.UserID,
						},
					},
				},
			},
		},
	}
	application, err := NewVoiceApplication(repository, "speakup.user")
	if err != nil {
		t.Fatalf("NewVoiceApplication: %v", err)
	}

	participantID, err := application.ResolveActorParticipant(
		context.Background(),
		actor,
		"practice-session",
	)
	if err != nil {
		t.Fatalf("ResolveActorParticipant: %v", err)
	}
	if participantID != "actor-participant" {
		t.Fatalf("participant ID = %q", participantID)
	}
}

func TestVoiceApplicationHidesMissingActorParticipant(t *testing.T) {
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000002",
		SessionID: "20000000-0000-4000-8000-000000000002",
	}
	application, err := NewVoiceApplication(
		&voiceRepositoryStub{session: persistence.Session{
			ID: "practice-session",
			Snapshot: persistence.SessionSnapshot{
				Participants: []persistence.ParticipantSnapshot{{
					ParticipantID: "another-user",
					SubjectRef: persistence.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: "10000000-0000-4000-8000-000000000003",
					},
				}},
			},
		}},
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("NewVoiceApplication: %v", err)
	}

	if _, err := application.ResolveActorParticipant(
		context.Background(),
		actor,
		"practice-session",
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("ResolveActorParticipant error = %v", err)
	}
}

func TestVoiceApplicationAppliesStableTurnWithoutContent(t *testing.T) {
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000004",
		SessionID: "20000000-0000-4000-8000-000000000004",
	}
	repository := &voiceRepositoryStub{
		turnResult: persistence.TurnResult{
			SessionID:      "practice-session",
			TurnID:         "confirmed-turn",
			EffectiveTurns: 3,
			SessionVersion: 4,
			TurnLimit:      3,
			Completed:      true,
		},
	}
	application, err := NewVoiceApplication(repository, "speakup.user")
	if err != nil {
		t.Fatalf("NewVoiceApplication: %v", err)
	}

	progress, err := application.ApplyEffectiveTurn(
		context.Background(),
		actor,
		"practice-session",
		"confirmed-turn",
	)
	if err != nil {
		t.Fatalf("ApplyEffectiveTurn: %v", err)
	}
	if progress.EffectiveTurns != 3 ||
		progress.SessionVersion != 4 ||
		progress.TurnLimit != 3 ||
		!progress.SessionCompleted {
		t.Fatalf("progress = %#v", progress)
	}
	if repository.consumed.SessionID != "practice-session" ||
		repository.consumed.TurnID != "confirmed-turn" ||
		string(repository.consumed.Payload) != voiceEffectiveTurnPayload {
		t.Fatalf("ConsumeTurn command = %#v", repository.consumed)
	}
}

func TestNewVoiceApplicationRejectsInvalidNamespace(t *testing.T) {
	for _, namespace := range []string{"", " speakup.user", "SpeakUp.User", "user/name"} {
		if _, err := NewVoiceApplication(
			&voiceRepositoryStub{},
			namespace,
		); !errors.Is(err, persistence.ErrInvalidArgument) {
			t.Fatalf("namespace %q error = %v", namespace, err)
		}
	}
}

func TestVoiceApplicationRejectsIncompleteProgressEvidence(t *testing.T) {
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000005",
		SessionID: "20000000-0000-4000-8000-000000000005",
	}
	application, err := NewVoiceApplication(
		&voiceRepositoryStub{turnResult: persistence.TurnResult{
			SessionID:      "practice-session",
			TurnID:         "confirmed-turn",
			EffectiveTurns: 1,
			SessionVersion: 2,
		}},
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("NewVoiceApplication: %v", err)
	}

	if _, err := application.ApplyEffectiveTurn(
		context.Background(),
		actor,
		"practice-session",
		"confirmed-turn",
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("ApplyEffectiveTurn error = %v", err)
	}
}

type voiceRepositoryStub struct {
	session    persistence.Session
	getErr     error
	turnResult persistence.TurnResult
	consumeErr error
	consumed   persistence.ConsumeTurnCommand
}

func (r *voiceRepositoryStub) GetSession(
	context.Context,
	persistence.Actor,
	string,
) (persistence.Session, error) {
	return r.session, r.getErr
}

func (r *voiceRepositoryStub) ConsumeTurn(
	_ context.Context,
	_ persistence.Actor,
	command persistence.ConsumeTurnCommand,
) (persistence.TurnResult, error) {
	r.consumed = command
	return r.turnResult, r.consumeErr
}

var _ VoiceRepository = (*voiceRepositoryStub)(nil)
