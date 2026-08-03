BEGIN;

ALTER TABLE agent_threads
    ADD COLUMN sidebar_deleted_at timestamptz;

CREATE INDEX agent_threads_owner_visible_updated_idx
    ON agent_threads (owner_user_id, updated_at DESC, id DESC)
    WHERE sidebar_deleted_at IS NULL;

COMMIT;
