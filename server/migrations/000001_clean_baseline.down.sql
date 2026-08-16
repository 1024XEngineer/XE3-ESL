BEGIN;

DROP TABLE evaluation_feedback_items;
DROP TABLE evaluations;
DROP TABLE practice_turns;
DROP TABLE practice_questions;
DROP TABLE practice_sessions;
DROP TABLE interview_preparations;
DROP TABLE practice_plans;
DROP TABLE agent_voice_drafts;
DROP TABLE agent_message_attachments;
DROP TABLE media_assets;
ALTER TABLE agent_messages
    DROP CONSTRAINT agent_messages_produced_by_run_fkey;
DROP TABLE agent_runs;
DROP TABLE agent_messages;
DROP TABLE agent_threads;
DROP TABLE coaching_user_profiles;
DROP TABLE auth_sessions;
DROP TABLE credentials;
DROP TABLE users;

COMMIT;
