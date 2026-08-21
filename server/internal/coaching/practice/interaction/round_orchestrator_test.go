package interaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestRoundConfirmReturnsAtomicPracticeTurn(t *testing.T) {
	candidate := roundCandidate()
	turn := roundTurn(candidate)
	rounds := &roundVoice{candidate: candidate, turn: turn}
	orchestrator, err := NewRoundOrchestrator(
		rounds,
		roundPractice{},
		roundFeedback{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}

	got, err := orchestrator.Confirm(
		context.Background(),
		roundActor(),
		ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: "confirm-1",
		},
	)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if got.ID != turn.ID || got.EffectiveTurns != 1 ||
		got.SpeechFeedbackStatusURL != "/v1/feedback/turn-1" {
		t.Fatalf("Turn = %#v", got)
	}
}

func TestRoundRejectsTurnWithoutAtomicProgress(t *testing.T) {
	candidate := roundCandidate()
	turn := roundTurn(candidate)
	turn.EffectiveTurns = 0
	orchestrator, err := NewRoundOrchestrator(
		&roundVoice{candidate: candidate, turn: turn},
		roundPractice{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}
	_, err = orchestrator.Confirm(
		context.Background(),
		roundActor(),
		ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: "confirm-1",
		},
	)
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Confirm error = %v", err)
	}
}

type roundVoice struct {
	candidate TranscriptionCandidate
	turn      practice.Turn
}

func (c *roundVoice) Transcribe(
	context.Context,
	requestcontext.Actor,
	string,
	TranscribeVoiceCommand,
) (TranscriptionCandidate, error) {
	return c.candidate, nil
}

func (c *roundVoice) TranscribeStream(
	context.Context,
	requestcontext.Actor,
	string,
	TranscribeVoiceStreamCommand,
	TranscriptionObserver,
) (TranscriptionCandidate, error) {
	return c.candidate, nil
}

func (c *roundVoice) GetTranscriptionCandidate(
	context.Context,
	requestcontext.Actor,
	string,
) (TranscriptionCandidate, error) {
	return c.candidate, nil
}

func (c *roundVoice) Confirm(
	context.Context,
	requestcontext.Actor,
	ConfirmVoiceTurnCommand,
) (practice.Turn, error) {
	return c.turn, nil
}

func (c *roundVoice) SynthesizeQuestion(
	context.Context,
	string,
) (QuestionSpeech, error) {
	return QuestionSpeech{}, nil
}

func (c *roundVoice) SubmitTextAnswer(
	context.Context,
	requestcontext.Actor,
	string,
	SubmitTextAnswerCommand,
) (TranscriptionCandidate, error) {
	return c.candidate, nil
}

func (c *roundVoice) ConfirmText(
	context.Context,
	requestcontext.Actor,
	ConfirmVoiceTurnCommand,
) (practice.Turn, error) {
	return c.turn, nil
}

type roundPractice struct{}

func (roundPractice) ResolveActorParticipant(
	context.Context,
	requestcontext.Actor,
	string,
) (string, error) {
	return "learner-1", nil
}

type roundFeedback struct{}

func (roundFeedback) EnsureTurn(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	turnID string,
) (TurnFeedbackReference, error) {
	return TurnFeedbackReference{
		StatusURL:  "/v1/feedback/" + turnID,
		Applicable: true,
	}, nil
}

func roundActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "auth-1"}
}

func agentVoiceActor(userIDs ...string) requestcontext.Actor {
	actor := roundActor()
	if len(userIDs) == 1 {
		actor.UserID = userIDs[0]
	}
	return actor
}

func roundCandidate() TranscriptionCandidate {
	return TranscriptionCandidate{
		ID:                      "candidate-1",
		SessionID:               "session-1",
		QuestionID:              "question-1",
		QuestionSpeakerID:       "facilitator-1",
		AddresseeParticipantIDs: []string{"learner-1"},
		RespondentParticipantID: "learner-1",
		TranscriptID:            "transcript-1",
		EvidenceVersion:         1,
		Transcript:              "My answer",
	}
}

func roundTurn(candidate TranscriptionCandidate) practice.Turn {
	return practice.Turn{
		ID:                      "turn-1",
		SessionID:               candidate.SessionID,
		QuestionID:              candidate.QuestionID,
		SpeakerParticipantID:    candidate.QuestionSpeakerID,
		AddresseeParticipantIDs: candidate.AddresseeParticipantIDs,
		RespondentParticipantID: candidate.RespondentParticipantID,
		Sequence:                1,
		InteractionMode:         "PUSH_TO_TALK",
		AnswerText:              candidate.Transcript,
		CandidateID:             candidate.ID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		Kind:                    practice.TurnKindEffective,
		CountsTowardTurnLimit:   true,
		EffectiveTurns:          1,
		ConfirmedAt:             time.Now(),
		CreatedAt:               time.Now(),
	}
}
