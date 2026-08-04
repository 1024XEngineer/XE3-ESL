package evaluation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) DeleteUserData(
	ctx context.Context,
	command DeleteUserDataCommand,
) error {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(command.OwnerUserID) ||
		command.DeletionGeneration <= 0 {
		return ErrInvalidRequest
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
		return ErrAccountUnavailable
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
			return ErrNotFound
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
		return ErrDeletionGenerationStale
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

func lockActiveIdentityUser(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	deletionGeneration int64,
) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT owner.account_status
		FROM identity_users AS owner
		WHERE owner.id = $1
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_deletion_fences AS fence
		      WHERE fence.owner_user_id = owner.id
		        AND fence.deletion_generation >= $2
		  )
		FOR SHARE OF owner
	`, userID, deletionGeneration).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return ErrAccountUnavailable
	}
	return err
}
