BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM preparation_practice_plan_revisions
        WHERE session_policy ->> 'completion_mode' = 'USER_CONTROLLED'
    ) OR EXISTS (
        SELECT 1 FROM practice_session_snapshots
        WHERE turn_limit = 0
           OR snapshot_document -> 'session_policy' ->> 'completion_mode' =
              'USER_CONTROLLED'
    ) THEN
        RAISE EXCEPTION
            'Cannot remove user-controlled Practice policy while dependent Plans or Sessions exist.'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE practice_session_snapshots
    DROP CONSTRAINT practice_session_snapshots_turn_limit_check,
    ADD CONSTRAINT practice_session_snapshots_turn_limit_check
        CHECK (turn_limit > 0);

ALTER TABLE practice_idempotency_records
    DROP CONSTRAINT practice_idempotency_resource_check,
    ADD CONSTRAINT practice_idempotency_resource_check
        CHECK (
            resource_kind IN (
                'session',
                'pause',
                'resume',
                'end_early'
            )
            AND btrim(resource_id) <> ''
        );

ALTER TABLE practice_session_snapshots
    DISABLE TRIGGER practice_session_snapshots_immutable;

UPDATE practice_session_snapshots
SET snapshot_document = snapshot_document #- '{session_policy,completion_mode}'
WHERE snapshot_document -> 'session_policy' ? 'completion_mode';

ALTER TABLE practice_session_snapshots
    ENABLE TRIGGER practice_session_snapshots_immutable;

ALTER TABLE preparation_practice_plan_revisions
    DISABLE TRIGGER preparation_practice_plan_revisions_are_immutable;

UPDATE preparation_practice_plan_revisions
SET session_policy = session_policy - 'completion_mode'
WHERE session_policy ? 'completion_mode';

ALTER TABLE preparation_practice_plan_revisions
    ENABLE TRIGGER preparation_practice_plan_revisions_are_immutable;

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
           'suggested_duration_seconds', 'min_effective_turns',
           'max_effective_turns', 'coverage_checkpoint_turn',
           'max_follow_ups_per_question', 'early_completion_rule',
           'retry_allowed', 'question_translation_allowed',
           'question_tips_allowed', 'avatar_allowed',
           'speech_feedback_allowed'
       ]
       OR payload - ARRAY[
           'suggested_duration_seconds', 'min_effective_turns',
           'max_effective_turns', 'coverage_checkpoint_turn',
           'max_follow_ups_per_question', 'early_completion_rule',
           'retry_allowed', 'question_translation_allowed',
           'question_tips_allowed', 'avatar_allowed',
           'speech_feedback_allowed'
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
       OR payload ->> 'suggested_duration_seconds' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'min_effective_turns' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'max_effective_turns' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'coverage_checkpoint_turn' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'max_follow_ups_per_question' !~ '^[0-9]{1,9}$'
       OR (payload ->> 'min_effective_turns')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR (payload ->> 'coverage_checkpoint_turn')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR (payload ->> 'max_effective_turns')::integer > 64
       OR jsonb_typeof(payload -> 'early_completion_rule') <> 'string'
       OR payload ->> 'early_completion_rule' <>
           'COVERAGE_SATISFIED_AFTER_CHECKPOINT' THEN
        RETURN false;
    END IF;
    RETURN true;
END;
$$;

COMMIT;
