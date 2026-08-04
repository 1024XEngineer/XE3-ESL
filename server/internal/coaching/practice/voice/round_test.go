package voice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestRoundConfirmReturnsAtomicPracticeTurn(t *testing.T) {
	candidate := roundCandidate()
	turn := roundTurn(candidate)
	conversations := &roundConversation{candidate: candidate, turn: turn}
	orchestrator, err := NewRoundOrchestrator(
		conversations,
		roundPractice{},
		roundFeedback{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}

	got, err := orchestrator.Confirm(
		context.Background(),
		roundActor(),
		practiceinput.ConfirmVoiceTurnCommand{
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
		&roundConversation{candidate: candidate, turn: turn},
		roundPractice{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}
	_, err = orchestrator.Confirm(
		context.Background(),
		roundActor(),
		practiceinput.ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: "confirm-1",
		},
	)
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Confirm error = %v", err)
	}
}

type roundConversation struct {
	candidate practiceinput.TranscriptionCandidate
	turn      practice.Turn
}

func (c *roundConversation) Transcribe(
	context.Context,
	requestcontext.Actor,
	string,
	practiceinput.TranscribeVoiceCommand,
) (practiceinput.TranscriptionCandidate, error) {
	return c.candidate, nil
}

func (c *roundConversation) GetTranscriptionCandidate(
	context.Context,
	requestcontext.Actor,
	string,
) (practiceinput.TranscriptionCandidate, error) {
	return c.candidate, nil
}

func (c *roundConversation) Confirm(
	context.Context,
	requestcontext.Actor,
	practiceinput.ConfirmVoiceTurnCommand,
) (practice.Turn, error) {
	return c.turn, nil
}

func (c *roundConversation) SynthesizeQuestion(
	context.Context,
	string,
) (practiceinput.QuestionSpeech, error) {
	return practiceinput.QuestionSpeech{}, nil
}

func (c *roundConversation) SubmitTextAnswer(
	context.Context,
	requestcontext.Actor,
	string,
	practiceinput.SubmitTextAnswerCommand,
) (practiceinput.TranscriptionCandidate, error) {
	return c.candidate, nil
}

func (c *roundConversation) ConfirmText(
	context.Context,
	requestcontext.Actor,
	practiceinput.ConfirmVoiceTurnCommand,
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

func (roundFeedback) EnsureConversationTurn(
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

func roundCandidate() practiceinput.TranscriptionCandidate {
	return practiceinput.TranscriptionCandidate{
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

func roundTurn(candidate practiceinput.TranscriptionCandidate) practice.Turn {
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
