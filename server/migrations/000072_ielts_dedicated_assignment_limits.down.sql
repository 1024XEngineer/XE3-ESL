BEGIN;

ALTER TABLE preparation_practice_plan_revisions
    DROP CONSTRAINT preparation_practice_plan_revisions_ielts_check,
    ADD CONSTRAINT preparation_practice_plan_revisions_ielts_check
        CHECK (
            preparation_plan_ielts_assignment_is_valid_v1(
                scene_selection,
                ielts_assignment
            )
        );

DROP FUNCTION preparation_plan_ielts_assignment_is_valid_v2(jsonb, jsonb);

COMMIT;
