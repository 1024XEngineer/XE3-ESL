BEGIN;

CREATE UNIQUE INDEX practice_turns_one_progressed_effective_question_idx
    ON practice_turns (session_id, question_id)
    WHERE turn_kind = 'EFFECTIVE' AND progressed_at IS NOT NULL;

COMMIT;
