BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

ALTER TABLE agent_runs
    ADD CONSTRAINT agent_runs_id_owner_thread_input_key
        UNIQUE (id, owner_user_id, thread_id, input_message_id),
    DROP CONSTRAINT agent_runs_retry_of_fkey,
    ADD CONSTRAINT agent_runs_retry_of_fkey
        FOREIGN KEY (
            retry_of_run_id,
            owner_user_id,
            thread_id,
            input_message_id
        )
        REFERENCES agent_runs (
            id,
            owner_user_id,
            thread_id,
            input_message_id
        )
        ON DELETE RESTRICT,
    DROP CONSTRAINT agent_runs_result_numbers_check,
    ADD CONSTRAINT agent_runs_result_numbers_check
        CHECK (
            (input_tokens IS NULL OR input_tokens >= 0)
            AND (output_tokens IS NULL OR output_tokens >= 0)
            AND (total_tokens IS NULL OR total_tokens >= 0)
            AND (
                total_tokens IS NULL
                OR total_tokens::bigint =
                    input_tokens::bigint + output_tokens::bigint
            )
        ),
    ADD CONSTRAINT agent_runs_result_model_check
        CHECK (
            provider_model IS NULL
            OR provider_model = requested_model
        ),
    -- Version four permitted providers to report more output usage than the
    -- requested generation budget. Preserve those immutable audit facts while
    -- enforcing the corrected invariant for every post-migration write.
    ADD CONSTRAINT agent_runs_result_budget_check
        CHECK (
            output_tokens IS NULL
            OR output_tokens <= max_output_tokens
        )
        NOT VALID,
    DROP CONSTRAINT agent_runs_state_shape_check,
    ADD CONSTRAINT agent_runs_state_shape_check
        CHECK (
            (
                status = 'pending'
                AND started_at IS NULL
                AND completed_at IS NULL
                AND assistant_message_id IS NULL
                AND provider_completion_id IS NULL
                AND provider_model IS NULL
                AND finish_reason IS NULL
                AND input_tokens IS NULL
                AND output_tokens IS NULL
                AND total_tokens IS NULL
                AND failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                status = 'running'
                AND started_at IS NOT NULL
                AND completed_at IS NULL
                AND assistant_message_id IS NULL
                AND provider_completion_id IS NULL
                AND provider_model IS NULL
                AND finish_reason IS NULL
                AND input_tokens IS NULL
                AND output_tokens IS NULL
                AND total_tokens IS NULL
                AND failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                status = 'completed'
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
                AND assistant_message_id IS NOT NULL
                AND provider_completion_id IS NOT NULL
                AND provider_model IS NOT NULL
                AND finish_reason IS NOT NULL
                AND input_tokens IS NOT NULL
                AND output_tokens IS NOT NULL
                AND total_tokens IS NOT NULL
                AND failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                status = 'failed'
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
                AND assistant_message_id IS NULL
                AND provider_completion_id IS NULL
                AND provider_model IS NULL
                AND finish_reason IS NULL
                AND input_tokens IS NULL
                AND output_tokens IS NULL
                AND total_tokens IS NULL
                AND failure_kind IS NOT NULL
                AND failure_retryable IS NOT NULL
            )
        );

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_matter_shape_check,
    DROP COLUMN active_matter_title,
    ADD CONSTRAINT agent_context_manifests_matter_shape_check
        CHECK (
            (
                active_matter_id IS NULL
                AND active_matter_version IS NULL
            )
            OR
            (
                active_matter_id IS NOT NULL
                AND active_matter_version > 0
            )
        );

COMMIT;
