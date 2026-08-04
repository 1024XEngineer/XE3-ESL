package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDeletionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresDeletionRepository(
	pool *pgxpool.Pool,
) *PostgresDeletionRepository {
	return &PostgresDeletionRepository{pool: pool}
}

func (r *PostgresDeletionRepository) DeleteUserData(
	ctx context.Context,
	command evaluation.DeleteUserDataCommand,
) error {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(command.OwnerUserID) ||
		command.DeletionGeneration <= 0 {
		return evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Evaluation user deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var accountStatus string
	err = tx.QueryRow(ctx, `
		SELECT account_status
		FROM identity_users
		WHERE id = $1
		FOR UPDATE
	`, command.OwnerUserID).Scan(&accountStatus)
	identityMissing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !identityMissing {
		return fmt.Errorf("lock Evaluation deletion owner: %w", err)
	}
	if !identityMissing &&
		accountStatus != "deleting" &&
		accountStatus != "deleted" {
		return evaluation.ErrAccountUnavailable
	}
	if identityMissing {
		var fenceExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM evaluation_deletion_fences
				WHERE owner_user_id = $1
			)
		`, command.OwnerUserID).Scan(&fenceExists); err != nil {
			return fmt.Errorf(
				"verify Evaluation deletion fence: %w",
				err,
			)
		}
		if !fenceExists {
			return evaluation.ErrNotFound
		}
	}

	var persistedGeneration int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO evaluation_deletion_fences (
			owner_user_id,
			deletion_generation
		)
		VALUES ($1, $2)
		ON CONFLICT (owner_user_id) DO UPDATE
		SET deletion_generation = GREATEST(
		        evaluation_deletion_fences.deletion_generation,
		        EXCLUDED.deletion_generation
		    ),
		    updated_at = CASE
		        WHEN EXCLUDED.deletion_generation >
		             evaluation_deletion_fences.deletion_generation
		        THEN transaction_timestamp()
		        ELSE evaluation_deletion_fences.updated_at
		    END
		RETURNING deletion_generation
	`, command.OwnerUserID, command.DeletionGeneration).Scan(
		&persistedGeneration,
	); err != nil {
		return fmt.Errorf("upsert Evaluation deletion fence: %w", err)
	}
	if command.DeletionGeneration < persistedGeneration {
		return evaluation.ErrDeletionGenerationStale
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM learning_profile_dimensions
		WHERE owner_user_id = $1
	`, command.OwnerUserID); err != nil {
		return fmt.Errorf(
			"delete Evaluation Learning Profile: %w",
			err,
		)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM evaluation_speech_feedbacks
		WHERE owner_user_id = $1
	`, command.OwnerUserID); err != nil {
		return fmt.Errorf(
			"delete Evaluation speech feedback: %w",
			err,
		)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM evaluation_speech_feedback_turn_snapshots
		WHERE owner_user_id = $1
	`, command.OwnerUserID); err != nil {
		return fmt.Errorf(
			"delete Evaluation speech feedback evidence snapshots: %w",
			err,
		)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM evaluation_module_runs
		WHERE owner_user_id = $1
	`, command.OwnerUserID); err != nil {
		return fmt.Errorf(
			"delete Evaluation module runs: %w",
			err,
		)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM evaluation_ledgers
		WHERE owner_user_id = $1
	`, command.OwnerUserID); err != nil {
		return fmt.Errorf("delete Evaluation ledgers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM evaluation_evidence_snapshots
		WHERE owner_user_id = $1
	`, command.OwnerUserID); err != nil {
		return fmt.Errorf("delete Evaluation evidence snapshots: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Evaluation user deletion: %w", err)
	}
	return nil
}
