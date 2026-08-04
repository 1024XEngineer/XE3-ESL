BEGIN;

ALTER TABLE practice_completed
    ADD COLUMN delivery_status text NOT NULL DEFAULT 'PENDING',
    ADD COLUMN attempt_count integer NOT NULL DEFAULT 0,
    ADD COLUMN fencing_token bigint NOT NULL DEFAULT 0,
    ADD COLUMN lease_expires_at timestamptz,
    ADD COLUMN available_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    ADD COLUMN failure_code text,
    ADD COLUMN failure_retryable boolean,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    ADD COLUMN delivered_at timestamptz,
    ADD CONSTRAINT practice_completed_delivery_status_check
        CHECK (delivery_status IN ('PENDING', 'RUNNING', 'DELIVERED', 'FAILED')),
    ADD CONSTRAINT practice_completed_delivery_attempt_check
        CHECK (
            attempt_count >= 0
            AND fencing_token >= attempt_count
        ),
    ADD CONSTRAINT practice_completed_delivery_failure_check
        CHECK (
            failure_code IS NULL
            OR failure_code ~ '^[a-z][a-z0-9_.:-]{0,127}$'
        ),
    ADD CONSTRAINT practice_completed_delivery_state_check
        CHECK (
            (
                delivery_status = 'PENDING'
                AND lease_expires_at IS NULL
                AND failure_code IS NULL
                AND failure_retryable IS NULL
                AND delivered_at IS NULL
            )
            OR (
                delivery_status = 'RUNNING'
                AND lease_expires_at IS NOT NULL
                AND failure_code IS NULL
                AND failure_retryable IS NULL
                AND delivered_at IS NULL
            )
            OR (
                delivery_status = 'DELIVERED'
                AND lease_expires_at IS NULL
                AND failure_code IS NULL
                AND failure_retryable IS NULL
                AND delivered_at IS NOT NULL
            )
            OR (
                delivery_status = 'FAILED'
                AND lease_expires_at IS NULL
                AND failure_code IS NOT NULL
                AND failure_retryable IS NOT NULL
                AND delivered_at IS NULL
            )
        ),
    ADD CONSTRAINT practice_completed_delivery_timestamps_check
        CHECK (
            updated_at >= created_at
            AND available_at >= created_at
            AND (lease_expires_at IS NULL OR lease_expires_at > updated_at)
            AND (delivered_at IS NULL OR delivered_at >= created_at)
        );

CREATE INDEX practice_completed_delivery_pending_idx
    ON practice_completed (available_at, created_at, owner_user_id, session_id)
    WHERE delivery_status IN ('PENDING', 'RUNNING');

COMMIT;
