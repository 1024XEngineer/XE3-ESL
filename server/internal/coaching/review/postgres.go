package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

type rowScanner interface {
	Scan(dest ...any) error
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *PostgresRepository) DeleteUserData(
	ctx context.Context,
	command DeleteUserReviewsCommand,
) error {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(command.UserID) || command.DeletionGeneration <= 0 {
		return ErrInvalidReview
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Review user deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var accountStatus string
	err = tx.QueryRow(ctx, `
		SELECT account_status
		FROM identity_users
		WHERE id = $1
		FOR UPDATE
	`, command.UserID).Scan(&accountStatus)
	identityMissing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !identityMissing {
		return fmt.Errorf("lock Review deletion owner: %w", err)
	}
	if !identityMissing && accountStatus != "deleting" &&
		accountStatus != "deleted" {
		return ErrInvalidReview
	}

	var persistedGeneration int64
	err = tx.QueryRow(ctx, `
		SELECT deletion_generation
		FROM review_deletion_fences
		WHERE owner_user_id = $1
		FOR UPDATE
	`, command.UserID).Scan(&persistedGeneration)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read Review deletion fence: %w", err)
	}
	if err == nil && command.DeletionGeneration < persistedGeneration {
		return ErrDeletionGenerationStale
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO review_deletion_fences (
			owner_user_id,
			deletion_generation,
			deleted_at
		)
		VALUES ($1, $2, transaction_timestamp())
		ON CONFLICT (owner_user_id) DO UPDATE
		SET deletion_generation = GREATEST(
		        review_deletion_fences.deletion_generation,
		        EXCLUDED.deletion_generation
		    ),
		    deleted_at = LEAST(
		        review_deletion_fences.deleted_at,
		        EXCLUDED.deleted_at
		    )
	`, command.UserID, command.DeletionGeneration); err != nil {
		return fmt.Errorf("upsert Review deletion fence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM review_repractice_requests
		WHERE owner_user_id = $1
	`, command.UserID); err != nil {
		return fmt.Errorf("delete Review repractice requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Review user deletion: %w", err)
	}
	return nil
}

func lockActiveIdentityUser(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	deletionGeneration int64,
) error {
	if ctx == nil || tx == nil || !validUUID(userID) ||
		deletionGeneration < 0 {
		return ErrInvalidReview
	}
	var status string
	err := tx.QueryRow(ctx, `
		SELECT users.account_status
		FROM identity_users AS users
		WHERE users.id = $1
		  AND NOT EXISTS (
		      SELECT 1
		      FROM review_deletion_fences AS fence
		      WHERE fence.owner_user_id = users.id
		        AND fence.deletion_generation >= $2
		  )
		FOR SHARE OF users
	`, userID, deletionGeneration).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return ErrAccountDeleted
	}
	if err != nil {
		return fmt.Errorf("lock active Review owner: %w", err)
	}
	return nil
}
