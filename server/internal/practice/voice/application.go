package voice

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

const voiceEffectiveTurnPayload = "agent.voice_effective_turn/v1"
const voiceFollowUpTurnPayload = "agent.voice_follow_up_turn/v1"

// repository is the Practice-owned persistence view needed by the
// voice-round application boundary. It deliberately excludes deletion and
// aggregate-creation operations.
type repository interface {
	GetContextSession(
		context.Context,
		persistence.Actor,
		string,
	) (persistence.ContextSession, error)
	GetContextSessionSnapshot(
		context.Context,
		persistence.Actor,
		string,
	) (persistence.ContextSessionSnapshot, error)
	AdvanceContextVoiceTurn(
		context.Context,
		persistence.Actor,
		persistence.ConsumeTurnCommand,
	) (persistence.TurnResult, error)
}

// TurnProgress is the Practice decision produced after Conversation confirms
// an effective Turn.
type TurnProgress struct {
	EffectiveTurns   int
	SessionVersion   int
	TurnLimit        int
	SessionCompleted bool
}

// RequiresSessionReview reports whether completing this frozen Session must
// synchronously create a formal Review. Interview and IELTS reports are
// delivered through asynchronous Evaluation workflows, so neither flow should
// depend on the legacy Review generation timeout.
func (a *Application) RequiresSessionReview(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (bool, error) {
	if a == nil || a.repository == nil ||
		sessionID == "" || sessionID != strings.TrimSpace(sessionID) {
		return false, ErrInvalidRequest
	}
	if err := validateVoiceActor(ctx, actor); err != nil {
		return false, err
	}
	session, err := a.repository.GetContextSession(
		ctx,
		toPersistenceActor(actor),
		sessionID,
	)
	if err != nil {
		return false, mapRepositoryError(err)
	}
	if session.ScenarioType == persistence.ScenarioFamilyInterview {
		return false, nil
	}
	switch session.ScenarioModel {
	case persistence.ScenarioModelIELTSSpeakingFullMock,
		persistence.ScenarioModelIELTSSpeakingPart1,
		persistence.ScenarioModelIELTSSpeakingPart2,
		persistence.ScenarioModelIELTSSpeakingPart3:
		return false, nil
	default:
		return true, nil
	}
}

// Application exposes Practice progression without leaking its Repository to
// Conversation. actorSubjectNamespace is supplied by
// composition because Practice treats SubjectRef namespaces as opaque.
type Application struct {
	repository            repository
	actorSubjectNamespace string
}

func NewApplication(
	repository repository,
	actorSubjectNamespace string,
) (*Application, error) {
	namespace := strings.TrimSpace(actorSubjectNamespace)
	if repository == nil || namespace != actorSubjectNamespace ||
		!validSubjectNamespace(namespace) {
		return nil, ErrInvalidRequest
	}
	return &Application{
		repository:            repository,
		actorSubjectNamespace: namespace,
	}, nil
}

// ResolveActorParticipant resolves the authenticated user against the frozen
// Session snapshot. Namespace and subject ID are compared exactly.
func (a *Application) ResolveActorParticipant(
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
	session, err := a.repository.GetContextSession(
		ctx,
		persistenceActor,
		sessionID,
	)
	if err != nil {
		return "", mapRepositoryError(err)
	}
	if session.Status != persistence.ContextSessionProgress {
		return "", ErrConflict
	}
	snapshot, err := a.repository.GetContextSessionSnapshot(
		ctx,
		persistenceActor,
		sessionID,
	)
	if err != nil {
		return "", mapRepositoryError(err)
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
		case "FACILITATOR", "INTERVIEWER":
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
		case "LEARNER", "CANDIDATE":
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

// ApplyEffectiveTurn atomically advances Practice through its Repository. The
// payload is a stable contract marker: the owner-scoped Turn ID is the
// application idempotency identity, while Practice never receives transcript
// or audio content.
func (a *Application) ApplyEffectiveTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	turnID string,
	countsTowardTurnLimit bool,
) (TurnProgress, error) {
	if a == nil || a.repository == nil ||
		sessionID == "" || sessionID != strings.TrimSpace(sessionID) ||
		turnID == "" || turnID != strings.TrimSpace(turnID) {
		return TurnProgress{}, ErrInvalidRequest
	}
	if err := validateVoiceActor(ctx, actor); err != nil {
		return TurnProgress{}, err
	}

	payload := voiceEffectiveTurnPayload
	if !countsTowardTurnLimit {
		payload = voiceFollowUpTurnPayload
	}
	result, err := a.repository.AdvanceContextVoiceTurn(
		ctx,
		toPersistenceActor(actor),
		persistence.ConsumeTurnCommand{
			SessionID:             sessionID,
			TurnID:                turnID,
			CountsTowardTurnLimit: countsTowardTurnLimit,
			Payload:               []byte(payload),
		},
	)
	if err != nil {
		return TurnProgress{}, mapRepositoryError(err)
	}
	if result.SessionID != sessionID ||
		result.TurnID != turnID ||
		result.EffectiveTurns < 1 ||
		result.SessionVersion <= 1 ||
		result.TurnLimit < 1 ||
		result.TurnLimit > 14 ||
		result.TurnLimit < result.EffectiveTurns ||
		result.Completed != (result.EffectiveTurns == result.TurnLimit) {
		return TurnProgress{}, ErrConflict
	}
	return TurnProgress{
		EffectiveTurns:   result.EffectiveTurns,
		SessionVersion:   result.SessionVersion,
		TurnLimit:        result.TurnLimit,
		SessionCompleted: result.Completed,
	}, nil
}

func toPersistenceActor(actor requestcontext.Actor) persistence.Actor {
	return persistence.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapRepositoryError(err error) error {
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

var _ PracticePort = (*Application)(nil)

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
