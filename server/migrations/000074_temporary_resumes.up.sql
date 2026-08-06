ALTER TABLE resumes
    ADD COLUMN is_temporary BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN expires_at TIMESTAMPTZ;

ALTER TABLE resumes
    ADD CONSTRAINT resumes_temporary_expiry_check CHECK (
        (is_temporary AND expires_at IS NOT NULL)
        OR (NOT is_temporary AND expires_at IS NULL)
    );

CREATE INDEX resumes_expired_temporary_idx
    ON resumes (expires_at, resume_id)
    WHERE is_temporary = TRUE AND deleted_at IS NULL;
