BEGIN;

DROP TABLE IF EXISTS conversation_turn_confirmations;
DROP TABLE IF EXISTS conversation_confirmed_turns;
ALTER TABLE IF EXISTS conversation_transcription_reservations
    DROP CONSTRAINT IF EXISTS conversation_reservation_candidate_fk;
DROP TABLE IF EXISTS conversation_transcript_candidates;
DROP TABLE IF EXISTS conversation_processing_attempts;
DROP TABLE IF EXISTS conversation_transcription_reservations;
DROP TABLE IF EXISTS conversation_questions;
DROP TABLE IF EXISTS conversation_deletion_fences;

COMMIT;
