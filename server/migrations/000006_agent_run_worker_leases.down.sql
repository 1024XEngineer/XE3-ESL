BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

UPDATE agent_runs
SET
    status = 'failed',
    failure_kind = 'interrupted',
    failure_retryable = true,
    worker_lease_token = NULL,
    worker_lease_expires_at = NULL,
    completed_at = GREATEST(CURRENT_TIMESTAMP, started_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE status = 'running';

DROP INDEX agent_runs_expired_worker_lease_idx;

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_state_shape_check,
    ADD CONSTRAINT agent_runs_state_shape_check
        CHECK (
            (
                status = 'pending'
                AND started_at IS NULL
                AND completed_at IS NULL
                AND assistant_message_id IS NULL
                AND provider_completion_id IS NULL
                AND provider_model IS NULL
                AND finish_reason IS NULL
                AND input_tokens IS NULL
                AND output_tokens IS NULL
                AND total_tokens IS NULL
                AND failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                status = 'running'
                AND started_at IS NOT NULL
                AND completed_at IS NULL
                AND assistant_message_id IS NULL
                AND provider_completion_id IS NULL
                AND provider_model IS NULL
                AND finish_reason IS NULL
                AND input_tokens IS NULL
                AND output_tokens IS NULL
                AND total_tokens IS NULL
                AND failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                status = 'completed'
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
                AND assistant_message_id IS NOT NULL
                AND provider_completion_id IS NOT NULL
                AND provider_model IS NOT NULL
                AND finish_reason IS NOT NULL
                AND input_tokens IS NOT NULL
                AND output_tokens IS NOT NULL
                AND total_tokens IS NOT NULL
                AND failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                status = 'failed'
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
                AND assistant_message_id IS NULL
                AND provider_completion_id IS NULL
                AND provider_model IS NULL
                AND finish_reason IS NULL
                AND input_tokens IS NULL
                AND output_tokens IS NULL
                AND total_tokens IS NULL
                AND failure_kind IS NOT NULL
                AND failure_retryable IS NOT NULL
            )
        ),
    DROP CONSTRAINT agent_runs_input_budget_check,
    DROP COLUMN worker_lease_expires_at,
    DROP COLUMN worker_lease_token,
    DROP COLUMN max_input_characters;

COMMIT;
