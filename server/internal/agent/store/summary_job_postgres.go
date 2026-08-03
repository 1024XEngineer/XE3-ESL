package store

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *PostgresStore) ClaimJob(
	ctx context.Context,
	configuration agentsummary.WorkerConfiguration,
) (agentsummary.JobClaim, bool, error) {
	if ctx == nil || !configuration.Valid() {
		return agentsummary.JobClaim{}, false, conversation.ErrInvalidRequest
	}
	leaseToken, err := r.ids.NewID()
	if err != nil {
		return agentsummary.JobClaim{}, false, conversation.ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentsummary.JobClaim{}, false, conversation.ErrRepository
	}
	defer rollback(tx)

	if _, err := tx.Exec(ctx, `
UPDATE agent_thread_summary_jobs
SET
    status = 'failed',
    lease_token = NULL,
    lease_expires_at = NULL,
    outcome_reason = 'attempts_exhausted',
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE status = 'running'
  AND lease_expires_at <= clock_timestamp()
  AND attempt_count >= $1`,
		configuration.MaxAttempts,
	); err != nil {
		return agentsummary.JobClaim{}, false, mapSummaryPostgresError(err)
	}

	job, err := scanSummaryJob(tx.QueryRow(ctx, `
WITH next_job AS (
    SELECT jobs.source_run_id
    FROM agent_thread_summary_jobs AS jobs
    INNER JOIN agent_threads AS threads
        ON threads.id = jobs.source_thread_id
       AND threads.owner_user_id = jobs.owner_user_id
    WHERE (
        (
                jobs.status = 'pending'
                AND jobs.next_attempt_at <= transaction_timestamp()
            )
            OR (
                jobs.status = 'running'
                AND jobs.lease_expires_at <= clock_timestamp()
                AND jobs.attempt_count < $1
            )
        )
    AND NOT EXISTS (
        SELECT 1
        FROM agent_thread_summary_jobs AS active
        WHERE active.owner_user_id = jobs.owner_user_id
          AND active.source_thread_id = jobs.source_thread_id
          AND active.source_run_id <> jobs.source_run_id
          AND active.status = 'running'
          AND active.lease_expires_at > clock_timestamp()
    )
    ORDER BY
        CASE WHEN jobs.status = 'running' THEN 0 ELSE 1 END,
        jobs.next_attempt_at,
        jobs.source_completed_at,
        jobs.source_run_id
    FOR UPDATE OF jobs, threads SKIP LOCKED
    LIMIT 1
)
UPDATE agent_thread_summary_jobs AS jobs
SET
    status = 'running',
    attempt_count = jobs.attempt_count + 1,
    lease_token = $2,
    lease_expires_at =
        transaction_timestamp() + make_interval(secs => $3),
    trigger_policy_version = $4,
    summary_policy_version = $5,
    prompt_version = $6,
    provider = $7,
    model = $8,
    target_covered_through_sequence = NULL,
    outcome_reason = NULL,
    updated_at = transaction_timestamp()
FROM next_job
WHERE jobs.source_run_id = next_job.source_run_id
RETURNING `+summaryJobColumns("jobs"),
		configuration.MaxAttempts,
		leaseToken,
		configuration.LeaseDuration.Seconds(),
		configuration.TriggerPolicyVersion,
		configuration.Summary.PolicyVersion,
		configuration.Summary.PromptVersion,
		configuration.Summary.Provider,
		configuration.Summary.Model,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return agentsummary.JobClaim{}, false, conversation.ErrRepository
		}
		return agentsummary.JobClaim{}, false, nil
	}
	if err != nil {
		return agentsummary.JobClaim{}, false, mapSummaryPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return agentsummary.JobClaim{}, false, conversation.ErrRepository
	}
	claim := agentsummary.JobClaim{Job: job}
	if !claim.Valid() {
		return agentsummary.JobClaim{}, false, conversation.ErrRepository
	}
	return claim, true, nil
}

func (r *PostgresStore) CompleteJob(
	ctx context.Context,
	claim agentsummary.JobClaim,
	targetCoveredThrough int64,
	checkpoint agentsummary.Checkpoint,
) (agentsummary.Job, error) {
	if ctx == nil ||
		!claim.Valid() ||
		targetCoveredThrough < 1 ||
		!checkpoint.Valid() ||
		checkpoint.OwnerID != claim.OwnerID ||
		checkpoint.ThreadID != claim.ThreadID ||
		checkpoint.CoveredThroughSequence != targetCoveredThrough {
		return agentsummary.Job{}, conversation.ErrInvalidRequest
	}
	job, err := scanSummaryJob(r.database.QueryRow(ctx, `
UPDATE agent_thread_summary_jobs
SET
    status = 'completed',
    lease_token = NULL,
    lease_expires_at = NULL,
    target_covered_through_sequence = $4,
    checkpoint_id = $5,
    outcome_reason = NULL,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE source_run_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
RETURNING `+summaryJobColumns("agent_thread_summary_jobs"),
		claim.SourceRunID,
		claim.OwnerID,
		claim.LeaseToken,
		targetCoveredThrough,
		checkpoint.ID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentsummary.Job{}, conversation.ErrConflict
	}
	if err != nil {
		return agentsummary.Job{}, mapSummaryPostgresError(err)
	}
	return job, nil
}

func (r *PostgresStore) FinishJob(
	ctx context.Context,
	claim agentsummary.JobClaim,
	status agentsummary.JobStatus,
	targetCoveredThrough int64,
	reason string,
) (agentsummary.Job, error) {
	validTarget := status == agentsummary.JobSkipped &&
		targetCoveredThrough == 0 ||
		status == agentsummary.JobSuperseded &&
			targetCoveredThrough >= 1
	if ctx == nil ||
		!claim.Valid() ||
		!validTarget ||
		!summaryJobReasonPattern.MatchString(reason) {
		return agentsummary.Job{}, conversation.ErrInvalidRequest
	}
	var target any
	if targetCoveredThrough > 0 {
		target = targetCoveredThrough
	}
	job, err := scanSummaryJob(r.database.QueryRow(ctx, `
UPDATE agent_thread_summary_jobs
SET
    status = $4,
    lease_token = NULL,
    lease_expires_at = NULL,
    target_covered_through_sequence = $5,
    checkpoint_id = NULL,
    outcome_reason = $6,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE source_run_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
RETURNING `+summaryJobColumns("agent_thread_summary_jobs"),
		claim.SourceRunID,
		claim.OwnerID,
		claim.LeaseToken,
		status,
		target,
		reason,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentsummary.Job{}, conversation.ErrConflict
	}
	if err != nil {
		return agentsummary.Job{}, mapSummaryPostgresError(err)
	}
	return job, nil
}

func (r *PostgresStore) FailJob(
	ctx context.Context,
	claim agentsummary.JobClaim,
	targetCoveredThrough int64,
	failureKind string,
	retryable bool,
	configuration agentsummary.WorkerConfiguration,
) (agentsummary.Job, error) {
	if ctx == nil ||
		!claim.Valid() ||
		targetCoveredThrough < 0 ||
		!summaryJobReasonPattern.MatchString(failureKind) ||
		!configuration.Valid() {
		return agentsummary.Job{}, conversation.ErrInvalidRequest
	}
	status := agentsummary.JobFailed
	if retryable && claim.AttemptCount < configuration.MaxAttempts {
		status = agentsummary.JobPending
	}
	var target any
	if targetCoveredThrough > 0 {
		target = targetCoveredThrough
	}
	backoff := summaryJobBackoff(claim.AttemptCount)
	job, err := scanSummaryJob(r.database.QueryRow(ctx, `
UPDATE agent_thread_summary_jobs
SET
    status = $4,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = CASE
        WHEN $4 = 'pending'
        THEN transaction_timestamp() + make_interval(secs => $5)
        ELSE next_attempt_at
    END,
    target_covered_through_sequence = $6,
    checkpoint_id = NULL,
    outcome_reason = $7,
    updated_at = transaction_timestamp(),
    completed_at = CASE
        WHEN $4 = 'pending' THEN NULL
        ELSE transaction_timestamp()
    END
WHERE source_run_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
RETURNING `+summaryJobColumns("agent_thread_summary_jobs"),
		claim.SourceRunID,
		claim.OwnerID,
		claim.LeaseToken,
		status,
		backoff.Seconds(),
		target,
		failureKind,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentsummary.Job{}, conversation.ErrConflict
	}
	if err != nil {
		return agentsummary.Job{}, mapSummaryPostgresError(err)
	}
	return job, nil
}

func summaryJobBackoff(attempt int) time.Duration {
	if attempt < 1 {
		return time.Second
	}
	backoff := time.Second << min(attempt-1, 8)
	if backoff > 5*time.Minute {
		return 5 * time.Minute
	}
	return backoff
}

func summaryJobColumns(alias string) string {
	return `
    ` + alias + `.source_run_id::text,
    ` + alias + `.owner_user_id::text,
    ` + alias + `.source_thread_id::text,
    ` + alias + `.observed_through_sequence,
    ` + alias + `.source_completed_at,
    ` + alias + `.status,
    ` + alias + `.attempt_count,
    COALESCE(` + alias + `.lease_token::text, ''),
    ` + alias + `.lease_expires_at,
    ` + alias + `.next_attempt_at,
    COALESCE(` + alias + `.trigger_policy_version, ''),
    COALESCE(` + alias + `.summary_policy_version, ''),
    COALESCE(` + alias + `.prompt_version, ''),
    COALESCE(` + alias + `.provider, ''),
    COALESCE(` + alias + `.model, ''),
    COALESCE(` + alias + `.target_covered_through_sequence, 0),
    COALESCE(` + alias + `.checkpoint_id::text, ''),
    COALESCE(` + alias + `.outcome_reason, ''),
    ` + alias + `.created_at,
    ` + alias + `.updated_at,
    ` + alias + `.completed_at`
}

func scanSummaryJob(row rowScanner) (agentsummary.Job, error) {
	var job agentsummary.Job
	var status string
	var leaseExpiresAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	if err := row.Scan(
		&job.SourceRunID,
		&job.OwnerID,
		&job.ThreadID,
		&job.ObservedThroughSequence,
		&job.SourceCompletedAt,
		&status,
		&job.AttemptCount,
		&job.LeaseToken,
		&leaseExpiresAt,
		&job.NextAttemptAt,
		&job.TriggerPolicyVersion,
		&job.SummaryPolicyVersion,
		&job.PromptVersion,
		&job.Provider,
		&job.Model,
		&job.TargetCoveredThrough,
		&job.CheckpointID,
		&job.OutcomeReason,
		&job.CreatedAt,
		&job.UpdatedAt,
		&completedAt,
	); err != nil {
		return agentsummary.Job{}, err
	}
	job.Status = agentsummary.JobStatus(status)
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
		!conversation.ValidUUID(job.SourceRunID) ||
		!conversation.ValidUUID(job.OwnerID) ||
		!conversation.ValidUUID(job.ThreadID) ||
		job.ObservedThroughSequence < 1 ||
		job.SourceCompletedAt.IsZero() ||
		job.AttemptCount < 0 {
		return agentsummary.Job{}, conversation.ErrRepository
	}
	return job, nil
}

var (
	summaryJobReasonPattern                            = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	_                       agentsummary.JobRepository = (*PostgresStore)(nil)
)
