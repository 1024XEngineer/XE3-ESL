BEGIN;

ALTER TABLE practice_idempotency_records
    DROP CONSTRAINT practice_idempotency_resource_check,
    ADD CONSTRAINT practice_idempotency_resource_check
        CHECK (
            resource_kind IN (
                'plan',
                'session',
                'pause',
                'resume',
                'end_early'
            )
            AND btrim(resource_id) <> ''
        );

ALTER TABLE practice_plans
    DROP CONSTRAINT IF EXISTS practice_plans_preview_shape_check,
    DROP CONSTRAINT IF EXISTS
        practice_plans_preparation_snapshot_fkey,
    DROP COLUMN IF EXISTS practice_focuses,
    DROP COLUMN IF EXISTS session_policy,
    DROP COLUMN IF EXISTS catalog_snapshot,
    DROP COLUMN IF EXISTS preparation_snapshot_id;

ALTER TABLE preparation_snapshots
    DROP CONSTRAINT IF EXISTS preparation_snapshots_profile_identity_key,
    DROP CONSTRAINT IF EXISTS
        preparation_snapshots_source_profile_target_fkey,
    DROP CONSTRAINT IF EXISTS
        preparation_snapshots_job_target_shape_check,
    DROP COLUMN IF EXISTS job_target_candidate_snapshot,
    DROP COLUMN IF EXISTS job_target_input_snapshot,
    DROP COLUMN IF EXISTS source_job_target_confirmation_version,
    DROP COLUMN IF EXISTS source_job_target_id;

ALTER TABLE preparation_profiles
    DROP CONSTRAINT IF EXISTS
        preparation_profiles_job_target_identity_key,
    DROP CONSTRAINT IF EXISTS preparation_profiles_job_target_fkey,
    DROP CONSTRAINT IF EXISTS
        preparation_profiles_job_target_shape_check,
    DROP COLUMN IF EXISTS job_target_confirmation_version,
    DROP COLUMN IF EXISTS job_target_id;

ALTER TABLE preparation_job_target_confirmations
    DROP CONSTRAINT IF EXISTS
        preparation_job_target_confirmation_input_check,
    DROP COLUMN IF EXISTS input_snapshot;

COMMIT;
