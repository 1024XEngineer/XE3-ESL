BEGIN;

DROP INDEX IF EXISTS resumes_expired_temporary_idx;

ALTER TABLE resumes
    DROP CONSTRAINT IF EXISTS resumes_temporary_expiry_check,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS is_temporary;

COMMIT;
