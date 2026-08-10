BEGIN;

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_provider_check,
    ADD CONSTRAINT agent_runs_provider_check
        CHECK (
            requested_provider ~ '^[a-z][a-z0-9_-]{0,63}$'
            AND octet_length(requested_model) BETWEEN 1 AND 128
            AND requested_model ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$'
            AND max_output_tokens BETWEEN 1 AND 1000000
        ),
    DROP CONSTRAINT agent_runs_result_text_check,
    ADD CONSTRAINT agent_runs_result_text_check
        CHECK (
            (
                provider_completion_id IS NULL
                OR provider_completion_id ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
            AND (
                provider_model IS NULL
                OR (
                    octet_length(provider_model) BETWEEN 1 AND 128
                    AND provider_model ~
                        '^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$'
                )
            )
            AND (finish_reason IS NULL OR finish_reason IN ('stop', 'length'))
            AND (
                failure_kind IS NULL
                OR failure_kind ~ '^[a-z][a-z0-9_]{0,63}$'
            )
        );

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_provider_check,
    ADD CONSTRAINT agent_context_manifests_provider_check
        CHECK (
            requested_provider ~ '^[a-z][a-z0-9_-]{0,63}$'
            AND octet_length(requested_model) BETWEEN 1 AND 128
            AND requested_model ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$'
        );

ALTER TABLE agent_memory_extraction_jobs
    DROP CONSTRAINT agent_memory_extraction_jobs_provider_check,
    ADD CONSTRAINT agent_memory_extraction_jobs_provider_check
        CHECK (
            (provider IS NULL OR provider ~ '^[a-z][a-z0-9_-]{0,63}$')
            AND (
                model IS NULL
                OR (
                    octet_length(model) BETWEEN 1 AND 128
                    AND model ~
                        '^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$'
                )
            )
        );

ALTER TABLE agent_thread_summary_checkpoints
    DROP CONSTRAINT agent_thread_summary_checkpoints_model_check,
    ADD CONSTRAINT agent_thread_summary_checkpoints_model_check
        CHECK (
            octet_length(model) BETWEEN 1 AND 128
            AND model ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$'
        );

ALTER TABLE agent_thread_summary_jobs
    DROP CONSTRAINT agent_thread_summary_jobs_versions_check,
    ADD CONSTRAINT agent_thread_summary_jobs_versions_check
        CHECK (
            (
                trigger_policy_version IS NULL
                AND summary_policy_version IS NULL
                AND prompt_version IS NULL
                AND provider IS NULL
                AND model IS NULL
            )
            OR
            (
                trigger_policy_version IS NOT NULL
                AND summary_policy_version IS NOT NULL
                AND prompt_version IS NOT NULL
                AND provider IS NOT NULL
                AND model IS NOT NULL
                AND trigger_policy_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND summary_policy_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND prompt_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND provider ~ '^[a-z][a-z0-9_-]{0,63}$'
                AND octet_length(model) BETWEEN 1 AND 128
                AND model ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$'
            )
        );

ALTER TABLE agent_thread_title_jobs
    DROP CONSTRAINT agent_thread_title_jobs_generation_check,
    ADD CONSTRAINT agent_thread_title_jobs_generation_check
        CHECK (
            (
                prompt_version IS NULL
                AND provider IS NULL
                AND model IS NULL
            )
            OR (
                prompt_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
                AND provider ~ '^[a-z][a-z0-9_-]{0,63}$'
                AND octet_length(model) BETWEEN 1 AND 128
                AND model ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$'
            )
        );

COMMIT;
