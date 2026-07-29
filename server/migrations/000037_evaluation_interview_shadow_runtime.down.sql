BEGIN;

DROP TRIGGER IF EXISTS evaluation_interview_scene_results_immutable
    ON evaluation_interview_scene_results;
DROP FUNCTION IF EXISTS
    reject_evaluation_interview_scene_result_mutation();
DROP TRIGGER IF EXISTS evaluation_interview_scene_results_binding
    ON evaluation_interview_scene_results;
DROP FUNCTION IF EXISTS
    evaluation_assert_interview_scene_result_binding();
DROP TABLE IF EXISTS evaluation_interview_scene_results;
DROP FUNCTION IF EXISTS
    evaluation_interview_result_refs_are_consistent(text, jsonb);

DROP TRIGGER IF EXISTS evaluation_module_runs_identity_immutable
    ON evaluation_module_runs;
DROP FUNCTION IF EXISTS
    reject_evaluation_module_run_identity_mutation();
DROP TRIGGER IF EXISTS evaluation_module_runs_binding
    ON evaluation_module_runs;
DROP FUNCTION IF EXISTS
    evaluation_assert_interview_shadow_run_binding();
DROP TABLE IF EXISTS evaluation_module_runs;

DROP INDEX IF EXISTS evaluation_outbox_pending_idx;
ALTER TABLE evaluation_outbox
    DROP CONSTRAINT evaluation_outbox_runtime_timestamps_check,
    DROP CONSTRAINT evaluation_outbox_delivery_shape_check,
    DROP CONSTRAINT evaluation_outbox_failure_code_check,
    DROP CONSTRAINT evaluation_outbox_fencing_token_check,
    DROP CONSTRAINT evaluation_outbox_delivery_status_check;

UPDATE evaluation_outbox
SET delivery_status = 'DELIVERED',
    delivered_at = coalesce(failed_at, transaction_timestamp())
WHERE delivery_status = 'FAILED';

UPDATE evaluation_outbox
SET lease_expires_at = NULL
WHERE delivery_status = 'PENDING';

ALTER TABLE evaluation_outbox
    DROP COLUMN updated_at,
    DROP COLUMN failed_at,
    DROP COLUMN last_failure_code,
    DROP COLUMN fencing_token,
    DROP COLUMN lease_expires_at,
    ADD CONSTRAINT evaluation_outbox_delivery_status_check
        CHECK (delivery_status IN ('PENDING', 'DELIVERED')),
    ADD CONSTRAINT evaluation_outbox_delivery_shape_check
        CHECK (
            (delivery_status = 'PENDING' AND delivered_at IS NULL)
            OR (
                delivery_status = 'DELIVERED'
                AND delivered_at IS NOT NULL
            )
        );

CREATE INDEX evaluation_outbox_pending_idx
    ON evaluation_outbox (available_at, created_at, id)
    WHERE delivery_status = 'PENDING';

COMMIT;
