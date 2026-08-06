BEGIN;

ALTER TABLE practice_turn_results
    ADD CONSTRAINT practice_turn_results_owner_user_id_session_id_round_number_key
    UNIQUE (owner_user_id, session_id, round_number);

COMMIT;
