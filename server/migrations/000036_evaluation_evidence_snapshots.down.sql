BEGIN;

DROP TRIGGER IF EXISTS evaluation_evidence_snapshots_immutable
    ON evaluation_evidence_snapshots;
DROP FUNCTION IF EXISTS reject_evaluation_evidence_snapshot_mutation();
DROP TABLE IF EXISTS evaluation_evidence_snapshots;
DROP FUNCTION IF EXISTS evaluation_evidence_refs_are_consistent(text, jsonb);
DROP TABLE IF EXISTS evaluation_deletion_fences;

COMMIT;
