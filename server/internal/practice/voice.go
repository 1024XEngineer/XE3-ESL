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
	GetSession(
		context.Context,
		persistence.Actor,
		string,
	) (persistence.Session, error)
	ConsumeTurn(
		context.Context,
		persistence.Actor,
		persistence.ConsumeTurnCommand,
	) (persistence.TurnResult, error)
}

// VoiceTurnProgress is the minimum Practice decision consumed by the Agent
// application orchestrator after Conversation confirms an effective Turn.
type VoiceTurnProgress struct {
	EffectiveTurns   int
	SessionCompleted bool
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

	session, err := a.repository.GetSession(ctx, actor, sessionID)
	if err != nil {
		return "", err
	}

	participantID := ""
	for _, participant := range session.Snapshot.Participants {
		if participant.SubjectRef.Namespace != a.actorSubjectNamespace ||
			participant.SubjectRef.SubjectID != actor.UserID {
			continue
		}
		if participantID != "" {
			return "", persistence.ErrConflict
		}
		participantID = participant.ParticipantID
	}
	if participantID == "" {
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

	result, err := a.repository.ConsumeTurn(
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
	return VoiceTurnProgress{
		EffectiveTurns:   result.EffectiveTurns,
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
