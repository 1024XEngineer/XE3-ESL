BEGIN;

-- Version 5 restores legacy Catalog-only scene selections.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM practice_plans
        WHERE scene_selection #>> '{source,type}' = 'CUSTOM'
    ) THEN
        RAISE EXCEPTION 'cannot roll back scene selection source while CUSTOM snapshots exist';
    END IF;
END
$$;

WITH migrated AS (
    SELECT
        plans.plan_id,
        (plans.scene_selection - 'source' - 'scene') || jsonb_build_object(
            'scene',
                ((plans.scene_selection->'scene') - 'scene_key' - 'scene_revision') ||
                jsonb_build_object(
                    'scene_id', plans.scene_selection #>> '{source,scene_id}',
                    'scene_version', (plans.scene_selection #>> '{source,scene_version}')::integer,
                    'status', 'active',
                    'roles', (
                        SELECT jsonb_agg(
                            (role_value - 'scene_key') || jsonb_build_object(
                                'scene_id', plans.scene_selection #>> '{source,scene_id}'
                            ) ORDER BY role_order
                        )
                        FROM jsonb_array_elements(plans.scene_selection #> '{scene,roles}')
                            WITH ORDINALITY AS roles(role_value, role_order)
                    ),
                    'practice_options', (
                        SELECT jsonb_agg(
                            (option_value - 'scene_key') || jsonb_build_object(
                                'scene_id', plans.scene_selection #>> '{source,scene_id}'
                            ) ORDER BY option_order
                        )
                        FROM jsonb_array_elements(plans.scene_selection #> '{scene,practice_options}')
                            WITH ORDINALITY AS options(option_value, option_order)
                    )
                )
        ) AS selection
    FROM practice_plans AS plans
    WHERE plans.scene_selection ? 'source'
)
UPDATE practice_plans AS plans
SET scene_selection = migrated.selection
FROM migrated
WHERE plans.plan_id = migrated.plan_id;

COMMIT;
