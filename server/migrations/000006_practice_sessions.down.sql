BEGIN;

DROP TABLE IF EXISTS practice_turn_results;
DROP TRIGGER IF EXISTS practice_session_snapshots_immutable
    ON practice_session_snapshots;
DROP FUNCTION IF EXISTS reject_practice_snapshot_mutation();
DROP TABLE IF EXISTS practice_session_snapshots;
DROP TABLE IF EXISTS practice_sessions;

COMMIT;
