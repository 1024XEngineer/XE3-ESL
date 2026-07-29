BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DROP TABLE agent_tool_calls;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_tool_snapshot_check,
    DROP COLUMN tool_schema_hashes,
    DROP COLUMN tool_policy_version,
    DROP COLUMN intent_guard_version,
    DROP COLUMN intent_reason_code,
    DROP COLUMN intent_mode,
    DROP COLUMN blocked_tools,
    DROP COLUMN exposed_tools;

COMMIT;
