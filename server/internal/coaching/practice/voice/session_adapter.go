package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
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
	TransitionSession(
		context.Context,
		practice.Actor,
		practice.TransitionSessionCommand,
	) (practice.Session, bool, error)
}

func (adapter *sessionAdapter) PrepareStart(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	idempotencyKey string,
) (Session, bool, error) {
	if adapter == nil || adapter.repository == nil ||
		!actor.Valid() ||
		strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return Session{}, false, ErrInvalidRequest
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
		return Session{}, false, mapPracticeError(err)
	}
	if found {
		if replayed.Session.ID != sessionID {
			return Session{}, false, ErrIdempotencyConflict
		}
		session, mapErr := mapPracticeSession(replayed, actor.UserID)
		return session, true, mapErr
	}
	session, err := adapter.repository.GetSession(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return Session{}, false, mapPracticeError(err)
	}
	if session.ID != sessionID || strings.TrimSpace(session.PlanID) == "" {
		return Session{}, false, ErrInvalidContext
	}
	snapshot, err := adapter.repository.GetSessionSnapshot(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return Session{}, false, mapPracticeError(err)
	}
	if _, err := mapPracticeSession(
		practice.SessionBootstrap{
			Session:  session,
			Snapshot: snapshot,
		},
		actor.UserID,
	); err != nil {
		return Session{}, false, err
	}
	mapped, err := mapPracticeSession(
		practice.SessionBootstrap{Session: session, Snapshot: snapshot},
		actor.UserID,
	)
	return mapped, false, err
}

func (adapter *sessionAdapter) CommitStart(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	planID string,
	idempotencyKey string,
) (Session, error) {
	if adapter == nil || adapter.repository == nil || !actor.Valid() ||
		strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(planID) == "" ||
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
	activated, err := adapter.repository.ActivateSession(
		ctx,
		practiceActor,
		sessionID,
		planID,
		intent,
	)
	if err != nil {
		return Session{}, mapPracticeError(err)
	}
	if activated.Session.ID != sessionID ||
		activated.Session.PlanID != planID {
		return Session{}, ErrInvalidContext
	}
	return mapPracticeSession(activated, actor.UserID)
}

func (adapter *sessionAdapter) AbortStart(
	ctx context.Context,
	actor requestcontext.Actor,
	session Session,
	idempotencyKey string,
) error {
	if adapter == nil || adapter.repository == nil || !actor.Valid() ||
		session.ID == "" || session.SessionVersion < 1 ||
		(session.Status != "starting" && session.Status != "in_progress") ||
		session.EffectiveTurns != 0 ||
		strings.TrimSpace(idempotencyKey) == "" {
		return ErrInvalidRequest
	}
	payload, err := json.Marshal(struct {
		ExpectedSessionVersion int `json:"expected_session_version"`
	}{ExpectedSessionVersion: session.SessionVersion})
	if err != nil {
		return err
	}
	keyFingerprint := sha256.Sum256([]byte(idempotencyKey))
	intent := practice.IdempotencyIntent{
		Method:             "POST",
		CanonicalPath:      "/v1/practice-sessions/" + session.ID + "/end-early",
		Key:                "voice-activation-abort-" + hex.EncodeToString(keyFingerprint[:]),
		PayloadFingerprint: sha256.Sum256(payload),
	}
	ended, _, err := adapter.repository.TransitionSession(
		ctx,
		practiceActor(actor),
		practice.TransitionSessionCommand{
			SessionID:              session.ID,
			ExpectedSessionVersion: session.SessionVersion,
			Transition:             practice.SessionEndEarly,
			Intent:                 intent,
		},
	)
	if err != nil {
		return mapPracticeError(err)
	}
	if ended.ID != session.ID || ended.Status != practice.SessionEndedEarly ||
		ended.EffectiveTurns != 0 {
		return ErrInvalidContext
	}
	return nil
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
	option, err := selection.PracticeOption()
	if err != nil {
		return Session{}, ErrInvalidContext
	}
	turnPolicy, err := practice.ResolveTurnPolicy(option.TurnPolicyRef)
	if err != nil ||
		(turnPolicy.Kind == practice.TurnPolicyFrozenIELTS &&
			!practice.ValidIELTSAssignment(
				snapshot.IELTSAssignment,
				turnPolicy.Mode,
				selection.Scene.Prompt.TurnBlueprints,
			)) ||
		(turnPolicy.Kind != practice.TurnPolicyFrozenIELTS &&
			snapshot.IELTSAssignment != nil) {
		return Session{}, ErrInvalidContext
	}
	if session.ID == "" ||
		session.PlanID == "" ||
		session.PlanRevision < 1 ||
		snapshot.ID != session.SnapshotID ||
		snapshot.SessionID != session.ID ||
		snapshot.PlanRevision != session.PlanRevision ||
		snapshot.Experience != session.Experience ||
		snapshot.Category != session.Category ||
		snapshot.PracticeMode != session.PracticeMode ||
		selection.Scene.ID == "" ||
		selection.Scene.Version < 1 ||
		selection.Scene.Experience != session.Experience ||
		selection.Scene.Category != session.Category ||
		option.Mode != session.PracticeMode ||
		option.EvaluationPolicyRef != session.EvaluationPolicyRef ||
		!validVoiceTurnPolicy(option.TurnPolicyRef) ||
		len(selection.SelectedRoleIDs) == 0 {
		return Session{}, ErrInvalidContext
	}
	result := Session{
		ID:                 session.ID,
		PlanID:             session.PlanID,
		SceneID:            selection.Scene.ID,
		SceneVersion:       selection.Scene.Version,
		PracticeExperience: string(snapshot.Experience),
		SceneCategory:      string(snapshot.Category),
		PracticeMode:       string(snapshot.PracticeMode),
		TurnPolicyRef:      option.TurnPolicyRef,
		Prompt:             cloneScenePrompt(selection.Scene.Prompt),
		ScenarioContext:    cloneScenarioPreparationContext(snapshot.Preparation.ScenarioContext),
		IELTSAssignment:    cloneIELTSAssignment(snapshot.IELTSAssignment),
		SessionVersion:     session.Version,
		EffectiveTurns:     session.EffectiveTurns,
		TurnLimit:          snapshot.SessionPolicy.MaxEffectiveTurns,
		CompletionMode: practice.NormalizeCompletionMode(
			snapshot.SessionPolicy.CompletionMode,
		),
		MaxFollowUpsPerQuestion:    snapshot.SessionPolicy.MaxFollowUpsPerQuestion,
		QuestionTranslationAllowed: snapshot.SessionPolicy.QuestionTranslationAllowed,
		QuestionTipsAllowed:        snapshot.SessionPolicy.QuestionTipsAllowed,
		AvatarAllowed:              snapshot.SessionPolicy.AvatarAllowed,
		RetryAllowed:               snapshot.SessionPolicy.RetryAllowed,
		SpeechFeedbackAllowed:      snapshot.SessionPolicy.SpeechFeedbackAllowed,
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
		result.EffectiveTurns < 0 || !validVoiceProgress(result) ||
		(result.Status == string(
			practice.SessionCompleted,
		)) != result.Completed ||
		!validPersistedSessionLifecycle(
			session,
			result.TurnLimit,
			result.CompletionMode,
		) {
		return Session{}, ErrInvalidContext
	}
	return result, nil
}

func cloneScenePrompt(source practice.ScenePrompt) practice.ScenePrompt {
	result := source
	result.FocusAreas = append([]string(nil), source.FocusAreas...)
	result.TurnBlueprints = append([]string(nil), source.TurnBlueprints...)
	return result
}

func cloneScenarioPreparationContext(
	source *practice.ScenarioPreparationContext,
) *practice.ScenarioPreparationContext {
	if source == nil {
		return nil
	}
	result := *source
	return &result
}

func cloneIELTSAssignment(
	source *practice.IELTSAssignment,
) *practice.IELTSAssignment {
	if source == nil {
		return nil
	}
	result := *source
	result.Parts = make([]practice.IELTSPart, len(source.Parts))
	for index, part := range source.Parts {
		result.Parts[index] = part
		result.Parts[index].TurnBlueprints = append(
			[]string(nil),
			part.TurnBlueprints...,
		)
	}
	return &result
}

func validPersistedSessionLifecycle(
	session practice.Session,
	turnLimit int,
	completionMode practice.CompletionMode,
) bool {
	if session.EffectiveTurns < 0 ||
		(completionMode == practice.CompletionModeUserControlled && turnLimit != 0) ||
		(completionMode == practice.CompletionModeTurnLimited &&
			(turnLimit < 1 || turnLimit > practice.MaxPracticeTurns ||
				session.EffectiveTurns > turnLimit)) ||
		(completionMode != practice.CompletionModeUserControlled &&
			completionMode != practice.CompletionModeTurnLimited) {
		return false
	}
	turnAvailable := completionMode == practice.CompletionModeUserControlled ||
		session.EffectiveTurns < turnLimit
	switch session.Status {
	case practice.SessionStarting:
		return session.EffectiveTurns == 0 &&
			session.StartedAt == nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case practice.SessionInProgress,
		practice.SessionPaused:
		return turnAvailable &&
			session.StartedAt != nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case practice.SessionCompleted:
		return session.EffectiveTurns > 0 &&
			session.StartedAt != nil &&
			session.EndedAt != nil &&
			strings.TrimSpace(session.EndReason) != ""
	case practice.SessionEndedEarly:
		return turnAvailable &&
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
