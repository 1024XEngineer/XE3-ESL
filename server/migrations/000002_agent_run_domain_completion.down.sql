BEGIN;

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_state_check,
    DROP CONSTRAINT agent_runs_domain_result_check,
    DROP COLUMN domain_result,
    ADD CONSTRAINT agent_runs_state_check CHECK (
        (
            status = 'pending' AND phase = 'queued'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NULL AND completed_at IS NULL
            AND model_result IS NULL AND usage IS NULL AND error IS NULL
        ) OR (
            status = 'running' AND phase IN ('context', 'model', 'tool')
            AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL
            AND started_at IS NOT NULL AND completed_at IS NULL
            AND model_result IS NULL AND usage IS NULL AND error IS NULL
        ) OR (
            status = 'completed' AND phase = 'completed'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NOT NULL AND completed_at IS NOT NULL
            AND model_result IS NOT NULL AND usage IS NOT NULL AND error IS NULL
        ) OR (
            status = 'failed' AND phase = 'failed'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NOT NULL AND completed_at IS NOT NULL
            AND model_result IS NULL AND usage IS NULL AND error IS NOT NULL
        )
    );

COMMIT;
