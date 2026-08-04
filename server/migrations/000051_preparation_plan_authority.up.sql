BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM practice_plans)
       OR EXISTS (SELECT 1 FROM practice_sessions)
       OR EXISTS (SELECT 1 FROM practice_session_snapshots)
       OR EXISTS (
           SELECT 1
           FROM reviews
           WHERE evaluation_context IS NOT NULL
       )
       OR EXISTS (
           SELECT 1
           FROM practice_idempotency_records
           WHERE resource_kind IN ('plan', 'plan_revision')
       ) THEN
        RAISE EXCEPTION
            'Preparation Plan authority migration requires empty legacy Plan, Session, and Review context data slices; recreate the development or test database before applying migration 000051'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_context_snapshot_fkey,
    DROP CONSTRAINT practice_sessions_goal_context_anchor_fkey,
    DROP CONSTRAINT practice_sessions_context_thread_fkey,
    DROP CONSTRAINT practice_sessions_context_plan_fkey,
    DROP CONSTRAINT practice_sessions_context_fields_check,
    DROP CONSTRAINT practice_sessions_lifecycle_check,
    DROP CONSTRAINT practice_sessions_scenario_model_check;

ALTER TABLE practice_session_snapshots
    DROP CONSTRAINT practice_session_snapshots_context_session_fkey,
    DROP CONSTRAINT practice_session_snapshots_context_fields_check,
    DROP CONSTRAINT practice_session_snapshots_context_binding_key,
    DROP CONSTRAINT practice_session_snapshots_context_snapshot_key,
    DROP CONSTRAINT practice_session_snapshots_preparation_fkey,
    DROP CONSTRAINT practice_session_snapshots_context_plan_fkey;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_context_binding_key,
    DROP CONSTRAINT practice_sessions_context_snapshot_key;

DROP INDEX practice_one_effective_session_per_context_plan;
DROP INDEX practice_one_effective_session_per_agent_thread;

DROP TABLE practice_plans;

CREATE FUNCTION preparation_plan_objectives_are_valid_v1(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'array'
       OR jsonb_array_length(payload) = 0
       OR EXISTS (
           SELECT 1
           FROM jsonb_array_elements(payload) AS objective(item)
           WHERE jsonb_typeof(item) <> 'object'
              OR NOT item ?& ARRAY['objective_id', 'description']
              OR item - ARRAY['objective_id', 'description'] <> '{}'::jsonb
              OR jsonb_typeof(item -> 'objective_id') <> 'string'
              OR jsonb_typeof(item -> 'description') <> 'string'
              OR item ->> 'objective_id' !~
                  '^[a-z][a-z0-9_]{0,127}$'
              OR item ->> 'description' = ''
              OR item ->> 'description' <>
                  btrim(item ->> 'description')
       ) THEN
        RETURN false;
    END IF;

    RETURN (
        SELECT count(*) = count(DISTINCT item ->> 'objective_id')
        FROM jsonb_array_elements(payload) AS objective(item)
    );
END;
$$;

CREATE FUNCTION preparation_plan_session_policy_is_valid_v1(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'object'
       OR NOT payload ?& ARRAY[
           'suggested_duration_seconds',
           'min_effective_turns',
           'max_effective_turns',
           'coverage_checkpoint_turn',
           'max_follow_ups_per_question',
           'early_completion_rule'
       ]
       OR payload - ARRAY[
           'suggested_duration_seconds',
           'min_effective_turns',
           'max_effective_turns',
           'coverage_checkpoint_turn',
           'max_follow_ups_per_question',
           'early_completion_rule'
       ] <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'suggested_duration_seconds') <> 'number'
       OR jsonb_typeof(payload -> 'min_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'max_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'coverage_checkpoint_turn') <> 'number'
       OR jsonb_typeof(payload -> 'max_follow_ups_per_question') <> 'number'
       OR (payload ->> 'suggested_duration_seconds') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'min_effective_turns') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'max_effective_turns') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'coverage_checkpoint_turn') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'max_follow_ups_per_question') !~ '^[0-9]{1,9}$'
       OR (payload ->> 'min_effective_turns')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR (payload ->> 'coverage_checkpoint_turn')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR jsonb_typeof(payload -> 'early_completion_rule') <> 'string'
       OR payload ->> 'early_completion_rule' <>
           'COVERAGE_SATISFIED_AFTER_CHECKPOINT' THEN
        RETURN false;
    END IF;

    RETURN true;
END;
$$;

CREATE FUNCTION preparation_plan_snapshot_is_valid_v1(
    expected_snapshot_id text,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    has_job_target boolean;
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'object'
       OR NOT payload ?& ARRAY[
           'preparation_snapshot_id',
           'source_profile_id',
           'source_version',
           'background_snapshot',
           'created_at'
       ]
       OR payload - ARRAY[
           'preparation_snapshot_id',
           'source_profile_id',
           'source_version',
           'source_job_target_id',
           'source_job_target_confirmation_version',
           'job_target_input_snapshot',
           'job_target_candidate_snapshot',
           'resume_snapshot',
           'job_description_snapshot',
           'background_snapshot',
           'created_at'
       ] <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'preparation_snapshot_id') <> 'string'
       OR payload ->> 'preparation_snapshot_id' <> expected_snapshot_id
       OR jsonb_typeof(payload -> 'source_profile_id') <> 'string'
       OR payload ->> 'source_profile_id' !~
           '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
       OR jsonb_typeof(payload -> 'source_version') <> 'number'
       OR (payload ->> 'source_version') !~ '^[1-9][0-9]{0,8}$'
       OR jsonb_typeof(payload -> 'background_snapshot') <> 'string'
       OR payload ->> 'background_snapshot' = ''
       OR payload ->> 'background_snapshot' <>
           btrim(payload ->> 'background_snapshot')
       OR jsonb_typeof(payload -> 'created_at') <> 'string'
       OR payload ->> 'created_at' = '' THEN
        RETURN false;
    END IF;

    IF payload ? 'resume_snapshot'
       AND (
           jsonb_typeof(payload -> 'resume_snapshot') <> 'string'
           OR payload ->> 'resume_snapshot' = ''
       ) THEN
        RETURN false;
    END IF;

    IF payload ? 'job_description_snapshot'
       AND (
           jsonb_typeof(payload -> 'job_description_snapshot') <> 'string'
           OR payload ->> 'job_description_snapshot' = ''
       ) THEN
        RETURN false;
    END IF;

    has_job_target := payload ? 'source_job_target_id';
    IF has_job_target <> (payload ? 'source_job_target_confirmation_version')
       OR has_job_target <> (payload ? 'job_target_input_snapshot')
       OR has_job_target <> (payload ? 'job_target_candidate_snapshot') THEN
        RETURN false;
    END IF;

    IF has_job_target
       AND (
           jsonb_typeof(payload -> 'source_job_target_id') <> 'string'
           OR payload ->> 'source_job_target_id' !~
               '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
           OR jsonb_typeof(
               payload -> 'source_job_target_confirmation_version'
           ) <> 'number'
           OR (payload ->> 'source_job_target_confirmation_version') !~
               '^[1-9][0-9]{0,8}$'
           OR jsonb_typeof(payload -> 'job_target_input_snapshot') <>
               'object'
           OR jsonb_typeof(payload -> 'job_target_candidate_snapshot') <>
               'object'
       ) THEN
        RETURN false;
    END IF;

    RETURN true;
END;
$$;

CREATE FUNCTION preparation_plan_goal_snapshot_is_valid_v1(
    expected_goal_id uuid,
    expected_goal_version bigint,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
BEGIN
    IF expected_goal_id IS NULL
       OR expected_goal_version IS NULL
       OR payload IS NULL THEN
        RETURN expected_goal_id IS NULL
           AND expected_goal_version IS NULL
           AND payload IS NULL;
    END IF;

    RETURN jsonb_typeof(payload) = 'object'
       AND payload ?& ARRAY['goal_id', 'title', 'version']
       AND payload - ARRAY['goal_id', 'title', 'version'] = '{}'::jsonb
       AND jsonb_typeof(payload -> 'goal_id') = 'string'
       AND payload ->> 'goal_id' = expected_goal_id::text
       AND jsonb_typeof(payload -> 'title') = 'string'
       AND payload ->> 'title' <> ''
       AND payload ->> 'title' = btrim(payload ->> 'title')
       AND jsonb_typeof(payload -> 'version') = 'number'
       AND (payload ->> 'version') ~ '^[1-9][0-9]{0,17}$'
       AND (payload ->> 'version')::bigint = expected_goal_version;
END;
$$;

CREATE FUNCTION preparation_plan_ielts_assignment_is_valid_v1(
    scene_selection jsonb,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    scene_family text;
    scene_model text;
    expected_mode text;
    required_keys text[];
    part_1_questions integer;
    part_2_questions integer;
    part_3_questions integer;
BEGIN
    scene_family := scene_selection #>> '{scene,scene_family}';
    scene_model := scene_selection #>> '{scene,scene_model}';
    IF scene_family = 'EXAM' THEN
        expected_mode := CASE scene_model
            WHEN 'IELTS_SPEAKING_FULL_MOCK' THEN 'FULL_MOCK'
            WHEN 'IELTS_SPEAKING_PART_1' THEN 'PART_1'
            WHEN 'IELTS_SPEAKING_PART_2' THEN 'PART_2'
            WHEN 'IELTS_SPEAKING_PART_3' THEN 'PART_3'
            ELSE NULL
        END;
    END IF;

    IF expected_mode IS NULL THEN
        RETURN payload IS NULL;
    END IF;
    IF payload IS NULL OR jsonb_typeof(payload) <> 'object' THEN
        RETURN false;
    END IF;

    required_keys := CASE expected_mode
        WHEN 'FULL_MOCK' THEN ARRAY[
            'bank_id',
            'season',
            'mode',
            'part_1_set_id',
            'topic_group_id',
            'topic_title',
            'part_2_cue_card',
            'part_1_questions',
            'part_2_questions',
            'part_3_questions',
            'turn_blueprints'
        ]
        WHEN 'PART_1' THEN ARRAY[
            'bank_id',
            'season',
            'mode',
            'part_1_set_id',
            'part_1_questions',
            'part_2_questions',
            'part_3_questions',
            'turn_blueprints'
        ]
        WHEN 'PART_2' THEN ARRAY[
            'bank_id',
            'season',
            'mode',
            'topic_group_id',
            'topic_title',
            'part_2_cue_card',
            'part_1_questions',
            'part_2_questions',
            'part_3_questions',
            'turn_blueprints'
        ]
        WHEN 'PART_3' THEN ARRAY[
            'bank_id',
            'season',
            'mode',
            'topic_group_id',
            'topic_title',
            'part_1_questions',
            'part_2_questions',
            'part_3_questions',
            'turn_blueprints'
        ]
    END;

    IF NOT payload ?& required_keys
       OR payload - required_keys <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'bank_id') <> 'string'
       OR payload ->> 'bank_id' !~
           '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
       OR jsonb_typeof(payload -> 'season') <> 'string'
       OR payload ->> 'season' = ''
       OR payload ->> 'season' <> btrim(payload ->> 'season')
       OR jsonb_typeof(payload -> 'mode') <> 'string'
       OR payload ->> 'mode' <> expected_mode
       OR jsonb_typeof(payload -> 'part_1_questions') <> 'number'
       OR jsonb_typeof(payload -> 'part_2_questions') <> 'number'
       OR jsonb_typeof(payload -> 'part_3_questions') <> 'number'
       OR (payload ->> 'part_1_questions') !~ '^[0-9]{1,3}$'
       OR (payload ->> 'part_2_questions') !~ '^[0-9]{1,3}$'
       OR (payload ->> 'part_3_questions') !~ '^[0-9]{1,3}$'
       OR NOT coaching_scene_nonempty_string_array_is_valid_v1(
           payload -> 'turn_blueprints'
       ) THEN
        RETURN false;
    END IF;

    part_1_questions := (payload ->> 'part_1_questions')::integer;
    part_2_questions := (payload ->> 'part_2_questions')::integer;
    part_3_questions := (payload ->> 'part_3_questions')::integer;

    IF payload -> 'turn_blueprints' IS DISTINCT FROM
           scene_selection #> '{scene,prompt,turn_blueprints}' THEN
        RETURN false;
    END IF;
    IF jsonb_typeof(
           scene_selection #> '{scene,prompt,public_scene_brief}'
       ) IS DISTINCT FROM 'string'
       OR scene_selection #>> '{scene,prompt,public_scene_brief}' = ''
       OR scene_selection #>> '{scene,prompt,public_scene_brief}' <>
           btrim(scene_selection #>> '{scene,prompt,public_scene_brief}') THEN
        RETURN false;
    END IF;

    CASE expected_mode
        WHEN 'FULL_MOCK' THEN
            IF jsonb_typeof(payload -> 'part_1_set_id') <> 'string'
               OR jsonb_typeof(payload -> 'topic_group_id') <> 'string'
               OR jsonb_typeof(payload -> 'topic_title') <> 'string'
               OR jsonb_typeof(payload -> 'part_2_cue_card') <> 'string'
               OR payload ->> 'part_1_set_id' !~
                   '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
               OR payload ->> 'topic_group_id' !~
                   '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
               OR payload ->> 'topic_title' = ''
               OR payload ->> 'topic_title' <>
                   btrim(payload ->> 'topic_title')
               OR payload ->> 'part_2_cue_card' = ''
               OR payload ->> 'part_2_cue_card' <>
                   btrim(payload ->> 'part_2_cue_card')
               OR part_1_questions <> 8
               OR part_2_questions <> 1
               OR part_3_questions NOT BETWEEN 1 AND 5
               OR jsonb_array_length(payload -> 'turn_blueprints') <>
                   9 + part_3_questions THEN
                RETURN false;
            END IF;
        WHEN 'PART_1' THEN
            IF jsonb_typeof(payload -> 'part_1_set_id') <> 'string'
               OR payload ->> 'part_1_set_id' !~
                   '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
               OR part_1_questions <> 8
               OR part_2_questions <> 0
               OR part_3_questions <> 0
               OR jsonb_array_length(payload -> 'turn_blueprints') <> 8 THEN
                RETURN false;
            END IF;
        WHEN 'PART_2' THEN
            IF jsonb_typeof(payload -> 'topic_group_id') <> 'string'
               OR jsonb_typeof(payload -> 'topic_title') <> 'string'
               OR jsonb_typeof(payload -> 'part_2_cue_card') <> 'string'
               OR payload ->> 'topic_group_id' !~
                   '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
               OR payload ->> 'topic_title' = ''
               OR payload ->> 'topic_title' <>
                   btrim(payload ->> 'topic_title')
               OR payload ->> 'part_2_cue_card' = ''
               OR payload ->> 'part_2_cue_card' <>
                   btrim(payload ->> 'part_2_cue_card')
               OR part_1_questions <> 0
               OR part_2_questions <> 1
               OR part_3_questions NOT BETWEEN 1 AND 5
               OR jsonb_array_length(payload -> 'turn_blueprints') <>
                   1 + part_3_questions THEN
                RETURN false;
            END IF;
        WHEN 'PART_3' THEN
            IF jsonb_typeof(payload -> 'topic_group_id') <> 'string'
               OR jsonb_typeof(payload -> 'topic_title') <> 'string'
               OR payload ->> 'topic_group_id' !~
                   '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
               OR payload ->> 'topic_title' = ''
               OR payload ->> 'topic_title' <>
                   btrim(payload ->> 'topic_title')
               OR part_1_questions <> 0
               OR part_2_questions <> 0
               OR part_3_questions NOT BETWEEN 1 AND 5
               OR jsonb_array_length(payload -> 'turn_blueprints') <>
                   part_3_questions THEN
                RETURN false;
            END IF;
    END CASE;

    RETURN true;
END;
$$;

CREATE TABLE preparation_practice_plans (
    owner_user_id uuid NOT NULL,
    plan_id text COLLATE "C" NOT NULL,
    current_revision integer NOT NULL,
    status text NOT NULL,
    source_thread_id uuid,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT preparation_practice_plans_pkey
        PRIMARY KEY (owner_user_id, plan_id),
    CONSTRAINT preparation_practice_plans_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_practice_plans_source_thread_fkey
        FOREIGN KEY (source_thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE SET NULL (source_thread_id),
    CONSTRAINT preparation_practice_plans_id_check
        CHECK (plan_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT preparation_practice_plans_revision_check
        CHECK (current_revision > 0),
    CONSTRAINT preparation_practice_plans_status_check
        CHECK (status IN ('ready', 'archived')),
    CONSTRAINT preparation_practice_plans_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE INDEX preparation_practice_plans_owner_updated_idx
    ON preparation_practice_plans (
        owner_user_id,
        updated_at DESC,
        plan_id
    );

CREATE INDEX preparation_practice_plans_source_thread_idx
    ON preparation_practice_plans (
        owner_user_id,
        source_thread_id,
        updated_at DESC,
        plan_id
    )
    WHERE source_thread_id IS NOT NULL;

CREATE TABLE preparation_practice_plan_revisions (
    owner_user_id uuid NOT NULL,
    plan_id text COLLATE "C" NOT NULL,
    revision integer NOT NULL,
    goal_id uuid,
    goal_version bigint,
    goal_snapshot jsonb,
    preparation_snapshot_id text COLLATE "C" NOT NULL,
    preparation_snapshot jsonb NOT NULL,
    scene_id text COLLATE "C" NOT NULL,
    scene_version bigint NOT NULL,
    scene_selection jsonb NOT NULL,
    session_policy jsonb NOT NULL,
    practice_objectives jsonb NOT NULL,
    ielts_assignment jsonb,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT preparation_practice_plan_revisions_pkey
        PRIMARY KEY (owner_user_id, plan_id, revision),
    CONSTRAINT preparation_practice_plan_revisions_plan_fkey
        FOREIGN KEY (owner_user_id, plan_id)
        REFERENCES preparation_practice_plans (owner_user_id, plan_id)
        ON DELETE CASCADE,
    CONSTRAINT preparation_practice_plan_revisions_goal_fkey
        FOREIGN KEY (goal_id, owner_user_id)
        REFERENCES coaching_goals (goal_id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_practice_plan_revisions_snapshot_fkey
        FOREIGN KEY (owner_user_id, preparation_snapshot_id)
        REFERENCES preparation_snapshots (owner_user_id, snapshot_id)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_practice_plan_revisions_scene_fkey
        FOREIGN KEY (scene_id, scene_version)
        REFERENCES coaching_scene_versions (scene_id, scene_version)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_practice_plan_revisions_revision_check
        CHECK (revision > 0),
    CONSTRAINT preparation_practice_plan_revisions_goal_check
        CHECK (
            preparation_plan_goal_snapshot_is_valid_v1(
                goal_id,
                goal_version,
                goal_snapshot
            )
        ),
    CONSTRAINT preparation_practice_plan_revisions_snapshot_check
        CHECK (
            preparation_plan_snapshot_is_valid_v1(
                preparation_snapshot_id,
                preparation_snapshot
            )
        ),
    CONSTRAINT preparation_practice_plan_revisions_scene_check
        CHECK (
            jsonb_typeof(scene_selection) = 'object'
            AND scene_selection ?& ARRAY[
                'scene',
                'selected_role_ids',
                'practice_option_id'
            ]
            AND scene_selection - ARRAY[
                'scene',
                'selected_role_ids',
                'practice_option_id'
            ] = '{}'::jsonb
            AND coaching_scene_nonempty_string_array_is_valid_v1(
                scene_selection -> 'selected_role_ids'
            )
            AND jsonb_typeof(scene_selection -> 'practice_option_id') =
                'string'
            AND scene_selection ->> 'practice_option_id' ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT preparation_practice_plan_revisions_policy_check
        CHECK (
            preparation_plan_session_policy_is_valid_v1(
                session_policy
            )
            AND preparation_plan_objectives_are_valid_v1(
                practice_objectives
            )
        ),
    CONSTRAINT preparation_practice_plan_revisions_ielts_check
        CHECK (
            preparation_plan_ielts_assignment_is_valid_v1(
                scene_selection,
                ielts_assignment
            )
        )
);

ALTER TABLE preparation_practice_plans
    ADD CONSTRAINT preparation_practice_plans_current_revision_fkey
        FOREIGN KEY (owner_user_id, plan_id, current_revision)
        REFERENCES preparation_practice_plan_revisions (
            owner_user_id,
            plan_id,
            revision
        )
        DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION validate_preparation_practice_plan_current_revision_advance()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_revision integer;
BEGIN
    IF NEW.current_revision = OLD.current_revision THEN
        RETURN NEW;
    END IF;

    SELECT max(revision)
    INTO latest_revision
    FROM preparation_practice_plan_revisions
    WHERE owner_user_id = NEW.owner_user_id
      AND plan_id = NEW.plan_id;

    IF NEW.current_revision <> OLD.current_revision + 1
       OR NEW.current_revision IS DISTINCT FROM latest_revision THEN
        RAISE EXCEPTION
            'Preparation Practice Plan current revision must advance by one to the latest immutable revision'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plans_current_revision_advance_check';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER preparation_practice_plans_current_revision_advances
    BEFORE UPDATE OF current_revision ON preparation_practice_plans
    FOR EACH ROW
    EXECUTE FUNCTION validate_preparation_practice_plan_current_revision_advance();

CREATE FUNCTION validate_preparation_practice_plan_scene_selection()
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

CREATE TRIGGER preparation_practice_plan_revisions_validate_scene
    BEFORE INSERT ON preparation_practice_plan_revisions
    FOR EACH ROW
    EXECUTE FUNCTION validate_preparation_practice_plan_scene_selection();

CREATE FUNCTION require_preparation_practice_plan_revision_publication()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    persisted_current_revision integer;
BEGIN
    SELECT current_revision
    INTO persisted_current_revision
    FROM preparation_practice_plans
    WHERE owner_user_id = NEW.owner_user_id
      AND plan_id = NEW.plan_id;

    IF persisted_current_revision IS DISTINCT FROM NEW.revision THEN
        RAISE EXCEPTION
            'Preparation Practice Plan revision must become current in the transaction that appends it'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plans_current_revision_advance_check';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER preparation_practice_plan_revision_is_current
    AFTER INSERT ON preparation_practice_plan_revisions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    EXECUTE FUNCTION require_preparation_practice_plan_revision_publication();

CREATE FUNCTION reject_preparation_practice_plan_revision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION
        'Preparation Practice Plan revisions are immutable; insert a new revision instead'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER preparation_practice_plan_revisions_are_immutable
    BEFORE UPDATE OR DELETE ON preparation_practice_plan_revisions
    FOR EACH ROW
    EXECUTE FUNCTION reject_preparation_practice_plan_revision_mutation();

ALTER TABLE preparation_idempotency_records
    DROP CONSTRAINT preparation_idempotency_resource_check,
    ADD COLUMN resource_revision integer,
    ADD CONSTRAINT preparation_idempotency_resource_check
        CHECK (
            btrim(resource_id) <> ''
            AND (
                (
                    resource_kind IN ('profile', 'snapshot')
                    AND resource_revision IS NULL
                )
                OR (
                    resource_kind = 'plan'
                    AND resource_revision > 0
                )
            )
        ),
    ADD CONSTRAINT preparation_idempotency_plan_revision_fkey
        FOREIGN KEY (owner_user_id, resource_id, resource_revision)
        REFERENCES preparation_practice_plan_revisions (
            owner_user_id,
            plan_id,
            revision
        )
        ON DELETE CASCADE;

ALTER TABLE practice_idempotency_records
    DROP CONSTRAINT practice_idempotency_resource_check,
    ADD CONSTRAINT practice_idempotency_resource_check
        CHECK (
            resource_kind IN (
                'session',
                'pause',
                'resume',
                'end_early'
            )
            AND btrim(resource_id) <> ''
        );

ALTER TABLE practice_sessions
    RENAME COLUMN scenario_type TO scene_family;

ALTER TABLE practice_sessions
    RENAME COLUMN scenario_model TO scene_model;

ALTER TABLE practice_sessions
    DROP COLUMN context_plan_id,
    DROP COLUMN agent_thread_id,
    DROP COLUMN goal_id,
    ADD COLUMN plan_revision integer NOT NULL,
    ALTER COLUMN snapshot_id SET NOT NULL,
    ALTER COLUMN scene_family SET NOT NULL,
    ALTER COLUMN scene_model SET NOT NULL,
    ADD CONSTRAINT practice_sessions_plan_revision_fkey
        FOREIGN KEY (owner_user_id, plan_id, plan_revision)
        REFERENCES preparation_practice_plan_revisions (
            owner_user_id,
            plan_id,
            revision
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_sessions_plan_revision_check
        CHECK (plan_revision > 0),
    ADD CONSTRAINT practice_sessions_snapshot_key
        UNIQUE (owner_user_id, snapshot_id),
    ADD CONSTRAINT practice_sessions_snapshot_binding_key
        UNIQUE (owner_user_id, session_id, snapshot_id),
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            btrim(snapshot_id) <> ''
            AND btrim(scene_family) <> ''
            AND btrim(scene_model) <> ''
        ),
    ADD CONSTRAINT practice_sessions_lifecycle_check
        CHECK (
            (
                status = 'starting'
                AND started_at IS NULL
                AND completed_at IS NULL
                AND end_reason IS NULL
            )
            OR (
                status IN ('in_progress', 'paused')
                AND started_at IS NOT NULL
                AND completed_at IS NULL
                AND end_reason IS NULL
            )
            OR (
                status IN ('completed', 'ended_early')
                AND started_at IS NOT NULL
                AND completed_at IS NOT NULL
                AND end_reason IS NOT NULL
                AND btrim(end_reason) <> ''
            )
        ),
    ADD CONSTRAINT practice_sessions_scene_model_check
        CHECK (
            (
                scene_family = 'INTERVIEW'
                AND scene_model IN (
                    'PROJECT_EXPERIENCE_DEEP_DIVE',
                    'INTERVIEW_BASIC_DIALOGUE'
                )
            )
            OR (
                scene_family = 'EXAM'
                AND scene_model IN (
                    'IELTS_SPEAKING_PART_1',
                    'IELTS_SPEAKING_PART_2',
                    'IELTS_SPEAKING_PART_3',
                    'IELTS_SPEAKING_FULL_MOCK',
                    'EXAM_BASIC_DIALOGUE'
                )
            )
            OR (
                scene_family = 'WORKPLACE'
                AND scene_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR (
                scene_family = 'DAILY'
                AND scene_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        );

CREATE UNIQUE INDEX practice_one_effective_session_per_plan
    ON practice_sessions (owner_user_id, plan_id)
    WHERE status IN ('starting', 'in_progress', 'paused');

ALTER TABLE practice_session_snapshots
    DROP COLUMN context_plan_id,
    DROP COLUMN preparation_snapshot_id,
    ALTER COLUMN snapshot_id SET NOT NULL,
    ALTER COLUMN snapshot_document SET NOT NULL,
    ADD CONSTRAINT practice_session_snapshots_snapshot_key
        UNIQUE (owner_user_id, snapshot_id),
    ADD CONSTRAINT practice_session_snapshots_session_binding_key
        UNIQUE (owner_user_id, session_id, snapshot_id),
    ADD CONSTRAINT practice_session_snapshots_document_check
        CHECK (
            btrim(snapshot_id) <> ''
            AND jsonb_typeof(snapshot_document) = 'object'
        ),
    ADD CONSTRAINT practice_session_snapshots_session_binding_fkey
        FOREIGN KEY (owner_user_id, session_id, snapshot_id)
        REFERENCES practice_sessions (
            owner_user_id,
            session_id,
            snapshot_id
        )
        ON DELETE CASCADE
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE practice_sessions
    ADD CONSTRAINT practice_sessions_snapshot_fkey
        FOREIGN KEY (owner_user_id, session_id, snapshot_id)
        REFERENCES practice_session_snapshots (
            owner_user_id,
            session_id,
            snapshot_id
        )
        DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION require_current_ready_preparation_plan_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    persisted_revision integer;
    persisted_status text;
BEGIN
    SELECT current_revision, status
    INTO persisted_revision, persisted_status
    FROM preparation_practice_plans
    WHERE owner_user_id = NEW.owner_user_id
      AND plan_id = NEW.plan_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION
            'Practice Session must reference an actor-owned Preparation Plan'
            USING
                ERRCODE = '23503',
                CONSTRAINT = 'practice_sessions_plan_revision_fkey';
    END IF;

    IF persisted_status <> 'ready'
       OR persisted_revision <> NEW.plan_revision THEN
        RAISE EXCEPTION
            'Practice Session must use the current ready Preparation Plan revision'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'practice_sessions_executable_plan_revision_check';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER practice_sessions_require_current_ready_plan_revision
    BEFORE INSERT OR UPDATE OF owner_user_id, plan_id, plan_revision
    ON practice_sessions
    FOR EACH ROW
    EXECUTE FUNCTION require_current_ready_preparation_plan_revision();

ALTER TABLE reviews
    DROP CONSTRAINT reviews_evaluation_context_check,
    ADD CONSTRAINT reviews_evaluation_context_check
        CHECK (
            evaluation_context IS NULL
            OR (
                jsonb_typeof(evaluation_context) = 'object'
                AND evaluation_context ?& ARRAY[
                    'schema_version',
                    'context_type',
                    'scene_key',
                    'scene_id',
                    'scene_version',
                    'practice_option_type',
                    'difficulty_ref',
                    'assistance_ref',
                    'turn_policy_ref',
                    'session_policy_ref',
                    'scene_specific_context'
                ]
                AND evaluation_context->>'schema_version' =
                    'evaluation-context.v1'
                AND octet_length(evaluation_context::text) <= 16384
            )
        );

ALTER TABLE practice_retry_turn_authorizations
    DROP CONSTRAINT practice_retry_turn_authorizations_scenario_check;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN scenario_type TO scene_family;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN scenario_model TO scene_model;

ALTER TABLE practice_retry_turn_authorizations
    ADD CONSTRAINT practice_retry_turn_authorizations_scene_check
        CHECK (
            (
                scene_family = 'WORKPLACE'
                AND scene_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR (
                scene_family = 'DAILY'
                AND scene_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        );

COMMIT;
