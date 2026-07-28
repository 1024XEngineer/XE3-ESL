BEGIN;

CREATE TABLE agent_memory_extraction_jobs (
    source_run_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    source_thread_id uuid NOT NULL,
    source_input_message_id uuid NOT NULL,
    source_assistant_message_id uuid NOT NULL,
    source_attempt integer NOT NULL CHECK (source_attempt > 0),
    source_completed_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    attempt_count integer NOT NULL DEFAULT 0,
    lease_token uuid,
    lease_expires_at timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    policy_version text COLLATE "C",
    prompt_version text COLLATE "C",
    provider text,
    model text,
    candidate_count integer,
    applied_count integer,
    rejected_count integer,
    failure_kind text,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    CONSTRAINT agent_memory_extraction_jobs_status_check
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'discarded')),
    CONSTRAINT agent_memory_extraction_jobs_attempt_check
        CHECK (attempt_count >= 0),
    CONSTRAINT agent_memory_extraction_jobs_version_check
        CHECK (
            (policy_version IS NULL OR (
                octet_length(policy_version) BETWEEN 1 AND 64
                AND policy_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
            ))
            AND (prompt_version IS NULL OR (
                octet_length(prompt_version) BETWEEN 1 AND 64
                AND prompt_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
            ))
        ),
    CONSTRAINT agent_memory_extraction_jobs_provider_check
        CHECK (
            (provider IS NULL OR provider ~ '^[a-z][a-z0-9_-]{0,63}$')
            AND (model IS NULL OR model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$')
        ),
    CONSTRAINT agent_memory_extraction_jobs_result_check
        CHECK (
            (candidate_count IS NULL OR candidate_count BETWEEN 0 AND 5)
            AND (applied_count IS NULL OR applied_count BETWEEN 0 AND 5)
            AND (rejected_count IS NULL OR rejected_count BETWEEN 0 AND 5)
            AND (
                candidate_count IS NULL
                OR (
                    applied_count IS NOT NULL
                    AND rejected_count IS NOT NULL
                    AND candidate_count = applied_count + rejected_count
                )
            )
            AND (
                failure_kind IS NULL
                OR failure_kind ~ '^[a-z][a-z0-9_]{0,63}$'
            )
        ),
    CONSTRAINT agent_memory_extraction_jobs_state_shape_check
        CHECK (
            (
                status = 'pending'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NULL
                AND candidate_count IS NULL
                AND applied_count IS NULL
                AND rejected_count IS NULL
            )
            OR (
                status = 'running'
                AND attempt_count > 0
                AND lease_token IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND completed_at IS NULL
                AND candidate_count IS NULL
                AND applied_count IS NULL
                AND rejected_count IS NULL
                AND failure_kind IS NULL
            )
            OR (
                status = 'completed'
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NOT NULL
                AND candidate_count IS NOT NULL
                AND applied_count IS NOT NULL
                AND rejected_count IS NOT NULL
                AND failure_kind IS NULL
            )
            OR (
                status IN ('failed', 'discarded')
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NOT NULL
                AND candidate_count IS NULL
                AND applied_count IS NULL
                AND rejected_count IS NULL
                AND failure_kind IS NOT NULL
            )
        ),
    CONSTRAINT agent_memory_extraction_jobs_timestamps_check
        CHECK (
            updated_at >= created_at
            AND source_completed_at <= created_at
            AND next_attempt_at >= created_at
            AND (lease_expires_at IS NULL OR lease_expires_at > updated_at)
            AND (completed_at IS NULL OR completed_at >= created_at)
        )
);

CREATE INDEX agent_memory_extraction_jobs_claim_idx
    ON agent_memory_extraction_jobs (
        next_attempt_at,
        source_completed_at,
        source_run_id
    )
    WHERE status = 'pending';

CREATE INDEX agent_memory_extraction_jobs_recovery_idx
    ON agent_memory_extraction_jobs (
        lease_expires_at,
        source_run_id
    )
    WHERE status = 'running';

CREATE INDEX agent_memory_extraction_jobs_owner_idx
    ON agent_memory_extraction_jobs (
        owner_user_id,
        created_at DESC,
        source_run_id DESC
    );

CREATE FUNCTION enqueue_agent_memory_extraction_job()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO agent_memory_extraction_jobs (
        source_run_id,
        owner_user_id,
        source_thread_id,
        source_input_message_id,
        source_assistant_message_id,
        source_attempt,
        source_completed_at,
        status,
        attempt_count,
        next_attempt_at,
        created_at,
        updated_at
    ) VALUES (
        NEW.id,
        NEW.owner_user_id,
        NEW.thread_id,
        NEW.input_message_id,
        NEW.assistant_message_id,
        NEW.attempt_no,
        NEW.completed_at,
        'pending',
        0,
        transaction_timestamp(),
        transaction_timestamp(),
        transaction_timestamp()
    )
    ON CONFLICT (source_run_id) DO NOTHING;
    RETURN NEW;
END;
$$;

CREATE TRIGGER agent_runs_enqueue_memory_extraction
AFTER UPDATE OF status ON agent_runs
FOR EACH ROW
WHEN (
    NEW.status = 'completed'
    AND OLD.status IS DISTINCT FROM NEW.status
)
EXECUTE FUNCTION enqueue_agent_memory_extraction_job();

COMMIT;
