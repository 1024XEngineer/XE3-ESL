package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Claim(
	ctx context.Context,
	configuration agentsummary.WorkerConfiguration,
) (agentsummary.Claim, bool, error) {
	if ctx == nil || !configuration.Valid() {
		return agentsummary.Claim{}, false, conversation.ErrInvalidRequest
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentsummary.Claim{}, false, conversation.ErrRepository
	}
	defer rollback(tx)

	// Select an owner without taking a row lock, then lock that owner before
	// touching any Thread row. Account deletion uses the same user -> domain-row
	// order, so summary claims cannot race an active account into deletion or
	// create the reverse half of a deadlock.
	var ownerID string
	err = tx.QueryRow(ctx, `
SELECT thread.user_id::text
FROM agent_threads AS thread
INNER JOIN users AS owner ON owner.id = thread.user_id
WHERE thread.deleted_at IS NULL AND owner.status = 'active'
  AND thread.summary_target_sequence IS NOT NULL
  AND thread.summary_target_sequence >
      COALESCE(thread.summary_through_sequence, 0)
  AND thread.summary_error IS NULL
  AND thread.summary_attempt_count < $1
  AND (
      (thread.summary_lease_token IS NULL
       AND thread.summary_available_at <= CURRENT_TIMESTAMP)
      OR thread.summary_lease_expires_at <= CURRENT_TIMESTAMP
  )
ORDER BY thread.summary_available_at, thread.updated_at, thread.id
LIMIT 1`, configuration.MaxAttempts).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return agentsummary.Claim{}, false, conversation.ErrRepository
		}
		return agentsummary.Claim{}, false, nil
	}
	if err != nil {
		return agentsummary.Claim{}, false, mapSummaryPostgresError(err)
	}
	active, err := lockActiveSummaryOwner(ctx, tx, ownerID)
	if err != nil {
		return agentsummary.Claim{}, false, err
	}
	if !active {
		if err := tx.Commit(ctx); err != nil {
			return agentsummary.Claim{}, false, conversation.ErrRepository
		}
		return agentsummary.Claim{}, false, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET summary_lease_token = NULL,
    summary_lease_expires_at = NULL,
    summary_error = 'attempts_exhausted',
    updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND summary_lease_token IS NOT NULL
  AND summary_lease_expires_at <= CURRENT_TIMESTAMP
  AND summary_attempt_count >= $2`, ownerID, configuration.MaxAttempts); err != nil {
		return agentsummary.Claim{}, false, mapSummaryPostgresError(err)
	}

	var claim agentsummary.Claim
	err = tx.QueryRow(ctx, `
WITH candidate AS (
    SELECT thread.id
    FROM agent_threads AS thread
    WHERE thread.user_id = $3
      AND thread.deleted_at IS NULL
      AND thread.summary_target_sequence IS NOT NULL
      AND thread.summary_target_sequence >
          COALESCE(thread.summary_through_sequence, 0)
      AND thread.summary_error IS NULL
      AND thread.summary_attempt_count < $1
      AND thread.summary_available_at <= CURRENT_TIMESTAMP
      AND (
          thread.summary_lease_token IS NULL
          OR thread.summary_lease_expires_at <= CURRENT_TIMESTAMP
      )
    ORDER BY thread.summary_available_at, thread.updated_at, thread.id
    FOR UPDATE OF thread SKIP LOCKED
    LIMIT 1
), claimed AS (
    UPDATE agent_threads AS thread
    SET summary_attempt_count = thread.summary_attempt_count + 1,
        summary_lease_token = gen_random_uuid(),
        summary_lease_expires_at =
            CURRENT_TIMESTAMP + make_interval(secs => $2),
        summary_error = NULL,
        updated_at = CURRENT_TIMESTAMP
    FROM candidate
    WHERE thread.id = candidate.id
    RETURNING thread.*
)
SELECT claimed.user_id::text, claimed.id::text,
       claimed.summary_target_sequence, claimed.summary_attempt_count,
       claimed.summary_lease_token::text, claimed.summary_lease_expires_at
FROM claimed`,
		configuration.MaxAttempts,
		configuration.LeaseDuration.Seconds(),
		ownerID,
	).Scan(
		&claim.OwnerID,
		&claim.ThreadID,
		&claim.TargetSequence,
		&claim.AttemptCount,
		&claim.LeaseToken,
		&claim.LeaseExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return agentsummary.Claim{}, false, conversation.ErrRepository
		}
		return agentsummary.Claim{}, false, nil
	}
	if err != nil {
		return agentsummary.Claim{}, false, mapSummaryPostgresError(err)
	}
	claim.LeaseExpiresAt = claim.LeaseExpiresAt.UTC()
	if !claim.Valid() {
		return agentsummary.Claim{}, false, conversation.ErrRepository
	}
	if err := tx.Commit(ctx); err != nil {
		return agentsummary.Claim{}, false, conversation.ErrRepository
	}
	return claim, true, nil
}

func lockActiveSummaryOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
) (bool, error) {
	var status string
	err := tx.QueryRow(ctx, `
SELECT status
FROM users
WHERE id = $1
FOR SHARE`, ownerID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapSummaryPostgresError(err)
	}
	return status == "active", nil
}

func (r *Repository) Complete(
	ctx context.Context,
	claim agentsummary.Claim,
	coveredThrough int64,
	content agentsummary.Content,
) error {
	if ctx == nil || !claim.Valid() || coveredThrough < 1 ||
		coveredThrough > claim.TargetSequence || !content.Valid() {
		return conversation.ErrInvalidRequest
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return conversation.ErrInvalidRequest
	}
	tag, err := r.database.Exec(ctx, `
UPDATE agent_threads
SET summary_content = $4::jsonb,
    summary_through_sequence = $5,
    summary_target_sequence = CASE
        WHEN summary_target_sequence > $5 THEN summary_target_sequence
        ELSE NULL
    END,
    summary_attempt_count = 0,
    summary_lease_token = NULL,
    summary_lease_expires_at = NULL,
    summary_available_at = CURRENT_TIMESTAMP,
    summary_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND user_id = $2
  AND summary_lease_token = $3
  AND summary_lease_expires_at > CURRENT_TIMESTAMP
  AND summary_target_sequence >= $5
  AND COALESCE(summary_through_sequence, 0) < $5`,
		claim.ThreadID,
		claim.OwnerID,
		claim.LeaseToken,
		encoded,
		coveredThrough,
	)
	if err != nil {
		return mapSummaryPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return conversation.ErrConflict
	}
	return nil
}

func (r *Repository) Skip(ctx context.Context, claim agentsummary.Claim) error {
	if ctx == nil || !claim.Valid() {
		return conversation.ErrInvalidRequest
	}
	tag, err := r.database.Exec(ctx, `
UPDATE agent_threads
SET summary_target_sequence = CASE
        WHEN summary_target_sequence > $4 THEN summary_target_sequence
        ELSE NULL
    END,
    summary_attempt_count = 0,
    summary_lease_token = NULL,
    summary_lease_expires_at = NULL,
    summary_available_at = CURRENT_TIMESTAMP,
    summary_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND user_id = $2
  AND summary_lease_token = $3
  AND summary_lease_expires_at > CURRENT_TIMESTAMP`,
		claim.ThreadID,
		claim.OwnerID,
		claim.LeaseToken,
		claim.TargetSequence,
	)
	if err != nil {
		return mapSummaryPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return conversation.ErrConflict
	}
	return nil
}

func (r *Repository) Fail(
	ctx context.Context,
	claim agentsummary.Claim,
	failureKind string,
	retryable bool,
	configuration agentsummary.WorkerConfiguration,
) (bool, error) {
	if ctx == nil || !claim.Valid() ||
		!failureKindPattern.MatchString(failureKind) || !configuration.Valid() {
		return false, conversation.ErrInvalidRequest
	}
	retryCurrent := retryable && claim.AttemptCount < configuration.MaxAttempts
	backoff := summaryBackoff(claim.AttemptCount)
	var retry bool
	err := r.database.QueryRow(ctx, `
UPDATE agent_threads
SET summary_attempt_count = CASE
        WHEN summary_target_sequence > $4 THEN 0
        ELSE summary_attempt_count
    END,
    summary_lease_token = NULL,
    summary_lease_expires_at = NULL,
    summary_available_at = CASE
        WHEN summary_target_sequence > $4 THEN CURRENT_TIMESTAMP
        WHEN $5 THEN CURRENT_TIMESTAMP + make_interval(secs => $6)
        ELSE summary_available_at
    END,
    summary_error = CASE
        WHEN summary_target_sequence > $4 OR $5 THEN NULL
        ELSE $7
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND user_id = $2
  AND summary_lease_token = $3
  AND summary_lease_expires_at > CURRENT_TIMESTAMP
RETURNING summary_error IS NULL AND summary_target_sequence IS NOT NULL`,
		claim.ThreadID,
		claim.OwnerID,
		claim.LeaseToken,
		claim.TargetSequence,
		retryCurrent,
		backoff.Seconds(),
		failureKind,
	).Scan(&retry)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, conversation.ErrConflict
	}
	if err != nil {
		return false, mapSummaryPostgresError(err)
	}
	return retry, nil
}

func summaryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		return time.Second
	}
	backoff := time.Second << min(attempt-1, 8)
	if backoff > 5*time.Minute {
		return 5 * time.Minute
	}
	return backoff
}

var failureKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
