BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM preparation_profiles WHERE resume_ref IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM preparation_snapshots WHERE resume_snapshot IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM preparation_job_targets WHERE resume_ref IS NOT NULL
    ) THEN
        RAISE EXCEPTION
            'Preparation Resume reference data exists; recreate the development or test database before applying migration 000068'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

-- Stored idempotency responses contain the retired request/response shape and
-- cannot be replayed under the Revision contract.
DELETE FROM preparation_idempotency_records;
DELETE FROM preparation_job_target_idempotency_records;

UPDATE preparation_job_target_confirmations
SET input_snapshot = input_snapshot - 'resume_ref'
WHERE input_snapshot ? 'resume_ref';

ALTER TABLE preparation_profiles
    DROP CONSTRAINT preparation_profiles_optional_refs_check,
    DROP COLUMN resume_ref,
    ADD COLUMN resume_id uuid,
    ADD COLUMN resume_revision bigint,
    ADD COLUMN resume_material jsonb,
    ADD CONSTRAINT preparation_profiles_optional_refs_check
        CHECK (
            job_description_ref IS NULL
            OR btrim(job_description_ref) <> ''
        ),
    ADD CONSTRAINT preparation_profiles_resume_shape_check
        CHECK (
            (
                resume_id IS NULL
                AND resume_revision IS NULL
                AND resume_material IS NULL
            )
            OR
            (
                resume_id IS NOT NULL
                AND resume_revision >= 1
                AND jsonb_typeof(resume_material) = 'object'
                AND octet_length(resume_material::text) <= 524288
            )
        ),
    ADD CONSTRAINT preparation_profiles_resume_identity_key
        UNIQUE (
            owner_user_id,
            profile_id,
            resume_id,
            resume_revision
        );

ALTER TABLE preparation_snapshots
    DROP CONSTRAINT preparation_snapshots_optional_content_check,
    DROP COLUMN resume_snapshot,
    ADD COLUMN resume_id uuid,
    ADD COLUMN resume_revision bigint,
    ADD COLUMN resume_material jsonb,
    ADD CONSTRAINT preparation_snapshots_optional_content_check
        CHECK (
            job_description_snapshot IS NULL
            OR btrim(job_description_snapshot) <> ''
        ),
    ADD CONSTRAINT preparation_snapshots_resume_shape_check
        CHECK (
            (
                resume_id IS NULL
                AND resume_revision IS NULL
                AND resume_material IS NULL
            )
            OR
            (
                resume_id IS NOT NULL
                AND resume_revision >= 1
                AND jsonb_typeof(resume_material) = 'object'
                AND octet_length(resume_material::text) <= 524288
            )
        ),
    ADD CONSTRAINT preparation_snapshots_profile_resume_fkey
        FOREIGN KEY (
            owner_user_id,
            source_profile_id,
            resume_id,
            resume_revision
        )
        REFERENCES preparation_profiles (
            owner_user_id,
            profile_id,
            resume_id,
            resume_revision
        )
        ON DELETE CASCADE;

ALTER TABLE preparation_job_targets
    DROP CONSTRAINT preparation_job_targets_text_check,
    DROP COLUMN resume_ref,
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
                practice_focus IS NULL
                OR (
                    btrim(practice_focus) = practice_focus
                    AND btrim(practice_focus) <> ''
                    AND octet_length(practice_focus) <= 8192
                )
            )
        );

COMMIT;
