BEGIN;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_summary_checkpoint_fkey,
    DROP CONSTRAINT agent_context_manifests_summary_shape_check,
    DROP CONSTRAINT agent_context_manifests_summary_status_check,
    DROP CONSTRAINT agent_context_manifests_summary_policy_check,
    DROP CONSTRAINT agent_context_manifests_budget_check,
    DROP COLUMN selected_summary_model,
    DROP COLUMN selected_summary_provider,
    DROP COLUMN selected_summary_prompt_version,
    DROP COLUMN selected_summary_policy_version,
    DROP COLUMN selected_summary_covered_through_sequence,
    DROP COLUMN selected_summary_source_from_sequence,
    DROP COLUMN selected_summary_checkpoint_id,
    DROP COLUMN summary_context_status,
    DROP COLUMN summary_context_policy_version,
    ADD CONSTRAINT agent_context_manifests_budget_check
        CHECK (
            omitted_message_count >= 0
            AND trim_reason IN ('none', 'context_budget')
            AND max_input_characters BETWEEN 5000 AND 1000000
            AND used_input_characters > 0
            AND used_input_characters <= max_input_characters
            AND max_output_tokens BETWEEN 1 AND 1000000
        );

ALTER TABLE agent_thread_summary_checkpoints
    DROP CONSTRAINT
        agent_thread_summary_checkpoints_manifest_identity_unique;

COMMIT;
