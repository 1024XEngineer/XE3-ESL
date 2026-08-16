package interaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const retryVoiceTurnKind = "RETRY"

// SameQuestionRetryTurnPort exposes only the actor-owned Practice Interaction draft
// needed by the dedicated retry answer path.
type SameQuestionRetryTurnPort interface {
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (RetryTurnDraft, error)
}

// SameQuestionRetryRoundPort contains only the voice-round operations
// used by the retry flow. Retry never writes ordinary Practice checkpoints.
type SameQuestionRetryRoundPort interface {
	Transcribe(
		context.Context,
		requestcontext.Actor,
		string,
		TranscribeVoiceCommand,
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
}

type RetryTranscriptionCommand struct {
	RetryTurnID    string
	IdempotencyKey string
	ContentType    string
	Audio          io.Reader
}

type RetryTranscriptionCandidate struct {
	RetryTurnID string
	Candidate   TranscriptionCandidate
}

type ConfirmRetryTranscriptionCommand struct {
	RetryTurnID    string
	CandidateID    string
	IdempotencyKey string
}

type ConfirmedRetryTurn struct {
	Turn           practice.Turn
	OriginalTurnID string
	CreatedAt      time.Time
	ConfirmedAt    time.Time
}

// SameQuestionRetryApplication is a narrow retry-only orchestration. It never
// calls Practice's effective-turn transition and therefore cannot advance or
// complete a Session.
type SameQuestionRetryApplication struct {
	turns  SameQuestionRetryTurnPort
	rounds SameQuestionRetryRoundPort
}

func NewSameQuestionRetryApplication(
	turns SameQuestionRetryTurnPort,
	rounds SameQuestionRetryRoundPort,
) (*SameQuestionRetryApplication, error) {
	if turns == nil || rounds == nil {
		return nil, errors.New(
			"practice interaction: same-question retry dependency is required",
		)
	}
	return &SameQuestionRetryApplication{
		turns:  turns,
		rounds: rounds,
	}, nil
}

func (application *SameQuestionRetryApplication) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command RetryTranscriptionCommand,
) (RetryTranscriptionCandidate, error) {
	if application == nil || application.turns == nil ||
		application.rounds == nil ||
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
	if draft.Status != RetryTurnAnswering && draft.Status != RetryTurnFailed {
		return RetryTranscriptionCandidate{}, ErrConflict
	}
	participantID := draft.RespondentParticipantID
	if !validRetryVoiceID(participantID) {
		return RetryTranscriptionCandidate{}, ErrInvalidContext
	}
	candidate, err := application.rounds.Transcribe(
		ctx,
		actor,
		participantID,
		TranscribeVoiceCommand{
			TurnID:     draft.TurnID,
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
		RetryTurnID: draft.TurnID,
		Candidate:   candidate,
	}, nil
}

func (application *SameQuestionRetryApplication) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmRetryTranscriptionCommand,
) (ConfirmedRetryTurn, error) {
	if application == nil || application.turns == nil ||
		application.rounds == nil ||
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
	if draft.Status == RetryTurnConfirmed &&
		draft.CandidateID != command.CandidateID {
		return ConfirmedRetryTurn{}, ErrConflict
	}
	if draft.Status != RetryTurnReady && draft.Status != RetryTurnConfirmed {
		return ConfirmedRetryTurn{}, ErrConflict
	}
	if draft.CandidateID != command.CandidateID {
		return ConfirmedRetryTurn{}, ErrConflict
	}
	candidate, err := application.rounds.
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
	turn, err := application.rounds.Confirm(
		ctx,
		actor,
		ConfirmVoiceTurnCommand{
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
		OriginalTurnID: confirmed.OriginalTurnID,
		CreatedAt:      confirmed.CreatedAt,
		ConfirmedAt:    *confirmed.ConfirmedAt,
	}, nil
}

func validRetryDraft(
	draft RetryTurnDraft,
	retryTurnID string,
) bool {
	return draft.TurnID == retryTurnID &&
		validRetryVoiceIdempotencyKey(draft.ClientRequestID) &&
		validRetryVoiceID(draft.PracticeSessionID) &&
		validRetryVoiceID(draft.OriginalTurnID) &&
		validRetryVoiceID(draft.QuestionID) &&
		validRetryVoiceID(draft.RespondentParticipantID) &&
		!draft.CreatedAt.IsZero() &&
		!draft.UpdatedAt.Before(draft.CreatedAt) &&
		(draft.Status == RetryTurnAnswering ||
			draft.Status == RetryTurnReady ||
			draft.Status == RetryTurnFailed ||
			draft.Status == RetryTurnConfirmed)
}

func validConfirmedRetry(
	before RetryTurnDraft,
	after RetryTurnDraft,
	candidate TranscriptionCandidate,
	turn practice.Turn,
) bool {
	return validRetryDraft(after, before.TurnID) &&
		after.ClientRequestID == before.ClientRequestID &&
		after.PracticeSessionID == before.PracticeSessionID &&
		after.OriginalTurnID == before.OriginalTurnID &&
		after.QuestionID == before.QuestionID &&
		after.Status == RetryTurnConfirmed &&
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
		string(turn.Kind) == retryVoiceTurnKind &&
		turn.ClientRequestID == after.ClientRequestID &&
		turn.OriginalTurnID == after.OriginalTurnID &&
		!turn.CountsTowardTurnLimit &&
		turn.EffectiveTurns > 0
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
