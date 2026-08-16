package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
)

const questionTipColumns = `
	SELECT q.tip_id, q.session_id, q.question_id, q.tip_client_request_id,
	       q.tip_status, q.tip_fencing_token, q.tip_lease_expires_at,
	       COALESCE(q.tip_content, ''), q.tip_created_at, q.updated_at,
	       q.tip_completed_at
	FROM practice_questions q
	JOIN practice_sessions s ON s.session_id=q.session_id
`

func (r *Repository) ClaimQuestionTip(
	ctx context.Context,
	actor practiceinteraction.Actor,
	command practiceinteraction.ClaimQuestionTipCommand,
) (practiceinteraction.QuestionTip, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		len(command.IdempotencyKey) > 128 || command.LeaseDuration <= 0 {
		return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practiceinteraction.QuestionTip{}, err
	}
	if err := lockKey(ctx, tx, actor.UserID, "question-tip", command.SessionID, command.QuestionID); err != nil {
		return practiceinteraction.QuestionTip{}, err
	}
	var keyedSession, keyedQuestion string
	err = tx.QueryRow(ctx, `
		SELECT q.session_id, q.question_id FROM practice_questions q
		JOIN practice_sessions s ON s.session_id=q.session_id
		WHERE s.user_id = $1 AND q.session_id = $2
		  AND q.tip_client_request_id = $3
	`, actor.UserID, command.SessionID, command.IdempotencyKey).
		Scan(&keyedSession, &keyedQuestion)
	if err == nil && keyedQuestion != command.QuestionID {
		return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	now := databaseTime(r.now)
	tip, err := getQuestionTip(ctx, tx, actor.UserID, command.SessionID, command.QuestionID, true)
	if errors.Is(err, practiceinteraction.ErrPersistenceNotFound) {
		var exists bool
		err := tx.QueryRow(ctx, `
			SELECT true FROM practice_questions q
			JOIN practice_sessions s ON s.session_id=q.session_id
			WHERE s.user_id = $1 AND q.session_id = $2 AND q.question_id = $3
			FOR UPDATE OF q
		`, actor.UserID, command.SessionID, command.QuestionID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceNotFound
		}
		if err != nil {
			return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
		}
		tipID, idErr := r.ids.NewID()
		if idErr != nil || !practice.ValidAggregateID(tipID) {
			return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceUnavailable
		}
		tip = practiceinteraction.QuestionTip{
			ID: tipID, SessionID: command.SessionID,
			QuestionID: command.QuestionID, IdempotencyKey: command.IdempotencyKey,
			Status: practiceinteraction.QuestionTipProcessing, FencingToken: 1,
			DeletionGeneration: 0, LeaseAcquired: true,
			LeaseExpiresAt: now.Add(command.LeaseDuration), CreatedAt: now,
			UpdatedAt: now,
		}
		_, err = tx.Exec(ctx, `
			UPDATE practice_questions q
			SET tip_id = $4, tip_client_request_id = $5,
			    tip_status = 'processing', tip_fencing_token = 1,
			    tip_lease_expires_at = $6, tip_created_at = $7, updated_at = $7
			FROM practice_sessions s
			WHERE s.session_id=q.session_id AND s.user_id = $1
			  AND q.session_id = $2 AND q.question_id = $3
		`, actor.UserID, command.SessionID, command.QuestionID, tip.ID,
			command.IdempotencyKey, tip.LeaseExpiresAt, now)
		if err != nil {
			return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
		}
	} else if err != nil {
		return practiceinteraction.QuestionTip{}, err
	} else if tip.Status != practiceinteraction.QuestionTipCompleted &&
		(tip.Status == practiceinteraction.QuestionTipFailed || !tip.LeaseExpiresAt.After(now)) {
		tip.Status = practiceinteraction.QuestionTipProcessing
		tip.FencingToken++
		tip.IdempotencyKey = command.IdempotencyKey
		tip.LeaseAcquired = true
		tip.LeaseExpiresAt = now.Add(command.LeaseDuration)
		tip.UpdatedAt = now
		_, err = tx.Exec(ctx, `
			UPDATE practice_questions q
			SET tip_client_request_id = $4, tip_status = 'processing',
			    tip_fencing_token = $5, tip_lease_expires_at = $6,
			    tip_content = NULL, tip_provider = NULL, tip_model = NULL,
			    tip_provider_request_id = NULL, tip_completed_at = NULL,
			    updated_at = $7
			FROM practice_sessions s
			WHERE s.session_id=q.session_id AND s.user_id = $1
			  AND q.session_id = $2 AND q.question_id = $3
		`, actor.UserID, command.SessionID, command.QuestionID,
			command.IdempotencyKey, tip.FencingToken, tip.LeaseExpiresAt, now)
		if err != nil {
			return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	return tip, nil
}

func (r *Repository) GetQuestionTip(
	ctx context.Context,
	actor practiceinteraction.Actor,
	sessionID, questionID string,
) (practiceinteraction.QuestionTip, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(sessionID) == "" || strings.TrimSpace(questionID) == "" {
		return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practiceinteraction.QuestionTip{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tip, err := getQuestionTip(ctx, tx, actor.UserID, sessionID, questionID, false)
	if err != nil {
		return practiceinteraction.QuestionTip{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	return tip, nil
}

func (r *Repository) CompleteQuestionTip(
	ctx context.Context,
	actor practiceinteraction.Actor,
	command practiceinteraction.CompleteQuestionTipCommand,
) (practiceinteraction.QuestionTip, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(command.TipID) == "" || command.FencingToken <= 0 ||
		command.DeletionGeneration != 0 || strings.TrimSpace(command.Content) == "" ||
		strings.TrimSpace(command.Provider) == "" || !modelid.Valid(command.Model) ||
		strings.TrimSpace(command.ProviderRequestID) == "" {
		return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practiceinteraction.QuestionTip{}, err
	}
	tip, err := scanQuestionTip(tx.QueryRow(ctx, questionTipColumns+`
		WHERE s.user_id = $1 AND q.tip_id = $2 FOR UPDATE OF q
	`, actor.UserID, command.TipID))
	if err != nil {
		return practiceinteraction.QuestionTip{}, err
	}
	content := strings.TrimSpace(command.Content)
	if tip.Status == practiceinteraction.QuestionTipCompleted {
		if tip.Content != content {
			return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
		}
		return tip, nil
	}
	now := databaseTime(r.now)
	if tip.Status != practiceinteraction.QuestionTipProcessing ||
		tip.FencingToken != command.FencingToken ||
		tip.LeaseExpiresAt.IsZero() || !tip.LeaseExpiresAt.After(now) {
		return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceConflict
	}
	_, err = tx.Exec(ctx, `
		UPDATE practice_questions q
		SET tip_status = 'completed', tip_content = $3, tip_provider = $4,
		    tip_model = $5, tip_provider_request_id = $6,
		    tip_lease_expires_at = NULL, tip_completed_at = $7, updated_at = $7
		FROM practice_sessions s
		WHERE s.session_id=q.session_id AND s.user_id = $1 AND q.tip_id = $2
	`, actor.UserID, command.TipID, content, command.Provider, command.Model,
		command.ProviderRequestID, now)
	if err != nil {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	tip.Status = practiceinteraction.QuestionTipCompleted
	tip.Content = content
	tip.UpdatedAt = now
	tip.CompletedAt = &now
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	return tip, nil
}

func (r *Repository) FailQuestionTip(
	ctx context.Context,
	actor practiceinteraction.Actor,
	command practiceinteraction.FailQuestionTipCommand,
) error {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(command.TipID) == "" || command.FencingToken <= 0 ||
		command.DeletionGeneration != 0 {
		return practiceinteraction.ErrPersistenceInvalid
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE practice_questions q SET tip_status = 'failed',
		    tip_lease_expires_at = NULL, updated_at = $4
		FROM practice_sessions s
		WHERE s.session_id=q.session_id AND s.user_id = $1 AND q.tip_id = $2
		  AND q.tip_status = 'processing' AND q.tip_fencing_token = $3
	`, actor.UserID, command.TipID, command.FencingToken, databaseTime(r.now))
	if err != nil {
		return safeDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return practiceinteraction.ErrPersistenceConflict
	}
	return nil
}

func getQuestionTip(
	ctx context.Context,
	database queryRow,
	ownerUserID, sessionID, questionID string,
	lock bool,
) (practiceinteraction.QuestionTip, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF q"
	}
	return scanQuestionTip(database.QueryRow(ctx, questionTipColumns+`
		WHERE s.user_id = $1 AND q.session_id = $2 AND q.question_id = $3
		  AND q.tip_status IS NOT NULL
	`+suffix, ownerUserID, sessionID, questionID))
}

func scanQuestionTip(row rowScanner) (practiceinteraction.QuestionTip, error) {
	var tip practiceinteraction.QuestionTip
	var leaseExpiresAt sql.NullTime
	err := row.Scan(
		&tip.ID, &tip.SessionID, &tip.QuestionID, &tip.IdempotencyKey,
		&tip.Status, &tip.FencingToken, &leaseExpiresAt, &tip.Content,
		&tip.CreatedAt, &tip.UpdatedAt, &tip.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinteraction.QuestionTip{}, practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return practiceinteraction.QuestionTip{}, safeDatabaseError(err)
	}
	tip.DeletionGeneration = 0
	tip.CreatedAt = tip.CreatedAt.UTC()
	tip.UpdatedAt = tip.UpdatedAt.UTC()
	if leaseExpiresAt.Valid {
		tip.LeaseExpiresAt = leaseExpiresAt.Time.UTC()
	}
	if tip.CompletedAt != nil {
		completedAt := tip.CompletedAt.UTC()
		tip.CompletedAt = &completedAt
	}
	return tip, nil
}

var _ practiceinteraction.QuestionTipStore = (*Repository)(nil)
