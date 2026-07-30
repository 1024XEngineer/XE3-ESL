BEGIN;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_scenario_model_check,
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
                    'IELTS_SPEAKING_FULL_MOCK',
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
        );

ALTER TABLE practice_plans
    DROP CONSTRAINT practice_plans_scenario_model_check,
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
                    'IELTS_SPEAKING_FULL_MOCK',
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
        );

COMMIT;
