BEGIN;

ALTER TABLE practice_sessions
    DROP CONSTRAINT IF EXISTS practice_sessions_context_thread_fkey,
    DROP CONSTRAINT IF EXISTS practice_sessions_scenario_model_check,
    DROP CONSTRAINT IF EXISTS practice_sessions_context_fields_check,
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            (
                context_plan_id IS NULL
                AND agent_thread_id IS NULL
                AND matter_id IS NULL
                AND snapshot_id IS NULL
                AND scenario_type IS NULL
            )
            OR
            (
                context_plan_id IS NOT NULL
                AND agent_thread_id IS NOT NULL
                AND matter_id IS NOT NULL
                AND snapshot_id IS NOT NULL
                AND btrim(snapshot_id) <> ''
                AND scenario_type IS NOT NULL
                AND btrim(scenario_type) <> ''
                AND plan_id = context_plan_id
            )
        ),
    DROP COLUMN IF EXISTS scenario_model;

ALTER TABLE practice_plans
    DROP CONSTRAINT IF EXISTS practice_plans_context_thread_key,
    DROP CONSTRAINT IF EXISTS practice_plans_scenario_model_check,
    DROP CONSTRAINT IF EXISTS practice_plans_catalog_fields_check,
    ADD CONSTRAINT practice_plans_catalog_fields_check
        CHECK (
            btrim(scenario_definition_id) <> ''
            AND btrim(scenario_type) <> ''
            AND btrim(scenario_config_id) <> ''
            AND jsonb_typeof(selected_role_ids) = 'array'
            AND jsonb_array_length(selected_role_ids) > 0
        ),
    ALTER COLUMN matter_id SET NOT NULL,
    DROP COLUMN IF EXISTS scenario_model;

COMMIT;
