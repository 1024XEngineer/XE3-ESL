BEGIN;

ALTER TABLE evaluation_module_runs
    DROP CONSTRAINT evaluation_module_runs_practice_session_check,
    ADD CONSTRAINT evaluation_module_runs_practice_session_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
        );

ALTER TABLE evaluation_evidence_snapshots
    DROP CONSTRAINT evaluation_evidence_snapshots_practice_session_check,
    ADD CONSTRAINT evaluation_evidence_snapshots_practice_session_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
        );

ALTER TABLE evaluation_ledgers
    DROP CONSTRAINT evaluation_ledgers_practice_session_check,
    ADD CONSTRAINT evaluation_ledgers_practice_session_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
        );

COMMIT;
