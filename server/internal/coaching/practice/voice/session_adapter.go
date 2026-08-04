package voice

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type sessionAdapter struct {
	repository sessionRepository
}

type sessionRepository interface {
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
	ReplayVoiceStart(
		context.Context,
		practice.Actor,
		practice.IdempotencyIntent,
	) (practice.SessionBootstrap, bool, error)
	ActivateSession(
		context.Context,
		practice.Actor,
		string,
		string,
		practice.IdempotencyIntent,
	) (practice.SessionBootstrap, error)
}

func (adapter *sessionAdapter) Start(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	idempotencyKey string,
) (Session, error) {
	if adapter == nil || adapter.repository == nil ||
		!actor.Valid() ||
		strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return Session{}, ErrInvalidRequest
	}
	practiceActor := practiceActor(actor)
	intent := practice.IdempotencyIntent{
		Method: "POST",
		CanonicalPath: "/v1/practice-sessions/" + sessionID +
			"/voice-activation",
		Key:                idempotencyKey,
		PayloadFingerprint: sha256.Sum256(nil),
	}
	replayed, found, err := adapter.repository.ReplayVoiceStart(
		ctx,
		practiceActor,
		intent,
	)
	if err != nil {
		return Session{}, mapPracticeError(err)
	}
	if found {
		if replayed.Session.ID != sessionID {
			return Session{}, ErrIdempotencyConflict
		}
		return mapPracticeSession(replayed, actor.UserID)
	}
	session, err := adapter.repository.GetSession(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return Session{}, mapPracticeError(err)
	}
	if session.ID != sessionID || strings.TrimSpace(session.PlanID) == "" {
		return Session{}, ErrInvalidContext
	}
	activated, err := adapter.repository.ActivateSession(
		ctx,
		practiceActor,
		sessionID,
		session.PlanID,
		intent,
	)
	if err != nil {
		return Session{}, mapPracticeError(err)
	}
	if activated.Session.ID != sessionID ||
		activated.Session.PlanID != session.PlanID {
		return Session{}, ErrInvalidContext
	}
	return mapPracticeSession(activated, actor.UserID)
}

func (adapter *sessionAdapter) GetByID(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (Session, error) {
	if adapter == nil || adapter.repository == nil || !actor.Valid() ||
		strings.TrimSpace(sessionID) == "" {
		return Session{}, ErrInvalidRequest
	}
	practiceActor := practiceActor(actor)
	session, err := adapter.repository.GetSession(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return Session{}, mapPracticeError(err)
	}
	snapshot, err := adapter.repository.GetSessionSnapshot(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return Session{}, mapPracticeError(err)
	}
	return mapPracticeSession(
		practice.SessionBootstrap{
			Session:  session,
			Snapshot: snapshot,
		},
		actor.UserID,
	)
}

func practiceActor(actor requestcontext.Actor) practice.Actor {
	return practice.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapPracticeSession(
	bootstrap practice.SessionBootstrap,
	actorUserID string,
) (Session, error) {
	session := bootstrap.Session
	snapshot := bootstrap.Snapshot
	selection := snapshot.SceneSelection
	if session.ID == "" ||
		session.PlanID == "" ||
		session.PlanRevision < 1 ||
		snapshot.ID != session.SnapshotID ||
		snapshot.SessionID != session.ID ||
		snapshot.PlanRevision != session.PlanRevision ||
		snapshot.SceneFamily != session.SceneFamily ||
		snapshot.SceneModel != session.SceneModel ||
		selection.Scene.ID == "" ||
		selection.Scene.Version < 1 ||
		selection.Scene.Family != session.SceneFamily ||
		selection.Scene.Model != session.SceneModel ||
		len(selection.SelectedRoleIDs) == 0 {
		return Session{}, ErrInvalidContext
	}
	result := Session{
		ID:                      session.ID,
		PlanID:                  session.PlanID,
		SceneID:                 selection.Scene.ID,
		SceneVersion:            selection.Scene.Version,
		SceneFamily:             string(snapshot.SceneFamily),
		SceneModel:              string(snapshot.SceneModel),
		Prompt:                  cloneScenePrompt(selection.Scene.Prompt),
		SessionVersion:          session.Version,
		EffectiveTurns:          session.EffectiveTurns,
		TurnLimit:               snapshot.SessionPolicy.MaxEffectiveTurns,
		MaxFollowUpsPerQuestion: snapshot.SessionPolicy.MaxFollowUpsPerQuestion,
		Completed: session.Status ==
			practice.SessionCompleted,
		Status: string(session.Status),
	}
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	participantOrders := make(map[int]struct{}, len(snapshot.Participants))
	facilitatorRoles := make(map[string]struct{})
	selectedRoles := make(map[string]struct{}, len(selection.SelectedRoleIDs))
	for _, roleID := range selection.SelectedRoleIDs {
		if strings.TrimSpace(roleID) == "" {
			return Session{}, ErrInvalidContext
		}
		if _, duplicate := selectedRoles[roleID]; duplicate {
			return Session{}, ErrInvalidContext
		}
		selectedRoles[roleID] = struct{}{}
	}
	facilitatorOrder := 0
	for _, participant := range snapshot.Participants {
		if participant.ID == "" ||
			participant.SessionID != session.ID ||
			participant.Order < 1 {
			return Session{}, ErrInvalidContext
		}
		if _, duplicate := participantIDs[participant.ID]; duplicate {
			return Session{}, ErrInvalidContext
		}
		if _, duplicate := participantOrders[participant.Order]; duplicate {
			return Session{}, ErrInvalidContext
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
				return Session{}, ErrInvalidContext
			}
			if _, selected := selectedRoles[participant.RoleDefinitionID]; !selected {
				return Session{}, ErrInvalidContext
			}
			if _, duplicate := facilitatorRoles[participant.RoleDefinitionID]; duplicate {
				return Session{}, ErrInvalidContext
			}
			facilitatorRoles[participant.RoleDefinitionID] = struct{}{}
			if result.FacilitatorParticipantID == "" ||
				participant.Order < facilitatorOrder {
				result.FacilitatorParticipantID = participant.ID
				facilitatorOrder = participant.Order
			}
		case "LEARNER":
			if result.LearnerParticipantID != "" ||
				participant.SubjectRef.Namespace != "speakup.user" ||
				participant.SubjectRef.SubjectID != actorUserID ||
				participant.RoleDefinitionID != "" ||
				participant.RoleSnapshot != nil {
				return Session{}, ErrNotFound
			}
			result.LearnerParticipantID = participant.ID
		default:
			return Session{}, ErrInvalidContext
		}
	}
	if result.FacilitatorParticipantID == "" ||
		result.LearnerParticipantID == "" ||
		len(facilitatorRoles) != len(selectedRoles) ||
		result.TurnLimit < 1 ||
		result.TurnLimit > 14 ||
		result.EffectiveTurns < 0 ||
		result.EffectiveTurns > result.TurnLimit ||
		(result.Status == string(
			practice.SessionCompleted,
		)) != result.Completed ||
		!validPersistedSessionLifecycle(session, result.TurnLimit) {
		return Session{}, ErrInvalidContext
	}
	return result, nil
}

func cloneScenePrompt(source scene.ScenePrompt) scene.ScenePrompt {
	result := source
	result.FocusAreas = slices.Clone(source.FocusAreas)
	result.TurnBlueprints = slices.Clone(source.TurnBlueprints)
	return result
}

func validPersistedSessionLifecycle(
	session practice.Session,
	turnLimit int,
) bool {
	if turnLimit < 1 || turnLimit > 14 ||
		session.EffectiveTurns < 0 ||
		session.EffectiveTurns > turnLimit {
		return false
	}
	switch session.Status {
	case practice.SessionStarting:
		return session.EffectiveTurns == 0 &&
			session.StartedAt == nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case practice.SessionInProgress,
		practice.SessionPaused:
		return session.EffectiveTurns < turnLimit &&
			session.StartedAt != nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case practice.SessionCompleted:
		return session.EffectiveTurns > 0 &&
			session.StartedAt != nil &&
			session.EndedAt != nil &&
			strings.TrimSpace(session.EndReason) != ""
	case practice.SessionEndedEarly:
		return session.EffectiveTurns < turnLimit &&
			session.StartedAt != nil &&
			session.EndedAt != nil &&
			strings.TrimSpace(session.EndReason) != ""
	default:
		return false
	}
}

func mapPracticeError(err error) error {
	switch {
	case errors.Is(err, practice.ErrInvalidArgument):
		return ErrInvalidRequest
	case errors.Is(err, practice.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, practice.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	case errors.Is(err, practice.ErrConflict),
		errors.Is(err, practice.ErrSessionCompleted):
		return ErrConflict
	default:
		return err
	}
}

var _ SessionPort = (*sessionAdapter)(nil)
