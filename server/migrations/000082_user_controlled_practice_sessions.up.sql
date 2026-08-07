BEGIN;

CREATE OR REPLACE FUNCTION preparation_plan_session_policy_is_valid_v1(
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    completion_mode text;
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'object'
       OR NOT payload ?& ARRAY[
           'suggested_duration_seconds', 'min_effective_turns',
           'max_effective_turns', 'coverage_checkpoint_turn',
           'max_follow_ups_per_question', 'early_completion_rule',
           'retry_allowed', 'question_translation_allowed',
           'question_tips_allowed', 'avatar_allowed',
           'speech_feedback_allowed'
       ]
       OR payload - ARRAY[
           'completion_mode', 'suggested_duration_seconds',
           'min_effective_turns', 'max_effective_turns',
           'coverage_checkpoint_turn', 'max_follow_ups_per_question',
           'early_completion_rule', 'retry_allowed',
           'question_translation_allowed', 'question_tips_allowed',
           'avatar_allowed', 'speech_feedback_allowed'
       ] <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'suggested_duration_seconds') <> 'number'
       OR jsonb_typeof(payload -> 'min_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'max_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'coverage_checkpoint_turn') <> 'number'
       OR jsonb_typeof(payload -> 'max_follow_ups_per_question') <> 'number'
       OR jsonb_typeof(payload -> 'retry_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'question_translation_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'question_tips_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'avatar_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'speech_feedback_allowed') <> 'boolean'
       OR (payload ? 'completion_mode'
           AND jsonb_typeof(payload -> 'completion_mode') <> 'string')
       OR payload ->> 'suggested_duration_seconds' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'min_effective_turns' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'max_effective_turns' !~ '^(0|[1-9][0-9]{0,8})$'
       OR payload ->> 'coverage_checkpoint_turn' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'max_follow_ups_per_question' !~ '^[0-9]{1,9}$'
       OR jsonb_typeof(payload -> 'early_completion_rule') <> 'string'
       OR payload ->> 'early_completion_rule' <>
           'COVERAGE_SATISFIED_AFTER_CHECKPOINT' THEN
        RETURN false;
    END IF;

    completion_mode := COALESCE(payload ->> 'completion_mode', 'TURN_LIMITED');
    IF completion_mode = 'USER_CONTROLLED' THEN
        RETURN (payload ->> 'max_effective_turns')::integer = 0
            AND (payload ->> 'coverage_checkpoint_turn')::integer = 1;
    END IF;
    IF completion_mode <> 'TURN_LIMITED' THEN
        RETURN false;
    END IF;
    RETURN (payload ->> 'min_effective_turns')::integer <=
               (payload ->> 'max_effective_turns')::integer
        AND (payload ->> 'coverage_checkpoint_turn')::integer <=
               (payload ->> 'max_effective_turns')::integer
        AND (payload ->> 'max_effective_turns')::integer BETWEEN 1 AND 64;
END;
$$;

ALTER TABLE preparation_practice_plan_revisions
    DISABLE TRIGGER preparation_practice_plan_revisions_are_immutable;

UPDATE preparation_practice_plan_revisions
SET session_policy = jsonb_set(
    session_policy, '{completion_mode}', '"TURN_LIMITED"'::jsonb, true
)
WHERE NOT session_policy ? 'completion_mode';

ALTER TABLE preparation_practice_plan_revisions
    ENABLE TRIGGER preparation_practice_plan_revisions_are_immutable;

ALTER TABLE practice_session_snapshots
    DISABLE TRIGGER practice_session_snapshots_immutable;

UPDATE practice_session_snapshots
SET snapshot_document = jsonb_set(
    snapshot_document,
    '{session_policy,completion_mode}',
    '"TURN_LIMITED"'::jsonb,
    true
)
WHERE NOT (snapshot_document -> 'session_policy') ? 'completion_mode';

ALTER TABLE practice_session_snapshots
    ENABLE TRIGGER practice_session_snapshots_immutable;

ALTER TABLE practice_session_snapshots
    DROP CONSTRAINT practice_session_snapshots_turn_limit_check,
    ADD CONSTRAINT practice_session_snapshots_turn_limit_check
        CHECK (turn_limit >= 0);

ALTER TABLE practice_idempotency_records
    DROP CONSTRAINT practice_idempotency_resource_check,
    ADD CONSTRAINT practice_idempotency_resource_check
        CHECK (
            resource_kind IN (
                'session',
                'pause',
                'resume',
                'complete',
                'end_early'
            )
            AND btrim(resource_id) <> ''
        );

COMMIT;
