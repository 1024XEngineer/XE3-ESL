package interaction

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSameQuestionRetryTranscribeUsesAuthorizedDraftContext(t *testing.T) {
	t.Parallel()

	createdAt := time.Unix(1_700_000_000, 0).UTC()
	draft := RetryTurnDraft{
		TurnID:                  "retry-turn-1",
		ClientRequestID:         "11111111-1111-4111-8111-111111111111",
		PracticeSessionID:       "practice-session-1",
		OriginalTurnID:          "original-turn-1",
		QuestionID:              "question-1",
		RespondentParticipantID: "participant-learner",
		Status:                  RetryTurnAnswering,
		CreatedAt:               createdAt,
		UpdatedAt:               createdAt,
	}
	turns := &retryTurnPortStub{drafts: []RetryTurnDraft{draft}}
	rounds := &retryRoundPortStub{
		candidate: TranscriptionCandidate{
			ID:                      "candidate-1",
			SessionID:               draft.PracticeSessionID,
			QuestionID:              draft.QuestionID,
			RespondentParticipantID: draft.RespondentParticipantID,
			TranscriptID:            "transcript-1",
			EvidenceVersion:         1,
			Transcript:              "I would like to try again.",
			CreatedAt:               createdAt.Add(time.Second),
		},
	}
	application, err := NewSameQuestionRetryApplication(
		turns,
		rounds,
	)
	if err != nil {
		t.Fatal(err)
	}

	audio := []byte("RIFF retry wav")
	result, err := application.Transcribe(
		context.Background(),
		agentVoiceActor("a"),
		RetryTranscriptionCommand{
			RetryTurnID:    draft.TurnID,
			IdempotencyKey: "client-transcribe-key",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(audio),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetryTurnID != draft.TurnID ||
		result.Candidate.ID != rounds.candidate.ID {
		t.Fatalf("unexpected result: %#v", result)
	}
	command := rounds.transcribeCommand
	if command.SessionID != draft.PracticeSessionID ||
		command.QuestionID != draft.QuestionID ||
		command.ContentType != "audio/wav" {
		t.Fatalf("transcription command = %#v", command)
	}
	if rounds.participantID != draft.RespondentParticipantID {
		t.Fatalf(
			"participant = %q",
			rounds.participantID,
		)
	}
	if command.IdempotencyKey == "client-transcribe-key" ||
		command.IdempotencyKey != retryTranscriptionIdempotencyKey(
			draft.TurnID,
			"client-transcribe-key",
		) {
		t.Fatalf("idempotency key was not retry-scoped: %q", command.IdempotencyKey)
	}
	if !bytes.Equal(rounds.audio, audio) {
		t.Fatalf("audio = %q", rounds.audio)
	}
}

func TestSameQuestionRetryConfirmDoesNotAdvancePractice(t *testing.T) {
	t.Parallel()

	createdAt := time.Unix(1_700_000_000, 0).UTC()
	confirmedAt := createdAt.Add(2 * time.Second)
	before := RetryTurnDraft{
		TurnID:                  "retry-turn-1",
		ClientRequestID:         "11111111-1111-4111-8111-111111111111",
		PracticeSessionID:       "practice-session-1",
		OriginalTurnID:          "original-turn-1",
		QuestionID:              "question-1",
		RespondentParticipantID: "participant-learner",
		Status:                  RetryTurnReady,
		CandidateID:             "candidate-1",
		CreatedAt:               createdAt,
		UpdatedAt:               createdAt,
	}
	after := before
	after.Status = RetryTurnConfirmed
	after.CandidateID = "candidate-1"
	after.UpdatedAt = confirmedAt
	after.ConfirmedAt = &confirmedAt
	candidate := TranscriptionCandidate{
		ID:                      after.CandidateID,
		SessionID:               after.PracticeSessionID,
		QuestionID:              after.QuestionID,
		RespondentParticipantID: "participant-learner",
		TranscriptID:            "transcript-1",
		EvidenceVersion:         1,
		Transcript:              "This retry is clearer.",
		CreatedAt:               createdAt.Add(time.Second),
	}
	turn := practice.Turn{
		ID:                      after.TurnID,
		SessionID:               after.PracticeSessionID,
		QuestionID:              after.QuestionID,
		RespondentParticipantID: candidate.RespondentParticipantID,
		CandidateID:             candidate.ID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		AnswerText:              candidate.Transcript,
		Kind:                    practice.TurnKindRetry,
		ClientRequestID:         after.ClientRequestID,
		OriginalTurnID:          after.OriginalTurnID,
		CountsTowardTurnLimit:   false,
		EffectiveTurns:          3,
		SessionCompleted:        true,
	}
	turns := &retryTurnPortStub{
		drafts: []RetryTurnDraft{before, after},
	}
	rounds := &retryRoundPortStub{
		candidate: candidate,
		turn:      turn,
	}
	application, err := NewSameQuestionRetryApplication(
		turns,
		rounds,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := application.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		ConfirmRetryTranscriptionCommand{
			RetryTurnID:    before.TurnID,
			CandidateID:    candidate.ID,
			IdempotencyKey: "client-confirm-key",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Turn.ID != before.TurnID ||
		result.OriginalTurnID != before.OriginalTurnID ||
		!result.ConfirmedAt.Equal(confirmedAt) {
		t.Fatalf("unexpected confirmed retry: %#v", result)
	}
	command := rounds.confirmCommand
	if command.RetryTurnID != before.TurnID ||
		command.CandidateID != candidate.ID {
		t.Fatalf("confirmation command = %#v", command)
	}
	if command.IdempotencyKey == "client-confirm-key" ||
		command.IdempotencyKey != retryConfirmationIdempotencyKey(
			before.TurnID,
			"client-confirm-key",
		) {
		t.Fatalf("idempotency key was not retry-scoped: %q", command.IdempotencyKey)
	}
}

type retryTurnPortStub struct {
	drafts []RetryTurnDraft
	calls  int
}

func (port *retryTurnPortStub) Get(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
) (RetryTurnDraft, error) {
	index := port.calls
	port.calls++
	if index >= len(port.drafts) {
		index = len(port.drafts) - 1
	}
	return port.drafts[index], nil
}

type retryRoundPortStub struct {
	candidate         TranscriptionCandidate
	turn              practice.Turn
	participantID     string
	transcribeCommand TranscribeVoiceCommand
	confirmCommand    ConfirmVoiceTurnCommand
	audio             []byte
}

func (port *retryRoundPortStub) Transcribe(
	_ context.Context,
	_ requestcontext.Actor,
	participantID string,
	command TranscribeVoiceCommand,
) (TranscriptionCandidate, error) {
	port.participantID = participantID
	port.transcribeCommand = command
	var err error
	port.audio, err = io.ReadAll(command.Audio)
	return port.candidate, err
}

func (port *retryRoundPortStub) GetTranscriptionCandidate(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
) (TranscriptionCandidate, error) {
	return port.candidate, nil
}

func (port *retryRoundPortStub) Confirm(
	_ context.Context,
	_ requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
) (practice.Turn, error) {
	port.confirmCommand = command
	return port.turn, nil
}

func (port *retryRoundPortStub) SynthesizeQuestion(
	context.Context,
	string,
) (QuestionSpeech, error) {
	return QuestionSpeech{}, nil
}
