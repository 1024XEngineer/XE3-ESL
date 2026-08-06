BEGIN;

ALTER TABLE review_speech_feedback_acoustic_evidence
    DROP CONSTRAINT review_speech_feedback_acoustic_provider_check,
    DROP CONSTRAINT review_speech_feedback_acoustic_scores_check;

DELETE FROM review_speech_feedback_acoustic_evidence
WHERE category = 'topic';

ALTER TABLE review_speech_feedback_acoustic_evidence
    DROP COLUMN speaking_speed_wpm,
    DROP COLUMN phone_score,
    ALTER COLUMN accuracy_score SET NOT NULL,
    ALTER COLUMN fluency_score SET NOT NULL,
    ALTER COLUMN integrity_score SET NOT NULL,
    ADD CONSTRAINT review_speech_feedback_acoustic_provider_check
        CHECK (
            provider = 'xfyun-ise'
            AND provider_session_id !~ '^[[:space:]]*$'
            AND octet_length(provider_session_id) <= 256
            AND category IN ('read_word', 'read_sentence')
        ),
    ADD CONSTRAINT review_speech_feedback_acoustic_scores_check
        CHECK (
            accuracy_score BETWEEN 0 AND 100
            AND fluency_score BETWEEN 0 AND 100
            AND integrity_score BETWEEN 0 AND 100
        );

COMMIT;
