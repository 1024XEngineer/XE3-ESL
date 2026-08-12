BEGIN;

DROP TRIGGER IF EXISTS evaluation_general_scene_atomic_attempts_immutable
    ON evaluation_general_scene_atomic_attempts;
DROP FUNCTION IF EXISTS reject_evaluation_general_scene_atomic_attempt_mutation();
DROP TRIGGER IF EXISTS evaluation_general_scene_atomic_attempts_binding
    ON evaluation_general_scene_atomic_attempts;
DROP FUNCTION IF EXISTS evaluation_assert_general_scene_atomic_attempt_binding();
DROP TABLE IF EXISTS evaluation_general_scene_atomic_attempts;
DROP FUNCTION IF EXISTS evaluation_general_scene_atomic_result_shape_is_valid(
    jsonb
);

COMMIT;
