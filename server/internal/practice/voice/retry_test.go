package voice

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSameQuestionRetryTranscribeUsesAuthorizedDraftContext(t *testing.T) {
	t.Parallel()

	createdAt := time.Unix(1_700_000_000, 0).UTC()
	draft := conversation.RetryTurnDraft{
		TurnID:            "retry-turn-1",
		RetryRequestID:    "11111111-1111-4111-8111-111111111111",
		PracticeSessionID: "practice-session-1",
		OriginalTurnID:    "original-turn-1",
		QuestionID:        "question-1",
		Status:            conversation.RetryTurnAnswering,
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt,
	}
	turns := &retryTurnPortStub{drafts: []conversation.RetryTurnDraft{draft}}
	practice := &retryPracticePortStub{
		participantID: "participant-learner",
	}
	conversations := &retryConversationPortStub{
		candidate: conversation.TranscriptionCandidate{
			ID:                      "candidate-1",
			SessionID:               draft.PracticeSessionID,
			QuestionID:              draft.QuestionID,
			RespondentParticipantID: practice.participantID,
			TranscriptID:            "transcript-1",
			EvidenceVersion:         1,
			Transcript:              "I would like to try again.",
			CreatedAt:               createdAt.Add(time.Second),
		},
	}
	application, err := NewSameQuestionRetryApplication(
		turns,
		practice,
		conversations,
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
		result.RetryRequestID != draft.RetryRequestID ||
		result.Candidate.ID != conversations.candidate.ID {
		t.Fatalf("unexpected result: %#v", result)
	}
	if practice.retryRequestID != draft.RetryRequestID {
		t.Fatalf(
			"authorized retry request = %q",
			practice.retryRequestID,
		)
	}
	command := conversations.transcribeCommand
	if command.SessionID != draft.PracticeSessionID ||
		command.QuestionID != draft.QuestionID ||
		command.ContentType != "audio/wav" {
		t.Fatalf("transcription command = %#v", command)
	}
	if conversations.participantID != practice.participantID {
		t.Fatalf(
			"participant = %q",
			conversations.participantID,
		)
	}
	if command.IdempotencyKey == "client-transcribe-key" ||
		command.IdempotencyKey != retryTranscriptionIdempotencyKey(
			draft.TurnID,
			"client-transcribe-key",
		) {
		t.Fatalf("idempotency key was not retry-scoped: %q", command.IdempotencyKey)
	}
	if !bytes.Equal(conversations.audio, audio) {
		t.Fatalf("audio = %q", conversations.audio)
	}
}

func TestSameQuestionRetryConfirmDoesNotAdvancePractice(t *testing.T) {
	t.Parallel()

	createdAt := time.Unix(1_700_000_000, 0).UTC()
	confirmedAt := createdAt.Add(2 * time.Second)
	before := conversation.RetryTurnDraft{
		TurnID:            "retry-turn-1",
		RetryRequestID:    "11111111-1111-4111-8111-111111111111",
		PracticeSessionID: "practice-session-1",
		OriginalTurnID:    "original-turn-1",
		QuestionID:        "question-1",
		Status:            conversation.RetryTurnAnswering,
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt,
	}
	after := before
	after.Status = conversation.RetryTurnConfirmed
	after.CandidateID = "candidate-1"
	after.UpdatedAt = confirmedAt
	after.ConfirmedAt = &confirmedAt
	candidate := conversation.TranscriptionCandidate{
		ID:                      after.CandidateID,
		SessionID:               after.PracticeSessionID,
		QuestionID:              after.QuestionID,
		RespondentParticipantID: "participant-learner",
		TranscriptID:            "transcript-1",
		EvidenceVersion:         1,
		Transcript:              "This retry is clearer.",
		CreatedAt:               createdAt.Add(time.Second),
	}
	turn := conversation.ConfirmedVoiceTurn{
		ID:                      after.TurnID,
		SessionID:               after.PracticeSessionID,
		QuestionID:              after.QuestionID,
		RespondentParticipantID: candidate.RespondentParticipantID,
		CandidateID:             candidate.ID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		AnswerText:              candidate.Transcript,
		TurnKind:                retryVoiceTurnKind,
		RetryRequestID:          after.RetryRequestID,
		OriginalTurnID:          after.OriginalTurnID,
		CountsTowardTurnLimit:   false,
	}
	turns := &retryTurnPortStub{
		drafts: []conversation.RetryTurnDraft{before, after},
	}
	conversations := &retryConversationPortStub{
		candidate: candidate,
		turn:      turn,
	}
	application, err := NewSameQuestionRetryApplication(
		turns,
		&retryPracticePortStub{participantID: "participant-learner"},
		conversations,
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
		result.RetryRequestID != before.RetryRequestID ||
		result.OriginalTurnID != before.OriginalTurnID ||
		!result.ConfirmedAt.Equal(confirmedAt) {
		t.Fatalf("unexpected confirmed retry: %#v", result)
	}
	command := conversations.confirmCommand
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
	if conversations.progressSaveCalls != 0 ||
		conversations.reviewSaveCalls != 0 {
		t.Fatalf(
			"retry advanced ordinary saga: progress=%d review=%d",
			conversations.progressSaveCalls,
			conversations.reviewSaveCalls,
		)
	}
}

type retryTurnPortStub struct {
	drafts []conversation.RetryTurnDraft
	calls  int
}

func (port *retryTurnPortStub) Get(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
) (conversation.RetryTurnDraft, error) {
	index := port.calls
	port.calls++
	if index >= len(port.drafts) {
		index = len(port.drafts) - 1
	}
	return port.drafts[index], nil
}

type retryPracticePortStub struct {
	participantID  string
	retryRequestID string
}

func (port *retryPracticePortStub) ResolveAuthorizedParticipant(
	_ context.Context,
	_ requestcontext.Actor,
	retryRequestID string,
) (string, error) {
	port.retryRequestID = retryRequestID
	return port.participantID, nil
}

type retryConversationPortStub struct {
	candidate         conversation.TranscriptionCandidate
	turn              conversation.ConfirmedVoiceTurn
	participantID     string
	transcribeCommand conversation.TranscribeVoiceCommand
	confirmCommand    conversation.ConfirmVoiceTurnCommand
	audio             []byte
	progressSaveCalls int
	reviewSaveCalls   int
}

func (port *retryConversationPortStub) Transcribe(
	_ context.Context,
	_ requestcontext.Actor,
	participantID string,
	command conversation.TranscribeVoiceCommand,
) (conversation.TranscriptionCandidate, error) {
	port.participantID = participantID
	port.transcribeCommand = command
	var err error
	port.audio, err = io.ReadAll(command.Audio)
	return port.candidate, err
}

func (port *retryConversationPortStub) GetTranscriptionCandidate(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
) (conversation.TranscriptionCandidate, error) {
	return port.candidate, nil
}

func (port *retryConversationPortStub) Confirm(
	_ context.Context,
	_ requestcontext.Actor,
	command conversation.ConfirmVoiceTurnCommand,
) (conversation.ConfirmedVoiceTurn, error) {
	port.confirmCommand = command
	return port.turn, nil
}

func (port *retryConversationPortStub) SaveTurnProgress(
	context.Context,
	requestcontext.Actor,
	string,
	conversation.VoiceTurnProgress,
) (conversation.ConfirmedVoiceTurn, error) {
	port.progressSaveCalls++
	return conversation.ConfirmedVoiceTurn{}, nil
}

func (port *retryConversationPortStub) SaveTurnReview(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (conversation.ConfirmedVoiceTurn, error) {
	port.reviewSaveCalls++
	return conversation.ConfirmedVoiceTurn{}, nil
}

func (port *retryConversationPortStub) SynthesizeQuestion(
	context.Context,
	string,
) (conversation.QuestionSpeech, error) {
	return conversation.QuestionSpeech{}, nil
}
