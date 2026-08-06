BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM preparation_profiles WHERE resume_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM preparation_snapshots WHERE resume_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION
            'Preparation Resume Revision data exists; recreate the development or test database before reverting migration 000068'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DELETE FROM preparation_idempotency_records;
DELETE FROM preparation_job_target_idempotency_records;

ALTER TABLE preparation_job_targets
    DROP CONSTRAINT preparation_job_targets_text_check,
    ADD COLUMN resume_ref text,
    ADD CONSTRAINT preparation_job_targets_text_check
        CHECK (
            (
                job_title IS NULL
                OR (
                    btrim(job_title) = job_title
                    AND octet_length(job_title) BETWEEN 1 AND 512
                )
            )
            AND octet_length(coalesce(job_description, '')) <= 65536
            AND (
                company IS NULL
                OR (
                    btrim(company) = company
                    AND octet_length(company) BETWEEN 1 AND 512
                )
            )
            AND (
                seniority IS NULL
                OR (
                    btrim(seniority) = seniority
                    AND octet_length(seniority) BETWEEN 1 AND 256
                )
            )
            AND (
                candidate_background IS NULL
                OR (
                    btrim(candidate_background) = candidate_background
                    AND btrim(candidate_background) <> ''
                    AND octet_length(candidate_background) <= 16384
                )
            )
            AND (
                resume_ref IS NULL
                OR (
                    btrim(resume_ref) = resume_ref
                    AND btrim(resume_ref) <> ''
                    AND octet_length(resume_ref) <= 16384
                )
            )
            AND (
                practice_focus IS NULL
                OR (
                    btrim(practice_focus) = practice_focus
                    AND btrim(practice_focus) <> ''
                    AND octet_length(practice_focus) <= 8192
                )
            )
        );

ALTER TABLE preparation_snapshots
    DROP CONSTRAINT preparation_snapshots_profile_resume_fkey,
    DROP CONSTRAINT preparation_snapshots_resume_shape_check,
    DROP CONSTRAINT preparation_snapshots_optional_content_check,
    DROP COLUMN resume_id,
    DROP COLUMN resume_revision,
    DROP COLUMN resume_material,
    ADD COLUMN resume_snapshot text,
    ADD CONSTRAINT preparation_snapshots_optional_content_check
        CHECK (
            (resume_snapshot IS NULL OR btrim(resume_snapshot) <> '')
            AND
            (
                job_description_snapshot IS NULL
                OR btrim(job_description_snapshot) <> ''
            )
        );

ALTER TABLE preparation_profiles
    DROP CONSTRAINT preparation_profiles_resume_identity_key,
    DROP CONSTRAINT preparation_profiles_resume_shape_check,
    DROP CONSTRAINT preparation_profiles_optional_refs_check,
    DROP COLUMN resume_id,
    DROP COLUMN resume_revision,
    DROP COLUMN resume_material,
    ADD COLUMN resume_ref text,
    ADD CONSTRAINT preparation_profiles_optional_refs_check
        CHECK (
            (resume_ref IS NULL OR btrim(resume_ref) <> '')
            AND
            (job_description_ref IS NULL OR btrim(job_description_ref) <> '')
        );

COMMIT;
