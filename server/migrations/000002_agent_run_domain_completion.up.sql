BEGIN;

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_state_check,
    ADD COLUMN domain_result jsonb,
    ADD CONSTRAINT agent_runs_domain_result_check CHECK (
        domain_result IS NULL OR (
            jsonb_typeof(domain_result) = 'object'
            AND domain_result ?& ARRAY['tool_call_id', 'tool_name']
            AND domain_result - ARRAY['tool_call_id', 'tool_name'] = '{}'::jsonb
            AND domain_result->>'tool_call_id' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND domain_result->>'tool_name' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        )
    ),
    ADD CONSTRAINT agent_runs_state_check CHECK (
        (
            status = 'pending' AND phase = 'queued'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NULL AND completed_at IS NULL
            AND model_result IS NULL AND usage IS NULL
            AND domain_result IS NULL AND error IS NULL
        ) OR (
            status = 'running' AND phase IN ('context', 'model', 'tool')
            AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL
            AND started_at IS NOT NULL AND completed_at IS NULL
            AND model_result IS NULL AND usage IS NULL
            AND domain_result IS NULL AND error IS NULL
        ) OR (
            status = 'completed' AND phase = 'completed'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NOT NULL AND completed_at IS NOT NULL
            AND (
                (model_result IS NOT NULL AND usage IS NOT NULL AND domain_result IS NULL)
                OR
                (model_result IS NULL AND usage IS NULL AND domain_result IS NOT NULL)
            )
            AND error IS NULL
        ) OR (
            status = 'failed' AND phase = 'failed'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NOT NULL AND completed_at IS NOT NULL
            AND model_result IS NULL AND usage IS NULL
            AND domain_result IS NULL AND error IS NOT NULL
        )
    );

COMMIT;
