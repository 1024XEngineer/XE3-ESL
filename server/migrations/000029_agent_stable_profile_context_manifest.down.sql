BEGIN;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_stable_profile_check,
    DROP CONSTRAINT agent_context_manifests_stable_profile_policy_check,
    DROP COLUMN selected_stable_profile,
    DROP COLUMN stable_profile_context_policy_version;

COMMIT;
