BEGIN;

ALTER TABLE review_speech_feedback_acoustic_evidence
    DROP CONSTRAINT review_speech_feedback_acoustic_provider_check,
    DROP CONSTRAINT review_speech_feedback_acoustic_scores_check,
    ALTER COLUMN accuracy_score DROP NOT NULL,
    ALTER COLUMN fluency_score DROP NOT NULL,
    ALTER COLUMN integrity_score DROP NOT NULL,
    ADD COLUMN phone_score double precision,
    ADD COLUMN speaking_speed_wpm double precision,
    ADD CONSTRAINT review_speech_feedback_acoustic_provider_check
        CHECK (
            provider = 'xfyun-ise'
            AND provider_session_id !~ '^[[:space:]]*$'
            AND octet_length(provider_session_id) <= 256
            AND category IN ('read_word', 'read_sentence', 'topic')
        ),
    ADD CONSTRAINT review_speech_feedback_acoustic_scores_check
        CHECK (
            (
                category IN ('read_word', 'read_sentence')
                AND accuracy_score BETWEEN 0 AND 100
                AND fluency_score BETWEEN 0 AND 100
                AND integrity_score BETWEEN 0 AND 100
                AND phone_score IS NULL
                AND speaking_speed_wpm IS NULL
            )
            OR
            (
                category = 'topic'
                AND accuracy_score BETWEEN 0 AND 100
                AND fluency_score IS NULL
                AND integrity_score IS NULL
                AND phone_score BETWEEN 0 AND 100
                AND speaking_speed_wpm > 0
                AND speaking_speed_wpm <= 1000
            )
        );

COMMIT;
