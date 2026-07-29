BEGIN;

CREATE INDEX agent_memory_extraction_jobs_owner_cutoff_idx
    ON agent_memory_extraction_jobs (
        owner_user_id,
        source_completed_at,
        source_run_id
    )
    INCLUDE (status);

COMMIT;
