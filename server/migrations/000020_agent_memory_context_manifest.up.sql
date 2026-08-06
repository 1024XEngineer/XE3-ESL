BEGIN;

ALTER TABLE agent_context_manifests
    ADD COLUMN memory_context_policy_version text NOT NULL
        DEFAULT 'none',
    ADD COLUMN selected_memories jsonb NOT NULL
        DEFAULT '[]'::jsonb,
    ADD CONSTRAINT agent_context_manifests_memory_policy_check
        CHECK (
            memory_context_policy_version
                ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
        ),
    ADD CONSTRAINT agent_context_manifests_memories_check
        CHECK (jsonb_typeof(selected_memories) = 'array');

COMMIT;
