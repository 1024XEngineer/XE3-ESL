BEGIN;

DROP TABLE IF EXISTS review_speech_feedback_acoustic_evidence;

ALTER TABLE review_speech_feedback_turn_snapshots
    DROP CONSTRAINT IF EXISTS review_speech_feedback_turn_snapshots_audio_check,
    DROP COLUMN IF EXISTS audio_checksum_sha256,
    DROP COLUMN IF EXISTS audio_asset_version,
    DROP COLUMN IF EXISTS audio_asset_id;

COMMIT;
