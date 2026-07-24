BEGIN;

DROP TABLE agent_context_manifests;

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_assistant_message_fkey;

DROP INDEX agent_messages_one_assistant_per_run_idx;

ALTER TABLE agent_messages
    DROP CONSTRAINT agent_messages_produced_by_run_fkey;

DROP TABLE agent_runs;

ALTER TABLE agent_messages
    DROP CONSTRAINT agent_messages_id_owner_thread_key,
    DROP CONSTRAINT agent_messages_role_check,
    DROP CONSTRAINT agent_messages_origin_check,
    DROP CONSTRAINT agent_messages_client_id_check,
    DROP COLUMN produced_by_run_id,
    ALTER COLUMN client_message_id SET NOT NULL,
    ADD CONSTRAINT agent_messages_role_check
        CHECK (role = 'user'),
    ADD CONSTRAINT agent_messages_client_id_check
        CHECK (
            octet_length(client_message_id) BETWEEN 1 AND 128
            AND client_message_id ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        );

COMMIT;
