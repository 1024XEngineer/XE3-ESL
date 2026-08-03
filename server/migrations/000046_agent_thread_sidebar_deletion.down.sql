BEGIN;

DROP INDEX IF EXISTS agent_threads_owner_visible_updated_idx;

ALTER TABLE agent_threads
    DROP COLUMN IF EXISTS sidebar_deleted_at;

COMMIT;
