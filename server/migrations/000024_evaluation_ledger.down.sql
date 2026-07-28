BEGIN;

DROP TABLE IF EXISTS evaluation_outbox;
DROP TABLE IF EXISTS evaluation_revision_states;
DROP TRIGGER IF EXISTS evaluation_revisions_immutable
    ON evaluation_revisions;
DROP FUNCTION IF EXISTS reject_evaluation_revision_mutation();
DROP TRIGGER IF EXISTS evaluation_revision_chain
    ON evaluation_revisions;
DROP FUNCTION IF EXISTS evaluation_assert_revision_chain();
DROP TABLE IF EXISTS evaluation_revisions;
DROP TABLE IF EXISTS evaluation_ledgers;

COMMIT;
