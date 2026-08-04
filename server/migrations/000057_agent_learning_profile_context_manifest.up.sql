BEGIN;

ALTER TABLE agent_context_manifests
    ADD COLUMN learning_profile_context_policy_version text COLLATE "C" NOT NULL
        DEFAULT 'none',
    ADD COLUMN selected_learning_profile jsonb NOT NULL
        DEFAULT '[]'::jsonb,
    ADD CONSTRAINT agent_context_manifests_learning_profile_policy_check
        CHECK (
            learning_profile_context_policy_version
                ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
        ),
    ADD CONSTRAINT agent_context_manifests_learning_profile_check
        CHECK (
            jsonb_typeof(selected_learning_profile) = 'array'
            AND jsonb_array_length(selected_learning_profile) <= 8
            AND octet_length(selected_learning_profile::text) <= 65536
        );

COMMIT;
