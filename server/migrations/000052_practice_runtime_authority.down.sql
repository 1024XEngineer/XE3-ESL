BEGIN;

CREATE TABLE conversation_deletion_fences (
    owner_user_id uuid PRIMARY KEY,
    deletion_generation bigint NOT NULL CHECK (deletion_generation > 0),
    applied_at timestamptz NOT NULL
);

DROP TABLE practice_completed;

ALTER TABLE practice_turns
    ADD COLUMN review_id text,
    ADD COLUMN review_source_turn_id text,
    ADD COLUMN review_recorded_at timestamptz;
CREATE UNIQUE INDEX conversation_turns_review_owner_idx
    ON practice_turns (owner_user_id, review_id)
    WHERE review_id IS NOT NULL;

ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_pkey TO conversation_audio_assets_pkey;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_owner_user_id_fkey
    TO conversation_audio_assets_owner_user_id_fkey;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_owner_upload_request_key
    TO conversation_audio_assets_owner_upload_request_key;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_object_key_key
    TO conversation_audio_assets_object_key_key;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_id_length_check
    TO conversation_audio_assets_id_length_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_upload_request_length_check
    TO conversation_audio_assets_upload_request_length_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_object_key_check
    TO conversation_audio_assets_object_key_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_binding_lengths_check
    TO conversation_audio_assets_binding_lengths_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_binding_pair_check
    TO conversation_audio_assets_binding_pair_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_content_type_check
    TO conversation_audio_assets_content_type_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_size_check
    TO conversation_audio_assets_size_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_checksum_check
    TO conversation_audio_assets_checksum_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_duration_check
    TO conversation_audio_assets_duration_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_etag_length_check
    TO conversation_audio_assets_etag_length_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_staged_etag_check
    TO conversation_audio_assets_staged_etag_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_committed_etag_check
    TO conversation_audio_assets_committed_etag_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_status_check
    TO conversation_audio_assets_status_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_readable_binding_check
    TO conversation_audio_assets_readable_binding_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_unconfirmed_binding_check
    TO conversation_audio_assets_unconfirmed_binding_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_cleanup_lease_check
    TO conversation_audio_assets_cleanup_lease_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_upload_lease_check
    TO conversation_audio_assets_upload_lease_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_upload_fence_check
    TO conversation_audio_assets_upload_fence_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_cleanup_fence_check
    TO conversation_audio_assets_cleanup_fence_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_timestamps_check
    TO conversation_audio_assets_timestamps_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_deleted_at_check
    TO conversation_audio_assets_deleted_at_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    practice_audio_assets_version_check
    TO conversation_audio_assets_version_check;

ALTER INDEX practice_audio_assets_owner_candidate_key
    RENAME TO conversation_audio_assets_owner_candidate_key;
ALTER INDEX practice_audio_assets_owner_turn_key
    RENAME TO conversation_audio_assets_owner_turn_key;
ALTER INDEX practice_audio_assets_expired_cleanup_idx
    RENAME TO conversation_audio_assets_expired_cleanup_idx;
ALTER INDEX practice_audio_assets_upload_recovery_idx
    RENAME TO conversation_audio_assets_upload_recovery_idx;
ALTER INDEX practice_audio_assets_deleting_cleanup_idx
    RENAME TO conversation_audio_assets_deleting_cleanup_idx;
ALTER INDEX practice_audio_assets_owner_cleanup_idx
    RENAME TO conversation_audio_assets_owner_cleanup_idx;
ALTER INDEX practice_audio_assets_owner_deleted_purge_idx
    RENAME TO conversation_audio_assets_owner_deleted_purge_idx;

ALTER TABLE practice_retry_turn_drafts RENAME TO conversation_retry_turn_drafts;
ALTER TABLE practice_audio_assets RENAME TO conversation_audio_assets;
ALTER TABLE practice_turn_confirmations RENAME TO conversation_turn_confirmations;
ALTER TABLE practice_turns RENAME TO conversation_confirmed_turns;
ALTER TABLE practice_transcript_candidates RENAME TO conversation_transcript_candidates;
ALTER TABLE practice_processing_attempts RENAME TO conversation_processing_attempts;
ALTER TABLE practice_transcription_reservations RENAME TO conversation_transcription_reservations;
ALTER TABLE practice_questions RENAME TO conversation_questions;

COMMIT;
