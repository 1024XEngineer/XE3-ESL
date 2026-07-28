BEGIN;

ALTER TABLE practice_plans
    ADD COLUMN IF NOT EXISTS scenario_model text;

UPDATE practice_plans
SET scenario_model = 'PROJECT_EXPERIENCE_DEEP_DIVE'
WHERE scenario_model IS NULL
  AND scenario_type = 'INTERVIEW';

ALTER TABLE practice_plans
    ALTER COLUMN scenario_model SET NOT NULL,
    ALTER COLUMN matter_id DROP NOT NULL,
    DROP CONSTRAINT practice_plans_catalog_fields_check,
    ADD CONSTRAINT practice_plans_catalog_fields_check
        CHECK (
            btrim(scenario_definition_id) <> ''
            AND btrim(scenario_type) <> ''
            AND btrim(scenario_model) <> ''
            AND btrim(scenario_config_id) <> ''
            AND jsonb_typeof(selected_role_ids) = 'array'
            AND jsonb_array_length(selected_role_ids) > 0
        ),
    ADD CONSTRAINT practice_plans_scenario_model_check
        CHECK (
            (
                scenario_type = 'INTERVIEW'
                AND scenario_model IN (
                    'PROJECT_EXPERIENCE_DEEP_DIVE',
                    'INTERVIEW_BASIC_DIALOGUE'
                )
            )
            OR
            (
                scenario_type = 'EXAM'
                AND scenario_model IN (
                    'IELTS_SPEAKING_PART_2',
                    'EXAM_BASIC_DIALOGUE'
                )
            )
            OR
            (
                scenario_type = 'WORKPLACE'
                AND scenario_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR
            (
                scenario_type = 'DAILY'
                AND scenario_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        ),
    ADD CONSTRAINT practice_plans_context_thread_key
        UNIQUE (owner_user_id, plan_id, agent_thread_id);

ALTER TABLE practice_sessions
    ADD COLUMN IF NOT EXISTS scenario_model text;

UPDATE practice_sessions
SET scenario_model = 'PROJECT_EXPERIENCE_DEEP_DIVE'
WHERE context_plan_id IS NOT NULL
  AND scenario_model IS NULL
  AND scenario_type = 'INTERVIEW';

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_context_fields_check,
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            (
                context_plan_id IS NULL
                AND agent_thread_id IS NULL
                AND matter_id IS NULL
                AND snapshot_id IS NULL
                AND scenario_type IS NULL
                AND scenario_model IS NULL
            )
            OR
            (
                context_plan_id IS NOT NULL
                AND agent_thread_id IS NOT NULL
                AND snapshot_id IS NOT NULL
                AND btrim(snapshot_id) <> ''
                AND scenario_type IS NOT NULL
                AND btrim(scenario_type) <> ''
                AND scenario_model IS NOT NULL
                AND btrim(scenario_model) <> ''
                AND plan_id = context_plan_id
            )
        ),
    ADD CONSTRAINT practice_sessions_scenario_model_check
        CHECK (
            context_plan_id IS NULL
            OR
            (
                scenario_type = 'INTERVIEW'
                AND scenario_model IN (
                    'PROJECT_EXPERIENCE_DEEP_DIVE',
                    'INTERVIEW_BASIC_DIALOGUE'
                )
            )
            OR
            (
                scenario_type = 'EXAM'
                AND scenario_model IN (
                    'IELTS_SPEAKING_PART_2',
                    'EXAM_BASIC_DIALOGUE'
                )
            )
            OR
            (
                scenario_type = 'WORKPLACE'
                AND scenario_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR
            (
                scenario_type = 'DAILY'
                AND scenario_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        ),
    ADD CONSTRAINT practice_sessions_context_thread_fkey
        FOREIGN KEY (
            owner_user_id,
            context_plan_id,
            agent_thread_id
        )
        REFERENCES practice_plans (
            owner_user_id,
            plan_id,
            agent_thread_id
        )
        ON DELETE RESTRICT;

COMMIT;
