package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
)

const questionTipColumns = `SELECT tip_id, practice_session_id, question_id,
       idempotency_key, status, fencing_token, deletion_generation,
       lease_expires_at, COALESCE(content, ''), created_at, updated_at,
       completed_at
  FROM practice_question_tips`

func (r *Repository) ClaimQuestionTip(
	ctx context.Context,
	actor practicevoice.Actor,
	command practicevoice.ClaimQuestionTipCommand,
) (practicevoice.QuestionTip, error) {
	if !validInputActor(actor) || strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		command.LeaseDuration <= 0 {
		return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practicevoice.QuestionTip{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	generation, err := ensureActorWritable(ctx, tx, actor.UserID)
	if err != nil {
		return practicevoice.QuestionTip{}, err
	}
	if err := lockKey(ctx, tx, actor.UserID, "question-tip", command.SessionID, command.QuestionID); err != nil {
		return practicevoice.QuestionTip{}, err
	}

	byKey, keyErr := getQuestionTipByKey(ctx, tx, actor.UserID, command.IdempotencyKey)
	if keyErr == nil && (byKey.SessionID != command.SessionID || byKey.QuestionID != command.QuestionID) {
		return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceConflict
	}
	if keyErr != nil && !errors.Is(keyErr, practicevoice.ErrPersistenceNotFound) {
		return practicevoice.QuestionTip{}, keyErr
	}

	now := databaseTime(r.now)
	tip, getErr := getQuestionTip(ctx, tx, actor.UserID, command.SessionID, command.QuestionID)
	if errors.Is(getErr, practicevoice.ErrPersistenceNotFound) {
		tip = practicevoice.QuestionTip{
			ID:                 newID("question_tip"),
			SessionID:          command.SessionID,
			QuestionID:         command.QuestionID,
			IdempotencyKey:     command.IdempotencyKey,
			Status:             practicevoice.QuestionTipProcessing,
			FencingToken:       1,
			DeletionGeneration: generation,
			LeaseAcquired:      true,
			LeaseExpiresAt:     now.Add(command.LeaseDuration),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		_, err = tx.Exec(ctx, `INSERT INTO practice_question_tips (
			owner_user_id, tip_id, practice_session_id, question_id,
			idempotency_key, status, fencing_token, deletion_generation,
			lease_expires_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			actor.UserID, tip.ID, tip.SessionID, tip.QuestionID,
			tip.IdempotencyKey, tip.Status, tip.FencingToken,
			tip.DeletionGeneration, tip.LeaseExpiresAt, tip.CreatedAt, tip.UpdatedAt)
		if err != nil {
			return practicevoice.QuestionTip{}, safeDatabaseError(err)
		}
	} else if getErr != nil {
		return practicevoice.QuestionTip{}, getErr
	} else if tip.Status != practicevoice.QuestionTipCompleted &&
		(tip.Status == practicevoice.QuestionTipFailed || !tip.LeaseExpiresAt.After(now)) {
		tip.Status = practicevoice.QuestionTipProcessing
		tip.FencingToken++
		tip.DeletionGeneration = generation
		tip.LeaseAcquired = true
		tip.LeaseExpiresAt = now.Add(command.LeaseDuration)
		tip.UpdatedAt = now
		_, err = tx.Exec(ctx, `UPDATE practice_question_tips
			SET status = 'processing', fencing_token = $4,
			    deletion_generation = $5, lease_expires_at = $6,
			    updated_at = $7, content = NULL, completed_at = NULL
			WHERE owner_user_id = $1 AND practice_session_id = $2 AND question_id = $3`,
			actor.UserID, command.SessionID, command.QuestionID,
			tip.FencingToken, generation, tip.LeaseExpiresAt, now)
		if err != nil {
			return practicevoice.QuestionTip{}, safeDatabaseError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return practicevoice.QuestionTip{}, safeDatabaseError(err)
	}
	return tip, nil
}

func (r *Repository) GetQuestionTip(
	ctx context.Context,
	actor practicevoice.Actor,
	sessionID string,
	questionID string,
) (practicevoice.QuestionTip, error) {
	if !validInputActor(actor) || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(questionID) == "" {
		return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practicevoice.QuestionTip{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tip, err := getQuestionTip(ctx, tx, actor.UserID, sessionID, questionID)
	if err != nil {
		return practicevoice.QuestionTip{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practicevoice.QuestionTip{}, safeDatabaseError(err)
	}
	return tip, nil
}

func (r *Repository) CompleteQuestionTip(
	ctx context.Context,
	actor practicevoice.Actor,
	command practicevoice.CompleteQuestionTipCommand,
) (practicevoice.QuestionTip, error) {
	if !validInputActor(actor) || strings.TrimSpace(command.TipID) == "" ||
		command.FencingToken <= 0 || command.DeletionGeneration < 0 ||
		strings.TrimSpace(command.Content) == "" {
		return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practicevoice.QuestionTip{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	generation, err := ensureActorWritable(ctx, tx, actor.UserID)
	if err != nil {
		return practicevoice.QuestionTip{}, err
	}
	if generation != command.DeletionGeneration {
		return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceConflict
	}
	now := databaseTime(r.now)
	row := tx.QueryRow(ctx, questionTipColumns+` WHERE owner_user_id = $1 AND tip_id = $2
		FOR UPDATE`, actor.UserID, command.TipID)
	tip, err := scanQuestionTip(row)
	if err != nil {
		return practicevoice.QuestionTip{}, err
	}
	if tip.Status == practicevoice.QuestionTipCompleted {
		if tip.Content != strings.TrimSpace(command.Content) {
			return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return practicevoice.QuestionTip{}, safeDatabaseError(err)
		}
		return tip, nil
	}
	if tip.Status != practicevoice.QuestionTipProcessing ||
		tip.FencingToken != command.FencingToken ||
		tip.DeletionGeneration != command.DeletionGeneration {
		return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceConflict
	}
	content := strings.TrimSpace(command.Content)
	_, err = tx.Exec(ctx, `UPDATE practice_question_tips
		SET status = 'completed', content = $3, provider = $4, model = $5,
		    provider_request_id = $6, completed_at = $7, updated_at = $7
		WHERE owner_user_id = $1 AND tip_id = $2`, actor.UserID, command.TipID,
		content, command.Provider, command.Model, command.ProviderRequestID, now)
	if err != nil {
		return practicevoice.QuestionTip{}, safeDatabaseError(err)
	}
	tip.Status = practicevoice.QuestionTipCompleted
	tip.Content = content
	tip.UpdatedAt = now
	tip.CompletedAt = &now
	if err := tx.Commit(ctx); err != nil {
		return practicevoice.QuestionTip{}, safeDatabaseError(err)
	}
	return tip, nil
}

func (r *Repository) FailQuestionTip(
	ctx context.Context,
	actor practicevoice.Actor,
	command practicevoice.FailQuestionTipCommand,
) error {
	if !validInputActor(actor) || strings.TrimSpace(command.TipID) == "" ||
		command.FencingToken <= 0 || command.DeletionGeneration < 0 {
		return practicevoice.ErrPersistenceInvalid
	}
	tag, err := r.pool.Exec(ctx, `UPDATE practice_question_tips SET status = 'failed',
		updated_at = $5 WHERE owner_user_id = $1 AND tip_id = $2
		AND status = 'processing' AND fencing_token = $3 AND deletion_generation = $4`,
		actor.UserID, command.TipID, command.FencingToken,
		command.DeletionGeneration, databaseTime(r.now))
	if err != nil {
		return safeDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return practicevoice.ErrPersistenceConflict
	}
	return nil
}

func getQuestionTip(ctx context.Context, row queryRow, owner, sessionID, questionID string) (practicevoice.QuestionTip, error) {
	return scanQuestionTip(row.QueryRow(ctx, questionTipColumns+`
		WHERE owner_user_id = $1 AND practice_session_id = $2 AND question_id = $3`,
		owner, sessionID, questionID))
}

func getQuestionTipByKey(ctx context.Context, row queryRow, owner, key string) (practicevoice.QuestionTip, error) {
	return scanQuestionTip(row.QueryRow(ctx, questionTipColumns+`
		WHERE owner_user_id = $1 AND idempotency_key = $2`, owner, key))
}

func scanQuestionTip(row pgx.Row) (practicevoice.QuestionTip, error) {
	var tip practicevoice.QuestionTip
	err := row.Scan(&tip.ID, &tip.SessionID, &tip.QuestionID, &tip.IdempotencyKey,
		&tip.Status, &tip.FencingToken, &tip.DeletionGeneration,
		&tip.LeaseExpiresAt, &tip.Content, &tip.CreatedAt, &tip.UpdatedAt,
		&tip.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return practicevoice.QuestionTip{}, practicevoice.ErrPersistenceNotFound
	}
	if err != nil {
		return practicevoice.QuestionTip{}, safeDatabaseError(err)
	}
	return tip, nil
}

var _ practicevoice.QuestionTipStore = (*Repository)(nil)
