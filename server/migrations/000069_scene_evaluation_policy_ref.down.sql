BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM preparation_practice_plans)
       OR EXISTS (SELECT 1 FROM practice_sessions) THEN
        RAISE EXCEPTION
            'Scene Evaluation Policy rollback requires empty development and test Practice data. RECREATE THE DEVELOPMENT OR TEST DATABASE before rollback.';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION validate_preparation_practice_plan_scene_selection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    expected_scene jsonb;
    expected_assignment jsonb;
    option_payload jsonb;
    persisted_current_revision integer;
    scene_owner_user_id uuid;
    selected_role_ids jsonb;
BEGIN
    IF jsonb_typeof(NEW.scene_selection) <> 'object'
       OR NOT NEW.scene_selection ?& ARRAY[
           'scene',
           'selected_role_ids',
           'practice_option_id'
       ]
       OR NOT coaching_scene_nonempty_string_array_is_valid_v1(
           NEW.scene_selection -> 'selected_role_ids'
       ) THEN
        RAISE EXCEPTION
            'Practice Plan revision contains an invalid Scene selection'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plan_revisions_scene_check';
    END IF;

    SELECT plan.current_revision
    INTO persisted_current_revision
    FROM preparation_practice_plans AS plan
    WHERE plan.owner_user_id = NEW.owner_user_id
      AND plan.plan_id = NEW.plan_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION
            'Practice Plan revision must reference an actor-owned Plan'
            USING
                ERRCODE = '23503',
                CONSTRAINT =
                    'preparation_practice_plan_revisions_plan_fkey';
    END IF;

    IF NEW.revision = 1 THEN
        SELECT scenes.owner_user_id
        INTO scene_owner_user_id
        FROM coaching_scenes AS scenes
        WHERE scenes.scene_id = NEW.scene_id
        FOR UPDATE;

        IF NOT FOUND
           OR (
               scene_owner_user_id IS NOT NULL
               AND scene_owner_user_id <> NEW.owner_user_id
           ) THEN
            RAISE EXCEPTION
                'Initial Practice Plan revision must reference an accessible Scene'
                USING
                    ERRCODE = '23503',
                    CONSTRAINT =
                        'preparation_practice_plan_revisions_scene_fkey';
        END IF;

        SELECT jsonb_build_object(
                   'scene_id', version.scene_id,
                   'scene_family', version.scene_family,
                   'scene_model', version.scene_model,
                   'name', version.name,
                   'scene_version', version.scene_version,
                   'status', version.status,
                   'turn_policy_ref', version.turn_policy_ref,
                   'session_policy_ref', version.session_policy_ref,
                   'prompt', version.prompt,
                   'roles', (
                       SELECT jsonb_agg(
                           role.value - 'display_order'
                           ORDER BY
                               (role.value ->> 'display_order')::integer,
                               role.value ->> 'role_definition_id'
                       )
                       FROM jsonb_array_elements(version.roles)
                           AS role(value)
                   ),
                   'practice_options', (
                       SELECT jsonb_agg(
                           practice_option.value - 'display_order'
                           ORDER BY
                               (
                                   practice_option.value ->> 'display_order'
                               )::integer,
                               practice_option.value ->> 'practice_option_id'
                       )
                       FROM jsonb_array_elements(version.practice_options)
                           AS practice_option(value)
                   )
               )
        INTO expected_scene
        FROM coaching_scene_versions AS version
        WHERE version.scene_id = NEW.scene_id
          AND version.scene_version = NEW.scene_version
          AND version.status = 'active'
          AND NOT EXISTS (
              SELECT 1
              FROM coaching_scene_versions AS later_version
              WHERE later_version.scene_id = version.scene_id
                AND later_version.scene_version > version.scene_version
          );

        IF expected_scene IS NULL THEN
            RAISE EXCEPTION
                'Initial Practice Plan revision must reference the latest active exact Scene version'
                USING
                    ERRCODE = '23503',
                    CONSTRAINT =
                        'preparation_practice_plan_revisions_scene_fkey';
        END IF;

        IF NEW.ielts_assignment IS NOT NULL THEN
            expected_scene := jsonb_set(
                jsonb_set(
                    expected_scene,
                    '{prompt,turn_blueprints}',
                    NEW.ielts_assignment -> 'turn_blueprints'
                ),
                '{prompt,public_scene_brief}',
                NEW.scene_selection #> '{scene,prompt,public_scene_brief}'
            );
        END IF;
    ELSE
        IF NEW.revision <> persisted_current_revision + 1 THEN
            RAISE EXCEPTION
                'Practice Plan revisions must append to the current revision'
                USING
                    ERRCODE = '23514',
                    CONSTRAINT =
                        'preparation_practice_plan_revisions_scene_check';
        END IF;

        SELECT
            previous.scene_selection -> 'scene',
            previous.ielts_assignment
        INTO expected_scene, expected_assignment
        FROM preparation_practice_plan_revisions AS previous
        WHERE previous.owner_user_id = NEW.owner_user_id
          AND previous.plan_id = NEW.plan_id
          AND previous.revision = NEW.revision - 1;

        IF expected_scene IS NULL THEN
            RAISE EXCEPTION
                'Practice Plan revisions must append to the preceding frozen revision'
                USING
                    ERRCODE = '23514',
                    CONSTRAINT =
                        'preparation_practice_plan_revisions_scene_check';
        END IF;

        IF NEW.ielts_assignment IS DISTINCT FROM expected_assignment THEN
            RAISE EXCEPTION
                'Practice Plan revisions cannot change their frozen IELTS assignment'
                USING
                    ERRCODE = '23514',
                    CONSTRAINT =
                        'preparation_practice_plan_revisions_ielts_check';
        END IF;
    END IF;

    IF expected_scene ->> 'scene_id' <> NEW.scene_id
       OR (expected_scene ->> 'scene_version')::bigint <>
           NEW.scene_version THEN
        RAISE EXCEPTION
            'Practice Plan revisions cannot change their frozen Scene version'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plan_revisions_scene_check';
    END IF;

    IF NEW.scene_selection -> 'scene' <> expected_scene THEN
        RAISE EXCEPTION
            'Practice Plan revision Scene snapshot must match the exact catalog version'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plan_revisions_scene_check';
    END IF;

    selected_role_ids := NEW.scene_selection -> 'selected_role_ids';
    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements_text(selected_role_ids) AS selected(role_id)
        WHERE NOT EXISTS (
            SELECT 1
            FROM jsonb_array_elements(expected_scene -> 'roles') AS role(value)
            WHERE role.value ->> 'role_definition_id' = selected.role_id
        )
    ) THEN
        RAISE EXCEPTION
            'Practice Plan revision contains a role outside the selected Scene version'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plan_revisions_scene_check';
    END IF;

    SELECT practice_option.value
    INTO option_payload
    FROM jsonb_array_elements(expected_scene -> 'practice_options')
        AS practice_option(value)
    WHERE practice_option.value ->> 'practice_option_id' =
        NEW.scene_selection ->> 'practice_option_id';

    IF option_payload IS NULL
       OR (
           option_payload ->> 'practice_option_type' = 'FOCUS'
           AND (
               jsonb_array_length(selected_role_ids) <> 1
               OR selected_role_ids ->> 0 <>
                   option_payload ->> 'role_definition_id'
           )
       ) THEN
        RAISE EXCEPTION
            'Practice Plan revision contains an invalid Scene practice option'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plan_revisions_scene_check';
    END IF;

    RETURN NEW;
END;
$$;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_evaluation_policy_ref_check,
    DROP COLUMN evaluation_policy_ref;

ALTER TABLE coaching_scene_versions
    DROP CONSTRAINT coaching_scene_versions_evaluation_policy_ref_check,
    DROP COLUMN evaluation_policy_ref;

COMMIT;
