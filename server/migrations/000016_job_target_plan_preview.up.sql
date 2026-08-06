BEGIN;

-- This migration follows #119's 000015_job_targets migration.
ALTER TABLE preparation_job_target_confirmations
    ADD COLUMN input_snapshot jsonb,
    ADD CONSTRAINT preparation_job_target_confirmation_input_check
        CHECK (
            input_snapshot IS NULL
            OR jsonb_typeof(input_snapshot) = 'object'
        );

UPDATE preparation_job_target_confirmations AS confirmation
SET input_snapshot = jsonb_strip_nulls(jsonb_build_object(
    'source', target.source_kind,
    'job_title', target.job_title,
    'job_description', target.job_description,
    'company', target.company,
    'seniority', target.seniority,
    'candidate_background', target.candidate_background,
    'resume_ref', target.resume_ref,
    'practice_focus', target.practice_focus
))
FROM preparation_job_targets AS target
WHERE target.owner_user_id = confirmation.owner_user_id
  AND target.target_id = confirmation.target_id
  AND target.input_version = confirmation.input_version;

ALTER TABLE preparation_profiles
    ADD COLUMN job_target_id text,
    ADD COLUMN job_target_confirmation_version integer,
    ADD CONSTRAINT preparation_profiles_job_target_shape_check
        CHECK (
            (
                job_target_id IS NULL
                AND job_target_confirmation_version IS NULL
            )
            OR
            (
                job_target_id IS NOT NULL
                AND btrim(job_target_id) = job_target_id
                AND btrim(job_target_id) <> ''
                AND job_target_confirmation_version IS NOT NULL
                AND job_target_confirmation_version > 0
            )
        ),
    ADD CONSTRAINT preparation_profiles_job_target_fkey
        FOREIGN KEY (
            owner_user_id,
            job_target_id,
            job_target_confirmation_version
        )
        REFERENCES preparation_job_target_confirmations (
            owner_user_id,
            target_id,
            confirmation_version
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT preparation_profiles_job_target_identity_key
        UNIQUE (
            owner_user_id,
            profile_id,
            job_target_id,
            job_target_confirmation_version
        );

ALTER TABLE preparation_snapshots
    ADD COLUMN source_job_target_id text,
    ADD COLUMN source_job_target_confirmation_version integer,
    ADD COLUMN job_target_input_snapshot jsonb,
    ADD COLUMN job_target_candidate_snapshot jsonb,
    ADD CONSTRAINT preparation_snapshots_job_target_shape_check
        CHECK (
            (
                source_job_target_id IS NULL
                AND source_job_target_confirmation_version IS NULL
                AND job_target_input_snapshot IS NULL
                AND job_target_candidate_snapshot IS NULL
            )
            OR
            (
                source_job_target_id IS NOT NULL
                AND btrim(source_job_target_id) = source_job_target_id
                AND btrim(source_job_target_id) <> ''
                AND source_job_target_confirmation_version IS NOT NULL
                AND source_job_target_confirmation_version > 0
                AND jsonb_typeof(job_target_input_snapshot) = 'object'
                AND jsonb_typeof(job_target_candidate_snapshot) = 'object'
            )
        ),
    ADD CONSTRAINT preparation_snapshots_source_profile_target_fkey
        FOREIGN KEY (
            owner_user_id,
            source_profile_id,
            source_job_target_id,
            source_job_target_confirmation_version
        )
        REFERENCES preparation_profiles (
            owner_user_id,
            profile_id,
            job_target_id,
            job_target_confirmation_version
        )
        ON DELETE CASCADE,
    ADD CONSTRAINT preparation_snapshots_profile_identity_key
        UNIQUE (
            owner_user_id,
            snapshot_id,
            source_profile_id
        );

ALTER TABLE practice_plans
    ADD COLUMN preparation_snapshot_id text,
    ADD COLUMN catalog_snapshot jsonb,
    ADD COLUMN session_policy jsonb,
    ADD COLUMN practice_focuses jsonb,
    ADD CONSTRAINT practice_plans_preparation_snapshot_fkey
        FOREIGN KEY (
            owner_user_id,
            preparation_snapshot_id,
            preparation_profile_id
        )
        REFERENCES preparation_snapshots (
            owner_user_id,
            snapshot_id,
            source_profile_id
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_plans_preview_shape_check
        CHECK (
            (
                preparation_snapshot_id IS NULL
                AND catalog_snapshot IS NULL
                AND session_policy IS NULL
                AND practice_focuses IS NULL
            )
            OR
            (
                preparation_snapshot_id IS NOT NULL
                AND btrim(preparation_snapshot_id) =
                    preparation_snapshot_id
                AND btrim(preparation_snapshot_id) <> ''
                AND jsonb_typeof(catalog_snapshot) = 'object'
                AND jsonb_typeof(session_policy) = 'object'
                AND jsonb_typeof(practice_focuses) = 'array'
                AND jsonb_array_length(practice_focuses) > 0
            )
        );

ALTER TABLE practice_idempotency_records
    DROP CONSTRAINT practice_idempotency_resource_check,
    ADD CONSTRAINT practice_idempotency_resource_check
        CHECK (
            resource_kind IN (
                'plan',
                'plan_revision',
                'session',
                'pause',
                'resume',
                'end_early'
            )
            AND btrim(resource_id) <> ''
        );

COMMIT;
