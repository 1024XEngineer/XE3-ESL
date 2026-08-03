BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

CREATE TABLE resumes (
    resume_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    title text NOT NULL,
    original_filename text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    checksum_sha256 text NOT NULL,
    object_key text NOT NULL,
    file_status text NOT NULL DEFAULT 'UPLOADING',
    parse_status text NOT NULL DEFAULT 'QUEUED',
    parse_failure_code text,
    current_revision bigint,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamptz,
    CONSTRAINT resumes_owner_identity_key
        UNIQUE (owner_user_id, resume_id),
    CONSTRAINT resumes_object_key_key UNIQUE (object_key),
    CONSTRAINT resumes_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE CASCADE,
    CONSTRAINT resumes_title_check
        CHECK (
            octet_length(title) BETWEEN 1 AND 360
            AND title = btrim(title)
            AND title !~ '[[:cntrl:]]'
        ),
    CONSTRAINT resumes_filename_check
        CHECK (
            octet_length(original_filename) BETWEEN 1 AND 1024
            AND original_filename = btrim(original_filename)
            AND original_filename !~ '[[:cntrl:]/\\]'
        ),
    CONSTRAINT resumes_file_metadata_check
        CHECK (
            content_type = 'application/pdf'
            AND size_bytes BETWEEN 1 AND 10485760
            AND checksum_sha256 ~ '^[0-9a-f]{64}$'
            AND octet_length(object_key) BETWEEN 16 AND 1024
            AND object_key NOT LIKE '%..%'
            AND object_key !~ '[[:cntrl:]\\]'
        ),
    CONSTRAINT resumes_file_status_check
        CHECK (file_status IN ('UPLOADING', 'AVAILABLE', 'DELETING', 'DELETED')),
    CONSTRAINT resumes_parse_status_check
        CHECK (parse_status IN ('QUEUED', 'PARSING', 'READY', 'FAILED')),
    CONSTRAINT resumes_parse_failure_check
        CHECK (
            (parse_status = 'FAILED'
                AND parse_failure_code ~ '^[a-z][a-z0-9_]{0,127}$')
            OR (parse_status <> 'FAILED' AND parse_failure_code IS NULL)
        ),
    CONSTRAINT resumes_revision_version_check
        CHECK (
            (current_revision IS NULL OR current_revision >= 1)
            AND version >= 1
        ),
    CONSTRAINT resumes_state_shape_check
        CHECK (
            (file_status = 'DELETED' AND deleted_at IS NOT NULL)
            OR (file_status <> 'DELETED' AND deleted_at IS NULL)
        ),
    CONSTRAINT resumes_timestamps_check
        CHECK (updated_at >= created_at AND (deleted_at IS NULL OR deleted_at >= created_at))
);

CREATE TABLE resume_revisions (
    owner_user_id uuid NOT NULL,
    resume_id uuid NOT NULL,
    revision bigint NOT NULL,
    source text NOT NULL,
    parser_version text,
    content jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (resume_id, revision),
    CONSTRAINT resume_revisions_owner_identity_key
        UNIQUE (owner_user_id, resume_id, revision),
    CONSTRAINT resume_revisions_resume_owner_fkey
        FOREIGN KEY (owner_user_id, resume_id)
        REFERENCES resumes (owner_user_id, resume_id)
        ON DELETE CASCADE,
    CONSTRAINT resume_revisions_revision_check CHECK (revision >= 1),
    CONSTRAINT resume_revisions_source_check CHECK (source IN ('PARSER', 'MANUAL')),
    CONSTRAINT resume_revisions_parser_version_check
        CHECK (
            (source = 'PARSER'
                AND octet_length(parser_version) BETWEEN 1 AND 128
                AND parser_version = btrim(parser_version))
            OR (source = 'MANUAL' AND parser_version IS NULL)
        ),
    CONSTRAINT resume_revisions_content_check
        CHECK (jsonb_typeof(content) = 'object')
);

ALTER TABLE resumes
    ADD CONSTRAINT resumes_current_revision_fkey
    FOREIGN KEY (owner_user_id, resume_id, current_revision)
    REFERENCES resume_revisions (owner_user_id, resume_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX resumes_owner_active_updated_idx
    ON resumes (owner_user_id, updated_at DESC, resume_id DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX resumes_parse_queue_idx
    ON resumes (updated_at, resume_id)
    WHERE deleted_at IS NULL
      AND file_status = 'AVAILABLE'
      AND parse_status = 'QUEUED';

CREATE FUNCTION resume_revisions_reject_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'resume revisions are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER resume_revisions_immutable
BEFORE UPDATE OR DELETE ON resume_revisions
FOR EACH ROW
EXECUTE FUNCTION resume_revisions_reject_mutation();

COMMIT;
