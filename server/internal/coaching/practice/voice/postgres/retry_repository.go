package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var _ practicevoice.RetryTurnStore = (*Repository)(nil)

type domainRetryTurnDraft struct {
	TurnID         string
	RetryRequestID string
	OriginalTurnID string
	Status         string
}

func (r *Repository) CreateRetryTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	command practicevoice.CreateRetryTurnCommand,
) (practicevoice.RetryTurnDraft, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!validUUID(actor.UserID) ||
		!validUUID(command.RetryRequestID) ||
		!validRetryTurnIdentifier(command.PracticeSessionID) ||
		!validRetryTurnIdentifier(command.OriginalTurnID) ||
		!validRetryTurnIdentifier(command.QuestionID) {
		return practicevoice.RetryTurnDraft{},
			practicevoice.ErrRetryTurnInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practicevoice.RetryTurnDraft{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practicevoice.RetryTurnDraft{},
			mapRetryTurnError(err)
	}
	r.reachedWriteFence()
	if err := lockKey(
		ctx,
		tx,
		actor.UserID,
		"retry-turn",
		command.RetryRequestID,
	); err != nil {
		return practicevoice.RetryTurnDraft{}, err
	}

	existing, err := getRetryTurnByRequest(
		ctx,
		tx,
		actor.UserID,
		command.RetryRequestID,
		" FOR UPDATE",
	)
	if err == nil {
		if existing.PracticeSessionID != command.PracticeSessionID ||
			existing.OriginalTurnID != command.OriginalTurnID ||
			existing.QuestionID != command.QuestionID {
			return practicevoice.RetryTurnDraft{},
				practicevoice.ErrRetryTurnConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return practicevoice.RetryTurnDraft{},
				safeDatabaseError(err)
		}
		return existing, nil
	}
	if !errors.Is(err, practicevoice.ErrRetryTurnNotFound) {
		return practicevoice.RetryTurnDraft{}, err
	}

	original, err := lockTurn(
		ctx,
		tx,
		actor.UserID,
		command.OriginalTurnID,
	)
	if err != nil {
		return practicevoice.RetryTurnDraft{},
			mapRetryTurnError(err)
	}
	if original.Kind != practice.TurnKindEffective ||
		!original.CountsTowardTurnLimit ||
		original.SessionID != command.PracticeSessionID ||
		original.QuestionID != command.QuestionID ||
		original.EffectiveTurns < 1 {
		return practicevoice.RetryTurnDraft{},
			practicevoice.ErrRetryTurnConflict
	}

	now := databaseTime(r.now)
	turnID := newID("turn")
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_retry_turn_drafts (
			owner_user_id,
			retry_request_id,
			turn_id,
			practice_session_id,
			original_turn_id,
			question_id,
			status,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'ANSWERING', $7, $7)
	`, actor.UserID, command.RetryRequestID, turnID,
		command.PracticeSessionID, command.OriginalTurnID,
		command.QuestionID, now); err != nil {
		return practicevoice.RetryTurnDraft{},
			mapRetryTurnWriteError(err)
	}
	created, err := getRetryTurnByRequest(
		ctx,
		tx,
		actor.UserID,
		command.RetryRequestID,
		"",
	)
	if err != nil {
		return practicevoice.RetryTurnDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practicevoice.RetryTurnDraft{},
			safeDatabaseError(err)
	}
	return created, nil
}

func (r *Repository) GetRetryTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
) (practicevoice.RetryTurnDraft, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!validUUID(actor.UserID) ||
		!validRetryTurnIdentifier(turnID) {
		return practicevoice.RetryTurnDraft{},
			practicevoice.ErrRetryTurnInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practicevoice.RetryTurnDraft{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practicevoice.RetryTurnDraft{},
			mapRetryTurnError(err)
	}
	draft, err := scanRetryTurn(tx.QueryRow(ctx, retryTurnColumns+`
		WHERE owner_user_id = $1
		  AND turn_id = $2
	`, actor.UserID, turnID))
	if err != nil {
		return practicevoice.RetryTurnDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practicevoice.RetryTurnDraft{},
			safeDatabaseError(err)
	}
	return draft, nil
}

const retryTurnColumns = `
	SELECT
		turn_id,
		retry_request_id::text,
		practice_session_id,
		original_turn_id,
		question_id,
		status,
		coalesce(candidate_id, ''),
		created_at,
		updated_at,
		confirmed_at
	FROM practice_retry_turn_drafts
`

func getRetryTurnByRequest(
	ctx context.Context,
	database queryRow,
	ownerUserID string,
	retryRequestID string,
	suffix string,
) (practicevoice.RetryTurnDraft, error) {
	return scanRetryTurn(database.QueryRow(ctx, retryTurnColumns+`
		WHERE owner_user_id = $1
		  AND retry_request_id = $2
	`+suffix, ownerUserID, retryRequestID))
}

func lockRetryTurn(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	turnID string,
) (practicevoice.RetryTurnDraft, error) {
	return scanRetryTurn(tx.QueryRow(ctx, retryTurnColumns+`
		WHERE owner_user_id = $1
		  AND turn_id = $2
		FOR UPDATE
	`, ownerUserID, turnID))
}

func scanRetryTurn(
	row rowScanner,
) (practicevoice.RetryTurnDraft, error) {
	var draft practicevoice.RetryTurnDraft
	var status string
	var confirmedAt sql.NullTime
	err := row.Scan(
		&draft.TurnID,
		&draft.RetryRequestID,
		&draft.PracticeSessionID,
		&draft.OriginalTurnID,
		&draft.QuestionID,
		&status,
		&draft.CandidateID,
		&draft.CreatedAt,
		&draft.UpdatedAt,
		&confirmedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practicevoice.RetryTurnDraft{},
			practicevoice.ErrRetryTurnNotFound
	}
	if err != nil {
		return practicevoice.RetryTurnDraft{},
			safeDatabaseError(err)
	}
	draft.Status = practicevoice.RetryTurnStatus(status)
	draft.CreatedAt = draft.CreatedAt.UTC()
	draft.UpdatedAt = draft.UpdatedAt.UTC()
	if confirmedAt.Valid {
		value := confirmedAt.Time.UTC()
		draft.ConfirmedAt = &value
	}
	return draft, nil
}

func mapRetryTurnError(err error) error {
	switch {
	case errors.Is(err, practicevoice.ErrPersistenceNotFound),
		errors.Is(err, practicevoice.ErrActorDeleted):
		return practicevoice.ErrRetryTurnNotFound
	case errors.Is(err, practicevoice.ErrPersistenceConflict):
		return practicevoice.ErrRetryTurnConflict
	default:
		return err
	}
}

func mapRetryConfirmationError(err error) error {
	switch {
	case errors.Is(err, practicevoice.ErrRetryTurnNotFound):
		return practicevoice.ErrPersistenceNotFound
	case errors.Is(err, practicevoice.ErrRetryTurnConflict),
		errors.Is(err, practicevoice.ErrRetryTurnNotReady):
		return practicevoice.ErrPersistenceConflict
	case errors.Is(err, practicevoice.ErrRetryTurnInvalid):
		return practicevoice.ErrPersistenceInvalid
	default:
		return err
	}
}

func mapRetryTurnWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(postgresError.Code == "23505" ||
			postgresError.Code == "23514" ||
			postgresError.Code == "23503") {
		return practicevoice.ErrRetryTurnConflict
	}
	return fmt.Errorf("persist Practice Voice retry Turn: %w", err)
}

func validRetryTurnIdentifier(value string) bool {
	if value == "" || len(value) > 128 ||
		value != strings.TrimSpace(value) {
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
