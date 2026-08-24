BEGIN;

ALTER TABLE evaluations DROP CONSTRAINT evaluations_kind_check;

ALTER TABLE evaluations ADD CONSTRAINT evaluations_kind_check CHECK (
    kind IN (
        'SESSION_REPORT',
        'PRACTICE_TURN_FEEDBACK',
        'AGENT_MESSAGE_FEEDBACK'
    )
);

COMMIT;
