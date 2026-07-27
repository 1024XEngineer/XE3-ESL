BEGIN;

CREATE TABLE agent_thread_focuses (
    owner_user_id UUID PRIMARY KEY,
    thread_id UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_thread_focuses_thread_owner_fkey
        FOREIGN KEY (thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE
);

COMMIT;
