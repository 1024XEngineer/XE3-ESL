BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM evaluations
        WHERE kind IN ('IELTS_PART1_PROFILE', 'IELTS_PART2_PROFILE')
    ) THEN
        RAISE EXCEPTION 'cannot roll back IELTS incremental profiles while profile evaluations exist';
    END IF;
END
$$;

ALTER TABLE evaluations DROP CONSTRAINT evaluations_kind_check;

ALTER TABLE evaluations ADD CONSTRAINT evaluations_kind_check CHECK (
    kind IN (
        'SESSION_REPORT',
        'PRACTICE_TURN_FEEDBACK',
        'AGENT_MESSAGE_FEEDBACK'
    )
);

COMMIT;
