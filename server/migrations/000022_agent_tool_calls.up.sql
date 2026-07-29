BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

ALTER TABLE agent_context_manifests
    ADD COLUMN exposed_tools jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN blocked_tools jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN intent_mode text COLLATE "C",
    ADD COLUMN intent_reason_code text COLLATE "C",
    ADD COLUMN intent_guard_version text COLLATE "C",
    ADD COLUMN tool_policy_version text COLLATE "C",
    ADD COLUMN tool_schema_hashes jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT agent_context_manifests_tool_snapshot_check
        CHECK (
            jsonb_typeof(exposed_tools) = 'array'
            AND jsonb_typeof(blocked_tools) = 'array'
            AND jsonb_typeof(tool_schema_hashes) = 'object'
            AND (
                intent_mode IS NULL
                OR intent_mode IN ('direct_only', 'tool_eligible')
            )
            AND (
                intent_reason_code IS NULL
                OR intent_reason_code ~ '^[a-z][a-z0-9_]{0,63}$'
            )
            AND (
                intent_guard_version IS NULL
                OR intent_guard_version ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
            AND (
                tool_policy_version IS NULL
                OR tool_policy_version ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
        );

CREATE TABLE agent_tool_calls (
    id text COLLATE "C" NOT NULL,
    run_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    tool_name text COLLATE "C" NOT NULL,
    schema_version text COLLATE "C" NOT NULL,
    input jsonb NOT NULL,
    status text NOT NULL,
    result jsonb,
    error_category text COLLATE "C",
    request_id text COLLATE "C",
    source_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    proposed_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, id),
    CONSTRAINT agent_tool_calls_run_owner_fkey
        FOREIGN KEY (run_id, owner_user_id, thread_id)
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_tool_calls_id_check
        CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT agent_tool_calls_tool_name_check
        CHECK (tool_name ~ '^[a-z][a-z0-9_.-]{0,127}$'),
    CONSTRAINT agent_tool_calls_schema_version_check
        CHECK (schema_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT agent_tool_calls_status_check
        CHECK (
            status IN (
                'proposed',
                'running',
                'succeeded',
                'failed',
                'rejected'
            )
        ),
    CONSTRAINT agent_tool_calls_error_check
        CHECK (
            error_category IS NULL
            OR error_category IN (
                'invalid_input',
                'not_found',
                'conflict',
                'permission_denied',
                'unavailable',
                'timeout',
                'internal',
                'unknown_tool'
            )
        ),
    CONSTRAINT agent_tool_calls_request_id_check
        CHECK (
            request_id IS NULL
            OR (
                octet_length(request_id) BETWEEN 1 AND 256
                AND request_id !~ '[[:cntrl:]]'
            )
        ),
    CONSTRAINT agent_tool_calls_json_shape_check
        CHECK (
            jsonb_typeof(input) = 'object'
            AND (result IS NULL OR jsonb_typeof(result) = 'object')
            AND jsonb_typeof(source_refs) = 'array'
        ),
    CONSTRAINT agent_tool_calls_state_shape_check
        CHECK (
            (
                status = 'proposed'
                AND started_at IS NULL
                AND completed_at IS NULL
                AND result IS NULL
                AND error_category IS NULL
            )
            OR
            (
                status = 'running'
                AND started_at IS NOT NULL
                AND completed_at IS NULL
                AND result IS NULL
                AND error_category IS NULL
            )
            OR
            (
                status = 'succeeded'
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
                AND result IS NOT NULL
                AND error_category IS NULL
            )
            OR
            (
                status IN ('failed', 'rejected')
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
                AND result IS NULL
                AND error_category IS NOT NULL
            )
        ),
    CONSTRAINT agent_tool_calls_timestamps_check
        CHECK (
            updated_at >= proposed_at
            AND (started_at IS NULL OR started_at >= proposed_at)
            AND (
                completed_at IS NULL
                OR (
                    started_at IS NOT NULL
                    AND completed_at >= started_at
                )
            )
        )
);

CREATE INDEX agent_tool_calls_owner_run_idx
    ON agent_tool_calls (owner_user_id, run_id, proposed_at, id);

COMMIT;
