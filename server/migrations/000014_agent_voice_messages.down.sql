BEGIN;

DROP TABLE IF EXISTS agent_voice_transcript_evidence;

ALTER TABLE IF EXISTS agent_voice_candidates
    DROP CONSTRAINT IF EXISTS agent_voice_candidates_message_audio_fkey,
    DROP CONSTRAINT IF EXISTS agent_voice_candidates_confirmed_run_fkey,
    DROP CONSTRAINT IF EXISTS agent_voice_candidates_confirmed_message_fkey;

DROP TABLE IF EXISTS agent_message_audios;
DROP TABLE IF EXISTS agent_voice_candidates;

ALTER TABLE agent_messages
    DROP CONSTRAINT IF EXISTS agent_messages_voice_role_check,
    DROP CONSTRAINT IF EXISTS agent_messages_modality_check,
    DROP COLUMN IF EXISTS modality;

COMMIT;
