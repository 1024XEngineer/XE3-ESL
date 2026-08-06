BEGIN;

ALTER TABLE agent_thread_summary_checkpoints
    ADD CONSTRAINT agent_thread_summary_checkpoints_manifest_identity_unique
        UNIQUE (
            id,
            owner_user_id,
            thread_id,
            source_from_sequence,
            covered_through_sequence,
            policy_version,
            prompt_version,
            provider,
            model
        );

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_budget_check,
    ADD COLUMN summary_context_policy_version TEXT COLLATE "C" NOT NULL
        DEFAULT 'none',
    ADD COLUMN summary_context_status TEXT COLLATE "C" NOT NULL
        DEFAULT 'not_available',
    ADD COLUMN selected_summary_checkpoint_id UUID,
    ADD COLUMN selected_summary_source_from_sequence BIGINT,
    ADD COLUMN selected_summary_covered_through_sequence BIGINT,
    ADD COLUMN selected_summary_policy_version TEXT COLLATE "C",
    ADD COLUMN selected_summary_prompt_version TEXT COLLATE "C",
    ADD COLUMN selected_summary_provider TEXT COLLATE "C",
    ADD COLUMN selected_summary_model TEXT COLLATE "C",
    ADD CONSTRAINT agent_context_manifests_summary_policy_check
        CHECK (
            summary_context_policy_version
                ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
        ),
    ADD CONSTRAINT agent_context_manifests_summary_status_check
        CHECK (
            summary_context_status IN (
                'not_available',
                'selected',
                'omitted_budget'
            )
        ),
    ADD CONSTRAINT agent_context_manifests_summary_shape_check
        CHECK (
            (
                summary_context_status = 'selected'
                AND summary_context_policy_version <> 'none'
                AND selected_summary_checkpoint_id IS NOT NULL
                AND selected_summary_source_from_sequence >= 1
                AND selected_summary_covered_through_sequence
                    >= selected_summary_source_from_sequence
                AND selected_summary_policy_version IS NOT NULL
                AND selected_summary_prompt_version IS NOT NULL
                AND selected_summary_provider IS NOT NULL
                AND selected_summary_model IS NOT NULL
            )
            OR
            (
                summary_context_status IN (
                    'not_available',
                    'omitted_budget'
                )
                AND (
                    summary_context_status = 'not_available'
                    OR summary_context_policy_version <> 'none'
                )
                AND selected_summary_checkpoint_id IS NULL
                AND selected_summary_source_from_sequence IS NULL
                AND selected_summary_covered_through_sequence IS NULL
                AND selected_summary_policy_version IS NULL
                AND selected_summary_prompt_version IS NULL
                AND selected_summary_provider IS NULL
                AND selected_summary_model IS NULL
            )
        ),
    ADD CONSTRAINT agent_context_manifests_summary_checkpoint_fkey
        FOREIGN KEY (
            selected_summary_checkpoint_id,
            owner_user_id,
            thread_id,
            selected_summary_source_from_sequence,
            selected_summary_covered_through_sequence,
            selected_summary_policy_version,
            selected_summary_prompt_version,
            selected_summary_provider,
            selected_summary_model
        )
        REFERENCES agent_thread_summary_checkpoints (
            id,
            owner_user_id,
            thread_id,
            source_from_sequence,
            covered_through_sequence,
            policy_version,
            prompt_version,
            provider,
            model
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT agent_context_manifests_budget_check
        CHECK (
            omitted_message_count >= 0
            AND trim_reason IN (
                'none',
                'context_budget',
                'summary_checkpoint',
                'summary_checkpoint_and_budget'
            )
            AND max_input_characters BETWEEN 5000 AND 1000000
            AND used_input_characters > 0
            AND used_input_characters <= max_input_characters
            AND max_output_tokens BETWEEN 1 AND 1000000
        );

COMMIT;
