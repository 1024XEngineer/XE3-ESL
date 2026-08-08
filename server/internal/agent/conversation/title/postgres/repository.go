package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agenttitle "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/title"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IDGenerator interface {
	NewID() (string, error)
}

type Repository struct {
	database *pgxpool.Pool
	ids      IDGenerator
}

func New(database *pgxpool.Pool, ids IDGenerator) (*Repository, error) {
	if database == nil || ids == nil {
		return nil, agenttitle.ErrInvalidArgument
	}
	return &Repository{database: database, ids: ids}, nil
}

func (repository *Repository) ClaimJob(
	ctx context.Context,
	configuration agenttitle.WorkerConfiguration,
) (agenttitle.JobClaim, bool, error) {
	if ctx == nil || !configuration.Valid() {
		return agenttitle.JobClaim{}, false, conversation.ErrInvalidRequest
	}
	leaseToken, err := repository.ids.NewID()
	if err != nil {
		return agenttitle.JobClaim{}, false, conversation.ErrRepository
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return agenttitle.JobClaim{}, false, conversation.ErrRepository
	}
	defer rollback(tx)

	if _, err := tx.Exec(ctx, `
UPDATE agent_thread_title_jobs
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
	); err != nil {
		return agenttitle.JobClaim{}, false, conversation.ErrRepository
	}

	claim, err := scanClaim(tx.QueryRow(ctx, `
WITH next_job AS (
    SELECT jobs.source_thread_id
    FROM agent_thread_title_jobs AS jobs
    INNER JOIN agent_threads AS threads
        ON threads.id = jobs.source_thread_id
       AND threads.owner_user_id = jobs.owner_user_id
    WHERE threads.title IS NULL
      AND (
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
    ORDER BY
        CASE WHEN jobs.status = 'running' THEN 0 ELSE 1 END,
        jobs.next_attempt_at,
        jobs.created_at,
        jobs.source_thread_id
    FOR UPDATE OF jobs, threads SKIP LOCKED
    LIMIT 1
),
claimed AS (
    UPDATE agent_thread_title_jobs AS jobs
    SET
        status = 'running',
        attempt_count = jobs.attempt_count + 1,
        lease_token = $2,
        lease_expires_at =
            transaction_timestamp() + make_interval(secs => $3),
        prompt_version = $4,
        provider = $5,
        model = $6,
        failure_kind = NULL,
        updated_at = transaction_timestamp()
    FROM next_job
    WHERE jobs.source_thread_id = next_job.source_thread_id
    RETURNING jobs.*
)
SELECT
    `+jobColumns("claimed")+`,
    input.content,
    assistant.content
FROM claimed
INNER JOIN agent_runs AS runs
    ON runs.id = claimed.source_run_id
   AND runs.owner_user_id = claimed.owner_user_id
   AND runs.thread_id = claimed.source_thread_id
INNER JOIN agent_messages AS input
    ON input.id = runs.input_message_id
   AND input.owner_user_id = runs.owner_user_id
   AND input.thread_id = runs.thread_id
   AND input.role = 'user'
INNER JOIN agent_messages AS assistant
    ON assistant.id = runs.assistant_message_id
   AND assistant.owner_user_id = runs.owner_user_id
   AND assistant.thread_id = runs.thread_id
   AND assistant.role = 'assistant'`,
		configuration.MaxAttempts,
		leaseToken,
		configuration.LeaseDuration.Seconds(),
		configuration.Generation.PromptVersion,
		configuration.Generation.Provider,
		configuration.Generation.Model,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return agenttitle.JobClaim{}, false, conversation.ErrRepository
		}
		return agenttitle.JobClaim{}, false, nil
	}
	if err != nil {
		return agenttitle.JobClaim{}, false, conversation.ErrRepository
	}
	if err := tx.Commit(ctx); err != nil {
		return agenttitle.JobClaim{}, false, conversation.ErrRepository
	}
	if !claim.Valid() {
		return agenttitle.JobClaim{}, false, conversation.ErrRepository
	}
	return claim, true, nil
}

func (repository *Repository) CompleteJob(
	ctx context.Context,
	claim agenttitle.JobClaim,
	title string,
) (agenttitle.Job, error) {
	if ctx == nil || !claim.Valid() || !agenttitle.ValidTitle(title) {
		return agenttitle.Job{}, conversation.ErrInvalidRequest
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return agenttitle.Job{}, conversation.ErrRepository
	}
	defer rollback(tx)

	command, err := tx.Exec(ctx, `
UPDATE agent_threads
SET title = $3
WHERE id = $1
  AND owner_user_id = $2
  AND title IS NULL`,
		claim.ThreadID,
		claim.OwnerID,
		title,
	)
	if err != nil {
		return agenttitle.Job{}, conversation.ErrRepository
	}
	if command.RowsAffected() != 1 {
		return agenttitle.Job{}, conversation.ErrConflict
	}

	job, err := scanJob(tx.QueryRow(ctx, `
UPDATE agent_thread_title_jobs
SET
    status = 'completed',
    lease_token = NULL,
    lease_expires_at = NULL,
    failure_kind = NULL,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE source_thread_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
RETURNING `+jobColumns("agent_thread_title_jobs"),
		claim.ThreadID,
		claim.OwnerID,
		claim.LeaseToken,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agenttitle.Job{}, conversation.ErrConflict
	}
	if err != nil {
		return agenttitle.Job{}, conversation.ErrRepository
	}
	if err := tx.Commit(ctx); err != nil {
		return agenttitle.Job{}, conversation.ErrRepository
	}
	return job, nil
}

func (repository *Repository) FailJob(
	ctx context.Context,
	claim agenttitle.JobClaim,
	failureKind string,
	retryable bool,
	configuration agenttitle.WorkerConfiguration,
) (agenttitle.Job, error) {
	if ctx == nil ||
		!claim.Valid() ||
		!agenttitle.ValidFailureKind(failureKind) ||
		!configuration.Valid() {
		return agenttitle.Job{}, conversation.ErrInvalidRequest
	}
	status := agenttitle.JobFailed
	if retryable && claim.AttemptCount < configuration.MaxAttempts {
		status = agenttitle.JobPending
	}
	job, err := scanJob(repository.database.QueryRow(ctx, `
UPDATE agent_thread_title_jobs
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
WHERE source_thread_id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND lease_token = $3
  AND lease_expires_at > clock_timestamp()
RETURNING `+jobColumns("agent_thread_title_jobs"),
		claim.ThreadID,
		claim.OwnerID,
		claim.LeaseToken,
		status,
		jobBackoff(claim.AttemptCount).Seconds(),
		failureKind,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agenttitle.Job{}, conversation.ErrConflict
	}
	if err != nil {
		return agenttitle.Job{}, conversation.ErrRepository
	}
	return job, nil
}

func jobBackoff(attempt int) time.Duration {
	if attempt < 1 {
		return time.Second
	}
	backoff := time.Second << min(attempt-1, 8)
	if backoff > 5*time.Minute {
		return 5 * time.Minute
	}
	return backoff
}

func jobColumns(alias string) string {
	return `
    ` + alias + `.source_run_id::text,
    ` + alias + `.owner_user_id::text,
    ` + alias + `.source_thread_id::text,
    ` + alias + `.status,
    ` + alias + `.attempt_count,
    COALESCE(` + alias + `.lease_token::text, ''),
    ` + alias + `.lease_expires_at,
    ` + alias + `.next_attempt_at,
    COALESCE(` + alias + `.prompt_version, ''),
    COALESCE(` + alias + `.provider, ''),
    COALESCE(` + alias + `.model, ''),
    COALESCE(` + alias + `.failure_kind, ''),
    ` + alias + `.created_at,
    ` + alias + `.updated_at,
    ` + alias + `.completed_at`
}

func scanClaim(row rowScanner) (agenttitle.JobClaim, error) {
	job, err := scanStoredJob(row, true)
	if err != nil {
		return agenttitle.JobClaim{}, err
	}
	return agenttitle.JobClaim{Job: job}, nil
}

func scanJob(row rowScanner) (agenttitle.Job, error) {
	return scanStoredJob(row, false)
}

func scanStoredJob(row rowScanner, withMessages bool) (agenttitle.Job, error) {
	var job agenttitle.Job
	var status string
	var leaseExpiresAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	destinations := []any{
		&job.SourceRunID,
		&job.OwnerID,
		&job.ThreadID,
		&status,
		&job.AttemptCount,
		&job.LeaseToken,
		&leaseExpiresAt,
		&job.NextAttemptAt,
		&job.PromptVersion,
		&job.Provider,
		&job.Model,
		&job.FailureKind,
		&job.CreatedAt,
		&job.UpdatedAt,
		&completedAt,
	}
	if withMessages {
		destinations = append(
			destinations,
			&job.UserMessage,
			&job.AssistantMessage,
		)
	}
	if err := row.Scan(destinations...); err != nil {
		return agenttitle.Job{}, err
	}
	job.Status = agenttitle.JobStatus(status)
	if leaseExpiresAt.Valid {
		job.LeaseExpiresAt = leaseExpiresAt.Time.UTC()
	}
	if completedAt.Valid {
		job.CompletedAt = completedAt.Time.UTC()
	}
	job.NextAttemptAt = job.NextAttemptAt.UTC()
	job.CreatedAt = job.CreatedAt.UTC()
	job.UpdatedAt = job.UpdatedAt.UTC()
	if !job.Status.Valid() ||
		!conversation.ValidUUID(job.SourceRunID) ||
		!conversation.ValidUUID(job.OwnerID) ||
		!conversation.ValidUUID(job.ThreadID) ||
		job.AttemptCount < 0 {
		return agenttitle.Job{}, conversation.ErrRepository
	}
	return job, nil
}

type rowScanner interface {
	Scan(...any) error
}

func rollback(tx pgx.Tx) {
	_ = tx.Rollback(context.Background())
}

var _ agenttitle.JobRepository = (*Repository)(nil)
