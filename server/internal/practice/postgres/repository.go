// Package postgres implements Practice's production PostgreSQL repository.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) DeleteUserData(
	ctx context.Context,
	deletion persistence.DeletionContext,
) error {
	if r == nil || r.pool == nil || !validUserID(deletion.UserID) ||
		deletion.Generation == 0 || deletion.Generation > math.MaxInt64 {
		return persistence.ErrInvalidArgument
	}
	generation := int64(deletion.Generation)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin delete practice user data: %w", err)
	}
	defer rollback(ctx, tx)

	var accountStatus string
	err = tx.QueryRow(ctx, `
		SELECT account_status
		FROM identity_users
		WHERE id = $1
		FOR SHARE
	`, deletion.UserID).Scan(&accountStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		var fenceExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM practice_deletion_fences
				WHERE owner_user_id = $1
			)
		`, deletion.UserID).Scan(&fenceExists); err != nil {
			return fmt.Errorf("verify existing practice deletion fence: %w", err)
		}
		if !fenceExists {
			return persistence.ErrNotFound
		}
	} else if err != nil {
		return fmt.Errorf("verify practice deletion identity state: %w", err)
	} else if accountStatus != "deleting" && accountStatus != "deleted" {
		return persistence.ErrNotFound
	}

	var currentGeneration int64
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_deletion_fences (
			owner_user_id, deletion_generation
		)
		VALUES ($1, $2)
		ON CONFLICT (owner_user_id) DO UPDATE
		SET deletion_generation = GREATEST(
		        practice_deletion_fences.deletion_generation,
		        EXCLUDED.deletion_generation
		    ),
		    updated_at = CASE
		        WHEN EXCLUDED.deletion_generation >
		             practice_deletion_fences.deletion_generation
		        THEN transaction_timestamp()
		        ELSE practice_deletion_fences.updated_at
		    END
		RETURNING deletion_generation
	`, deletion.UserID, generation).Scan(&currentGeneration)
	if err != nil {
		return fmt.Errorf("upsert practice deletion fence: %w", err)
	}
	if generation < currentGeneration {
		return persistence.ErrDeletionGeneration
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM practice_idempotency_records
		WHERE owner_user_id = $1
	`, deletion.UserID); err != nil {
		return fmt.Errorf("delete Practice idempotency records: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM practice_sessions WHERE owner_user_id = $1
	`, deletion.UserID); err != nil {
		return fmt.Errorf("delete Practice Sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit practice user deletion: %w", err)
	}
	return nil
}

func loadTurnResult(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	turnID string,
) (persistence.TurnResult, []byte, error) {
	var result persistence.TurnResult
	var fingerprint []byte
	err := tx.QueryRow(ctx, `
		SELECT
			result.session_id, result.turn_id,
			result.payload_fingerprint, result.round_number,
			result.effective_turns, result.session_version,
			snapshot.turn_limit, result.completed,
			result.completion_token, result.created_at
		FROM practice_turn_results AS result
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = result.owner_user_id
		 AND snapshot.session_id = result.session_id
		WHERE result.owner_user_id = $1 AND result.turn_id = $2
	`, ownerUserID, turnID).Scan(
		&result.SessionID,
		&result.TurnID,
		&fingerprint,
		&result.Round,
		&result.EffectiveTurns,
		&result.SessionVersion,
		&result.TurnLimit,
		&result.Completed,
		&result.CompletionToken,
		&result.CreatedAt,
	)
	if err != nil {
		return persistence.TurnResult{}, nil, err
	}
	return result, fingerprint, nil
}

// lockActiveActor joins a Practice write to Identity's account-deletion
// fence without copying account state into Practice. FOR SHARE conflicts with
// the coordinator's account-status update, so either this write commits first
// or it observes the durable non-active status and fails closed.
func lockActiveActor(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
) error {
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT true
		FROM identity_users AS owner
		WHERE owner.id = $1
		  AND owner.account_status = 'active'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM practice_deletion_fences AS fence
		      WHERE fence.owner_user_id = owner.id
		  )
		FOR SHARE OF owner
	`, userID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("verify active practice actor: %w", err)
	}
	return nil
}

func validActor(actor persistence.Actor) bool {
	return validUserID(actor.UserID) && validUserID(actor.SessionID)
}

func validUserID(userID string) bool {
	var parsed pgtype.UUID
	return parsed.Scan(strings.TrimSpace(userID)) == nil && parsed.Valid
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func classifyWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return persistence.ErrNotFound
		case "23505":
			if postgresError.ConstraintName ==
				"practice_turn_results_owner_turn_key" {
				return persistence.ErrIdempotencyConflict
			}
			return persistence.ErrConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
