BEGIN;

ALTER TABLE agent_threads
    ADD COLUMN title text,
    ADD CONSTRAINT agent_threads_title_check
        CHECK (
            title IS NULL
            OR (
                char_length(title) BETWEEN 2 AND 32
                AND octet_length(title) <= 128
                AND title = btrim(title)
                AND title !~ '[[:cntrl:]]'
            )
        );

CREATE TABLE agent_thread_title_jobs (
    source_thread_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    source_run_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    attempt_count integer NOT NULL DEFAULT 0,
    lease_token uuid,
    lease_expires_at timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    prompt_version text COLLATE "C",
    provider text,
    model text,
    failure_kind text,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    CONSTRAINT agent_thread_title_jobs_thread_owner_fkey
        FOREIGN KEY (source_thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_thread_title_jobs_run_owner_fkey
        FOREIGN KEY (source_run_id, owner_user_id, source_thread_id)
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_thread_title_jobs_status_check
        CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    CONSTRAINT agent_thread_title_jobs_attempt_check
        CHECK (attempt_count >= 0),
    CONSTRAINT agent_thread_title_jobs_generation_check
        CHECK (
            (
                prompt_version IS NULL
                AND provider IS NULL
                AND model IS NULL
            )
            OR (
                prompt_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND provider ~ '^[a-z][a-z0-9_-]{0,63}$'
                AND model
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
        ),
    CONSTRAINT agent_thread_title_jobs_failure_check
        CHECK (
            failure_kind IS NULL
            OR failure_kind ~ '^[a-z][a-z0-9_]{0,63}$'
        ),
    CONSTRAINT agent_thread_title_jobs_state_check
        CHECK (
            (
                status = 'pending'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NULL
            )
            OR (
                status = 'running'
                AND attempt_count > 0
                AND lease_token IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND failure_kind IS NULL
                AND completed_at IS NULL
            )
            OR (
                status = 'completed'
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND failure_kind IS NULL
                AND completed_at IS NOT NULL
            )
            OR (
                status = 'failed'
                AND attempt_count > 0
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND failure_kind IS NOT NULL
                AND completed_at IS NOT NULL
            )
        ),
    CONSTRAINT agent_thread_title_jobs_timestamps_check
        CHECK (
            updated_at >= created_at
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

CREATE INDEX agent_thread_title_jobs_claim_idx
    ON agent_thread_title_jobs (next_attempt_at, created_at, source_thread_id)
    WHERE status = 'pending';

CREATE INDEX agent_thread_title_jobs_recovery_idx
    ON agent_thread_title_jobs (lease_expires_at, source_thread_id)
    WHERE status = 'running';

CREATE FUNCTION enqueue_agent_thread_title_job()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO agent_thread_title_jobs (
        source_thread_id,
        owner_user_id,
        source_run_id
    )
    SELECT NEW.thread_id, NEW.owner_user_id, NEW.id
    FROM agent_threads AS threads
    WHERE threads.id = NEW.thread_id
      AND threads.owner_user_id = NEW.owner_user_id
      AND threads.title IS NULL
    ON CONFLICT (source_thread_id) DO NOTHING;
    RETURN NEW;
END;
$$;

CREATE TRIGGER agent_runs_enqueue_thread_title
AFTER UPDATE OF status ON agent_runs
FOR EACH ROW
WHEN (
    NEW.status = 'completed'
    AND OLD.status IS DISTINCT FROM NEW.status
)
EXECUTE FUNCTION enqueue_agent_thread_title_job();

INSERT INTO agent_thread_title_jobs (
    source_thread_id,
    owner_user_id,
    source_run_id
)
SELECT DISTINCT ON (runs.owner_user_id, runs.thread_id)
    runs.thread_id,
    runs.owner_user_id,
    runs.id
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads
    ON threads.id = runs.thread_id
   AND threads.owner_user_id = runs.owner_user_id
WHERE runs.status = 'completed'
  AND threads.title IS NULL
ORDER BY
    runs.owner_user_id,
    runs.thread_id,
    runs.completed_at,
    runs.id
ON CONFLICT (source_thread_id) DO NOTHING;

COMMIT;
