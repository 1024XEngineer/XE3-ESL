BEGIN;

CREATE TABLE agent_thread_summary_jobs (
    source_run_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    source_thread_id uuid NOT NULL,
    observed_through_sequence bigint NOT NULL,
    source_completed_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    attempt_count integer NOT NULL DEFAULT 0,
    lease_token uuid,
    lease_expires_at timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    trigger_policy_version text COLLATE "C",
    summary_policy_version text COLLATE "C",
    prompt_version text COLLATE "C",
    provider text,
    model text,
    target_covered_through_sequence bigint,
    checkpoint_id uuid,
    outcome_reason text,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    CONSTRAINT agent_thread_summary_jobs_run_owner_fkey
        FOREIGN KEY (
            source_run_id,
            owner_user_id,
            source_thread_id
        )
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_thread_summary_jobs_checkpoint_owner_fkey
        FOREIGN KEY (
            checkpoint_id,
            owner_user_id,
            source_thread_id
        )
        REFERENCES agent_thread_summary_checkpoints (
            id,
            owner_user_id,
            thread_id
        )
        ON DELETE CASCADE,
    CONSTRAINT agent_thread_summary_jobs_status_check
        CHECK (
            status IN (
                'pending',
                'running',
                'completed',
                'skipped',
                'superseded',
                'failed'
            )
        ),
    CONSTRAINT agent_thread_summary_jobs_sequence_check
        CHECK (
            observed_through_sequence >= 1
            AND (
                target_covered_through_sequence IS NULL
                OR (
                    target_covered_through_sequence >= 1
                    AND target_covered_through_sequence
                        <= observed_through_sequence
                )
            )
        ),
    CONSTRAINT agent_thread_summary_jobs_attempt_check
        CHECK (attempt_count >= 0),
    CONSTRAINT agent_thread_summary_jobs_versions_check
        CHECK (
            (
                trigger_policy_version IS NULL
                AND summary_policy_version IS NULL
                AND prompt_version IS NULL
                AND provider IS NULL
                AND model IS NULL
            )
            OR
            (
                trigger_policy_version IS NOT NULL
                AND summary_policy_version IS NOT NULL
                AND prompt_version IS NOT NULL
                AND provider IS NOT NULL
                AND model IS NOT NULL
                AND trigger_policy_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND summary_policy_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND prompt_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND provider ~ '^[a-z][a-z0-9_-]{0,63}$'
                AND model
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
        ),
    CONSTRAINT agent_thread_summary_jobs_reason_check
        CHECK (
            outcome_reason IS NULL
            OR outcome_reason ~ '^[a-z][a-z0-9_]{0,63}$'
        ),
    CONSTRAINT agent_thread_summary_jobs_state_shape_check
        CHECK (
            (
                status = 'pending'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND checkpoint_id IS NULL
                AND completed_at IS NULL
            )
            OR
            (
                status = 'running'
                AND attempt_count > 0
                AND lease_token IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND checkpoint_id IS NULL
                AND outcome_reason IS NULL
                AND completed_at IS NULL
            )
            OR
            (
                status = 'completed'
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND target_covered_through_sequence IS NOT NULL
                AND checkpoint_id IS NOT NULL
                AND outcome_reason IS NULL
                AND completed_at IS NOT NULL
            )
            OR
            (
                status = 'skipped'
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND target_covered_through_sequence IS NULL
                AND checkpoint_id IS NULL
                AND outcome_reason IS NOT NULL
                AND completed_at IS NOT NULL
            )
            OR
            (
                status = 'superseded'
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND target_covered_through_sequence IS NOT NULL
                AND checkpoint_id IS NULL
                AND outcome_reason IS NOT NULL
                AND completed_at IS NOT NULL
            )
            OR
            (
                status = 'failed'
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND checkpoint_id IS NULL
                AND outcome_reason IS NOT NULL
                AND completed_at IS NOT NULL
            )
        ),
    CONSTRAINT agent_thread_summary_jobs_timestamps_check
        CHECK (
            source_completed_at <= created_at
            AND updated_at >= created_at
            AND next_attempt_at >= created_at
            AND (
                lease_expires_at IS NULL
                OR lease_expires_at > updated_at
            )
            AND (
                completed_at IS NULL
                OR completed_at >= created_at
            )
        )
);

CREATE INDEX agent_thread_summary_jobs_claim_idx
    ON agent_thread_summary_jobs (
        next_attempt_at,
        source_completed_at,
        source_run_id
    )
    WHERE status = 'pending';

CREATE INDEX agent_thread_summary_jobs_recovery_idx
    ON agent_thread_summary_jobs (
        lease_expires_at,
        source_run_id
    )
    WHERE status = 'running';

CREATE INDEX agent_thread_summary_jobs_owner_thread_idx
    ON agent_thread_summary_jobs (
        owner_user_id,
        source_thread_id,
        observed_through_sequence,
        source_run_id
    );

CREATE UNIQUE INDEX agent_thread_summary_jobs_one_running_per_thread_idx
    ON agent_thread_summary_jobs (
        owner_user_id,
        source_thread_id
    )
    WHERE status = 'running';

CREATE FUNCTION enqueue_agent_thread_summary_job()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    observed_sequence bigint;
BEGIN
    SELECT sequence_no
    INTO STRICT observed_sequence
    FROM agent_messages
    WHERE id = NEW.assistant_message_id
      AND owner_user_id = NEW.owner_user_id
      AND thread_id = NEW.thread_id;

    INSERT INTO agent_thread_summary_jobs (
        source_run_id,
        owner_user_id,
        source_thread_id,
        observed_through_sequence,
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
        observed_sequence,
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

CREATE TRIGGER agent_runs_enqueue_thread_summary
AFTER UPDATE OF status ON agent_runs
FOR EACH ROW
WHEN (
    NEW.status = 'completed'
    AND OLD.status IS DISTINCT FROM NEW.status
)
EXECUTE FUNCTION enqueue_agent_thread_summary_job();

COMMIT;
