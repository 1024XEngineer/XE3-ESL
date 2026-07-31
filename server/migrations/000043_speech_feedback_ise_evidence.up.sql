BEGIN;

ALTER TABLE review_speech_feedback_turn_snapshots
    ADD COLUMN audio_asset_id text COLLATE "C",
    ADD COLUMN audio_asset_version bigint,
    ADD COLUMN audio_checksum_sha256 text COLLATE "C",
    ADD CONSTRAINT review_speech_feedback_turn_snapshots_audio_check
        CHECK (
            (
                audio_asset_id IS NULL
                AND audio_asset_version IS NULL
                AND audio_checksum_sha256 IS NULL
            )
            OR
            (
                audio_asset_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
                AND audio_asset_version > 0
                AND audio_checksum_sha256 ~ '^[0-9a-f]{64}$'
            )
        );

CREATE TABLE review_speech_feedback_acoustic_evidence (
    speech_feedback_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    provider text COLLATE "C" NOT NULL,
    provider_session_id text COLLATE "C" NOT NULL,
    category text COLLATE "C" NOT NULL,
    accuracy_score double precision NOT NULL,
    fluency_score double precision NOT NULL,
    integrity_score double precision NOT NULL,
    raw_result text NOT NULL,
    available_fields jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT review_speech_feedback_acoustic_feedback_fkey
        FOREIGN KEY (speech_feedback_id, owner_user_id)
        REFERENCES review_speech_feedbacks (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT review_speech_feedback_acoustic_provider_check
        CHECK (
            provider = 'xfyun-ise'
            AND provider_session_id !~ '^[[:space:]]*$'
            AND octet_length(provider_session_id) <= 256
            AND category IN ('read_word', 'read_sentence')
        ),
    CONSTRAINT review_speech_feedback_acoustic_scores_check
        CHECK (
            accuracy_score BETWEEN 0 AND 100
            AND fluency_score BETWEEN 0 AND 100
            AND integrity_score BETWEEN 0 AND 100
        ),
    CONSTRAINT review_speech_feedback_acoustic_payload_check
        CHECK (
            octet_length(raw_result) BETWEEN 1 AND 1048576
            AND jsonb_typeof(available_fields) = 'array'
        )
);

CREATE TRIGGER review_speech_feedback_acoustic_evidence_immutable
BEFORE UPDATE ON review_speech_feedback_acoustic_evidence
FOR EACH ROW
EXECUTE FUNCTION reject_review_speech_feedback_snapshot_update();

COMMIT;
