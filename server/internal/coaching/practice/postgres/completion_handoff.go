package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func (r *Repository) ClaimCompletionHandoff(
	ctx context.Context,
	leaseDuration time.Duration,
	maxAttempts int,
) (practice.CompletionHandoffClaim, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		leaseDuration < time.Second || leaseDuration > 10*time.Minute ||
		maxAttempts < 1 || maxAttempts > 10 {
		return practice.CompletionHandoffClaim{}, false,
			practice.ErrCompletionHandoffInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return practice.CompletionHandoffClaim{}, false,
			fmt.Errorf("begin Practice Evaluation handoff claim: %w", err)
	}
	defer rollback(ctx, tx)

	var claim practice.CompletionHandoffClaim
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT completed.owner_user_id, completed.session_id
			FROM practice_completed AS completed
			JOIN identity_users AS owner
			  ON owner.id = completed.owner_user_id
			LEFT JOIN practice_deletion_fences AS fence
			  ON fence.owner_user_id = completed.owner_user_id
			WHERE owner.account_status = 'active'
			  AND fence.owner_user_id IS NULL
			  AND completed.attempt_count < $1
			  AND (
			      (
			          completed.delivery_status = 'PENDING'
			          AND completed.available_at <= transaction_timestamp()
			      )
			      OR (
			          completed.delivery_status = 'RUNNING'
			          AND completed.lease_expires_at <= clock_timestamp()
			      )
			  )
			ORDER BY
				completed.available_at,
				completed.created_at,
				completed.owner_user_id,
				completed.session_id
			FOR UPDATE OF completed SKIP LOCKED
			LIMIT 1
		), claimed AS (
			UPDATE practice_completed AS completed
			SET delivery_status = 'RUNNING',
			    attempt_count = completed.attempt_count + 1,
			    fencing_token = completed.fencing_token + 1,
			    lease_expires_at =
			        clock_timestamp() + make_interval(secs => $2),
			    failure_code = NULL,
			    failure_retryable = NULL,
			    updated_at = transaction_timestamp()
			FROM candidate
			WHERE completed.owner_user_id = candidate.owner_user_id
			  AND completed.session_id = candidate.session_id
			RETURNING completed.*
		)
		SELECT
			claimed.owner_user_id::text,
			claimed.session_id,
			claimed.final_turn_id,
			claimed.session_version,
			claimed.completion_token,
			claimed.created_at,
			session.scene_family,
			session.scene_model,
			claimed.attempt_count,
			claimed.fencing_token,
			claimed.lease_expires_at
		FROM claimed
		JOIN practice_sessions AS session
		  ON session.owner_user_id = claimed.owner_user_id
		 AND session.session_id = claimed.session_id
		 AND session.status = 'completed'
	`, maxAttempts, leaseDuration.Seconds()).Scan(
		&claim.OwnerUserID,
		&claim.Completion.SessionID,
		&claim.Completion.FinalTurnID,
		&claim.Completion.SessionVersion,
		&claim.Completion.CompletionToken,
		&claim.Completion.CreatedAt,
		&claim.SceneFamily,
		&claim.SceneModel,
		&claim.AttemptCount,
		&claim.FencingToken,
		&claim.LeaseExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.CompletionHandoffClaim{}, false, nil
	}
	if err != nil {
		return practice.CompletionHandoffClaim{}, false,
			fmt.Errorf("claim Practice Evaluation handoff: %w", err)
	}
	claim.Completion.CreatedAt = claim.Completion.CreatedAt.UTC()
	claim.LeaseExpiresAt = claim.LeaseExpiresAt.UTC()
	if !claim.Valid() {
		return practice.CompletionHandoffClaim{}, false,
			practice.ErrCompletionHandoffInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.CompletionHandoffClaim{}, false,
			fmt.Errorf("commit Practice Evaluation handoff claim: %w", err)
	}
	return claim, true, nil
}

func (r *Repository) CompleteCompletionHandoff(
	ctx context.Context,
	claim practice.CompletionHandoffClaim,
) error {
	if r == nil || r.pool == nil || ctx == nil || !claim.Valid() {
		return practice.ErrCompletionHandoffInvalid
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE practice_completed
		SET delivery_status = 'DELIVERED',
		    lease_expires_at = NULL,
		    failure_code = NULL,
		    failure_retryable = NULL,
		    updated_at = transaction_timestamp(),
		    delivered_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND session_id = $2
		  AND completion_token = $3
		  AND delivery_status = 'RUNNING'
		  AND attempt_count = $4
		  AND fencing_token = $5
		  AND lease_expires_at > clock_timestamp()
	`, claim.OwnerUserID, claim.Completion.SessionID,
		claim.Completion.CompletionToken, claim.AttemptCount,
		claim.FencingToken)
	if err != nil {
		return fmt.Errorf("complete Practice Evaluation handoff: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return practice.ErrCompletionHandoffClaimLost
	}
	return nil
}

func (r *Repository) FailCompletionHandoff(
	ctx context.Context,
	claim practice.CompletionHandoffClaim,
	failure practice.CompletionHandoffFailure,
	retryDelay time.Duration,
	maxAttempts int,
) error {
	if r == nil || r.pool == nil || ctx == nil || !claim.Valid() ||
		!failure.Valid() || retryDelay < 0 || retryDelay > time.Hour ||
		maxAttempts < 1 || maxAttempts > 10 {
		return practice.ErrCompletionHandoffInvalid
	}
	retry := failure.Retryable && claim.AttemptCount < maxAttempts
	tag, err := r.pool.Exec(ctx, `
		UPDATE practice_completed
		SET delivery_status = CASE
		        WHEN $6::boolean THEN 'PENDING'
		        ELSE 'FAILED'
		    END,
		    lease_expires_at = NULL,
		    available_at = CASE
		        WHEN $6::boolean
		        THEN transaction_timestamp() + make_interval(secs => $7)
		        ELSE available_at
		    END,
		    failure_code = CASE WHEN $6::boolean THEN NULL ELSE $4 END,
		    failure_retryable = CASE WHEN $6::boolean THEN NULL ELSE $5 END,
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND session_id = $2
		  AND completion_token = $3
		  AND delivery_status = 'RUNNING'
		  AND attempt_count = $8
		  AND fencing_token = $9
		  AND lease_expires_at > clock_timestamp()
	`, claim.OwnerUserID, claim.Completion.SessionID,
		claim.Completion.CompletionToken, failure.Code,
		failure.Retryable, retry, retryDelay.Seconds(),
		claim.AttemptCount, claim.FencingToken)
	if err != nil {
		return fmt.Errorf("fail Practice Evaluation handoff: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return practice.ErrCompletionHandoffClaimLost
	}
	return nil
}

var _ practice.CompletionHandoffRepository = (*Repository)(nil)
