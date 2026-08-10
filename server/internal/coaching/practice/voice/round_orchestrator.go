package voice

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// RoundPort is the boundary to transcription, confirmation, and speech for one
// voice-practice round.
type RoundPort interface {
	Transcribe(
		context.Context,
		requestcontext.Actor,
		string,
		TranscribeVoiceCommand,
	) (TranscriptionCandidate, error)
	TranscribeStream(
		context.Context,
		requestcontext.Actor,
		string,
		TranscribeVoiceStreamCommand,
		TranscriptionObserver,
	) (TranscriptionCandidate, error)
	GetTranscriptionCandidate(
		context.Context,
		requestcontext.Actor,
		string,
	) (TranscriptionCandidate, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		ConfirmVoiceTurnCommand,
	) (practice.Turn, error)
	SynthesizeQuestion(
		context.Context,
		string,
	) (QuestionSpeech, error)
	SubmitTextAnswer(
		context.Context,
		requestcontext.Actor,
		string,
		SubmitTextAnswerCommand,
	) (TranscriptionCandidate, error)
	ConfirmText(
		context.Context,
		requestcontext.Actor,
		ConfirmVoiceTurnCommand,
	) (practice.Turn, error)
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
}

type TurnFeedbackReference struct {
	StatusURL  string
	Applicable bool
}

type TurnFeedbackPort interface {
	EnsureTurn(
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
	rounds   RoundPort
	practice PracticePort
	feedback TurnFeedbackPort
}

func NewRoundOrchestrator(
	rounds RoundPort,
	practice PracticePort,
	feedbackPorts ...TurnFeedbackPort,
) (*RoundOrchestrator, error) {
	if rounds == nil || practice == nil || len(feedbackPorts) > 1 ||
		(len(feedbackPorts) == 1 && feedbackPorts[0] == nil) {
		return nil, errors.New("practice voice: round dependency is required")
	}
	orchestrator := &RoundOrchestrator{
		rounds:   rounds,
		practice: practice,
	}
	if len(feedbackPorts) == 1 {
		orchestrator.feedback = feedbackPorts[0]
	}
	return orchestrator, nil
}

func (orchestrator *RoundOrchestrator) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command TranscribeVoiceCommand,
) (TranscriptionCandidate, error) {
	if err := validateVoiceActor(ctx, actor); err != nil {
		return TranscriptionCandidate{}, err
	}
	participantID, err := orchestrator.practice.ResolveActorParticipant(
		ctx,
		actor,
		command.SessionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if strings.TrimSpace(participantID) == "" {
		return TranscriptionCandidate{}, ErrInvalidContext
	}
	return orchestrator.rounds.Transcribe(
		ctx,
		actor,
		participantID,
		command,
	)
}

func (orchestrator *RoundOrchestrator) TranscribeStream(
	ctx context.Context,
	actor requestcontext.Actor,
	command TranscribeVoiceStreamCommand,
	observer TranscriptionObserver,
) (TranscriptionCandidate, error) {
	if err := validateVoiceActor(ctx, actor); err != nil || observer == nil {
		return TranscriptionCandidate{}, ErrInvalidRequest
	}
	participantID, err := orchestrator.practice.ResolveActorParticipant(
		ctx,
		actor,
		command.SessionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if strings.TrimSpace(participantID) == "" {
		return TranscriptionCandidate{}, ErrInvalidContext
	}
	return orchestrator.rounds.TranscribeStream(
		ctx,
		actor,
		participantID,
		command,
		observer,
	)
}

func (orchestrator *RoundOrchestrator) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
) (practice.Turn, error) {
	if err := validateVoiceActor(ctx, actor); err != nil {
		return practice.Turn{}, err
	}
	turn, err := orchestrator.rounds.Confirm(ctx, actor, command)
	if err != nil {
		return practice.Turn{}, err
	}
	candidate, err := orchestrator.rounds.GetTranscriptionCandidate(
		ctx,
		actor,
		command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	turn, err = orchestrator.finishTurn(ctx, actor, candidate, turn)
	if err != nil {
		return practice.Turn{}, err
	}
	return orchestrator.attachTurnFeedback(ctx, actor, turn)
}

func (orchestrator *RoundOrchestrator) attachTurnFeedback(
	ctx context.Context,
	actor requestcontext.Actor,
	turn practice.Turn,
) (practice.Turn, error) {
	if orchestrator.feedback == nil {
		return turn, nil
	}
	reference, err := orchestrator.feedback.EnsureTurn(
		ctx,
		actor,
		turn.SessionID,
		turn.ID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	if !reference.Applicable {
		return turn, nil
	}
	if strings.TrimSpace(reference.StatusURL) == "" {
		return practice.Turn{}, ErrInvalidContext
	}
	turn.SpeechFeedbackStatusURL = reference.StatusURL
	return turn, nil
}

func (orchestrator *RoundOrchestrator) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	command SubmitTextAnswerCommand,
) (practice.Turn, error) {
	if err := validateVoiceActor(ctx, actor); err != nil {
		return practice.Turn{}, err
	}
	participantID, err := orchestrator.practice.ResolveActorParticipant(
		ctx,
		actor,
		command.SessionID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	if strings.TrimSpace(participantID) == "" {
		return practice.Turn{}, ErrInvalidContext
	}
	candidate, err := orchestrator.rounds.SubmitTextAnswer(
		ctx,
		actor,
		participantID,
		command,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	turn, err := orchestrator.rounds.ConfirmText(
		ctx,
		actor,
		ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: command.IdempotencyKey,
		},
	)
	if err != nil {
		return practice.Turn{}, err
	}
	return orchestrator.finishTurn(ctx, actor, candidate, turn)
}

func (orchestrator *RoundOrchestrator) finishTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate TranscriptionCandidate,
	turn practice.Turn,
) (practice.Turn, error) {
	if !candidateMatchesTurn(candidate, turn) ||
		!validVoiceTurnCheckpoint(turn) {
		return practice.Turn{}, ErrInvalidContext
	}
	if turn.EffectiveTurns == 0 &&
		(turn.AnswerAssessment == nil || turn.CountsTowardTurnLimit ||
			turn.AdvanceAuthorized) {
		return practice.Turn{}, ErrInvalidContext
	}
	return turn, nil
}

func (orchestrator *RoundOrchestrator) SynthesizeQuestion(
	ctx context.Context,
	text string,
) (QuestionSpeech, error) {
	if ctx == nil {
		return QuestionSpeech{}, ErrInvalidRequest
	}
	return orchestrator.rounds.SynthesizeQuestion(ctx, text)
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
	candidate TranscriptionCandidate,
	turn practice.Turn,
) bool {
	return candidate.ID != "" &&
		turn.ID != "" &&
		candidate.SessionID == turn.SessionID &&
		candidate.QuestionID == turn.QuestionID &&
		candidate.QuestionSpeakerID == turn.SpeakerParticipantID &&
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

func validVoiceTurnCheckpoint(turn practice.Turn) bool {
	if turn.ID == "" || turn.SessionID == "" || turn.QuestionID == "" ||
		turn.SpeakerParticipantID == "" ||
		len(turn.AddresseeParticipantIDs) == 0 ||
		turn.RespondentParticipantID == "" ||
		turn.CandidateID == "" ||
		turn.TranscriptID == "" ||
		turn.EvidenceVersion < 1 ||
		strings.TrimSpace(turn.AnswerText) == "" ||
		turn.EffectiveTurns < 0 {
		return false
	}
	return turn.EffectiveTurns > 0 ||
		(turn.AnswerAssessment != nil &&
			!turn.CountsTowardTurnLimit && !turn.AdvanceAuthorized)
}
