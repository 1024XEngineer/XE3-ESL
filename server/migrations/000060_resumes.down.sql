BEGIN;

DROP TRIGGER IF EXISTS resume_revisions_immutable ON resume_revisions;
DROP FUNCTION IF EXISTS resume_revisions_reject_mutation();
ALTER TABLE IF EXISTS resumes
    DROP CONSTRAINT IF EXISTS resumes_current_revision_fkey;
DROP TABLE IF EXISTS resume_revisions;
DROP TABLE IF EXISTS resumes;

COMMIT;
