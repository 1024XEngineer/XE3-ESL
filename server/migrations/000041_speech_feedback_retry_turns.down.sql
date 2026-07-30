BEGIN;

DROP TABLE IF EXISTS review_speech_feedback_retry_requests;
DROP TABLE IF EXISTS practice_retry_turn_authorizations;
DROP TABLE IF EXISTS conversation_retry_turn_drafts;

DROP INDEX IF EXISTS conversation_retry_turn_request_unique;
DROP INDEX IF EXISTS conversation_effective_turn_question_unique;

ALTER TABLE conversation_confirmed_turns
    DROP CONSTRAINT IF EXISTS
        conversation_confirmed_turns_original_turn_fkey,
    DROP CONSTRAINT IF EXISTS
        conversation_confirmed_turns_retry_shape_check,
    DROP CONSTRAINT IF EXISTS
        conversation_confirmed_turns_kind_check,
    DROP COLUMN IF EXISTS counts_toward_effective_turn_limit,
    DROP COLUMN IF EXISTS original_turn_id,
    DROP COLUMN IF EXISTS retry_request_id,
    DROP COLUMN IF EXISTS turn_kind;

ALTER TABLE conversation_confirmed_turns
    ADD CONSTRAINT
        conversation_confirmed_turns_owner_user_id_practice_session_key
        UNIQUE (owner_user_id, practice_session_id, question_id);

COMMIT;
