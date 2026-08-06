BEGIN;

ALTER TABLE practice_turn_results
    DROP CONSTRAINT practice_turn_results_owner_user_id_session_id_round_number_key;

COMMIT;
