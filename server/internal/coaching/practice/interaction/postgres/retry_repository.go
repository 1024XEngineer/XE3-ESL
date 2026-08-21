package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var _ practiceinteraction.RetryTurnStore = (*Repository)(nil)

func (r *Repository) GetRetryTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
) (practiceinteraction.RetryTurnDraft, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!validUUID(actor.UserID) || !validRetryTurnIdentifier(turnID) {
		return practiceinteraction.RetryTurnDraft{}, practiceinteraction.ErrRetryTurnInvalid
	}
	tx, err := r.beginActorRead(ctx, practiceinteraction.Actor{
		UserID: actor.UserID, SessionID: actor.SessionID,
	})
	if err != nil {
		return practiceinteraction.RetryTurnDraft{}, mapRetryTurnError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	draft, err := scanRetryTurn(tx.QueryRow(ctx, retryTurnColumns+`
		WHERE s.user_id = $1 AND t.turn_id = $2 AND t.turn_kind = 'RETRY'
	`, actor.UserID, turnID))
	if err != nil {
		return practiceinteraction.RetryTurnDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.RetryTurnDraft{}, safeDatabaseError(err)
	}
	return draft, nil
}

const retryTurnColumns = `
	SELECT t.turn_id, t.client_request_id, t.session_id, t.original_turn_id,
	       t.question_id, t.respondent_participant_id, t.status,
	       COALESCE(t.candidate_id::text, ''), t.created_at, t.updated_at, t.confirmed_at
	FROM practice_turns t
	JOIN practice_sessions s ON s.session_id=t.session_id
`

func scanRetryTurn(row rowScanner) (practiceinteraction.RetryTurnDraft, error) {
	var draft practiceinteraction.RetryTurnDraft
	var status string
	err := row.Scan(
		&draft.TurnID,
		&draft.ClientRequestID,
		&draft.PracticeSessionID,
		&draft.OriginalTurnID,
		&draft.QuestionID,
		&draft.RespondentParticipantID,
		&status,
		&draft.CandidateID,
		&draft.CreatedAt,
		&draft.UpdatedAt,
		&draft.ConfirmedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinteraction.RetryTurnDraft{}, practiceinteraction.ErrRetryTurnNotFound
	}
	if err != nil {
		return practiceinteraction.RetryTurnDraft{}, safeDatabaseError(err)
	}
	switch status {
	case "answering", "transcribing":
		draft.Status = practiceinteraction.RetryTurnAnswering
	case "transcribed":
		draft.Status = practiceinteraction.RetryTurnReady
	case "failed":
		draft.Status = practiceinteraction.RetryTurnFailed
	case "confirmed":
		draft.Status = practiceinteraction.RetryTurnConfirmed
	default:
		return practiceinteraction.RetryTurnDraft{}, practiceinteraction.ErrRetryTurnConflict
	}
	draft.CreatedAt = draft.CreatedAt.UTC()
	draft.UpdatedAt = draft.UpdatedAt.UTC()
	if draft.ConfirmedAt != nil {
		confirmed := draft.ConfirmedAt.UTC()
		draft.ConfirmedAt = &confirmed
	}
	return draft, nil
}

func mapRetryTurnError(err error) error {
	switch {
	case errors.Is(err, practiceinteraction.ErrActorDeleted),
		errors.Is(err, practiceinteraction.ErrPersistenceNotFound):
		return practiceinteraction.ErrRetryTurnNotFound
	case errors.Is(err, practiceinteraction.ErrPersistenceInvalid):
		return practiceinteraction.ErrRetryTurnInvalid
	case errors.Is(err, practiceinteraction.ErrPersistenceConflict):
		return practiceinteraction.ErrRetryTurnConflict
	default:
		return err
	}
}

func validRetryTurnIdentifier(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
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
