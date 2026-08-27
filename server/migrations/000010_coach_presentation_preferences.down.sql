BEGIN;

ALTER TABLE practice_sessions DROP COLUMN presentation_snapshot;

DROP TABLE user_coach_presentation_preferences;
DROP TABLE coach_voice_options;
DROP TABLE coach_avatar_options;

COMMIT;
