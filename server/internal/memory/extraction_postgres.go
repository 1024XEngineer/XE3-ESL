package memory

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (repository *PostgresRepository) ClaimExtraction(
	ctx context.Context,
	configuration ExtractionConfig,
) (ExtractionClaim, bool, error) {
	if ctx == nil || !configuration.Valid() {
		return ExtractionClaim{}, false, ErrInvalidArgument
	}
	leaseToken, err := repository.newID()
	if err != nil {
		return ExtractionClaim{}, false, err
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return ExtractionClaim{}, false, ErrRepository
	}
	defer rollback(ctx, tx)

	exhausted, err := tx.Exec(ctx, `
UPDATE agent_memory_extraction_jobs
SET
    status = 'failed',
    lease_token = NULL,
    lease_expires_at = NULL,
    failure_kind = 'attempts_exhausted',
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE status = 'running'
  AND lease_expires_at <= clock_timestamp()
  AND attempt_count >= $1`,
		configuration.MaxAttempts,
	)
	if err != nil {
		return ExtractionClaim{}, false, mapPostgresError(err)
	}
	if exhausted.RowsAffected() > 0 {
		if err := tx.Commit(ctx); err != nil {
			return ExtractionClaim{}, false, ErrRepository
		}
		return ExtractionClaim{}, false, ErrExtractionExhausted
	}

	job, err := scanExtractionJob(tx.QueryRow(ctx, `
WITH next_job AS (
    SELECT source_run_id
    FROM agent_memory_extraction_jobs
    WHERE (
            status = 'pending'
            AND next_attempt_at <= transaction_timestamp()
        )
        OR (
            status = 'running'
            AND lease_expires_at <= clock_timestamp()
            AND attempt_count < $1
        )
    ORDER BY
        CASE WHEN status = 'running' THEN 0 ELSE 1 END,
        next_attempt_at,
        source_completed_at,
        source_run_id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE agent_memory_extraction_jobs AS jobs
SET
    status = 'running',
    attempt_count = jobs.attempt_count + 1,
    lease_token = $2,
    lease_expires_at = transaction_timestamp() + make_interval(secs => $3),
    policy_version = $4,
    prompt_version = $5,
    provider = $6,
    model = $7,
    failure_kind = NULL,
    updated_at = transaction_timestamp()
FROM next_job
WHERE jobs.source_run_id = next_job.source_run_id
RETURNING `+extractionJobColumns("jobs"),
		configuration.MaxAttempts,
		leaseToken,
		configuration.LeaseDuration.Seconds(),
		configuration.PolicyVersion,
		configuration.PromptVersion,
		configuration.Provider,
		configuration.Model,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return ExtractionClaim{}, false, ErrRepository
		}
		return ExtractionClaim{}, false, nil
	}
	if err != nil {
		return ExtractionClaim{}, false, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ExtractionClaim{}, false, ErrRepository
	}
	claim := ExtractionClaim{ExtractionJob: job}
	if !claim.Valid() {
		return ExtractionClaim{}, false, ErrRepository
	}
	return claim, true, nil
}

func (repository *PostgresRepository) CompleteExtraction(
	ctx context.Context,
	claim ExtractionClaim,
	batch ExtractionBatch,
) (ExtractionJob, error) {
	if ctx == nil || !claim.Valid() || !batch.Valid() ||
		batch.Source.Type != SourceAgentRun ||
		batch.Source.SourceID != claim.RunID ||
		batch.Source.Version != int64(claim.SourceAttempt) {
		return ExtractionJob{}, ErrInvalidArgument
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return ExtractionJob{}, ErrRepository
	}
	defer rollback(ctx, tx)
	if err := lockActiveOwner(ctx, tx, claim.OwnerID); err != nil {
		return ExtractionJob{}, err
	}
	if err := verifyExtractionClaim(ctx, tx, claim); err != nil {
		return ExtractionJob{}, err
	}

	applied := 0
	for _, decision := range batch.Decisions {
		if decision.Scope == ScopeMatter {
			if err := requireOwnedMatter(
				ctx,
				tx,
				claim.OwnerID,
				decision.MatterID,
			); err != nil {
				return ExtractionJob{}, err
			}
		}
		decisionApplied, err := repository.applyDecision(
			ctx,
			tx,
			claim,
			decision,
			batch.Source,
		)
		if err != nil {
			return ExtractionJob{}, err
		}
		if decisionApplied {
			applied++
		}
	}
	rejected := batch.CandidateCount - applied
	job, err := scanExtractionJob(tx.QueryRow(ctx, `
UPDATE agent_memory_extraction_jobs
SET
    status = 'completed',
    lease_token = NULL,
    lease_expires_at = NULL,
    candidate_count = $4,
    applied_count = $5,
    rejected_count = $6,
    failure_kind = NULL,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE source_run_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
RETURNING `+extractionJobColumns("agent_memory_extraction_jobs"),
		claim.RunID,
		claim.OwnerID,
		claim.LeaseToken,
		batch.CandidateCount,
		applied,
		rejected,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtractionJob{}, ErrConflict
	}
	if err != nil {
		return ExtractionJob{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ExtractionJob{}, ErrRepository
	}
	return job, nil
}

func (repository *PostgresRepository) FailExtraction(
	ctx context.Context,
	claim ExtractionClaim,
	failureKind string,
	retryable bool,
	configuration ExtractionConfig,
) (ExtractionJob, error) {
	if ctx == nil || !claim.Valid() ||
		!stableFailurePattern.MatchString(failureKind) ||
		!configuration.Valid() {
		return ExtractionJob{}, ErrInvalidArgument
	}
	requeue := retryable && claim.AttemptCount < configuration.MaxAttempts
	backoff := extractionBackoff(claim.AttemptCount)
	status := ExtractionFailed
	if requeue {
		status = ExtractionPending
	}
	job, err := scanExtractionJob(repository.database.QueryRow(ctx, `
UPDATE agent_memory_extraction_jobs
SET
    status = $4,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = CASE
        WHEN $4 = 'pending'
        THEN transaction_timestamp() + make_interval(secs => $5)
        ELSE next_attempt_at
    END,
    failure_kind = $6,
    updated_at = transaction_timestamp(),
    completed_at = CASE
        WHEN $4 = 'pending' THEN NULL
        ELSE transaction_timestamp()
    END
WHERE source_run_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > transaction_timestamp()
RETURNING `+extractionJobColumns("agent_memory_extraction_jobs"),
		claim.RunID,
		claim.OwnerID,
		claim.LeaseToken,
		status,
		backoff.Seconds(),
		failureKind,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtractionJob{}, ErrConflict
	}
	if err != nil {
		return ExtractionJob{}, mapPostgresError(err)
	}
	return job, nil
}

func (repository *PostgresRepository) DiscardExtraction(
	ctx context.Context,
	claim ExtractionClaim,
	failureKind string,
) (ExtractionJob, error) {
	if ctx == nil || !claim.Valid() ||
		!stableFailurePattern.MatchString(failureKind) {
		return ExtractionJob{}, ErrInvalidArgument
	}
	job, err := scanExtractionJob(repository.database.QueryRow(ctx, `
UPDATE agent_memory_extraction_jobs
SET
    status = 'discarded',
    lease_token = NULL,
    lease_expires_at = NULL,
    failure_kind = $4,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE source_run_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
RETURNING `+extractionJobColumns("agent_memory_extraction_jobs"),
		claim.RunID,
		claim.OwnerID,
		claim.LeaseToken,
		failureKind,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtractionJob{}, ErrConflict
	}
	if err != nil {
		return ExtractionJob{}, mapPostgresError(err)
	}
	return job, nil
}

func (repository *PostgresRepository) applyDecision(
	ctx context.Context,
	tx pgx.Tx,
	claim ExtractionClaim,
	decision MemoryDecision,
	source SourceInput,
) (bool, error) {
	item, found, err := findActiveDecisionMemory(
		ctx,
		tx,
		claim.OwnerID,
		decision,
	)
	if err != nil {
		return false, err
	}
	if decision.Action == CandidateInactivate {
		if !found {
			return false, nil
		}
		item, err = scanMemory(tx.QueryRow(ctx, `
UPDATE agent_memories
SET
    status = 'inactive',
    version = version + 1,
    inactivated_at = GREATEST(
        transaction_timestamp(),
        updated_at + INTERVAL '1 microsecond'
    ),
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2 AND status = 'active'
RETURNING
    id::text,
    owner_user_id::text,
    memory_type,
    canonical_key,
    content,
    scope_type,
    coalesce(matter_id::text, ''),
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at,
    inactivated_at`,
			item.ID,
			claim.OwnerID,
		))
		if err != nil {
			return false, mapPostgresError(err)
		}
	} else if found {
		item, err = scanMemory(tx.QueryRow(ctx, `
UPDATE agent_memories
SET
    content = $3,
    policy_version = $4,
    expires_at = $5,
    version = version + 1,
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2 AND status = 'active'
RETURNING
    id::text,
    owner_user_id::text,
    memory_type,
    canonical_key,
    content,
    scope_type,
    coalesce(matter_id::text, ''),
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at,
    inactivated_at`,
			item.ID,
			claim.OwnerID,
			decision.Content,
			claim.PolicyVersion,
			decision.ExpiresAt,
		))
		if err != nil {
			return false, mapPostgresError(err)
		}
	} else {
		memoryID, err := repository.newID()
		if err != nil {
			return false, err
		}
		item, err = scanMemory(tx.QueryRow(ctx, `
INSERT INTO agent_memories (
    id,
    owner_user_id,
    memory_type,
    canonical_key,
    content,
    scope_type,
    matter_id,
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    'active', 1, $8, $9,
    transaction_timestamp(),
    transaction_timestamp()
)
RETURNING
    id::text,
    owner_user_id::text,
    memory_type,
    canonical_key,
    content,
    scope_type,
    coalesce(matter_id::text, ''),
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at,
    inactivated_at`,
			memoryID,
			claim.OwnerID,
			decision.Type,
			decision.CanonicalKey,
			decision.Content,
			decision.Scope,
			nullableUUID(decision.MatterID),
			claim.PolicyVersion,
			decision.ExpiresAt,
		))
		if err != nil {
			return false, mapPostgresError(err)
		}
	}

	sourceID, err := repository.newID()
	if err != nil {
		return false, err
	}
	if err := insertSource(
		ctx,
		tx,
		sourceID,
		claim.OwnerID,
		item.ID,
		source,
	); err != nil {
		return false, err
	}
	return true, nil
}

func findActiveDecisionMemory(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	decision MemoryDecision,
) (Memory, bool, error) {
	item, err := scanMemory(tx.QueryRow(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    memory_type,
    canonical_key,
    content,
    scope_type,
    coalesce(matter_id::text, ''),
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at,
    inactivated_at
FROM agent_memories
WHERE owner_user_id = $1
  AND memory_type = $2
  AND canonical_key = $3
  AND scope_type = $4
  AND matter_id IS NOT DISTINCT FROM $5::uuid
  AND status = 'active'
FOR UPDATE`,
		ownerID,
		decision.Type,
		decision.CanonicalKey,
		decision.Scope,
		nullableUUID(decision.MatterID),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, mapPostgresError(err)
	}
	return item, true, nil
}

func verifyExtractionClaim(
	ctx context.Context,
	tx pgx.Tx,
	claim ExtractionClaim,
) error {
	var attemptCount int
	err := tx.QueryRow(ctx, `
SELECT attempt_count
FROM agent_memory_extraction_jobs
WHERE source_run_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
FOR UPDATE`,
		claim.RunID,
		claim.OwnerID,
		claim.LeaseToken,
	).Scan(&attemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return ErrRepository
	}
	if attemptCount != claim.AttemptCount {
		return ErrConflict
	}
	return nil
}

func extractionBackoff(attempt int) time.Duration {
	if attempt < 1 {
		return time.Second
	}
	backoff := time.Second << min(attempt-1, 8)
	if backoff > 5*time.Minute {
		return 5 * time.Minute
	}
	return backoff
}

func extractionJobColumns(alias string) string {
	return `
    ` + alias + `.source_run_id::text,
    ` + alias + `.owner_user_id::text,
    ` + alias + `.source_thread_id::text,
    ` + alias + `.source_input_message_id::text,
    ` + alias + `.source_assistant_message_id::text,
    ` + alias + `.source_attempt,
    ` + alias + `.source_completed_at,
    ` + alias + `.status,
    ` + alias + `.attempt_count,
    coalesce(` + alias + `.lease_token::text, ''),
    ` + alias + `.lease_expires_at,
    ` + alias + `.next_attempt_at,
    coalesce(` + alias + `.policy_version, ''),
    coalesce(` + alias + `.prompt_version, ''),
    coalesce(` + alias + `.provider, ''),
    coalesce(` + alias + `.model, ''),
    coalesce(` + alias + `.candidate_count, 0),
    coalesce(` + alias + `.applied_count, 0),
    coalesce(` + alias + `.rejected_count, 0),
    coalesce(` + alias + `.failure_kind, ''),
    ` + alias + `.created_at,
    ` + alias + `.updated_at,
    ` + alias + `.completed_at`
}

func scanExtractionJob(row rowScanner) (ExtractionJob, error) {
	var job ExtractionJob
	var status string
	var leaseExpiresAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	if err := row.Scan(
		&job.RunID,
		&job.OwnerID,
		&job.ThreadID,
		&job.InputMessageID,
		&job.AssistantMessageID,
		&job.SourceAttempt,
		&job.SourceCompletedAt,
		&status,
		&job.AttemptCount,
		&job.LeaseToken,
		&leaseExpiresAt,
		&job.NextAttemptAt,
		&job.PolicyVersion,
		&job.PromptVersion,
		&job.Provider,
		&job.Model,
		&job.CandidateCount,
		&job.AppliedCount,
		&job.RejectedCount,
		&job.FailureKind,
		&job.CreatedAt,
		&job.UpdatedAt,
		&completedAt,
	); err != nil {
		return ExtractionJob{}, err
	}
	job.Status = ExtractionStatus(status)
	job.SourceCompletedAt = job.SourceCompletedAt.UTC()
	job.NextAttemptAt = job.NextAttemptAt.UTC()
	job.CreatedAt = job.CreatedAt.UTC()
	job.UpdatedAt = job.UpdatedAt.UTC()
	if leaseExpiresAt.Valid {
		job.LeaseExpiresAt = leaseExpiresAt.Time.UTC()
	}
	if completedAt.Valid {
		job.CompletedAt = completedAt.Time.UTC()
	}
	if !job.Status.Valid() ||
		!validUUID(job.RunID) ||
		!validUUID(job.OwnerID) ||
		!validUUID(job.ThreadID) ||
		!validUUID(job.InputMessageID) ||
		!validUUID(job.AssistantMessageID) ||
		job.SourceAttempt < 1 ||
		job.AttemptCount < 0 {
		return ExtractionJob{}, ErrRepository
	}
	return job, nil
}

var _ ExtractionRepository = (*PostgresRepository)(nil)
