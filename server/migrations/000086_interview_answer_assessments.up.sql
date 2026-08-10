BEGIN;

ALTER TABLE practice_turns
    ADD COLUMN answer_assessment jsonb,
    ADD COLUMN assessment_policy_version text,
    ADD COLUMN advance_authorized boolean;

ALTER TABLE practice_turns
    ADD CONSTRAINT practice_turns_answer_assessment_shape_check CHECK (
        (answer_assessment IS NULL AND assessment_policy_version IS NULL
            AND advance_authorized IS NULL)
        OR
        (answer_assessment IS NOT NULL
            AND jsonb_typeof(answer_assessment) = 'object'
            AND btrim(assessment_policy_version) <> ''
            AND advance_authorized IS NOT NULL)
    );

ALTER TABLE practice_questions
    ADD COLUMN dialogue_act text,
    ADD CONSTRAINT practice_questions_dialogue_act_check CHECK (
        dialogue_act IS NULL OR dialogue_act IN (
            'PROBE',
            'REFRAME',
            'ACKNOWLEDGE_AND_PROBE',
            'REPEAT_OR_REPAIR',
            'TRANSITION'
        )
    );

ALTER TABLE practice_turn_results
    DROP CONSTRAINT practice_turn_results_effective_turns_check,
    ADD CONSTRAINT practice_turn_results_effective_turns_check CHECK (
        effective_turns >= 0 AND effective_turns <= round_number
    );

COMMIT;
