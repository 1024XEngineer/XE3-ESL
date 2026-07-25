package agent

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// VoiceConversationPort is the Agent-owned application view of Conversation.
// Its implementation owns only Conversation resources and local checkpoints.
type VoiceConversationPort interface {
	Transcribe(
		context.Context,
		requestcontext.Actor,
		string,
		conversation.TranscribeVoiceCommand,
	) (conversation.TranscriptionCandidate, error)
	GetTranscriptionCandidate(
		context.Context,
		requestcontext.Actor,
		string,
	) (conversation.TranscriptionCandidate, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		conversation.ConfirmVoiceTurnCommand,
	) (conversation.ConfirmedVoiceTurn, error)
	SaveTurnProgress(
		context.Context,
		requestcontext.Actor,
		string,
		conversation.VoiceTurnProgress,
	) (conversation.ConfirmedVoiceTurn, error)
	SaveTurnReview(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (conversation.ConfirmedVoiceTurn, error)
	SynthesizeQuestion(
		context.Context,
		string,
	) (conversation.QuestionSpeech, error)
}

// VoicePracticePort is the Agent-owned application view of Practice.
// Implementations remain authoritative for Actor-participant resolution and
// the effective three-turn state machine.
type VoicePracticePort interface {
	ResolveActorParticipant(
		context.Context,
		requestcontext.Actor,
		string,
	) (string, error)
	ApplyEffectiveTurn(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (VoiceTurnProgress, error)
}

type VoiceTurnProgress struct {
	EffectiveTurns   int
	SessionCompleted bool
}

// VoiceReviewPort is the Agent-owned application view of Review. Implementations
// must ensure one formal Review per Actor/session under concurrent retries.
type VoiceReviewPort interface {
	EnsureSessionReview(
		context.Context,
		requestcontext.Actor,
		VoiceReviewSource,
	) (VoiceSessionReview, error)
}

type VoiceSessionReview struct {
	ID        string
	SessionID string
	TurnID    string
}

type VoiceReviewSource struct {
	TurnID                  string
	SessionID               string
	QuestionID              string
	QuestionSpeakerID       string
	AddresseeParticipantIDs []string
	RespondentParticipantID string
	TranscriptID            string
	TranscriptVersion       string
	Transcript              string
	TranscriptionProvider   string
	TranscriptionModel      string
	TranscriptionRequestID  string
}

// VoiceRoundOrchestrator owns the cross-module voice-round saga. It never
// reaches into a module Repository and relies on stable Turn and Session IDs
// for idempotent recovery after any completed step.
type VoiceRoundOrchestrator struct {
	conversations VoiceConversationPort
	practice      VoicePracticePort
	reviews       VoiceReviewPort
}

func NewVoiceRoundOrchestrator(
	conversations VoiceConversationPort,
	practice VoicePracticePort,
	reviews VoiceReviewPort,
) (*VoiceRoundOrchestrator, error) {
	if conversations == nil || practice == nil || reviews == nil {
		return nil, errors.New("agent: voice round dependency is required")
	}
	return &VoiceRoundOrchestrator{
		conversations: conversations,
		practice:      practice,
		reviews:       reviews,
	}, nil
}

func (orchestrator *VoiceRoundOrchestrator) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.TranscribeVoiceCommand,
) (conversation.TranscriptionCandidate, error) {
	if err := validateVoiceActor(ctx, actor); err != nil {
		return conversation.TranscriptionCandidate{}, err
	}
	participantID, err := orchestrator.practice.ResolveActorParticipant(
		ctx,
		actor,
		command.SessionID,
	)
	if err != nil {
		return conversation.TranscriptionCandidate{}, err
	}
	if strings.TrimSpace(participantID) == "" {
		return conversation.TranscriptionCandidate{}, ErrInvalidContext
	}
	return orchestrator.conversations.Transcribe(
		ctx,
		actor,
		participantID,
		command,
	)
}

func (orchestrator *VoiceRoundOrchestrator) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.ConfirmVoiceTurnCommand,
) (conversation.ConfirmedVoiceTurn, error) {
	if err := validateVoiceActor(ctx, actor); err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	turn, err := orchestrator.conversations.Confirm(ctx, actor, command)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	if turn.EffectiveTurns == 0 {
		progress, applyErr := orchestrator.practice.ApplyEffectiveTurn(
			ctx,
			actor,
			turn.SessionID,
			turn.ID,
		)
		if applyErr != nil {
			return conversation.ConfirmedVoiceTurn{}, applyErr
		}
		if !validVoiceTurnProgress(progress) {
			return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
		}
		turn, err = orchestrator.conversations.SaveTurnProgress(
			ctx,
			actor,
			turn.ID,
			conversation.VoiceTurnProgress{
				EffectiveTurns:   progress.EffectiveTurns,
				SessionCompleted: progress.SessionCompleted,
			},
		)
		if err != nil {
			return conversation.ConfirmedVoiceTurn{}, err
		}
	}
	if !turn.SessionCompleted || turn.ReviewID != "" {
		return turn, nil
	}

	candidate, err := orchestrator.conversations.GetTranscriptionCandidate(
		ctx,
		actor,
		command.CandidateID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	if !candidateMatchesTurn(candidate, turn) {
		return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
	}
	sessionReview, err := orchestrator.reviews.EnsureSessionReview(
		ctx,
		actor,
		VoiceReviewSource{
			TurnID:            turn.ID,
			SessionID:         turn.SessionID,
			QuestionID:        turn.QuestionID,
			QuestionSpeakerID: turn.QuestionSpeakerID,
			AddresseeParticipantIDs: slices.Clone(
				turn.AddresseeParticipantIDs,
			),
			RespondentParticipantID: turn.RespondentParticipantID,
			TranscriptID:            turn.TranscriptID,
			TranscriptVersion:       turn.TranscriptVersion,
			Transcript:              turn.AnswerText,
			TranscriptionProvider:   candidate.Provider,
			TranscriptionModel:      candidate.Model,
			TranscriptionRequestID:  candidate.ProviderRequestID,
		},
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	if sessionReview.ID == "" ||
		sessionReview.SessionID != turn.SessionID ||
		sessionReview.TurnID != turn.ID {
		return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
	}
	return orchestrator.conversations.SaveTurnReview(
		ctx,
		actor,
		turn.ID,
		sessionReview.ID,
	)
}

func (orchestrator *VoiceRoundOrchestrator) SynthesizeQuestion(
	ctx context.Context,
	text string,
) (conversation.QuestionSpeech, error) {
	if ctx == nil {
		return conversation.QuestionSpeech{}, ErrInvalidRequest
	}
	return orchestrator.conversations.SynthesizeQuestion(ctx, text)
}

func validateVoiceActor(
	ctx context.Context,
	actor requestcontext.Actor,
) error {
	if ctx == nil || !actor.Valid() {
		return ErrInvalidRequest
	}
	return ctx.Err()
}

func candidateMatchesTurn(
	candidate conversation.TranscriptionCandidate,
	turn conversation.ConfirmedVoiceTurn,
) bool {
	return candidate.ID != "" &&
		candidate.SessionID == turn.SessionID &&
		candidate.QuestionID == turn.QuestionID &&
		candidate.QuestionSpeakerID == turn.QuestionSpeakerID &&
		slices.Equal(
			candidate.AddresseeParticipantIDs,
			turn.AddresseeParticipantIDs,
		) &&
		candidate.RespondentParticipantID == turn.RespondentParticipantID &&
		candidate.TranscriptID == turn.TranscriptID &&
		candidate.TranscriptVersion == turn.TranscriptVersion &&
		candidate.Transcript == turn.AnswerText
}

func validVoiceTurnProgress(progress VoiceTurnProgress) bool {
	return progress.EffectiveTurns >= 1 &&
		progress.EffectiveTurns <= 3 &&
		progress.SessionCompleted == (progress.EffectiveTurns == 3)
}
