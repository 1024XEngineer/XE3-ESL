BEGIN;

ALTER TABLE evaluation_speech_feedback_acoustic_evidence
    DROP CONSTRAINT evaluation_speech_feedback_acoustic_provider_check,
    ADD CONSTRAINT evaluation_speech_feedback_acoustic_provider_check
        CHECK (
            octet_length(provider) BETWEEN 1 AND 128
            AND provider ~ '^[A-Za-z0-9._:-]+$'
            AND provider_session_id !~ '^[[:space:]]*$'
            AND octet_length(provider_session_id) <= 256
            AND category IN ('read_word', 'read_sentence', 'topic')
        );

COMMIT;
