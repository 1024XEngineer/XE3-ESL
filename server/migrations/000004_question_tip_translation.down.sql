BEGIN;

ALTER TABLE practice_questions
    DROP CONSTRAINT practice_questions_tip_check;
ALTER TABLE practice_questions
    DROP COLUMN tip_translation;

ALTER TABLE practice_questions
    ADD CONSTRAINT practice_questions_tip_check CHECK (
        (
            tip_status IS NULL
            AND tip_id IS NULL
            AND tip_client_request_id IS NULL
            AND tip_fencing_token = 0
            AND tip_lease_expires_at IS NULL
            AND tip_content IS NULL
            AND tip_provider IS NULL
            AND tip_model IS NULL
            AND tip_provider_request_id IS NULL
            AND tip_created_at IS NULL
            AND tip_completed_at IS NULL
        ) OR (
            tip_status = 'processing'
            AND tip_id IS NOT NULL
            AND tip_client_request_id IS NOT NULL
            AND tip_fencing_token > 0
            AND tip_lease_expires_at > updated_at
            AND tip_content IS NULL
            AND tip_provider IS NULL
            AND tip_model IS NULL
            AND tip_provider_request_id IS NULL
            AND tip_created_at IS NOT NULL
            AND tip_completed_at IS NULL
        ) OR (
            tip_status = 'completed'
            AND tip_id IS NOT NULL
            AND tip_client_request_id IS NOT NULL
            AND tip_fencing_token > 0
            AND tip_lease_expires_at IS NULL
            AND tip_content IS NOT NULL
            AND tip_provider IS NOT NULL
            AND tip_model IS NOT NULL
            AND tip_provider_request_id IS NOT NULL
            AND tip_created_at IS NOT NULL
            AND tip_completed_at IS NOT NULL
        ) OR (
            tip_status = 'failed'
            AND tip_id IS NOT NULL
            AND tip_client_request_id IS NOT NULL
            AND tip_fencing_token > 0
            AND tip_lease_expires_at IS NULL
            AND tip_content IS NULL
            AND tip_provider IS NULL
            AND tip_model IS NULL
            AND tip_provider_request_id IS NULL
            AND tip_created_at IS NOT NULL
            AND tip_completed_at IS NULL
        )
    );

COMMIT;
