BEGIN;

ALTER TABLE agent_context_manifests
    ADD COLUMN blocked_tools jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN intent_mode text,
    ADD COLUMN intent_reason_code text,
    ADD COLUMN intent_guard_version text,
    ADD COLUMN tool_policy_version text,
    ADD CONSTRAINT agent_context_manifests_blocked_tools_check
        CHECK (jsonb_typeof(blocked_tools) = 'array'),
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

COMMIT;
