BEGIN;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_memories_check,
    DROP CONSTRAINT agent_context_manifests_memory_policy_check,
    DROP COLUMN selected_memories,
    DROP COLUMN memory_context_policy_version;

COMMIT;
