package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const retryVoiceTurnKind = "RETRY"

// SameQuestionRetryTurnPort exposes only the actor-owned Conversation draft
// needed by the dedicated retry answer path.
type SameQuestionRetryTurnPort interface {
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (conversation.RetryTurnDraft, error)
}

// SameQuestionRetryPracticePort resolves the learner from the frozen Practice
// authorization. Unlike ordinary progression, completed Sessions remain
// eligible for a same-question retry.
type SameQuestionRetryPracticePort interface {
	ResolveAuthorizedParticipant(
		context.Context,
		requestcontext.Actor,
		string,
	) (string, error)
}

// SameQuestionRetryConversationPort contains only the Conversation operations
// used by the retry flow. Retry never writes ordinary Practice checkpoints.
type SameQuestionRetryConversationPort interface {
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
}

type RetryTranscriptionCommand struct {
	RetryTurnID    string
	IdempotencyKey string
	ContentType    string
	Audio          io.Reader
}

type RetryTranscriptionCandidate struct {
	RetryTurnID    string
	RetryRequestID string
	Candidate      conversation.TranscriptionCandidate
}

type ConfirmRetryTranscriptionCommand struct {
	RetryTurnID    string
	CandidateID    string
	IdempotencyKey string
}

type ConfirmedRetryTurn struct {
	Turn           conversation.ConfirmedVoiceTurn
	RetryRequestID string
	OriginalTurnID string
	CreatedAt      time.Time
	ConfirmedAt    time.Time
}

// SameQuestionRetryApplication is a narrow retry-only orchestration. It never
// calls Practice's effective-turn transition and therefore cannot advance or
// complete a Session.
type SameQuestionRetryApplication struct {
	turns         SameQuestionRetryTurnPort
	practice      SameQuestionRetryPracticePort
	conversations SameQuestionRetryConversationPort
}

func NewSameQuestionRetryApplication(
	turns SameQuestionRetryTurnPort,
	practice SameQuestionRetryPracticePort,
	conversations SameQuestionRetryConversationPort,
) (*SameQuestionRetryApplication, error) {
	if turns == nil || practice == nil || conversations == nil {
		return nil, errors.New(
			"practice voice: same-question retry dependency is required",
		)
	}
	return &SameQuestionRetryApplication{
		turns:         turns,
		practice:      practice,
		conversations: conversations,
	}, nil
}

func (application *SameQuestionRetryApplication) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command RetryTranscriptionCommand,
) (RetryTranscriptionCandidate, error) {
	if application == nil || application.turns == nil ||
		application.practice == nil || application.conversations == nil ||
		!validRetryVoiceID(command.RetryTurnID) ||
		!validRetryVoiceIdempotencyKey(command.IdempotencyKey) ||
		strings.TrimSpace(command.ContentType) == "" ||
		command.Audio == nil {
		return RetryTranscriptionCandidate{}, ErrInvalidRequest
	}
	if err := validateVoiceActor(ctx, actor); err != nil {
		return RetryTranscriptionCandidate{}, err
	}
	draft, err := application.turns.Get(
		ctx,
		actor,
		command.RetryTurnID,
	)
	if err != nil {
		return RetryTranscriptionCandidate{}, err
	}
	if !validRetryDraft(draft, command.RetryTurnID) {
		return RetryTranscriptionCandidate{}, ErrInvalidContext
	}
	if draft.Status != conversation.RetryTurnAnswering {
		return RetryTranscriptionCandidate{}, ErrConflict
	}
	participantID, err := application.practice.
		ResolveAuthorizedParticipant(
			ctx,
			actor,
			draft.RetryRequestID,
		)
	if err != nil {
		return RetryTranscriptionCandidate{}, err
	}
	if !validRetryVoiceID(participantID) {
		return RetryTranscriptionCandidate{}, ErrInvalidContext
	}
	candidate, err := application.conversations.Transcribe(
		ctx,
		actor,
		participantID,
		conversation.TranscribeVoiceCommand{
			SessionID:  draft.PracticeSessionID,
			QuestionID: draft.QuestionID,
			IdempotencyKey: retryTranscriptionIdempotencyKey(
				draft.TurnID,
				command.IdempotencyKey,
			),
			ContentType: command.ContentType,
			Audio:       command.Audio,
		},
	)
	if err != nil {
		return RetryTranscriptionCandidate{}, err
	}
	if candidate.SessionID != draft.PracticeSessionID ||
		candidate.QuestionID != draft.QuestionID ||
		candidate.RespondentParticipantID != participantID ||
		!validRetryVoiceID(candidate.ID) {
		return RetryTranscriptionCandidate{}, ErrInvalidContext
	}
	return RetryTranscriptionCandidate{
		RetryTurnID:    draft.TurnID,
		RetryRequestID: draft.RetryRequestID,
		Candidate:      candidate,
	}, nil
}

func (application *SameQuestionRetryApplication) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmRetryTranscriptionCommand,
) (ConfirmedRetryTurn, error) {
	if application == nil || application.turns == nil ||
		application.conversations == nil ||
		!validRetryVoiceID(command.RetryTurnID) ||
		!validRetryVoiceID(command.CandidateID) ||
		!validRetryVoiceIdempotencyKey(command.IdempotencyKey) {
		return ConfirmedRetryTurn{}, ErrInvalidRequest
	}
	if err := validateVoiceActor(ctx, actor); err != nil {
		return ConfirmedRetryTurn{}, err
	}
	draft, err := application.turns.Get(
		ctx,
		actor,
		command.RetryTurnID,
	)
	if err != nil {
		return ConfirmedRetryTurn{}, err
	}
	if !validRetryDraft(draft, command.RetryTurnID) {
		return ConfirmedRetryTurn{}, ErrInvalidContext
	}
	if draft.Status == conversation.RetryTurnConfirmed &&
		draft.CandidateID != command.CandidateID {
		return ConfirmedRetryTurn{}, ErrConflict
	}
	candidate, err := application.conversations.
		GetTranscriptionCandidate(
			ctx,
			actor,
			command.CandidateID,
		)
	if err != nil {
		return ConfirmedRetryTurn{}, err
	}
	if candidate.SessionID != draft.PracticeSessionID ||
		candidate.QuestionID != draft.QuestionID {
		return ConfirmedRetryTurn{}, ErrConflict
	}
	turn, err := application.conversations.Confirm(
		ctx,
		actor,
		conversation.ConfirmVoiceTurnCommand{
			CandidateID: command.CandidateID,
			IdempotencyKey: retryConfirmationIdempotencyKey(
				command.RetryTurnID,
				command.IdempotencyKey,
			),
			RetryTurnID: command.RetryTurnID,
		},
	)
	if err != nil {
		return ConfirmedRetryTurn{}, err
	}
	confirmed, err := application.turns.Get(
		ctx,
		actor,
		command.RetryTurnID,
	)
	if err != nil {
		return ConfirmedRetryTurn{}, err
	}
	if !validConfirmedRetry(draft, confirmed, candidate, turn) {
		return ConfirmedRetryTurn{}, ErrInvalidContext
	}
	return ConfirmedRetryTurn{
		Turn:           turn,
		RetryRequestID: confirmed.RetryRequestID,
		OriginalTurnID: confirmed.OriginalTurnID,
		CreatedAt:      confirmed.CreatedAt,
		ConfirmedAt:    *confirmed.ConfirmedAt,
	}, nil
}

func validRetryDraft(
	draft conversation.RetryTurnDraft,
	retryTurnID string,
) bool {
	return draft.TurnID == retryTurnID &&
		validRetryVoiceID(draft.RetryRequestID) &&
		validRetryVoiceID(draft.PracticeSessionID) &&
		validRetryVoiceID(draft.OriginalTurnID) &&
		validRetryVoiceID(draft.QuestionID) &&
		!draft.CreatedAt.IsZero() &&
		!draft.UpdatedAt.Before(draft.CreatedAt) &&
		(draft.Status == conversation.RetryTurnAnswering ||
			draft.Status == conversation.RetryTurnConfirmed)
}

func validConfirmedRetry(
	before conversation.RetryTurnDraft,
	after conversation.RetryTurnDraft,
	candidate conversation.TranscriptionCandidate,
	turn conversation.ConfirmedVoiceTurn,
) bool {
	return validRetryDraft(after, before.TurnID) &&
		after.RetryRequestID == before.RetryRequestID &&
		after.PracticeSessionID == before.PracticeSessionID &&
		after.OriginalTurnID == before.OriginalTurnID &&
		after.QuestionID == before.QuestionID &&
		after.Status == conversation.RetryTurnConfirmed &&
		after.CandidateID == candidate.ID &&
		after.ConfirmedAt != nil &&
		!after.ConfirmedAt.Before(after.CreatedAt) &&
		turn.ID == after.TurnID &&
		turn.SessionID == after.PracticeSessionID &&
		turn.QuestionID == after.QuestionID &&
		turn.RespondentParticipantID ==
			candidate.RespondentParticipantID &&
		turn.CandidateID == candidate.ID &&
		turn.TranscriptID == candidate.TranscriptID &&
		turn.EvidenceVersion == candidate.EvidenceVersion &&
		turn.AnswerText == candidate.Transcript &&
		turn.TurnKind == retryVoiceTurnKind &&
		turn.RetryRequestID == after.RetryRequestID &&
		turn.OriginalTurnID == after.OriginalTurnID &&
		!turn.CountsTowardTurnLimit &&
		turn.EffectiveTurns == 0 &&
		!turn.SessionCompleted &&
		turn.ReviewID == ""
}

func retryTranscriptionIdempotencyKey(
	retryTurnID string,
	clientKey string,
) string {
	return retryVoiceIdempotencyKey(
		"retry-transcription/v1",
		retryTurnID,
		clientKey,
	)
}

func retryConfirmationIdempotencyKey(
	retryTurnID string,
	clientKey string,
) string {
	return retryVoiceIdempotencyKey(
		"retry-confirmation/v1",
		retryTurnID,
		clientKey,
	)
}

func retryVoiceIdempotencyKey(
	operation string,
	retryTurnID string,
	clientKey string,
) string {
	digest := sha256.Sum256([]byte(
		operation + "\x00" + retryTurnID + "\x00" + clientKey,
	))
	return "retry_" + hex.EncodeToString(digest[:])
}

func validRetryVoiceID(value string) bool {
	if value == "" || len(value) > 128 ||
		value != strings.TrimSpace(value) {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '.', '_', ':', '-':
		default:
			return false
		}
	}
	return true
}

func validRetryVoiceIdempotencyKey(value string) bool {
	if value == "" || len(value) > 128 ||
		value != strings.TrimSpace(value) {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
