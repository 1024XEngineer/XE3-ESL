BEGIN;

DROP TABLE agent_tool_calls;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_tool_snapshot_shape_check,
    DROP CONSTRAINT agent_context_manifests_tool_snapshot_versions_check,
    DROP CONSTRAINT agent_context_manifests_intent_mode_check,
    DROP CONSTRAINT agent_context_manifests_tool_schema_hashes_check,
    DROP CONSTRAINT agent_context_manifests_blocked_tools_check,
    DROP CONSTRAINT agent_context_manifests_exposed_tools_check,
    DROP COLUMN tool_schema_hashes,
    DROP COLUMN tool_policy_version,
    DROP COLUMN intent_guard_version,
    DROP COLUMN intent_reason_code,
    DROP COLUMN intent_mode,
    DROP COLUMN blocked_tools,
    DROP COLUMN exposed_tools;

COMMIT;
