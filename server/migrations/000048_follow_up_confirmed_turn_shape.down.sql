BEGIN;

ALTER TABLE conversation_confirmed_turns
    DROP CONSTRAINT conversation_confirmed_turns_retry_shape_check;

ALTER TABLE conversation_confirmed_turns
    ADD CONSTRAINT conversation_confirmed_turns_retry_shape_check
        CHECK (
            (
                turn_kind = 'EFFECTIVE'
                AND retry_request_id IS NULL
                AND original_turn_id IS NULL
                AND counts_toward_effective_turn_limit
            )
            OR
            (
                turn_kind = 'RETRY'
                AND retry_request_id IS NOT NULL
                AND original_turn_id IS NOT NULL
                AND NOT counts_toward_effective_turn_limit
                AND progress_recorded_at IS NULL
                AND effective_turns = 0
                AND NOT session_completed
                AND review_id IS NULL
                AND review_source_turn_id IS NULL
                AND review_recorded_at IS NULL
            )
        );

COMMIT;
