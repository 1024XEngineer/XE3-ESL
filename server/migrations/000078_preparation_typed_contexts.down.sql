BEGIN;

ALTER TABLE preparation_snapshots
    DROP CONSTRAINT IF EXISTS preparation_snapshots_context_shape_check,
    DROP COLUMN IF EXISTS preparation_context,
    DROP COLUMN IF EXISTS preparation_kind;

ALTER TABLE preparation_profiles
    DROP CONSTRAINT IF EXISTS preparation_profiles_context_shape_check,
    DROP COLUMN IF EXISTS preparation_context,
    DROP COLUMN IF EXISTS preparation_kind;

COMMIT;
