BEGIN;

ALTER TABLE agent_context_manifests
    ADD COLUMN exposed_tools jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN blocked_tools jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN intent_mode text,
    ADD COLUMN intent_reason_code text,
    ADD COLUMN intent_guard_version text,
    ADD COLUMN tool_policy_version text,
    ADD COLUMN tool_schema_hashes jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT agent_context_manifests_exposed_tools_check
        CHECK (jsonb_typeof(exposed_tools) = 'array'),
    ADD CONSTRAINT agent_context_manifests_blocked_tools_check
        CHECK (jsonb_typeof(blocked_tools) = 'array'),
    ADD CONSTRAINT agent_context_manifests_tool_schema_hashes_check
        CHECK (jsonb_typeof(tool_schema_hashes) = 'object'),
    ADD CONSTRAINT agent_context_manifests_intent_mode_check
        CHECK (
            intent_mode IS NULL
            OR intent_mode IN ('direct_only', 'tool_eligible')
        ),
    ADD CONSTRAINT agent_context_manifests_tool_snapshot_versions_check
        CHECK (
            (
                intent_guard_version IS NULL
                OR intent_guard_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
            )
            AND (
                tool_policy_version IS NULL
                OR tool_policy_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
            )
        ),
    ADD CONSTRAINT agent_context_manifests_tool_snapshot_shape_check
        CHECK (
            (
                intent_mode IS NULL
                AND intent_reason_code IS NULL
                AND intent_guard_version IS NULL
                AND tool_policy_version IS NULL
            )
            OR
            (
                intent_mode IS NOT NULL
                AND intent_reason_code IS NOT NULL
                AND intent_guard_version IS NOT NULL
                AND tool_policy_version IS NOT NULL
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
    status text NOT NULL DEFAULT 'proposed',
    result jsonb,
    error_category text,
    request_id text COLLATE "C",
    source_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    proposed_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT agent_tool_calls_pkey PRIMARY KEY (run_id, id),
    CONSTRAINT agent_tool_calls_run_owner_fkey
        FOREIGN KEY (run_id, owner_user_id, thread_id)
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_tool_calls_id_check
        CHECK (
            octet_length(id) BETWEEN 1 AND 128
            AND id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT agent_tool_calls_name_check
        CHECK (
            octet_length(tool_name) BETWEEN 1 AND 128
            AND tool_name ~ '^[a-z][a-z0-9._-]{0,127}$'
        ),
    CONSTRAINT agent_tool_calls_schema_version_check
        CHECK (
            octet_length(schema_version) BETWEEN 1 AND 64
            AND schema_version
                ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
        ),
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
    CONSTRAINT agent_tool_calls_error_category_check
        CHECK (
            error_category IS NULL
            OR (
                octet_length(error_category) BETWEEN 1 AND 64
                AND error_category ~ '^[a-z][a-z0-9_]{0,63}$'
            )
        ),
    CONSTRAINT agent_tool_calls_request_id_check
        CHECK (
            request_id IS NULL
            OR (
                octet_length(request_id) BETWEEN 1 AND 256
                AND request_id ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$'
            )
        ),
    CONSTRAINT agent_tool_calls_source_refs_check
        CHECK (jsonb_typeof(source_refs) = 'array'),
    CONSTRAINT agent_tool_calls_state_shape_check
        CHECK (
            (
                status = 'proposed'
                AND result IS NULL
                AND error_category IS NULL
                AND request_id IS NULL
                AND started_at IS NULL
                AND completed_at IS NULL
            )
            OR
            (
                status = 'running'
                AND result IS NULL
                AND error_category IS NULL
                AND request_id IS NOT NULL
                AND started_at IS NOT NULL
                AND completed_at IS NULL
            )
            OR
            (
                status = 'succeeded'
                AND result IS NOT NULL
                AND error_category IS NULL
                AND request_id IS NOT NULL
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
            )
            OR
            (
                status IN ('failed', 'rejected')
                AND result IS NULL
                AND error_category IS NOT NULL
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
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

CREATE INDEX agent_tool_calls_owner_run_proposed_idx
    ON agent_tool_calls (
        owner_user_id,
        run_id,
        proposed_at,
        id
    );

COMMIT;
