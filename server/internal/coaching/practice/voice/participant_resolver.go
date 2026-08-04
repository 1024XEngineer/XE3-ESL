package voice

import (
	"context"
	"errors"
	"strings"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// participantRepository is the Practice-owned persistence view needed by the
// voice-round application boundary. It deliberately excludes deletion and
// aggregate-creation operations.
type participantRepository interface {
	GetSession(
		context.Context,
		practice.Actor,
		string,
	) (practice.Session, error)
	GetSessionSnapshot(
		context.Context,
		practice.Actor,
		string,
	) (practice.SessionSnapshot, error)
}

// ParticipantResolver exposes Practice authorization without leaking its
// Repository to the voice-round runtime. actorSubjectNamespace is supplied by
// composition because Practice treats SubjectRef namespaces as opaque.
type ParticipantResolver struct {
	repository            participantRepository
	actorSubjectNamespace string
}

func NewParticipantResolver(
	repository participantRepository,
	actorSubjectNamespace string,
) (*ParticipantResolver, error) {
	namespace := strings.TrimSpace(actorSubjectNamespace)
	if repository == nil || namespace != actorSubjectNamespace ||
		!validSubjectNamespace(namespace) {
		return nil, ErrInvalidRequest
	}
	return &ParticipantResolver{
		repository:            repository,
		actorSubjectNamespace: namespace,
	}, nil
}

// ResolveActorParticipant resolves the authenticated user against the frozen
// Session snapshot. Namespace and subject ID are compared exactly.
func (a *ParticipantResolver) ResolveActorParticipant(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (string, error) {
	if a == nil || a.repository == nil ||
		sessionID == "" || sessionID != strings.TrimSpace(sessionID) {
		return "", ErrInvalidRequest
	}
	if err := validateVoiceActor(ctx, actor); err != nil {
		return "", err
	}

	persistenceActor := toPersistenceActor(actor)
	session, err := a.repository.GetSession(
		ctx,
		persistenceActor,
		sessionID,
	)
	if err != nil {
		return "", mapParticipantRepositoryError(err)
	}
	if session.Status != practice.SessionInProgress {
		return "", ErrConflict
	}
	snapshot, err := a.repository.GetSessionSnapshot(
		ctx,
		persistenceActor,
		sessionID,
	)
	if err != nil {
		return "", mapParticipantRepositoryError(err)
	}
	if snapshot.ID != session.SnapshotID ||
		snapshot.SessionID != session.ID {
		return "", ErrConflict
	}

	participantID := ""
	facilitators := 0
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	participantOrders := make(map[int]struct{}, len(snapshot.Participants))
	for _, participant := range snapshot.Participants {
		if participant.ID == "" ||
			participant.SessionID != session.ID ||
			participant.Order < 1 {
			return "", ErrConflict
		}
		if _, duplicate := participantIDs[participant.ID]; duplicate {
			return "", ErrConflict
		}
		if _, duplicate := participantOrders[participant.Order]; duplicate {
			return "", ErrConflict
		}
		participantIDs[participant.ID] = struct{}{}
		participantOrders[participant.Order] = struct{}{}
		switch participant.Role {
		case "FACILITATOR":
			if participant.SubjectRef.Namespace != "speakup.role" ||
				participant.SubjectRef.SubjectID !=
					participant.RoleDefinitionID ||
				participant.RoleDefinitionID == "" ||
				participant.RoleSnapshot == nil ||
				participant.RoleSnapshot.ID !=
					participant.RoleDefinitionID {
				return "", ErrConflict
			}
			facilitators++
		case "LEARNER":
			if participantID != "" {
				return "", ErrConflict
			}
			if participant.SubjectRef.Namespace !=
				a.actorSubjectNamespace ||
				participant.SubjectRef.SubjectID != actor.UserID ||
				participant.RoleDefinitionID != "" ||
				participant.RoleSnapshot != nil {
				return "", ErrNotFound
			}
			participantID = participant.ID
		default:
			return "", ErrConflict
		}
	}
	if participantID == "" || facilitators == 0 {
		return "", ErrNotFound
	}
	return participantID, nil
}

func toPersistenceActor(actor requestcontext.Actor) practice.Actor {
	return practice.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapParticipantRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return ErrRepository
	}
}

var _ PracticePort = (*ParticipantResolver)(nil)

func validSubjectNamespace(namespace string) bool {
	if namespace == "" || namespace[0] < 'a' || namespace[0] > 'z' {
		return false
	}
	for index := 1; index < len(namespace); index++ {
		character := namespace[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
