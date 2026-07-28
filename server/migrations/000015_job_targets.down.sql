BEGIN;

DROP TABLE IF EXISTS preparation_job_target_idempotency_records;
DROP TABLE IF EXISTS preparation_job_target_confirmations;
DROP TABLE IF EXISTS preparation_job_target_analysis_attempts;
DROP TABLE IF EXISTS preparation_job_targets;

COMMIT;
