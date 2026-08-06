BEGIN;

ALTER TABLE review_speech_feedback_turn_snapshots
    DROP CONSTRAINT review_speech_feedback_turn_snapshots_source_check,
    ADD CONSTRAINT review_speech_feedback_turn_snapshots_source_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND turn_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND input_revision > 0
            AND octet_length(transcript_text) BETWEEN 1 AND 16384
            AND transcript_text !~ '^[[:space:]]*$'
            AND octet_length(source_digest) = 32
            AND source_digest <> decode(repeat('00', 32), 'hex')
        );

COMMIT;
