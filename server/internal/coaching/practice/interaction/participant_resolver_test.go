package interaction

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestApplicationResolvesLearnerFromFrozenSnapshot(t *testing.T) {
	repository := applicationRepositoryStub{
		session: practice.Session{
			ID:     "session-1",
			Status: practice.SessionInProgress,
		},
		snapshot: practice.SessionSnapshot{
			SessionID: "session-1",
			Participants: []practice.Participant{
				{
					ID:        "facilitator-1",
					SessionID: "session-1",
					Order:     1,
					Role:      "FACILITATOR",
					SubjectRef: practice.SubjectRef{
						Namespace: "speakup.role",
						SubjectID: "role-1",
					},
					RoleDefinitionID: "role-1",
					RoleSnapshot: &practice.RoleDefinition{
						ID: "role-1",
					},
				},
				{
					ID:        "learner-1",
					SessionID: "session-1",
					Order:     2,
					Role:      "LEARNER",
					SubjectRef: practice.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: "user-1",
					},
				},
			},
		},
	}
	application, err := NewParticipantResolver(repository, "speakup.user")
	if err != nil {
		t.Fatalf("NewParticipantResolver: %v", err)
	}
	participantID, err := application.ResolveActorParticipant(
		context.Background(),
		requestcontext.Actor{UserID: "user-1", SessionID: "auth-1"},
		"session-1",
	)
	if err != nil || participantID != "learner-1" {
		t.Fatalf("ResolveActorParticipant = %q, %v", participantID, err)
	}
}

type applicationRepositoryStub struct {
	session  practice.Session
	snapshot practice.SessionSnapshot
}

func (r applicationRepositoryStub) GetSession(
	context.Context,
	practice.Actor,
	string,
) (practice.Session, error) {
	return r.session, nil
}

func (r applicationRepositoryStub) GetSessionSnapshot(
	context.Context,
	practice.Actor,
	string,
) (practice.SessionSnapshot, error) {
	return r.snapshot, nil
}
