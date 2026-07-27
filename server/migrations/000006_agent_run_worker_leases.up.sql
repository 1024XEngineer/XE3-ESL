BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

ALTER TABLE agent_runs
    ADD COLUMN max_input_characters integer,
    ADD COLUMN worker_lease_token uuid,
    ADD COLUMN worker_lease_expires_at timestamp with time zone;

UPDATE agent_runs
SET max_input_characters = 12000;

UPDATE agent_runs
SET
    status = 'failed',
    failure_kind = 'interrupted',
    failure_retryable = true,
    completed_at = GREATEST(CURRENT_TIMESTAMP, started_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE status = 'running';

ALTER TABLE agent_runs
    ALTER COLUMN max_input_characters SET NOT NULL,
    ADD CONSTRAINT agent_runs_input_budget_check
        CHECK (max_input_characters BETWEEN 5000 AND 1000000),
    DROP CONSTRAINT agent_runs_state_shape_check,
    ADD CONSTRAINT agent_runs_state_shape_check
        CHECK (
            (
                status = 'pending'
                AND started_at IS NULL
                AND completed_at IS NULL
                AND worker_lease_token IS NULL
                AND worker_lease_expires_at IS NULL
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
                AND worker_lease_token IS NOT NULL
                AND worker_lease_expires_at > started_at
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
                AND worker_lease_token IS NULL
                AND worker_lease_expires_at IS NULL
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
                AND worker_lease_token IS NULL
                AND worker_lease_expires_at IS NULL
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
        );

CREATE INDEX agent_runs_expired_worker_lease_idx
    ON agent_runs (worker_lease_expires_at)
    WHERE status = 'running';

COMMIT;
