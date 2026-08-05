BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM preparation_practice_plans)
       OR EXISTS (SELECT 1 FROM practice_sessions) THEN
        RAISE EXCEPTION
            'Practice Execution Policy rollback requires empty development and test Practice data. RECREATE THE DEVELOPMENT OR TEST DATABASE before rollback.';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION preparation_plan_session_policy_is_valid_v1(
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'object'
       OR NOT payload ?& ARRAY[
           'suggested_duration_seconds',
           'min_effective_turns',
           'max_effective_turns',
           'coverage_checkpoint_turn',
           'max_follow_ups_per_question',
           'early_completion_rule'
       ]
       OR payload - ARRAY[
           'suggested_duration_seconds',
           'min_effective_turns',
           'max_effective_turns',
           'coverage_checkpoint_turn',
           'max_follow_ups_per_question',
           'early_completion_rule'
       ] <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'suggested_duration_seconds') <> 'number'
       OR jsonb_typeof(payload -> 'min_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'max_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'coverage_checkpoint_turn') <> 'number'
       OR jsonb_typeof(payload -> 'max_follow_ups_per_question') <> 'number'
       OR (payload ->> 'suggested_duration_seconds') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'min_effective_turns') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'max_effective_turns') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'coverage_checkpoint_turn') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'max_follow_ups_per_question') !~ '^[0-9]{1,9}$'
       OR (payload ->> 'min_effective_turns')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR (payload ->> 'coverage_checkpoint_turn')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR jsonb_typeof(payload -> 'early_completion_rule') <> 'string'
       OR payload ->> 'early_completion_rule' <>
           'COVERAGE_SATISFIED_AFTER_CHECKPOINT' THEN
        RETURN false;
    END IF;

    RETURN true;
END;
$$;


ALTER TABLE coaching_scene_versions
    DISABLE TRIGGER coaching_scene_versions_are_immutable;

UPDATE coaching_scene_versions
SET session_policy_ref = 'generic.practice.session.v1'
WHERE (
    session_policy_ref = 'daily.practice.session.v1'
    AND scene_id IN (
        'scn_daily_airport_transport',
        'scn_daily_complaint_help',
        'scn_daily_custom',
        'scn_daily_medical_appointment',
        'scn_daily_phone_call',
        'scn_daily_rental_maintenance',
        'scn_daily_restaurant_ordering',
        'scn_daily_shopping_return',
        'scn_daily_small_talk',
        'scn_daily_social_invitation'
    )
)
OR (
    session_policy_ref = 'workplace.practice.session.v1'
    AND scene_id IN (
        'scn_workplace_client_delay',
        'scn_workplace_cross_team_alignment',
        'scn_workplace_custom',
        'scn_workplace_feedback_conflict',
        'scn_workplace_meeting_disagreement',
        'scn_workplace_negotiation',
        'scn_workplace_solution_presentation'
    )
)
OR (
    session_policy_ref = 'interview.practice.session.v1'
    AND scene_id IN (
        'scn_interview_behavioral',
        'scn_interview_custom',
        'scn_interview_hiring_manager',
        'scn_interview_recruiter_screening',
        'scn_interview_self_introduction',
        'scn_interview_system_design_spoken'
    )
)
OR (
    session_policy_ref = 'exam.practice.session.v1'
    AND scene_id = 'scn_speaking_exam_custom'
);

ALTER TABLE coaching_scene_versions
    ENABLE TRIGGER coaching_scene_versions_are_immutable;

COMMIT;
