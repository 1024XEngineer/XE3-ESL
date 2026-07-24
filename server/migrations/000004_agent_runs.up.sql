BEGIN;

ALTER TABLE agent_messages
    DROP CONSTRAINT agent_messages_role_check,
    DROP CONSTRAINT agent_messages_client_id_check,
    ALTER COLUMN client_message_id DROP NOT NULL,
    ADD COLUMN produced_by_run_id uuid,
    ADD CONSTRAINT agent_messages_id_owner_thread_key
        UNIQUE (id, owner_user_id, thread_id),
    ADD CONSTRAINT agent_messages_role_check
        CHECK (role IN ('user', 'assistant')),
    ADD CONSTRAINT agent_messages_origin_check
        CHECK (
            (
                role = 'user'
                AND client_message_id IS NOT NULL
                AND produced_by_run_id IS NULL
            )
            OR
            (
                role = 'assistant'
                AND client_message_id IS NULL
                AND produced_by_run_id IS NOT NULL
            )
        ),
    ADD CONSTRAINT agent_messages_client_id_check
        CHECK (
            client_message_id IS NULL
            OR
            (
                octet_length(client_message_id) BETWEEN 1 AND 128
                AND client_message_id ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
        );

CREATE TABLE agent_runs (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    input_message_id uuid NOT NULL,
    attempt_no integer NOT NULL,
    retry_of_run_id uuid,
    retry_client_id text COLLATE "C",
    status text NOT NULL DEFAULT 'pending',
    requested_provider text NOT NULL,
    requested_model text NOT NULL,
    max_output_tokens integer NOT NULL,
    assistant_message_id uuid,
    provider_completion_id text,
    provider_model text,
    finish_reason text,
    input_tokens integer,
    output_tokens integer,
    total_tokens integer,
    failure_kind text,
    failure_retryable boolean,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_runs_id_owner_thread_key
        UNIQUE (id, owner_user_id, thread_id),
    CONSTRAINT agent_runs_thread_owner_fkey
        FOREIGN KEY (thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_runs_input_message_fkey
        FOREIGN KEY (input_message_id, owner_user_id, thread_id)
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_runs_retry_of_fkey
        FOREIGN KEY (retry_of_run_id, owner_user_id, thread_id)
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_runs_input_attempt_key
        UNIQUE (owner_user_id, thread_id, input_message_id, attempt_no),
    CONSTRAINT agent_runs_attempt_check CHECK (attempt_no > 0),
    CONSTRAINT agent_runs_retry_shape_check
        CHECK (
            (
                attempt_no = 1
                AND retry_of_run_id IS NULL
                AND retry_client_id IS NULL
            )
            OR
            (
                attempt_no > 1
                AND retry_of_run_id IS NOT NULL
                AND retry_client_id IS NOT NULL
            )
        ),
    CONSTRAINT agent_runs_retry_client_id_check
        CHECK (
            retry_client_id IS NULL
            OR
            (
                octet_length(retry_client_id) BETWEEN 1 AND 128
                AND retry_client_id ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
        ),
    CONSTRAINT agent_runs_status_check
        CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    CONSTRAINT agent_runs_provider_check
        CHECK (
            requested_provider ~ '^[a-z][a-z0-9_-]{0,63}$'
            AND requested_model ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND max_output_tokens BETWEEN 1 AND 1000000
        ),
    CONSTRAINT agent_runs_result_text_check
        CHECK (
            (
                provider_completion_id IS NULL
                OR provider_completion_id ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
            AND (
                provider_model IS NULL
                OR provider_model ~
                    '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
            AND (finish_reason IS NULL OR finish_reason IN ('stop', 'length'))
            AND (
                failure_kind IS NULL
                OR failure_kind ~ '^[a-z][a-z0-9_]{0,63}$'
            )
        ),
    CONSTRAINT agent_runs_result_numbers_check
        CHECK (
            (input_tokens IS NULL OR input_tokens >= 0)
            AND (output_tokens IS NULL OR output_tokens >= 0)
            AND (total_tokens IS NULL OR total_tokens >= 0)
        ),
    CONSTRAINT agent_runs_state_shape_check
        CHECK (
            (
                status = 'pending'
                AND started_at IS NULL
                AND completed_at IS NULL
                AND assistant_message_id IS NULL
                AND failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                status = 'running'
                AND started_at IS NOT NULL
                AND completed_at IS NULL
                AND assistant_message_id IS NULL
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
        ),
    CONSTRAINT agent_runs_timestamps_check
        CHECK (
            updated_at >= created_at
            AND (started_at IS NULL OR started_at >= created_at)
            AND (
                completed_at IS NULL
                OR (started_at IS NOT NULL AND completed_at >= started_at)
            )
        )
);

CREATE UNIQUE INDEX agent_runs_retry_client_key
    ON agent_runs (owner_user_id, thread_id, retry_client_id)
    WHERE retry_client_id IS NOT NULL;

CREATE INDEX agent_runs_owner_thread_created_idx
    ON agent_runs (owner_user_id, thread_id, created_at DESC, id DESC);

CREATE INDEX agent_runs_recoverable_idx
    ON agent_runs (status, updated_at)
    WHERE status IN ('pending', 'running');

ALTER TABLE agent_messages
    ADD CONSTRAINT agent_messages_produced_by_run_fkey
        FOREIGN KEY (produced_by_run_id, owner_user_id, thread_id)
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE RESTRICT;

CREATE UNIQUE INDEX agent_messages_one_assistant_per_run_idx
    ON agent_messages (produced_by_run_id)
    WHERE produced_by_run_id IS NOT NULL;

ALTER TABLE agent_runs
    ADD CONSTRAINT agent_runs_assistant_message_fkey
        FOREIGN KEY (assistant_message_id, owner_user_id, thread_id)
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE RESTRICT;

CREATE TABLE agent_context_manifests (
    run_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    input_message_id uuid NOT NULL,
    active_matter_id uuid,
    active_matter_version bigint,
    active_matter_title text,
    instruction_version text NOT NULL,
    selected_messages jsonb NOT NULL,
    omitted_message_count integer NOT NULL,
    trim_reason text NOT NULL,
    max_input_characters integer NOT NULL,
    used_input_characters integer NOT NULL,
    requested_provider text NOT NULL,
    requested_model text NOT NULL,
    max_output_tokens integer NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_context_manifests_run_owner_fkey
        FOREIGN KEY (run_id, owner_user_id, thread_id)
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_context_manifests_input_message_fkey
        FOREIGN KEY (input_message_id, owner_user_id, thread_id)
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_context_manifests_matter_owner_fkey
        FOREIGN KEY (active_matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_context_manifests_matter_shape_check
        CHECK (
            (
                active_matter_id IS NULL
                AND active_matter_version IS NULL
                AND active_matter_title IS NULL
            )
            OR
            (
                active_matter_id IS NOT NULL
                AND active_matter_version > 0
                AND active_matter_title IS NOT NULL
            )
        ),
    CONSTRAINT agent_context_manifests_messages_check
        CHECK (
            jsonb_typeof(selected_messages) = 'array'
            AND jsonb_array_length(selected_messages) > 0
        ),
    CONSTRAINT agent_context_manifests_instruction_check
        CHECK (
            instruction_version ~ '^[a-z][a-z0-9._-]{0,63}$'
        ),
    CONSTRAINT agent_context_manifests_budget_check
        CHECK (
            omitted_message_count >= 0
            AND trim_reason IN ('none', 'context_budget')
            AND max_input_characters BETWEEN 5000 AND 1000000
            AND used_input_characters > 0
            AND used_input_characters <= max_input_characters
            AND max_output_tokens BETWEEN 1 AND 1000000
        ),
    CONSTRAINT agent_context_manifests_provider_check
        CHECK (
            requested_provider ~ '^[a-z][a-z0-9_-]{0,63}$'
            AND requested_model ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        )
);

COMMIT;
