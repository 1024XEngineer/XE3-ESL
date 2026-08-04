BEGIN;

DROP INDEX practice_completed_delivery_pending_idx;

ALTER TABLE practice_completed
    DROP CONSTRAINT practice_completed_delivery_timestamps_check,
    DROP CONSTRAINT practice_completed_delivery_state_check,
    DROP CONSTRAINT practice_completed_delivery_failure_check,
    DROP CONSTRAINT practice_completed_delivery_attempt_check,
    DROP CONSTRAINT practice_completed_delivery_status_check,
    DROP COLUMN delivered_at,
    DROP COLUMN updated_at,
    DROP COLUMN failure_retryable,
    DROP COLUMN failure_code,
    DROP COLUMN available_at,
    DROP COLUMN lease_expires_at,
    DROP COLUMN fencing_token,
    DROP COLUMN attempt_count,
    DROP COLUMN delivery_status;

COMMIT;
