package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	domainconversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var _ domainconversation.RetryTurnStore = (*Repository)(nil)

type domainRetryTurnDraft struct {
	TurnID         string
	RetryRequestID string
	OriginalTurnID string
	Status         string
}

func (r *Repository) CreateRetryTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	command domainconversation.CreateRetryTurnCommand,
) (domainconversation.RetryTurnDraft, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!validUUID(actor.UserID) ||
		!validUUID(command.RetryRequestID) ||
		!validRetryTurnIdentifier(command.PracticeSessionID) ||
		!validRetryTurnIdentifier(command.OriginalTurnID) ||
		!validRetryTurnIdentifier(command.QuestionID) {
		return domainconversation.RetryTurnDraft{},
			domainconversation.ErrRetryTurnInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domainconversation.RetryTurnDraft{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return domainconversation.RetryTurnDraft{},
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
		return domainconversation.RetryTurnDraft{}, err
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
			return domainconversation.RetryTurnDraft{},
				domainconversation.ErrRetryTurnConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return domainconversation.RetryTurnDraft{},
				safeDatabaseError(err)
		}
		return existing, nil
	}
	if !errors.Is(err, domainconversation.ErrRetryTurnNotFound) {
		return domainconversation.RetryTurnDraft{}, err
	}

	original, err := lockTurn(
		ctx,
		tx,
		actor.UserID,
		command.OriginalTurnID,
	)
	if err != nil {
		return domainconversation.RetryTurnDraft{},
			mapRetryTurnError(err)
	}
	if original.Kind != conversation.TurnKindEffective ||
		!original.CountsTowardTurnLimit ||
		original.SessionID != command.PracticeSessionID ||
		original.QuestionID != command.QuestionID ||
		original.Progress.EffectiveTurns < 1 {
		return domainconversation.RetryTurnDraft{},
			domainconversation.ErrRetryTurnConflict
	}

	now := databaseTime(r.now)
	turnID := newID("turn")
	if _, err := tx.Exec(ctx, `
		INSERT INTO conversation_retry_turn_drafts (
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
		return domainconversation.RetryTurnDraft{},
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
		return domainconversation.RetryTurnDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domainconversation.RetryTurnDraft{},
			safeDatabaseError(err)
	}
	return created, nil
}

func (r *Repository) GetRetryTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
) (domainconversation.RetryTurnDraft, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!validUUID(actor.UserID) ||
		!validRetryTurnIdentifier(turnID) {
		return domainconversation.RetryTurnDraft{},
			domainconversation.ErrRetryTurnInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domainconversation.RetryTurnDraft{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return domainconversation.RetryTurnDraft{},
			mapRetryTurnError(err)
	}
	draft, err := scanRetryTurn(tx.QueryRow(ctx, retryTurnColumns+`
		WHERE owner_user_id = $1
		  AND turn_id = $2
	`, actor.UserID, turnID))
	if err != nil {
		return domainconversation.RetryTurnDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domainconversation.RetryTurnDraft{},
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
	FROM conversation_retry_turn_drafts
`

func getRetryTurnByRequest(
	ctx context.Context,
	database queryRow,
	ownerUserID string,
	retryRequestID string,
	suffix string,
) (domainconversation.RetryTurnDraft, error) {
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
) (domainconversation.RetryTurnDraft, error) {
	return scanRetryTurn(tx.QueryRow(ctx, retryTurnColumns+`
		WHERE owner_user_id = $1
		  AND turn_id = $2
		FOR UPDATE
	`, ownerUserID, turnID))
}

func scanRetryTurn(
	row rowScanner,
) (domainconversation.RetryTurnDraft, error) {
	var draft domainconversation.RetryTurnDraft
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
		return domainconversation.RetryTurnDraft{},
			domainconversation.ErrRetryTurnNotFound
	}
	if err != nil {
		return domainconversation.RetryTurnDraft{},
			safeDatabaseError(err)
	}
	draft.Status = domainconversation.RetryTurnStatus(status)
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
	case errors.Is(err, conversation.ErrPersistenceNotFound),
		errors.Is(err, conversation.ErrActorDeleted):
		return domainconversation.ErrRetryTurnNotFound
	case errors.Is(err, conversation.ErrPersistenceConflict):
		return domainconversation.ErrRetryTurnConflict
	default:
		return err
	}
}

func mapRetryConfirmationError(err error) error {
	switch {
	case errors.Is(err, domainconversation.ErrRetryTurnNotFound):
		return conversation.ErrPersistenceNotFound
	case errors.Is(err, domainconversation.ErrRetryTurnConflict),
		errors.Is(err, domainconversation.ErrRetryTurnNotReady):
		return conversation.ErrPersistenceConflict
	case errors.Is(err, domainconversation.ErrRetryTurnInvalid):
		return conversation.ErrPersistenceInvalid
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
		return domainconversation.ErrRetryTurnConflict
	}
	return fmt.Errorf("persist Conversation retry Turn: %w", err)
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
