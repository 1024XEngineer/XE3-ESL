BEGIN;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_learning_profile_check,
    DROP CONSTRAINT agent_context_manifests_learning_profile_policy_check,
    DROP COLUMN selected_learning_profile,
    DROP COLUMN learning_profile_context_policy_version;

COMMIT;
