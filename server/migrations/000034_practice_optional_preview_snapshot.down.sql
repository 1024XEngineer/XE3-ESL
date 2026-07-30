BEGIN;

UPDATE practice_plans
SET
    catalog_snapshot = NULL,
    session_policy = NULL,
    practice_focuses = NULL
WHERE preparation_snapshot_id IS NULL
  AND (
      catalog_snapshot IS NOT NULL
      OR session_policy IS NOT NULL
      OR practice_focuses IS NOT NULL
  );

ALTER TABLE practice_plans
    DROP CONSTRAINT practice_plans_preview_shape_check,
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

COMMIT;
