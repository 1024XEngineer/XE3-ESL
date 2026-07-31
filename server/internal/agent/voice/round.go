package voice

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"

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

type VoiceTextConversationPort interface {
	SubmitTextAnswer(
		context.Context,
		requestcontext.Actor,
		string,
		conversation.SubmitTextAnswerCommand,
	) (conversation.TranscriptionCandidate, error)
	ConfirmText(
		context.Context,
		requestcontext.Actor,
		conversation.ConfirmVoiceTurnCommand,
	) (conversation.ConfirmedVoiceTurn, error)
}

// VoicePracticePort is the Agent-owned application view of Practice.
// Implementations remain authoritative for Actor-participant resolution and
// the frozen per-Session effective-turn state machine.
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
	RequiresSessionReview(
		context.Context,
		requestcontext.Actor,
		string,
	) (bool, error)
}

type VoiceTurnProgress struct {
	EffectiveTurns   int
	SessionVersion   int
	TurnLimit        int
	SessionCompleted bool
}

// VoiceReviewPort is the Agent-owned application view of Review. Implementations
// must ensure one formal Review per Actor/session under concurrent retries.
type VoiceReviewPort interface {
	EnsureSessionReview(
		context.Context,
		requestcontext.Actor,
		VoiceReviewSource,
	) (VoiceReviewCheckpoint, error)
}

type VoiceReviewCheckpoint struct {
	ID           string
	SessionID    string
	SourceTurnID string
}

type VoiceReviewSource struct {
	TurnID    string
	SessionID string
}

// VoiceCompletionEvaluationPort is the server-owned completion boundary for
// asynchronous Evaluation workflows. Implementations must be idempotent by
// completed Session and must return failures instead of silently skipping an
// applicable Evaluation.
type VoiceCompletionEvaluationPort interface {
	EnsureCompletedSessionEvaluation(
		context.Context,
		requestcontext.Actor,
		VoiceCompletionEvaluationSource,
	) error
}

type VoiceCompletionEvaluationSource struct {
	TurnID    string
	SessionID string
}

type VoiceTurnFeedbackReference struct {
	StatusURL  string
	Applicable bool
}

type VoiceTurnFeedbackPort interface {
	EnsureConversationTurn(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (VoiceTurnFeedbackReference, error)
}

// VoiceRoundOrchestrator owns the cross-module voice-round saga. It never
// reaches into a module Repository and relies on stable Turn and Session IDs
// for idempotent recovery after any completed step.
type VoiceRoundOrchestrator struct {
	conversations   VoiceConversationPort
	practice        VoicePracticePort
	reviews         VoiceReviewPort
	completions     VoiceCompletionEvaluationPort
	feedback        VoiceTurnFeedbackPort
	completionTasks sync.WaitGroup
}

func NewVoiceRoundOrchestrator(
	conversations VoiceConversationPort,
	practice VoicePracticePort,
	reviews VoiceReviewPort,
	completions VoiceCompletionEvaluationPort,
	feedbackPorts ...VoiceTurnFeedbackPort,
) (*VoiceRoundOrchestrator, error) {
	if conversations == nil || practice == nil || reviews == nil ||
		completions == nil || len(feedbackPorts) > 1 ||
		(len(feedbackPorts) == 1 && feedbackPorts[0] == nil) {
		return nil, errors.New("agent: voice round dependency is required")
	}
	orchestrator := &VoiceRoundOrchestrator{
		conversations: conversations,
		practice:      practice,
		reviews:       reviews,
		completions:   completions,
	}
	if len(feedbackPorts) == 1 {
		orchestrator.feedback = feedbackPorts[0]
	}
	return orchestrator, nil
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
	candidate, err := orchestrator.conversations.GetTranscriptionCandidate(
		ctx,
		actor,
		command.CandidateID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	turn, err = orchestrator.finishTurn(ctx, actor, candidate, turn)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	return orchestrator.attachTurnFeedback(ctx, actor, turn)
}

func (orchestrator *VoiceRoundOrchestrator) attachTurnFeedback(
	ctx context.Context,
	actor requestcontext.Actor,
	turn conversation.ConfirmedVoiceTurn,
) (conversation.ConfirmedVoiceTurn, error) {
	if orchestrator.feedback == nil {
		return turn, nil
	}
	reference, err := orchestrator.feedback.EnsureConversationTurn(
		ctx,
		actor,
		turn.SessionID,
		turn.ID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	if !reference.Applicable {
		return turn, nil
	}
	if strings.TrimSpace(reference.StatusURL) == "" {
		return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
	}
	turn.SpeechFeedbackStatusURL = reference.StatusURL
	return turn, nil
}

func (orchestrator *VoiceRoundOrchestrator) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.SubmitTextAnswerCommand,
) (conversation.ConfirmedVoiceTurn, error) {
	if err := validateVoiceActor(ctx, actor); err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	textConversations, ok := orchestrator.conversations.(VoiceTextConversationPort)
	if !ok {
		return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
	}
	participantID, err := orchestrator.practice.ResolveActorParticipant(
		ctx,
		actor,
		command.SessionID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	if strings.TrimSpace(participantID) == "" {
		return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
	}
	candidate, err := textConversations.SubmitTextAnswer(
		ctx,
		actor,
		participantID,
		command,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	turn, err := textConversations.ConfirmText(
		ctx,
		actor,
		conversation.ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: command.IdempotencyKey,
		},
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	return orchestrator.finishTurn(ctx, actor, candidate, turn)
}

func (orchestrator *VoiceRoundOrchestrator) finishTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate conversation.TranscriptionCandidate,
	turn conversation.ConfirmedVoiceTurn,
) (conversation.ConfirmedVoiceTurn, error) {
	if !candidateMatchesTurn(candidate, turn) ||
		!validVoiceTurnCheckpoint(turn) {
		return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
	}
	var err error
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
		if turn.EffectiveTurns != progress.EffectiveTurns ||
			turn.SessionCompleted != progress.SessionCompleted ||
			!candidateMatchesTurn(candidate, turn) ||
			!validVoiceTurnCheckpoint(turn) {
			return conversation.ConfirmedVoiceTurn{}, ErrInvalidContext
		}
	}
	if !turn.SessionCompleted {
		return turn, nil
	}
	orchestrator.startSessionCompletion(ctx, actor, candidate, turn)
	return turn, nil
}

func (orchestrator *VoiceRoundOrchestrator) startSessionCompletion(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate conversation.TranscriptionCandidate,
	turn conversation.ConfirmedVoiceTurn,
) {
	completionContext := context.WithoutCancel(ctx)
	orchestrator.completionTasks.Add(1)
	go func() {
		defer orchestrator.completionTasks.Done()
		if err := orchestrator.completeSession(
			completionContext,
			actor,
			candidate,
			turn,
		); err != nil {
			slog.ErrorContext(
				completionContext,
				"voice session completion failed",
				"session_id",
				turn.SessionID,
				"turn_id",
				turn.ID,
				"error",
				err,
			)
		}
	}()
}

func (orchestrator *VoiceRoundOrchestrator) completeSession(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate conversation.TranscriptionCandidate,
	turn conversation.ConfirmedVoiceTurn,
) error {
	reviewRequired, err := orchestrator.practice.RequiresSessionReview(
		ctx,
		actor,
		turn.SessionID,
	)
	if err != nil {
		return err
	}
	if reviewRequired {
		sessionReview, reviewErr := orchestrator.reviews.EnsureSessionReview(
			ctx,
			actor,
			VoiceReviewSource{
				TurnID:    turn.ID,
				SessionID: turn.SessionID,
			},
		)
		if reviewErr != nil {
			return reviewErr
		}
		if sessionReview.ID == "" ||
			sessionReview.SessionID != turn.SessionID ||
			sessionReview.SourceTurnID != turn.ID {
			return ErrInvalidContext
		}
		turn, err = orchestrator.conversations.SaveTurnReview(
			ctx,
			actor,
			turn.ID,
			sessionReview.ID,
		)
		if err != nil {
			return err
		}
		if turn.ReviewID != sessionReview.ID ||
			!candidateMatchesTurn(candidate, turn) ||
			!validVoiceTurnCheckpoint(turn) {
			return ErrInvalidContext
		}
	}
	if err := orchestrator.completions.EnsureCompletedSessionEvaluation(
		ctx,
		actor,
		VoiceCompletionEvaluationSource{
			TurnID:    turn.ID,
			SessionID: turn.SessionID,
		},
	); err != nil {
		return err
	}
	return nil
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
		turn.ID != "" &&
		candidate.SessionID == turn.SessionID &&
		candidate.QuestionID == turn.QuestionID &&
		candidate.QuestionSpeakerID == turn.QuestionSpeakerID &&
		slices.Equal(
			candidate.AddresseeParticipantIDs,
			turn.AddresseeParticipantIDs,
		) &&
		candidate.RespondentParticipantID == turn.RespondentParticipantID &&
		candidate.ID == turn.CandidateID &&
		candidate.TranscriptID == turn.TranscriptID &&
		candidate.EvidenceVersion == turn.EvidenceVersion &&
		candidate.Transcript == turn.AnswerText
}

func validVoiceTurnProgress(progress VoiceTurnProgress) bool {
	return progress.EffectiveTurns >= 1 &&
		progress.EffectiveTurns <= 14 &&
		progress.SessionVersion > 1 &&
		progress.TurnLimit >= 1 &&
		progress.TurnLimit <= 14 &&
		progress.EffectiveTurns <= progress.TurnLimit &&
		progress.SessionCompleted ==
			(progress.EffectiveTurns == progress.TurnLimit)
}

func validVoiceTurnCheckpoint(turn conversation.ConfirmedVoiceTurn) bool {
	if turn.ID == "" || turn.SessionID == "" || turn.QuestionID == "" ||
		turn.QuestionSpeakerID == "" ||
		len(turn.AddresseeParticipantIDs) == 0 ||
		turn.RespondentParticipantID == "" ||
		turn.CandidateID == "" ||
		turn.TranscriptID == "" ||
		turn.EvidenceVersion < 1 ||
		strings.TrimSpace(turn.AnswerText) == "" ||
		turn.EffectiveTurns < 0 ||
		turn.EffectiveTurns > 14 {
		return false
	}
	if turn.EffectiveTurns == 0 {
		return !turn.SessionCompleted && turn.ReviewID == ""
	}
	return turn.ReviewID == "" || turn.SessionCompleted
}
