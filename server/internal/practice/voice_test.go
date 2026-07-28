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
		session: persistence.ContextSession{
			ID:     "practice-session",
			Status: persistence.ContextSessionProgress,
		},
		snapshot: persistence.ContextSessionSnapshot{
			ID:        "snapshot-1",
			SessionID: "practice-session",
			Participants: []persistence.ContextParticipant{
				{
					ID:               "agent-participant",
					SessionID:        "practice-session",
					Role:             "FACILITATOR",
					RoleDefinitionID: "role-interviewer",
					RoleSnapshot: &persistence.RoleSnapshot{
						ID: "role-interviewer",
					},
					Order: 1,
					SubjectRef: persistence.SubjectRef{
						Namespace: "speakup.role",
						SubjectID: "role-interviewer",
					},
				},
				{
					ID:        "actor-participant",
					SessionID: "practice-session",
					Role:      "LEARNER",
					Order:     2,
					SubjectRef: persistence.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: actor.UserID,
					},
				},
			},
		},
	}
	repository.session.SnapshotID = repository.snapshot.ID
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
		&voiceRepositoryStub{
			session: persistence.ContextSession{
				ID:         "practice-session",
				SnapshotID: "snapshot-1",
				Status:     persistence.ContextSessionProgress,
			},
			snapshot: persistence.ContextSessionSnapshot{
				ID:        "snapshot-1",
				SessionID: "practice-session",
				Participants: []persistence.ContextParticipant{{
					ID:        "another-user",
					SessionID: "practice-session",
					Role:      "CANDIDATE",
					Order:     1,
					SubjectRef: persistence.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: "10000000-0000-4000-8000-000000000003",
					},
				}},
			},
		},
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
	session    persistence.ContextSession
	snapshot   persistence.ContextSessionSnapshot
	getErr     error
	turnResult persistence.TurnResult
	consumeErr error
	consumed   persistence.ConsumeTurnCommand
}

func (r *voiceRepositoryStub) GetContextSession(
	context.Context,
	persistence.Actor,
	string,
) (persistence.ContextSession, error) {
	return r.session, r.getErr
}

func (r *voiceRepositoryStub) GetContextSessionSnapshot(
	context.Context,
	persistence.Actor,
	string,
) (persistence.ContextSessionSnapshot, error) {
	return r.snapshot, r.getErr
}

func (r *voiceRepositoryStub) AdvanceContextVoiceTurn(
	_ context.Context,
	_ persistence.Actor,
	command persistence.ConsumeTurnCommand,
) (persistence.TurnResult, error) {
	r.consumed = command
	return r.turnResult, r.consumeErr
}

var _ VoiceRepository = (*voiceRepositoryStub)(nil)
