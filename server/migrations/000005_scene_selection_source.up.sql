BEGIN;

-- Version 5 adds the authoritative Catalog/Custom scene source snapshot.

WITH migrated AS (
    SELECT
        plans.plan_id,
        (plans.scene_selection - 'scene') || jsonb_build_object(
            'source', jsonb_build_object(
                'type', 'CATALOG',
                'scene_id', plans.scene_selection #>> '{scene,scene_id}',
                'scene_version', (plans.scene_selection #>> '{scene,scene_version}')::integer
            ),
            'scene',
                    ((plans.scene_selection->'scene') - 'scene_id' - 'scene_version' - 'status') ||
                jsonb_build_object(
                    'scene_key', plans.scene_selection #>> '{scene,scene_id}',
                    'scene_revision', (plans.scene_selection #>> '{scene,scene_version}')::integer,
                    'roles', (
                        SELECT jsonb_agg(
                            (role_value - 'scene_id') || jsonb_build_object(
                                'scene_key', plans.scene_selection #>> '{scene,scene_id}'
                            ) ORDER BY role_order
                        )
                        FROM jsonb_array_elements(plans.scene_selection #> '{scene,roles}')
                            WITH ORDINALITY AS roles(role_value, role_order)
                    ),
                    'practice_options', (
                        SELECT jsonb_agg(
                            (option_value - 'scene_id') || jsonb_build_object(
                                'scene_key', plans.scene_selection #>> '{scene,scene_id}'
                            ) ORDER BY option_order
                        )
                        FROM jsonb_array_elements(plans.scene_selection #> '{scene,practice_options}')
                            WITH ORDINALITY AS options(option_value, option_order)
                    )
                )
        ) AS selection
    FROM practice_plans AS plans
    WHERE NOT plans.scene_selection ? 'source'
)
UPDATE practice_plans AS plans
SET scene_selection = migrated.selection
FROM migrated
WHERE plans.plan_id = migrated.plan_id;

COMMIT;
