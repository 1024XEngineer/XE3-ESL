package memory

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (repository *PostgresRepository) ClaimIndex(
	ctx context.Context,
	configuration IndexConfig,
) (IndexClaim, bool, error) {
	if ctx == nil || !configuration.Valid() {
		return IndexClaim{}, false, ErrInvalidArgument
	}
	leaseToken, err := repository.newID()
	if err != nil {
		return IndexClaim{}, false, err
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return IndexClaim{}, false, ErrRepository
	}
	defer rollback(ctx, tx)
	if _, err := tx.Exec(ctx, `
UPDATE agent_memory_index_jobs
SET
    status = CASE
        WHEN attempt_count >= $1 THEN 'failed'
        ELSE 'pending'
    END,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = CASE
        WHEN attempt_count >= $1 THEN next_attempt_at
        ELSE transaction_timestamp()
    END,
    failure_kind = CASE
        WHEN attempt_count >= $1 THEN 'attempts_exhausted'
        ELSE 'lease_expired'
    END,
    updated_at = transaction_timestamp(),
    completed_at = CASE
        WHEN attempt_count >= $1 THEN transaction_timestamp()
        ELSE NULL
    END
WHERE status = 'running'
  AND lease_expires_at <= clock_timestamp()`,
		configuration.MaxAttempts,
	); err != nil {
		return IndexClaim{}, false, ErrRepository
	}
	// An embedding rollout does not change the Memory content version. Requeue
	// non-running jobs whose recorded embedding identity differs so unchanged
	// active memories cannot silently disappear behind the new search filters.
	if _, err := tx.Exec(ctx, `
UPDATE agent_memory_index_jobs AS jobs
SET
    status = 'pending',
    attempt_count = 0,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = transaction_timestamp(),
    embedding_policy_version = NULL,
    provider = NULL,
    model = NULL,
    dimension = NULL,
    failure_kind = NULL,
    input_tokens = NULL,
    total_tokens = NULL,
    updated_at = transaction_timestamp(),
    completed_at = NULL
FROM agent_memories AS memories
JOIN identity_users AS users
  ON users.id = memories.owner_user_id
 AND users.account_status = 'active'
WHERE jobs.memory_id = memories.id
  AND jobs.owner_user_id = memories.owner_user_id
  AND jobs.memory_version = memories.version
  AND jobs.status <> 'running'
  AND memories.status = 'active'
  AND (memories.expires_at IS NULL OR memories.expires_at > clock_timestamp())
  AND jobs.embedding_policy_version IS NOT NULL
  AND (
      jobs.embedding_policy_version IS DISTINCT FROM $1
      OR jobs.provider IS DISTINCT FROM $2
      OR jobs.model IS DISTINCT FROM $3
      OR jobs.dimension IS DISTINCT FROM $4
  )`,
		configuration.PolicyVersion,
		configuration.Provider,
		configuration.Model,
		configuration.Dimensions,
	); err != nil {
		return IndexClaim{}, false, ErrRepository
	}
	job, err := scanIndexJob(tx.QueryRow(ctx, `
WITH candidate AS (
    SELECT memory_id, memory_version
    FROM agent_memory_index_jobs
    WHERE status = 'pending'
      AND next_attempt_at <= clock_timestamp()
    ORDER BY next_attempt_at, created_at, memory_id, memory_version
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE agent_memory_index_jobs AS jobs
SET
    status = 'running',
    attempt_count = jobs.attempt_count + 1,
    lease_token = $1,
    lease_expires_at = transaction_timestamp() + make_interval(secs => $2),
    embedding_policy_version = $3,
    provider = $4,
    model = $5,
    dimension = $6,
    failure_kind = NULL,
    input_tokens = NULL,
    total_tokens = NULL,
    updated_at = transaction_timestamp(),
    completed_at = NULL
FROM candidate
WHERE jobs.memory_id = candidate.memory_id
  AND jobs.memory_version = candidate.memory_version
RETURNING `+indexJobColumns("jobs"),
		leaseToken,
		configuration.LeaseDuration.Seconds(),
		configuration.PolicyVersion,
		configuration.Provider,
		configuration.Model,
		configuration.Dimensions,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return IndexClaim{}, false, ErrRepository
		}
		return IndexClaim{}, false, nil
	}
	if err != nil {
		return IndexClaim{}, false, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return IndexClaim{}, false, ErrRepository
	}
	claim := IndexClaim{IndexJob: job}
	if !claim.Valid() {
		return IndexClaim{}, false, ErrRepository
	}
	return claim, true, nil
}

func (repository *PostgresRepository) ReadIndexSource(
	ctx context.Context,
	claim IndexClaim,
) (IndexSource, error) {
	if ctx == nil || !claim.Valid() {
		return IndexSource{}, ErrInvalidArgument
	}
	var accountStatus string
	if err := repository.database.QueryRow(ctx, `
SELECT account_status
FROM identity_users
WHERE id = $1`,
		claim.OwnerID,
	).Scan(&accountStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IndexSource{}, ErrAccountDeleted
		}
		return IndexSource{}, ErrRepository
	}
	if accountStatus != "active" {
		return IndexSource{}, ErrAccountDeleted
	}
	var source IndexSource
	if err := repository.database.QueryRow(ctx, `
SELECT id::text, owner_user_id::text, version, content
FROM agent_memories
WHERE id = $1
  AND owner_user_id = $2
  AND version = $3
  AND status = 'active'
  AND (expires_at IS NULL OR expires_at > clock_timestamp())`,
		claim.MemoryID,
		claim.OwnerID,
		claim.MemoryVersion,
	).Scan(
		&source.MemoryID,
		&source.OwnerID,
		&source.Version,
		&source.Content,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IndexSource{}, ErrNotFound
		}
		return IndexSource{}, ErrRepository
	}
	if !source.Valid() {
		return IndexSource{}, ErrRepository
	}
	return source, nil
}

func (repository *PostgresRepository) CompleteIndex(
	ctx context.Context,
	claim IndexClaim,
	result EmbeddingResult,
) (IndexJob, error) {
	if ctx == nil ||
		!claim.Valid() ||
		result.Provider != claim.Provider ||
		result.Model != claim.Model ||
		result.Dimensions != claim.Dimensions ||
		result.InputTokens < 0 ||
		result.TotalTokens < result.InputTokens {
		return IndexJob{}, ErrInvalidArgument
	}
	if err := ValidateEmbeddingResult(claim.Dimensions, result); err != nil {
		return IndexJob{}, ErrIndexResponse
	}
	vector, err := vectorLiteral(result.Vector, claim.Dimensions)
	if err != nil {
		return IndexJob{}, err
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return IndexJob{}, ErrRepository
	}
	defer rollback(ctx, tx)
	if err := verifyIndexClaim(ctx, tx, claim); err != nil {
		return IndexJob{}, err
	}
	commandTag, err := tx.Exec(ctx, `
INSERT INTO agent_memory_vectors (
    memory_id,
    owner_user_id,
    memory_version,
    provider,
    model,
    dimension,
    embedding_policy_version,
    embedding,
    created_at,
    updated_at
)
SELECT
    memories.id,
    memories.owner_user_id,
    memories.version,
    $4,
    $5,
    $6,
    $7,
    $8::public.vector,
    transaction_timestamp(),
    transaction_timestamp()
FROM agent_memories AS memories
JOIN identity_users AS users
  ON users.id = memories.owner_user_id
 AND users.account_status = 'active'
WHERE memories.id = $1
  AND memories.owner_user_id = $2
  AND memories.version = $3
  AND memories.status = 'active'
  AND (memories.expires_at IS NULL OR memories.expires_at > clock_timestamp())
ON CONFLICT (memory_id) DO UPDATE
SET
    owner_user_id = EXCLUDED.owner_user_id,
    memory_version = EXCLUDED.memory_version,
    provider = EXCLUDED.provider,
    model = EXCLUDED.model,
    dimension = EXCLUDED.dimension,
    embedding_policy_version = EXCLUDED.embedding_policy_version,
    embedding = EXCLUDED.embedding,
    updated_at = transaction_timestamp()
WHERE agent_memory_vectors.owner_user_id = EXCLUDED.owner_user_id
  AND agent_memory_vectors.memory_version <= EXCLUDED.memory_version`,
		claim.MemoryID,
		claim.OwnerID,
		claim.MemoryVersion,
		claim.Provider,
		claim.Model,
		claim.Dimensions,
		claim.PolicyVersion,
		vector,
	)
	if err != nil {
		return IndexJob{}, mapPostgresError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return IndexJob{}, ErrNotFound
	}
	job, err := scanIndexJob(tx.QueryRow(ctx, `
UPDATE agent_memory_index_jobs
SET
    status = 'completed',
    lease_token = NULL,
    lease_expires_at = NULL,
    failure_kind = NULL,
    input_tokens = $4,
    total_tokens = $5,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE memory_id = $1
  AND owner_user_id = $2
  AND memory_version = $3
  AND status = 'running'
  AND lease_token = $6
  AND lease_expires_at > clock_timestamp()
RETURNING `+indexJobColumns("agent_memory_index_jobs"),
		claim.MemoryID,
		claim.OwnerID,
		claim.MemoryVersion,
		result.InputTokens,
		result.TotalTokens,
		claim.LeaseToken,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return IndexJob{}, ErrConflict
	}
	if err != nil {
		return IndexJob{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return IndexJob{}, ErrRepository
	}
	return job, nil
}

func (repository *PostgresRepository) FailIndex(
	ctx context.Context,
	claim IndexClaim,
	failureKind string,
	retryable bool,
	configuration IndexConfig,
) (IndexJob, error) {
	if ctx == nil || !claim.Valid() ||
		!stableFailurePattern.MatchString(failureKind) ||
		!configuration.Valid() {
		return IndexJob{}, ErrInvalidArgument
	}
	status := IndexFailed
	if retryable && claim.AttemptCount < configuration.MaxAttempts {
		status = IndexPending
	}
	job, err := scanIndexJob(repository.database.QueryRow(ctx, `
UPDATE agent_memory_index_jobs
SET
    status = $5,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = CASE
        WHEN $5 = 'pending'
        THEN transaction_timestamp() + make_interval(secs => $6)
        ELSE next_attempt_at
    END,
    failure_kind = $7,
    updated_at = transaction_timestamp(),
    completed_at = CASE
        WHEN $5 = 'pending' THEN NULL
        ELSE transaction_timestamp()
    END
WHERE memory_id = $1
  AND owner_user_id = $2
  AND memory_version = $3
  AND status = 'running'
  AND lease_token = $4
  AND lease_expires_at > clock_timestamp()
RETURNING `+indexJobColumns("agent_memory_index_jobs"),
		claim.MemoryID,
		claim.OwnerID,
		claim.MemoryVersion,
		claim.LeaseToken,
		status,
		extractionBackoff(claim.AttemptCount).Seconds(),
		failureKind,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return IndexJob{}, ErrConflict
	}
	if err != nil {
		return IndexJob{}, mapPostgresError(err)
	}
	return job, nil
}

func (repository *PostgresRepository) DiscardIndex(
	ctx context.Context,
	claim IndexClaim,
	failureKind string,
) (IndexJob, error) {
	if ctx == nil || !claim.Valid() ||
		!stableFailurePattern.MatchString(failureKind) {
		return IndexJob{}, ErrInvalidArgument
	}
	job, err := scanIndexJob(repository.database.QueryRow(ctx, `
UPDATE agent_memory_index_jobs
SET
    status = 'discarded',
    lease_token = NULL,
    lease_expires_at = NULL,
    failure_kind = $5,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE memory_id = $1
  AND owner_user_id = $2
  AND memory_version = $3
  AND status = 'running'
  AND lease_token = $4
  AND lease_expires_at > clock_timestamp()
RETURNING `+indexJobColumns("agent_memory_index_jobs"),
		claim.MemoryID,
		claim.OwnerID,
		claim.MemoryVersion,
		claim.LeaseToken,
		failureKind,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return IndexJob{}, ErrConflict
	}
	if err != nil {
		return IndexJob{}, mapPostgresError(err)
	}
	return job, nil
}

func verifyIndexClaim(
	ctx context.Context,
	tx pgx.Tx,
	claim IndexClaim,
) error {
	var attempts int
	if err := tx.QueryRow(ctx, `
SELECT attempt_count
FROM agent_memory_index_jobs
WHERE memory_id = $1
  AND owner_user_id = $2
  AND memory_version = $3
  AND status = 'running'
  AND lease_token = $4
  AND lease_expires_at > clock_timestamp()
FOR UPDATE`,
		claim.MemoryID,
		claim.OwnerID,
		claim.MemoryVersion,
		claim.LeaseToken,
	).Scan(&attempts); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		return ErrRepository
	}
	if attempts != claim.AttemptCount {
		return ErrConflict
	}
	return nil
}

func vectorLiteral(vector []float32, dimensions int) (string, error) {
	if len(vector) != dimensions ||
		dimensions != MemoryEmbeddingDimensions {
		return "", ErrIndexResponse
	}
	var builder strings.Builder
	builder.Grow(dimensions * 12)
	builder.WriteByte('[')
	for index, value := range vector {
		if math.IsNaN(float64(value)) ||
			math.IsInf(float64(value), 0) {
			return "", ErrIndexResponse
		}
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(
			float64(value),
			'g',
			-1,
			32,
		))
	}
	builder.WriteByte(']')
	return builder.String(), nil
}

func indexJobColumns(alias string) string {
	return `
    ` + alias + `.memory_id::text,
    ` + alias + `.owner_user_id::text,
    ` + alias + `.memory_version,
    ` + alias + `.status,
    ` + alias + `.attempt_count,
    coalesce(` + alias + `.lease_token::text, ''),
    ` + alias + `.lease_expires_at,
    ` + alias + `.next_attempt_at,
    coalesce(` + alias + `.embedding_policy_version, ''),
    coalesce(` + alias + `.provider, ''),
    coalesce(` + alias + `.model, ''),
    coalesce(` + alias + `.dimension, 0),
    coalesce(` + alias + `.failure_kind, ''),
    coalesce(` + alias + `.input_tokens, 0),
    coalesce(` + alias + `.total_tokens, 0),
    ` + alias + `.created_at,
    ` + alias + `.updated_at,
    ` + alias + `.completed_at`
}

func scanIndexJob(row rowScanner) (IndexJob, error) {
	var job IndexJob
	var status string
	var leaseExpiresAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	if err := row.Scan(
		&job.MemoryID,
		&job.OwnerID,
		&job.MemoryVersion,
		&status,
		&job.AttemptCount,
		&job.LeaseToken,
		&leaseExpiresAt,
		&job.NextAttemptAt,
		&job.PolicyVersion,
		&job.Provider,
		&job.Model,
		&job.Dimensions,
		&job.FailureKind,
		&job.InputTokens,
		&job.TotalTokens,
		&job.CreatedAt,
		&job.UpdatedAt,
		&completedAt,
	); err != nil {
		return IndexJob{}, err
	}
	job.Status = IndexStatus(status)
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
		!validUUID(job.MemoryID) ||
		!validUUID(job.OwnerID) ||
		job.MemoryVersion < 1 ||
		job.AttemptCount < 0 {
		return IndexJob{}, ErrRepository
	}
	return job, nil
}
