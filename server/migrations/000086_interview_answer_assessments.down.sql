BEGIN;

ALTER TABLE practice_turns
    DROP CONSTRAINT practice_turns_answer_assessment_shape_check,
    DROP COLUMN advance_authorized,
    DROP COLUMN assessment_policy_version,
    DROP COLUMN answer_assessment;

ALTER TABLE practice_questions
    DROP CONSTRAINT practice_questions_dialogue_act_check,
    DROP COLUMN dialogue_act;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM practice_turn_results
        WHERE effective_turns <> round_number
    ) THEN
        RAISE EXCEPTION
            'cannot roll back interview answer assessments while non-counting turn results exist';
    END IF;
END
$$;

ALTER TABLE practice_turn_results
    DROP CONSTRAINT practice_turn_results_effective_turns_check,
    ADD CONSTRAINT practice_turn_results_effective_turns_check CHECK (
        effective_turns = round_number
    );

COMMIT;
