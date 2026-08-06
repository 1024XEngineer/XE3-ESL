BEGIN;

ALTER TABLE agent_context_manifests
    ADD COLUMN stable_profile_context_policy_version TEXT COLLATE "C" NOT NULL
        DEFAULT 'none',
    ADD COLUMN selected_stable_profile JSONB NOT NULL
        DEFAULT '[]'::jsonb,
    ADD CONSTRAINT agent_context_manifests_stable_profile_policy_check
        CHECK (
            stable_profile_context_policy_version
                ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
        ),
    ADD CONSTRAINT agent_context_manifests_stable_profile_check
        CHECK (
            jsonb_typeof(selected_stable_profile) = 'array'
            AND jsonb_array_length(selected_stable_profile) <= 6
        );

COMMIT;
