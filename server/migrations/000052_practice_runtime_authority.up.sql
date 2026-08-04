BEGIN;

ALTER TABLE conversation_questions RENAME TO practice_questions;
ALTER TABLE conversation_transcription_reservations RENAME TO practice_transcription_reservations;
ALTER TABLE conversation_processing_attempts RENAME TO practice_processing_attempts;
ALTER TABLE conversation_transcript_candidates RENAME TO practice_transcript_candidates;
ALTER TABLE conversation_confirmed_turns RENAME TO practice_turns;
ALTER TABLE conversation_turn_confirmations RENAME TO practice_turn_confirmations;
ALTER TABLE conversation_audio_assets RENAME TO practice_audio_assets;
ALTER TABLE conversation_retry_turn_drafts RENAME TO practice_retry_turn_drafts;

ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_pkey TO practice_audio_assets_pkey;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_owner_user_id_fkey
    TO practice_audio_assets_owner_user_id_fkey;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_owner_upload_request_key
    TO practice_audio_assets_owner_upload_request_key;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_object_key_key
    TO practice_audio_assets_object_key_key;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_id_length_check
    TO practice_audio_assets_id_length_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_upload_request_length_check
    TO practice_audio_assets_upload_request_length_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_object_key_check
    TO practice_audio_assets_object_key_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_binding_lengths_check
    TO practice_audio_assets_binding_lengths_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_binding_pair_check
    TO practice_audio_assets_binding_pair_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_content_type_check
    TO practice_audio_assets_content_type_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_size_check
    TO practice_audio_assets_size_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_checksum_check
    TO practice_audio_assets_checksum_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_duration_check
    TO practice_audio_assets_duration_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_etag_length_check
    TO practice_audio_assets_etag_length_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_staged_etag_check
    TO practice_audio_assets_staged_etag_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_committed_etag_check
    TO practice_audio_assets_committed_etag_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_status_check
    TO practice_audio_assets_status_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_readable_binding_check
    TO practice_audio_assets_readable_binding_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_unconfirmed_binding_check
    TO practice_audio_assets_unconfirmed_binding_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_cleanup_lease_check
    TO practice_audio_assets_cleanup_lease_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_upload_lease_check
    TO practice_audio_assets_upload_lease_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_upload_fence_check
    TO practice_audio_assets_upload_fence_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_cleanup_fence_check
    TO practice_audio_assets_cleanup_fence_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_timestamps_check
    TO practice_audio_assets_timestamps_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_deleted_at_check
    TO practice_audio_assets_deleted_at_check;
ALTER TABLE practice_audio_assets RENAME CONSTRAINT
    conversation_audio_assets_version_check
    TO practice_audio_assets_version_check;

ALTER INDEX conversation_audio_assets_owner_candidate_key
    RENAME TO practice_audio_assets_owner_candidate_key;
ALTER INDEX conversation_audio_assets_owner_turn_key
    RENAME TO practice_audio_assets_owner_turn_key;
ALTER INDEX conversation_audio_assets_expired_cleanup_idx
    RENAME TO practice_audio_assets_expired_cleanup_idx;
ALTER INDEX conversation_audio_assets_upload_recovery_idx
    RENAME TO practice_audio_assets_upload_recovery_idx;
ALTER INDEX conversation_audio_assets_deleting_cleanup_idx
    RENAME TO practice_audio_assets_deleting_cleanup_idx;
ALTER INDEX conversation_audio_assets_owner_cleanup_idx
    RENAME TO practice_audio_assets_owner_cleanup_idx;
ALTER INDEX conversation_audio_assets_owner_deleted_purge_idx
    RENAME TO practice_audio_assets_owner_deleted_purge_idx;

DROP INDEX conversation_turns_review_owner_idx;
ALTER TABLE practice_turns
    DROP COLUMN review_id,
    DROP COLUMN review_source_turn_id,
    DROP COLUMN review_recorded_at;

CREATE TABLE practice_completed (
    owner_user_id uuid NOT NULL,
    session_id text NOT NULL,
    final_turn_id text NOT NULL,
    session_version integer NOT NULL CHECK (session_version > 1),
    completion_token text NOT NULL CHECK (btrim(completion_token) <> ''),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, session_id),
    UNIQUE (owner_user_id, final_turn_id),
    UNIQUE (completion_token),
    FOREIGN KEY (owner_user_id, session_id, final_turn_id)
        REFERENCES practice_turn_results (owner_user_id, session_id, turn_id)
        ON DELETE CASCADE
);

DROP TABLE conversation_deletion_fences;

COMMIT;
