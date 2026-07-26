BEGIN;

DROP TABLE IF EXISTS practice_idempotency_records;

DROP INDEX IF EXISTS practice_one_effective_session_per_context_plan;
DROP INDEX IF EXISTS practice_one_effective_session_per_agent_thread;

-- Only 000009 full-context Sessions depend on the new Plan and Preparation
-- records. Legacy 000006 Sessions remain intact across this downgrade.
DELETE FROM practice_sessions
WHERE context_plan_id IS NOT NULL;

ALTER TABLE practice_sessions
    DROP CONSTRAINT IF EXISTS practice_sessions_context_snapshot_fkey;

ALTER TABLE practice_session_snapshots
    DROP CONSTRAINT IF EXISTS
        practice_session_snapshots_context_session_fkey,
    DROP CONSTRAINT IF EXISTS
        practice_session_snapshots_context_fields_check,
    DROP CONSTRAINT IF EXISTS
        practice_session_snapshots_context_binding_key,
    DROP CONSTRAINT IF EXISTS
        practice_session_snapshots_context_snapshot_key,
    DROP CONSTRAINT IF EXISTS
        practice_session_snapshots_preparation_fkey,
    DROP CONSTRAINT IF EXISTS
        practice_session_snapshots_context_plan_fkey,
    DROP COLUMN IF EXISTS snapshot_document,
    DROP COLUMN IF EXISTS preparation_snapshot_id,
    DROP COLUMN IF EXISTS context_plan_id,
    DROP COLUMN IF EXISTS snapshot_id;

ALTER TABLE practice_sessions
    DROP CONSTRAINT IF EXISTS practice_sessions_lifecycle_check,
    DROP CONSTRAINT IF EXISTS practice_sessions_context_fields_check,
    DROP CONSTRAINT IF EXISTS practice_sessions_status_check,
    DROP CONSTRAINT IF EXISTS practice_sessions_context_binding_key,
    DROP CONSTRAINT IF EXISTS practice_sessions_context_snapshot_key,
    DROP CONSTRAINT IF EXISTS practice_sessions_context_anchor_fkey,
    DROP CONSTRAINT IF EXISTS practice_sessions_context_plan_fkey,
    DROP COLUMN IF EXISTS end_reason,
    DROP COLUMN IF EXISTS scenario_type,
    DROP COLUMN IF EXISTS snapshot_id,
    DROP COLUMN IF EXISTS matter_id,
    DROP COLUMN IF EXISTS agent_thread_id,
    DROP COLUMN IF EXISTS context_plan_id,
    ALTER COLUMN started_at SET NOT NULL,
    ADD CONSTRAINT practice_sessions_status_check
        CHECK (status IN ('active', 'completed')),
    ADD CHECK (
        (status = 'active' AND completed_at IS NULL)
        OR (status = 'completed' AND completed_at IS NOT NULL)
    );

DROP TABLE IF EXISTS practice_plans;

ALTER TABLE agent_thread_matter_links
    DROP CONSTRAINT IF EXISTS
        agent_thread_matter_links_owner_thread_matter_key;

DROP TABLE IF EXISTS preparation_idempotency_records;

DROP TRIGGER IF EXISTS preparation_snapshots_immutable
    ON preparation_snapshots;
DROP FUNCTION IF EXISTS reject_preparation_snapshot_mutation();
DROP TABLE IF EXISTS preparation_snapshots;
DROP TABLE IF EXISTS preparation_profiles;
DROP TABLE IF EXISTS preparation_deletion_fences;

COMMIT;
