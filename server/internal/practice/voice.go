package practice

import (
	"context"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

const voiceEffectiveTurnPayload = "agent.voice_effective_turn/v1"

// VoiceRepository is the Practice-owned persistence view needed by the
// voice-round application boundary. It deliberately excludes deletion and
// aggregate-creation operations.
type VoiceRepository interface {
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

// VoiceTurnProgress is the minimum Practice decision consumed by the Agent
// application orchestrator after Conversation confirms an effective Turn.
type VoiceTurnProgress struct {
	EffectiveTurns   int
	SessionVersion   int
	TurnLimit        int
	SessionCompleted bool
}

// RequiresSessionReview reports whether completing this frozen Session must
// synchronously create a formal Review. IELTS reports and practice history are
// delivered separately, so no IELTS speaking flow depends on Review generation.
func (a *VoiceApplication) RequiresSessionReview(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
) (bool, error) {
	if a == nil || a.repository == nil || ctx == nil ||
		sessionID == "" || sessionID != strings.TrimSpace(sessionID) {
		return false, persistence.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	session, err := a.repository.GetContextSession(ctx, actor, sessionID)
	if err != nil {
		return false, err
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

// VoiceApplication exposes Practice capabilities without leaking its
// Repository to Agent or Conversation. actorSubjectNamespace is supplied by
// composition because Practice treats SubjectRef namespaces as opaque.
type VoiceApplication struct {
	repository            VoiceRepository
	actorSubjectNamespace string
}

func NewVoiceApplication(
	repository VoiceRepository,
	actorSubjectNamespace string,
) (*VoiceApplication, error) {
	namespace := strings.TrimSpace(actorSubjectNamespace)
	if repository == nil || namespace != actorSubjectNamespace ||
		!validSubjectNamespace(namespace) {
		return nil, persistence.ErrInvalidArgument
	}
	return &VoiceApplication{
		repository:            repository,
		actorSubjectNamespace: namespace,
	}, nil
}

// ResolveActorParticipant resolves the authenticated user against the frozen
// Session snapshot. Namespace and subject ID are compared exactly.
func (a *VoiceApplication) ResolveActorParticipant(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
) (string, error) {
	if a == nil || a.repository == nil || ctx == nil ||
		sessionID == "" || sessionID != strings.TrimSpace(sessionID) {
		return "", persistence.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	session, err := a.repository.GetContextSession(ctx, actor, sessionID)
	if err != nil {
		return "", err
	}
	if session.Status != persistence.ContextSessionProgress {
		return "", persistence.ErrConflict
	}
	snapshot, err := a.repository.GetContextSessionSnapshot(
		ctx,
		actor,
		sessionID,
	)
	if err != nil {
		return "", err
	}
	if snapshot.ID != session.SnapshotID ||
		snapshot.SessionID != session.ID {
		return "", persistence.ErrConflict
	}

	participantID := ""
	facilitators := 0
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	participantOrders := make(map[int]struct{}, len(snapshot.Participants))
	for _, participant := range snapshot.Participants {
		if participant.ID == "" ||
			participant.SessionID != session.ID ||
			participant.Order < 1 {
			return "", persistence.ErrConflict
		}
		if _, duplicate := participantIDs[participant.ID]; duplicate {
			return "", persistence.ErrConflict
		}
		if _, duplicate := participantOrders[participant.Order]; duplicate {
			return "", persistence.ErrConflict
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
				return "", persistence.ErrConflict
			}
			facilitators++
		case "LEARNER", "CANDIDATE":
			if participantID != "" {
				return "", persistence.ErrConflict
			}
			if participant.SubjectRef.Namespace !=
				a.actorSubjectNamespace ||
				participant.SubjectRef.SubjectID != actor.UserID ||
				participant.RoleDefinitionID != "" ||
				participant.RoleSnapshot != nil {
				return "", persistence.ErrNotFound
			}
			participantID = participant.ID
		default:
			return "", persistence.ErrConflict
		}
	}
	if participantID == "" || facilitators == 0 {
		return "", persistence.ErrNotFound
	}
	return participantID, nil
}

// ApplyEffectiveTurn atomically advances Practice through its Repository. The
// payload is a stable contract marker: the owner-scoped Turn ID is the
// application idempotency identity, while Practice never receives transcript
// or audio content.
func (a *VoiceApplication) ApplyEffectiveTurn(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
	turnID string,
) (VoiceTurnProgress, error) {
	if a == nil || a.repository == nil || ctx == nil ||
		sessionID == "" || sessionID != strings.TrimSpace(sessionID) ||
		turnID == "" || turnID != strings.TrimSpace(turnID) {
		return VoiceTurnProgress{}, persistence.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return VoiceTurnProgress{}, err
	}

	result, err := a.repository.AdvanceContextVoiceTurn(
		ctx,
		actor,
		persistence.ConsumeTurnCommand{
			SessionID: sessionID,
			TurnID:    turnID,
			Payload:   []byte(voiceEffectiveTurnPayload),
		},
	)
	if err != nil {
		return VoiceTurnProgress{}, err
	}
	if result.SessionID != sessionID ||
		result.TurnID != turnID ||
		result.EffectiveTurns < 1 ||
		result.SessionVersion <= 1 ||
		result.TurnLimit < 1 ||
		result.TurnLimit > 14 ||
		result.TurnLimit < result.EffectiveTurns ||
		result.Completed != (result.EffectiveTurns == result.TurnLimit) {
		return VoiceTurnProgress{}, persistence.ErrConflict
	}
	return VoiceTurnProgress{
		EffectiveTurns:   result.EffectiveTurns,
		SessionVersion:   result.SessionVersion,
		TurnLimit:        result.TurnLimit,
		SessionCompleted: result.Completed,
	}, nil
}

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
