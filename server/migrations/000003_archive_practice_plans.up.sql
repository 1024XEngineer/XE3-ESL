BEGIN;

ALTER TABLE practice_plans
    DROP CONSTRAINT practice_plans_status_check;
ALTER TABLE practice_plans
    ADD CONSTRAINT practice_plans_status_check
    CHECK (status IN ('draft', 'ready', 'archived'));

COMMIT;
