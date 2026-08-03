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

// ConversationPort is the voice-practice boundary to Conversation. Its
// implementation owns only Conversation resources and local checkpoints.
type ConversationPort interface {
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

// PracticePort exposes authoritative Practice progression. Implementations
// remain responsible for Actor-participant resolution and
// the frozen per-Session effective-turn state machine.
type PracticePort interface {
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
		bool,
	) (TurnProgress, error)
	RequiresSessionReview(
		context.Context,
		requestcontext.Actor,
		string,
	) (bool, error)
}

// ReviewPort is the voice-practice boundary to Review. Implementations must
// ensure one formal Review per Actor/session under concurrent retries.
type ReviewPort interface {
	EnsureSessionReview(
		context.Context,
		requestcontext.Actor,
		ReviewSource,
	) (ReviewCheckpoint, error)
}

type ReviewCheckpoint struct {
	ID           string
	SessionID    string
	SourceTurnID string
}

type ReviewSource struct {
	TurnID    string
	SessionID string
}

// CompletionEvaluationPort is the server-owned completion boundary for
// asynchronous Evaluation workflows. Implementations must be idempotent by
// completed Session and must return failures instead of silently skipping an
// applicable Evaluation.
type CompletionEvaluationPort interface {
	EnsureCompletedSessionEvaluation(
		context.Context,
		requestcontext.Actor,
		CompletionEvaluationSource,
	) error
}

type CompletionEvaluationSource struct {
	TurnID    string
	SessionID string
}

type TurnFeedbackReference struct {
	StatusURL  string
	Applicable bool
}

type TurnFeedbackPort interface {
	EnsureConversationTurn(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (TurnFeedbackReference, error)
}

// RoundOrchestrator owns the cross-module voice-round saga. It never
// reaches into a module Repository and relies on stable Turn and Session IDs
// for idempotent recovery after any completed step.
type RoundOrchestrator struct {
	conversations   ConversationPort
	practice        PracticePort
	reviews         ReviewPort
	completions     CompletionEvaluationPort
	feedback        TurnFeedbackPort
	completionTasks sync.WaitGroup
}

func NewRoundOrchestrator(
	conversations ConversationPort,
	practice PracticePort,
	reviews ReviewPort,
	completions CompletionEvaluationPort,
	feedbackPorts ...TurnFeedbackPort,
) (*RoundOrchestrator, error) {
	if conversations == nil || practice == nil || reviews == nil ||
		completions == nil || len(feedbackPorts) > 1 ||
		(len(feedbackPorts) == 1 && feedbackPorts[0] == nil) {
		return nil, errors.New("practice voice: round dependency is required")
	}
	orchestrator := &RoundOrchestrator{
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

func (orchestrator *RoundOrchestrator) Transcribe(
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

func (orchestrator *RoundOrchestrator) Confirm(
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

func (orchestrator *RoundOrchestrator) attachTurnFeedback(
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

func (orchestrator *RoundOrchestrator) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.SubmitTextAnswerCommand,
) (conversation.ConfirmedVoiceTurn, error) {
	if err := validateVoiceActor(ctx, actor); err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
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
	candidate, err := orchestrator.conversations.SubmitTextAnswer(
		ctx,
		actor,
		participantID,
		command,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, err
	}
	turn, err := orchestrator.conversations.ConfirmText(
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

func (orchestrator *RoundOrchestrator) finishTurn(
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
			turn.CountsTowardTurnLimit,
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

func (orchestrator *RoundOrchestrator) startSessionCompletion(
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

func (orchestrator *RoundOrchestrator) completeSession(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate conversation.TranscriptionCandidate,
	turn conversation.ConfirmedVoiceTurn,
) error {
	if err := orchestrator.completions.EnsureCompletedSessionEvaluation(
		ctx,
		actor,
		CompletionEvaluationSource{
			TurnID:    turn.ID,
			SessionID: turn.SessionID,
		},
	); err != nil {
		return err
	}
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
			ReviewSource{
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
	return nil
}

func (orchestrator *RoundOrchestrator) SynthesizeQuestion(
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

func validVoiceTurnProgress(progress TurnProgress) bool {
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
