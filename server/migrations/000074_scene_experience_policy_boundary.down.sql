BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM preparation_practice_plans)
       OR EXISTS (SELECT 1 FROM practice_sessions)
       OR EXISTS (SELECT 1 FROM evaluation_ledgers)
       OR EXISTS (SELECT 1 FROM evaluation_evidence_snapshots)
       OR EXISTS (SELECT 1 FROM evaluation_formal_reports)
       OR EXISTS (
           SELECT 1
           FROM coaching_scenes
           WHERE owner_user_id IS NOT NULL
       ) THEN
        RAISE EXCEPTION
            'Scene Experience and Policy boundary rollback requires empty development and test Practice/Evaluation data and no private Scenes. RECREATE THE DEVELOPMENT OR TEST DATABASE at migration 000072, then rerun migrations.'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER evaluation_ielts_scene_results_part_binding
    ON evaluation_ielts_speaking_scene_results;

DROP FUNCTION evaluation_assert_ielts_part_result_binding_v1();

ALTER TABLE evaluation_ielts_speaking_scene_results
    DROP CONSTRAINT evaluation_ielts_scene_results_payload_check;

CREATE OR REPLACE FUNCTION evaluation_ielts_result_shape_is_valid(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    criterion jsonb;
    question jsonb;
    criterion_index integer;
    question_index integer;
    expected_criterion text;
    expected_part text;
    status text;
BEGIN
    IF jsonb_typeof(payload) IS DISTINCT FROM 'object'
       OR payload ->> 'schema_version'
            IS DISTINCT FROM 'ielts-speaking-full-mock-shadow/v1'
       OR payload ->> 'scene_type' IS DISTINCT FROM 'IELTS_SPEAKING'
       OR payload ->> 'scope' IS DISTINCT FROM 'SESSION'
       OR payload ->> 'channel' IS DISTINCT FROM 'SCENE'
       OR jsonb_typeof(payload -> 'snapshot_id')
            IS DISTINCT FROM 'string'
       OR octet_length(payload ->> 'snapshot_id') NOT BETWEEN 1 AND 128
       OR payload ->> 'snapshot_id' <> btrim(payload ->> 'snapshot_id')
       OR (
            payload ->> 'scoreability_status'
                IS DISTINCT FROM 'PROVISIONAL'
            AND payload ->> 'scoreability_status'
                IS DISTINCT FROM 'INSUFFICIENT'
       )
       OR (
            payload ->> 'scoreability_status' = 'PROVISIONAL'
            AND payload ->> 'gate_status'
                IS DISTINCT FROM 'FEEDBACK_ONLY'
       )
       OR (
            payload ->> 'scoreability_status' = 'INSUFFICIENT'
            AND payload ->> 'gate_status' IS DISTINCT FROM 'BLOCKED'
       )
       OR jsonb_typeof(payload -> 'reason_codes')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'reason_codes') < 1
       OR jsonb_typeof(payload -> 'criteria') IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'criteria') <> 4
       OR jsonb_typeof(payload -> 'question_results')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'question_results') <> 14
       OR payload ? 'overall'
       OR payload ? 'speaking_overall'
       OR jsonb_path_exists(
            payload,
            '$.** ? (@.type() == "object").keyvalue() ? (
                @.key like_regex
                "^(score|band|overall|speaking[-_]?overall|weight|weights|raw|display|interval|total)$"
                flag "i"
            )'
       )
    THEN
        RETURN false;
    END IF;

    FOR criterion_index IN 0..3 LOOP
        criterion := payload -> 'criteria' -> criterion_index;
        expected_criterion := CASE criterion_index
            WHEN 0 THEN 'IELTS_FC'
            WHEN 1 THEN 'IELTS_LR'
            WHEN 2 THEN 'IELTS_GRA'
            ELSE 'IELTS_PR'
        END;
        status := criterion ->> 'scoreability_status';
        IF jsonb_typeof(criterion) IS DISTINCT FROM 'object'
           OR criterion ->> 'criterion_id'
                IS DISTINCT FROM expected_criterion
           OR (
                status IS DISTINCT FROM 'PROVISIONAL'
                AND status IS DISTINCT FROM 'INSUFFICIENT'
           )
           OR (
                payload ->> 'scoreability_status' = 'PROVISIONAL'
                AND (
                    (
                        expected_criterion = 'IELTS_PR'
                        AND status IS DISTINCT FROM 'INSUFFICIENT'
                    )
                    OR (
                        expected_criterion <> 'IELTS_PR'
                        AND status IS DISTINCT FROM 'PROVISIONAL'
                    )
                )
           )
           OR (
                payload ->> 'scoreability_status' = 'INSUFFICIENT'
                AND status IS DISTINCT FROM 'INSUFFICIENT'
           )
           OR (
                status = 'PROVISIONAL'
                AND criterion ->> 'gate_status'
                    IS DISTINCT FROM 'FEEDBACK_ONLY'
           )
           OR (
                status = 'INSUFFICIENT'
                AND criterion ->> 'gate_status'
                    IS DISTINCT FROM 'BLOCKED'
           )
           OR jsonb_typeof(criterion -> 'reason_codes')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(criterion -> 'reason_codes') < 1
           OR jsonb_typeof(criterion -> 'evidence_ref_ids')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'strengths')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'improvements')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'upgrade_examples')
                IS DISTINCT FROM 'array'
        THEN
            RETURN false;
        END IF;

        IF status = 'INSUFFICIENT' THEN
            IF criterion ? 'estimated_band'
               OR criterion ? 'band_descriptor'
               OR jsonb_array_length(
                    criterion -> 'evidence_ref_ids'
               ) <> 0
               OR jsonb_array_length(criterion -> 'strengths') <> 0
               OR jsonb_array_length(criterion -> 'improvements') <> 0
               OR jsonb_array_length(
                    criterion -> 'upgrade_examples'
               ) <> 0
            THEN
                RETURN false;
            END IF;
        ELSIF expected_criterion IN ('IELTS_LR', 'IELTS_GRA') THEN
            IF jsonb_typeof(criterion -> 'estimated_band')
                    IS DISTINCT FROM 'number'
               OR (criterion ->> 'estimated_band')::numeric
                    <> trunc((criterion ->> 'estimated_band')::numeric)
               OR (criterion ->> 'estimated_band')::integer
                    NOT BETWEEN 1 AND 9
               OR jsonb_typeof(criterion -> 'band_descriptor')
                    IS DISTINCT FROM 'string'
               OR octet_length(criterion ->> 'band_descriptor') < 1
               OR jsonb_array_length(
                    criterion -> 'evidence_ref_ids'
               ) < 1
            THEN
                RETURN false;
            END IF;
        ELSE
            IF criterion ? 'estimated_band'
               OR criterion ? 'band_descriptor'
               OR expected_criterion = 'IELTS_PR'
            THEN
                RETURN false;
            END IF;
        END IF;
    END LOOP;

    criterion := payload -> 'criteria' -> 3;
    IF criterion ->> 'scoreability_status'
            IS DISTINCT FROM 'INSUFFICIENT'
       OR criterion ->> 'gate_status' IS DISTINCT FROM 'BLOCKED'
       OR jsonb_array_length(criterion -> 'reason_codes') <> 1
       OR NOT (criterion -> 'reason_codes'
            @> '["PRONUNCIATION_ARTIFACT_UNAVAILABLE"]'::jsonb)
    THEN
        RETURN false;
    END IF;

    FOR question_index IN 0..13 LOOP
        question := payload -> 'question_results' -> question_index;
        expected_part := CASE
            WHEN question_index < 8 THEN 'PART_1'
            WHEN question_index = 8 THEN 'PART_2'
            ELSE 'PART_3'
        END;
        IF jsonb_typeof(question) IS DISTINCT FROM 'object'
           OR (question ->> 'index')::integer
                IS DISTINCT FROM question_index + 1
           OR question ->> 'part_id' IS DISTINCT FROM expected_part
           OR (
                question ->> 'opportunity_status'
                    IS DISTINCT FROM 'PROVIDED'
                AND question ->> 'opportunity_status'
                    IS DISTINCT FROM 'NOT_PROVIDED'
           )
           OR (
                question ->> 'opportunity_status' = 'PROVIDED'
                AND question ->> 'assessment_status'
                    IS DISTINCT FROM 'ASSESSED'
           )
           OR (
                question ->> 'opportunity_status' = 'NOT_PROVIDED'
                AND question ->> 'assessment_status'
                    IS DISTINCT FROM 'NOT_ASSESSED'
           )
           OR jsonb_typeof(question -> 'evidence_ref_ids')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(question -> 'criterion_findings')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(
                question -> 'criterion_findings'
           ) <> 4
        THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
EXCEPTION
    WHEN others THEN
        RETURN false;
END;
$$;

CREATE OR REPLACE FUNCTION evaluation_ielts_result_v4_shape_is_valid(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    criterion jsonb;
    question jsonb;
    criterion_index integer;
    question_index integer;
    expected_criterion text;
    expected_part text;
    root_status text;
    status text;
    full_acoustics boolean;
BEGIN
    root_status := payload ->> 'scoreability_status';
    full_acoustics := root_status = 'PROVISIONAL'
        AND payload -> 'criteria' -> 3 ->> 'scoreability_status'
            = 'PROVISIONAL';
    IF jsonb_typeof(payload) IS DISTINCT FROM 'object'
       OR payload ->> 'schema_version'
            IS DISTINCT FROM 'ielts-speaking-full-mock-shadow/v1'
       OR payload ->> 'scene_type' IS DISTINCT FROM 'IELTS_SPEAKING'
       OR payload ->> 'scope' IS DISTINCT FROM 'SESSION'
       OR payload ->> 'channel' IS DISTINCT FROM 'SCENE'
       OR jsonb_typeof(payload -> 'snapshot_id') IS DISTINCT FROM 'string'
       OR root_status NOT IN ('PROVISIONAL', 'INSUFFICIENT')
       OR (root_status = 'PROVISIONAL'
            AND payload ->> 'gate_status' IS DISTINCT FROM 'FEEDBACK_ONLY')
       OR (root_status = 'INSUFFICIENT'
            AND payload ->> 'gate_status' IS DISTINCT FROM 'BLOCKED')
       OR jsonb_typeof(payload -> 'reason_codes') IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'reason_codes') < 1
       OR jsonb_typeof(payload -> 'criteria') IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'criteria') <> 4
       OR jsonb_typeof(payload -> 'question_results') IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'question_results') <> 14
       OR payload ? 'overall'
       OR payload ? 'speaking_overall'
    THEN
        RETURN false;
    END IF;

    FOR criterion_index IN 0..3 LOOP
        criterion := payload -> 'criteria' -> criterion_index;
        expected_criterion := CASE criterion_index
            WHEN 0 THEN 'IELTS_FC'
            WHEN 1 THEN 'IELTS_LR'
            WHEN 2 THEN 'IELTS_GRA'
            ELSE 'IELTS_PR'
        END;
        status := criterion ->> 'scoreability_status';
        IF jsonb_typeof(criterion) IS DISTINCT FROM 'object'
           OR criterion ->> 'criterion_id' IS DISTINCT FROM expected_criterion
           OR status NOT IN ('PROVISIONAL', 'INSUFFICIENT')
           OR (root_status = 'INSUFFICIENT' AND status <> 'INSUFFICIENT')
           OR (root_status = 'PROVISIONAL' AND full_acoustics
                AND status <> 'PROVISIONAL')
           OR (root_status = 'PROVISIONAL' AND NOT full_acoustics
                AND ((expected_criterion = 'IELTS_PR'
                        AND status <> 'INSUFFICIENT')
                    OR (expected_criterion <> 'IELTS_PR'
                        AND status <> 'PROVISIONAL')))
           OR (status = 'PROVISIONAL'
                AND criterion ->> 'gate_status' <> 'FEEDBACK_ONLY')
           OR (status = 'INSUFFICIENT'
                AND criterion ->> 'gate_status' <> 'BLOCKED')
           OR jsonb_typeof(criterion -> 'reason_codes') IS DISTINCT FROM 'array'
           OR jsonb_array_length(criterion -> 'reason_codes') < 1
           OR jsonb_typeof(criterion -> 'evidence_ref_ids') IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'strengths') IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'improvements') IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'upgrade_examples') IS DISTINCT FROM 'array'
        THEN
            RETURN false;
        END IF;

        IF status = 'INSUFFICIENT' THEN
            IF criterion ? 'estimated_band'
               OR criterion ? 'band_descriptor'
               OR jsonb_array_length(criterion -> 'evidence_ref_ids') <> 0
               OR jsonb_array_length(criterion -> 'strengths') <> 0
               OR jsonb_array_length(criterion -> 'improvements') <> 0
               OR jsonb_array_length(criterion -> 'upgrade_examples') <> 0
            THEN
                RETURN false;
            END IF;
        ELSIF full_acoustics
              OR expected_criterion IN ('IELTS_LR', 'IELTS_GRA') THEN
            IF jsonb_typeof(criterion -> 'estimated_band')
                    IS DISTINCT FROM 'number'
               OR (criterion ->> 'estimated_band')::numeric
                    <> trunc((criterion ->> 'estimated_band')::numeric)
               OR (criterion ->> 'estimated_band')::integer NOT BETWEEN 1 AND 9
               OR jsonb_typeof(criterion -> 'band_descriptor')
                    IS DISTINCT FROM 'string'
               OR octet_length(criterion ->> 'band_descriptor') < 1
               OR jsonb_array_length(criterion -> 'evidence_ref_ids') < 1
            THEN
                RETURN false;
            END IF;
        ELSIF expected_criterion = 'IELTS_FC' THEN
            IF criterion ? 'estimated_band'
               OR criterion ? 'band_descriptor'
               OR jsonb_array_length(criterion -> 'evidence_ref_ids') < 1
            THEN
                RETURN false;
            END IF;
        END IF;
    END LOOP;

    FOR question_index IN 0..13 LOOP
        question := payload -> 'question_results' -> question_index;
        expected_part := CASE
            WHEN question_index < 8 THEN 'PART_1'
            WHEN question_index = 8 THEN 'PART_2'
            ELSE 'PART_3'
        END;
        IF jsonb_typeof(question) IS DISTINCT FROM 'object'
           OR (question ->> 'index')::integer <> question_index + 1
           OR question ->> 'part_id' IS DISTINCT FROM expected_part
           OR question ->> 'opportunity_status'
                NOT IN ('PROVIDED', 'NOT_PROVIDED')
           OR jsonb_typeof(question -> 'evidence_ref_ids')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(question -> 'criterion_findings')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(question -> 'criterion_findings') <> 4
        THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
EXCEPTION
    WHEN others THEN
        RETURN false;
END;
$$;

ALTER TABLE evaluation_ielts_speaking_scene_results
    ADD CONSTRAINT evaluation_ielts_scene_results_payload_check
        CHECK (
            (
                evaluation_ielts_result_shape_is_valid(result_payload)
                OR evaluation_ielts_result_v4_shape_is_valid(result_payload)
            )
            AND NOT jsonb_path_exists(
                result_payload,
                '$.** ? (@.type() == "object").keyvalue() ? (
                    @.key like_regex
                    "^(object[-_]?key|signed[-_]?url|audio[-_]?url|url)$"
                    flag "i"
                )'
            )
        );

CREATE FUNCTION coaching_scene_version_payload_is_valid_v1(
    expected_scene_id text,
    prompt_payload jsonb,
    roles_payload jsonb,
    options_payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    prompt_key_count integer;
BEGIN
    IF prompt_payload IS NULL OR jsonb_typeof(prompt_payload) <> 'object' THEN
        RETURN false;
    END IF;

    SELECT count(*)
    INTO prompt_key_count
    FROM jsonb_object_keys(prompt_payload);

    IF prompt_key_count <> 8
       OR NOT prompt_payload ?& ARRAY[
           'public_scene_brief',
           'practice_goal',
           'user_role',
           'ai_role',
           'persona_summary',
           'focus_areas',
           'turn_blueprints',
           'suggested_duration_seconds'
       ]
       OR jsonb_typeof(prompt_payload -> 'public_scene_brief') <> 'string'
       OR jsonb_typeof(prompt_payload -> 'practice_goal') <> 'string'
       OR jsonb_typeof(prompt_payload -> 'user_role') <> 'string'
       OR jsonb_typeof(prompt_payload -> 'ai_role') <> 'string'
       OR jsonb_typeof(prompt_payload -> 'persona_summary') <> 'string'
       OR prompt_payload ->> 'public_scene_brief' = ''
       OR prompt_payload ->> 'public_scene_brief' <>
           btrim(prompt_payload ->> 'public_scene_brief')
       OR prompt_payload ->> 'practice_goal' = ''
       OR prompt_payload ->> 'practice_goal' <>
           btrim(prompt_payload ->> 'practice_goal')
       OR prompt_payload ->> 'user_role' = ''
       OR prompt_payload ->> 'user_role' <>
           btrim(prompt_payload ->> 'user_role')
       OR prompt_payload ->> 'ai_role' = ''
       OR prompt_payload ->> 'ai_role' <>
           btrim(prompt_payload ->> 'ai_role')
       OR prompt_payload ->> 'persona_summary' = ''
       OR prompt_payload ->> 'persona_summary' <>
           btrim(prompt_payload ->> 'persona_summary')
       OR NOT coaching_scene_nonempty_string_array_is_valid_v1(
           prompt_payload -> 'focus_areas'
       )
       OR NOT coaching_scene_nonempty_string_array_is_valid_v1(
           prompt_payload -> 'turn_blueprints'
       )
       OR jsonb_typeof(prompt_payload -> 'suggested_duration_seconds') <>
           'number'
       OR (prompt_payload ->> 'suggested_duration_seconds') !~
           '^[1-9][0-9]{0,8}$' THEN
        RETURN false;
    END IF;

    IF roles_payload IS NULL
       OR jsonb_typeof(roles_payload) <> 'array'
       OR jsonb_array_length(roles_payload) = 0
       OR EXISTS (
           SELECT 1
           FROM jsonb_array_elements(roles_payload) AS role_item(role_payload)
           WHERE jsonb_typeof(role_payload) <> 'object'
       ) THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements(roles_payload) AS role_item(role_payload)
        CROSS JOIN LATERAL (
            SELECT count(*) AS key_count
            FROM jsonb_object_keys(role_payload)
        ) AS role_keys
        WHERE NOT role_payload ?& ARRAY[
                  'role_definition_id',
                  'scene_id',
                  'role_type',
                  'display_name',
                  'responsibilities',
                  'style',
                  'practice_objectives',
                  'display_order'
              ]
           OR role_keys.key_count NOT IN (8, 9)
           OR (
               role_keys.key_count = 9
               AND NOT role_payload ? 'voice_config_ref'
           )
           OR jsonb_typeof(role_payload -> 'role_definition_id') <> 'string'
           OR jsonb_typeof(role_payload -> 'scene_id') <> 'string'
           OR jsonb_typeof(role_payload -> 'role_type') <> 'string'
           OR jsonb_typeof(role_payload -> 'display_name') <> 'string'
           OR jsonb_typeof(role_payload -> 'responsibilities') <> 'string'
           OR jsonb_typeof(role_payload -> 'style') <> 'string'
           OR role_payload ->> 'role_definition_id' !~
               '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
           OR role_payload ->> 'scene_id' <> expected_scene_id
           OR role_payload ->> 'role_type' !~ '^[A-Z][A-Z0-9_]*$'
           OR role_payload ->> 'display_name' = ''
           OR role_payload ->> 'display_name' <>
               btrim(role_payload ->> 'display_name')
           OR role_payload ->> 'responsibilities' = ''
           OR role_payload ->> 'responsibilities' <>
               btrim(role_payload ->> 'responsibilities')
           OR role_payload ->> 'style' = ''
           OR role_payload ->> 'style' <> btrim(role_payload ->> 'style')
           OR NOT coaching_scene_practice_objectives_are_valid_v1(
               role_payload -> 'practice_objectives'
           )
           OR jsonb_typeof(role_payload -> 'display_order') <> 'number'
           OR (role_payload ->> 'display_order') !~ '^[0-9]{1,9}$'
           OR (
               role_payload ? 'voice_config_ref'
               AND (
                   jsonb_typeof(role_payload -> 'voice_config_ref') <> 'string'
                   OR role_payload ->> 'voice_config_ref' = ''
                   OR role_payload ->> 'voice_config_ref' <>
                       btrim(role_payload ->> 'voice_config_ref')
               )
           )
    ) THEN
        RETURN false;
    END IF;

    IF (
        SELECT count(*) <> count(DISTINCT role_payload ->> 'role_definition_id')
        FROM jsonb_array_elements(roles_payload) AS role_item(role_payload)
    ) THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT objective ->> 'objective_id'
        FROM jsonb_array_elements(roles_payload) AS role_item(role_payload)
        CROSS JOIN LATERAL jsonb_array_elements(
            role_payload -> 'practice_objectives'
        ) AS objective_item(objective)
        GROUP BY objective ->> 'objective_id'
        HAVING count(DISTINCT objective ->> 'description') > 1
    ) THEN
        RETURN false;
    END IF;

    IF options_payload IS NULL
       OR jsonb_typeof(options_payload) <> 'array'
       OR jsonb_array_length(options_payload) = 0
       OR EXISTS (
           SELECT 1
           FROM jsonb_array_elements(options_payload) AS option_item(option_payload)
           WHERE jsonb_typeof(option_payload) <> 'object'
       ) THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements(options_payload) AS option_item(option_payload)
        CROSS JOIN LATERAL (
            SELECT count(*) AS key_count
            FROM jsonb_object_keys(option_payload)
        ) AS option_keys
        WHERE NOT option_payload ?& ARRAY[
                  'practice_option_id',
                  'scene_id',
                  'practice_option_type',
                  'display_name',
                  'display_order'
              ]
           OR option_keys.key_count NOT IN (5, 6)
           OR jsonb_typeof(option_payload -> 'practice_option_id') <> 'string'
           OR jsonb_typeof(option_payload -> 'scene_id') <> 'string'
           OR jsonb_typeof(option_payload -> 'practice_option_type') <> 'string'
           OR jsonb_typeof(option_payload -> 'display_name') <> 'string'
           OR option_payload ->> 'practice_option_id' !~
               '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
           OR option_payload ->> 'scene_id' <> expected_scene_id
           OR option_payload ->> 'practice_option_type' NOT IN (
               'FULL_SIMULATION',
               'FOCUS'
           )
           OR option_payload ->> 'display_name' = ''
           OR option_payload ->> 'display_name' <>
               btrim(option_payload ->> 'display_name')
           OR jsonb_typeof(option_payload -> 'display_order') <> 'number'
           OR (option_payload ->> 'display_order') !~ '^[0-9]{1,9}$'
           OR (
               option_payload ->> 'practice_option_type' = 'FULL_SIMULATION'
               AND (
                   option_keys.key_count <> 5
                   OR option_payload ? 'role_definition_id'
               )
           )
           OR (
               option_payload ->> 'practice_option_type' = 'FOCUS'
               AND (
                   option_keys.key_count <> 6
                   OR jsonb_typeof(option_payload -> 'role_definition_id') <>
                       'string'
                   OR NOT EXISTS (
                       SELECT 1
                       FROM jsonb_array_elements(roles_payload)
                           AS role_item(role_payload)
                       WHERE role_payload ->> 'role_definition_id' =
                           option_payload ->> 'role_definition_id'
                   )
               )
           )
    ) THEN
        RETURN false;
    END IF;

    IF (
        SELECT count(*) <> count(DISTINCT option_payload ->> 'practice_option_id')
        FROM jsonb_array_elements(options_payload) AS option_item(option_payload)
    ) THEN
        RETURN false;
    END IF;

    IF (
        SELECT count(*)
        FROM jsonb_array_elements(options_payload) AS option_item(option_payload)
        WHERE option_payload ->> 'practice_option_type' = 'FULL_SIMULATION'
    ) <> 1 THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements(roles_payload) AS role_item(role_payload)
        WHERE (
            SELECT count(*)
            FROM jsonb_array_elements(options_payload)
                AS option_item(option_payload)
            WHERE option_payload ->> 'practice_option_type' = 'FOCUS'
              AND option_payload ->> 'role_definition_id' =
                  role_payload ->> 'role_definition_id'
        ) <> 1
    ) THEN
        RETURN false;
    END IF;

    RETURN true;
END;
$$;

ALTER TABLE coaching_scene_versions
    DISABLE TRIGGER coaching_scene_versions_are_immutable;

ALTER TABLE coaching_scene_versions
    DROP CONSTRAINT coaching_scene_versions_experience_category_check,
    DROP CONSTRAINT coaching_scene_versions_payload_check,
    ALTER COLUMN practice_experience DROP NOT NULL,
    ALTER COLUMN scene_category DROP NOT NULL,
    ADD COLUMN scene_family text,
    ADD COLUMN scene_model text,
    ADD COLUMN turn_policy_ref text COLLATE "C",
    ADD COLUMN session_policy_ref text COLLATE "C",
    ADD COLUMN evaluation_policy_ref text COLLATE "C";

DROP FUNCTION coaching_scene_version_payload_is_valid_v2(
    text,
    text,
    jsonb,
    jsonb,
    jsonb
);

DELETE FROM coaching_scene_versions
WHERE scene_id = 'scn_ielts_speaking';

DELETE FROM coaching_scenes
WHERE scene_id = 'scn_ielts_speaking';

UPDATE coaching_scene_versions AS version
SET scene_family = CASE
        WHEN version.practice_experience = 'INTERVIEW' THEN 'INTERVIEW'
        WHEN version.scene_category = 'ROLEPLAY_WORKPLACE' THEN 'WORKPLACE'
        ELSE 'DAILY'
    END,
    scene_model = CASE version.scene_id
        WHEN 'scn_programmer_interview' THEN
            'PROJECT_EXPERIENCE_DEEP_DIVE'
        WHEN 'scn_workplace_progress_risk_update' THEN
            'PROGRESS_AND_RISK_UPDATE'
        WHEN 'scn_daily_hotel_checkin_issue' THEN
            'HOTEL_CHECKIN_AND_ISSUE_HANDLING'
        ELSE CASE
            WHEN version.practice_experience = 'INTERVIEW' THEN
                'INTERVIEW_BASIC_DIALOGUE'
            WHEN version.scene_category = 'ROLEPLAY_WORKPLACE' THEN
                'WORKPLACE_BASIC_DIALOGUE'
            ELSE 'DAILY_BASIC_DIALOGUE'
        END
    END,
    turn_policy_ref = version.practice_options -> 0 ->> 'turn_policy_ref',
    session_policy_ref =
        version.practice_options -> 0 ->> 'session_policy_ref',
    evaluation_policy_ref =
        version.practice_options -> 0 ->> 'evaluation_policy_ref',
    prompt = jsonb_set(
        version.prompt,
        '{suggested_duration_seconds}',
        version.practice_options -> 0 -> 'suggested_duration_seconds'
    ),
    practice_options = (
        SELECT jsonb_agg(
            (
                practice_option.value - ARRAY[
                    'practice_mode',
                    'suggested_duration_seconds',
                    'turn_policy_ref',
                    'session_policy_ref',
                    'evaluation_policy_ref'
                ]
            ) || jsonb_build_object(
                'practice_option_type',
                practice_option.value -> 'practice_mode'
            )
            ORDER BY
                (practice_option.value ->> 'display_order')::integer,
                practice_option.value ->> 'practice_option_id'
        )
        FROM jsonb_array_elements(version.practice_options)
            AS practice_option(value)
    );

INSERT INTO coaching_scenes (scene_id, owner_user_id)
VALUES
    ('scn_daily_custom', NULL),
    ('scn_ielts_speaking_full', NULL),
    ('scn_ielts_speaking_part_1', NULL),
    ('scn_ielts_speaking_part_2', NULL),
    ('scn_ielts_speaking_part_3', NULL),
    ('scn_interview_custom', NULL),
    ('scn_speaking_exam_custom', NULL),
    ('scn_workplace_custom', NULL);

INSERT INTO coaching_scene_versions (
    scene_id,
    scene_version,
    scene_family,
    scene_model,
    name,
    status,
    turn_policy_ref,
    session_policy_ref,
    evaluation_policy_ref,
    prompt,
    roles,
    practice_options,
    display_order
) VALUES
    (
        'scn_daily_custom',
        1,
        'DAILY',
        'DAILY_BASIC_DIALOGUE',
        '自定义日常交流',
        'active',
        'generic.practice.turn.v1',
        'daily.practice.session.v1',
        'daily.general.evaluation.v1',
        $scene_catalog${"public_scene_brief":"使用默认生活场景，练习其他日常地点、身份和交流目标。","practice_goal":"根据用户补充的信息保持自然日常角色和对话边界。","user_role":"日常沟通者","ai_role":"日常交流对象","persona_summary":"A natural everyday conversation partner who follows the stated place, relationship, and goal without shifting into a job interview or exam.","focus_areas":["place","relationship","goal","confirmation"],"turn_blueprints":["根据用户目标建立日常语境","回应用户刚才的实际表达","澄清一个必要细节","自然完成交流"],"suggested_duration_seconds":420}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_daily_custom_counterpart","scene_id":"scn_daily_custom","role_type":"DAILY_COUNTERPART","display_name":"日常交流对象","responsibilities":"根据用户补充的信息保持自然日常角色和对话边界。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"place","description":"Establish where the conversation is happening and why it matters."},{"objective_id":"relationship","description":"Use language appropriate to the relationship between participants."},{"objective_id":"goal","description":"State the desired outcome of the conversation clearly."},{"objective_id":"confirmation","description":"Restate and confirm the agreed details accurately."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_daily_custom_full","scene_id":"scn_daily_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_custom_focus","scene_id":"scn_daily_custom","role_definition_id":"role_daily_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        110
    ),
    (
        'scn_interview_custom',
        1,
        'INTERVIEW',
        'INTERVIEW_BASIC_DIALOGUE',
        '自定义面试',
        'active',
        'generic.practice.turn.v1',
        'interview.practice.session.v1',
        'interview.shadow.evaluation.v1',
        $scene_catalog${"public_scene_brief":"使用默认的通用岗位背景练习一个暂未被正式子场景覆盖的面试目标。","practice_goal":"保持面试语境，围绕用户补充的一句话目标进行自然问答。","user_role":"候选人","ai_role":"通用面试官","persona_summary":"A professional interviewer who follows the user's stated practice goal without claiming to reproduce a specific company's real questions.","focus_areas":["custom_goal","evidence","clarification"],"turn_blueprints":["根据用户目标提出一个明确问题","回应用户刚才的实际回答","追问一个最相关的证据或细节","围绕目标自然收尾"],"suggested_duration_seconds":600}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_interview_custom_counterpart","scene_id":"scn_interview_custom","role_type":"CUSTOM_INTERVIEWER","display_name":"通用面试官","responsibilities":"保持面试语境，围绕用户补充的一句话目标进行自然问答。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"custom_goal","description":"Keep the conversation aligned with the user’s stated practice goal."},{"objective_id":"evidence","description":"Support claims with concrete evidence."},{"objective_id":"clarification","description":"Ask focused questions to resolve ambiguity."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_interview_custom_full","scene_id":"scn_interview_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_interview_custom_focus","scene_id":"scn_interview_custom","role_definition_id":"role_interview_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        70
    ),
    (
        'scn_speaking_exam_custom',
        1,
        'EXAM',
        'EXAM_BASIC_DIALOGUE',
        '自定义口语考试',
        'active',
        'generic.practice.turn.v1',
        'exam.practice.session.v1',
        'ielts.speaking_practice.evaluation.v1',
        $scene_catalog${"public_scene_brief":"使用默认考试设定，练习老师指定或其他考试形式的口语问题。","practice_goal":"围绕用户补充的考试名称、题型或目标完成自定义练习。","user_role":"考生","ai_role":"口语考官","persona_summary":"A neutral speaking examiner who follows the user's stated format while clearly treating it as custom practice.","focus_areas":["custom_format","clear_answer","supporting_detail"],"turn_blueprints":["根据用户目标提出一个考试式问题","根据回答追问一个相关细节","保持中立考官行为","完成自定义练习收尾"],"suggested_duration_seconds":600}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_speaking_exam_custom_counterpart","scene_id":"scn_speaking_exam_custom","role_type":"CUSTOM_EXAMINER","display_name":"口语考官","responsibilities":"围绕用户补充的考试名称、题型或目标完成自定义练习。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"custom_format","description":"Follow the user-defined response format consistently."},{"objective_id":"clear_answer","description":"Give a direct, well-structured answer to the question."},{"objective_id":"supporting_detail","description":"Add a specific reason, example, or detail that supports the answer."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_speaking_exam_custom_full","scene_id":"scn_speaking_exam_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_speaking_exam_custom_focus","scene_id":"scn_speaking_exam_custom","role_definition_id":"role_speaking_exam_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        50
    ),
    (
        'scn_workplace_custom',
        1,
        'WORKPLACE',
        'WORKPLACE_BASIC_DIALOGUE',
        '自定义职场沟通',
        'active',
        'generic.practice.turn.v1',
        'workplace.practice.session.v1',
        'workplace.general.evaluation.v1',
        $scene_catalog${"public_scene_brief":"使用默认工作关系和业务目标，练习其他真实工作交流。","practice_goal":"根据用户补充的对象、目标和边界保持专业对话。","user_role":"职场沟通者","ai_role":"工作对象","persona_summary":"A professional workplace counterpart who follows the user's stated relationship, business goal, and non-negotiable boundaries.","focus_areas":["relationship","business_goal","boundary","next_step"],"turn_blueprints":["根据用户目标建立工作语境","回应用户刚才的实际表达","澄清一个业务边界","推动形成具体下一步"],"suggested_duration_seconds":600}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_workplace_custom_counterpart","scene_id":"scn_workplace_custom","role_type":"WORKPLACE_COUNTERPART","display_name":"工作对象","responsibilities":"根据用户补充的对象、目标和边界保持专业对话。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"relationship","description":"Use language appropriate to the relationship between participants."},{"objective_id":"business_goal","description":"Clarify the business outcome the conversation should achieve."},{"objective_id":"boundary","description":"State a clear non-negotiable boundary professionally."},{"objective_id":"next_step","description":"Agree on a specific owner, action, and timing for the next step."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_workplace_custom_full","scene_id":"scn_workplace_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_custom_focus","scene_id":"scn_workplace_custom","role_definition_id":"role_workplace_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        80
    ),
    (
        'scn_ielts_speaking_full',
        2,
        'EXAM',
        'IELTS_SPEAKING_FULL_MOCK',
        'IELTS 口语完整模拟',
        'active',
        'ielts.speaking_full_mock.turn.v1',
        'ielts.speaking_full_mock.session.v1',
        'ielts.speaking_full_mock.evaluation.v1',
        $scene_catalog${"public_scene_brief":"按 Part 1、Part 2、Part 3 连续完成一轮 IELTS 口语完整模考。","practice_goal":"适应真实三段式流程，并在不同题型中保持连贯自然的表达。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who follows the frozen three-part mock-test sequence, asks exactly one item at a time, and never teaches or scores during the simulation.","focus_areas":["part_1_familiar_topics","part_2_long_turn","part_3_discussion","section_transition"],"turn_blueprints":["Part 1 question: Where is your hometown?","Part 1 question: Is there anything you do not like about your hometown?","Part 1 question: Would you say it is a good place for young people?","Part 1 question: Do you use artificial intelligence in your daily life?","Part 1 question: Has technology changed the way you learn things?","Part 1 question: Is there any technology you find difficult to use?","Part 1 question: What do you usually do in your free time?","Part 1 question: Do you prefer spending your free time alone or with other people?","Part 2 cue card: Describe a skill you would like to learn.\nYou should say:\n• What the skill is\n• Why you want to learn it\n• How you would learn it\n• And explain how learning this skill would benefit you","Part 3 question: What kinds of skills are most valuable in today's society?","Part 3 question: Some people say it is never too late to learn a new skill. Do you agree?","Part 3 question: Do you think schools should focus more on practical skills?","Part 3 question: How has technology changed the way people learn skills?","Part 3 question: Do you think some skills will become obsolete in the future?"],"suggested_duration_seconds":900}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_ielts_speaking_full_counterpart","scene_id":"scn_ielts_speaking_full","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Run the frozen Part 1, Part 2, and Part 3 sequence without coaching or scoring.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"part_1_familiar_topics","description":"Answer familiar-topic questions directly with relevant detail."},{"objective_id":"part_2_long_turn","description":"Deliver a coherent long turn that covers every cue-card point."},{"objective_id":"part_3_discussion","description":"Develop abstract ideas with reasons, examples, and comparisons."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_ielts_speaking_full_full","scene_id":"scn_ielts_speaking_full","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_speaking_full_focus","scene_id":"scn_ielts_speaking_full","role_definition_id":"role_ielts_speaking_full_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        40
    ),
    (
        'scn_ielts_speaking_part_1',
        1,
        'EXAM',
        'IELTS_SPEAKING_PART_1',
        'IELTS Speaking Part 1',
        'active',
        'ielts.speaking_part1.turn.v1',
        'ielts.speaking_part1.session.v1',
        'ielts.speaking_practice.evaluation.v1',
        $scene_catalog${"public_scene_brief":"连续回答三个熟悉话题中的八道 Part 1 问题。","practice_goal":"在正式 Part 1 节奏中自然、直接并适度展开回答。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who asks the frozen Part 1 questions one at a time without coaching or scoring.","focus_areas":["part_1_familiar_topics","direct_answer","natural_extension"],"turn_blueprints":["Part 1 question 1","Part 1 question 2","Part 1 question 3","Part 1 question 4","Part 1 question 5","Part 1 question 6","Part 1 question 7","Part 1 question 8"],"suggested_duration_seconds":300}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_ielts_part1_examiner","scene_id":"scn_ielts_speaking_part_1","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Ask the frozen familiar-topic questions in order.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"part_1_familiar_topics","description":"Answer familiar-topic questions directly with relevant detail."},{"objective_id":"natural_extension","description":"Extend short answers naturally with relevant reasons or examples."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_ielts_speaking_part_1_full","scene_id":"scn_ielts_speaking_part_1","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_speaking_part_1_focus","scene_id":"scn_ielts_speaking_part_1","role_definition_id":"role_ielts_part1_examiner","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        10
    ),
    (
        'scn_ielts_speaking_part_2',
        1,
        'EXAM',
        'IELTS_SPEAKING_PART_2',
        'IELTS Speaking Part 2',
        'active',
        'ielts.speaking_part2.turn.v1',
        'ielts.speaking_part2.session.v1',
        'ielts.speaking_practice.evaluation.v1',
        $scene_catalog${"public_scene_brief":"根据一张 Cue Card 进行连续表达，并回答考官的必要追问。","practice_goal":"围绕主题清楚展开观点、细节和理由。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who presents one task at a time and does not teach during the simulation.","focus_areas":["cue_card","topic_development","detail_and_reason","fluency_and_extension"],"turn_blueprints":["给出清楚的 Cue Card 并邀请作答","根据回答追问主体内容","追问一个细节或理由","检查表达的流利度和展开程度"],"suggested_duration_seconds":600}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_ielts_examiner","scene_id":"scn_ielts_speaking_part_2","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Present the cue card and ask concise, neutral follow-up questions.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"topic_development","description":"Develop the topic with a clear progression of connected ideas."},{"objective_id":"fluency_and_extension","description":"Speak fluently and extend answers with relevant detail."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_ielts_full_simulation","scene_id":"scn_ielts_speaking_part_2","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_examiner_focus","scene_id":"scn_ielts_speaking_part_2","role_definition_id":"role_ielts_examiner","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        20
    ),
    (
        'scn_ielts_speaking_part_3',
        1,
        'EXAM',
        'IELTS_SPEAKING_PART_3',
        'IELTS Speaking Part 3',
        'active',
        'ielts.speaking_part3.turn.v1',
        'ielts.speaking_part3.session.v1',
        'ielts.speaking_practice.evaluation.v1',
        $scene_catalog${"public_scene_brief":"围绕对应 Part 2 主题完成五道深入讨论题。","practice_goal":"解释观点，并从更一般和抽象的角度分析、讨论和推测。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who keeps every Part 3 question bound to the selected Part 2 topic.","focus_areas":["part_3_discussion","opinion_and_reason","analysis_and_speculation"],"turn_blueprints":["Part 3 question 1","Part 3 question 2","Part 3 question 3","Part 3 question 4","Part 3 question 5"],"suggested_duration_seconds":300}$scene_catalog$::jsonb,
        $scene_catalog$[{"role_definition_id":"role_ielts_part3_examiner","scene_id":"scn_ielts_speaking_part_3","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Discuss only the issues bound to the selected Part 2 topic.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"part_3_discussion","description":"Develop abstract ideas with reasons, examples, and comparisons."},{"objective_id":"analysis_and_speculation","description":"Separate supported analysis from speculation and explain both clearly."}],"display_order":10}]$scene_catalog$::jsonb,
        $scene_catalog$[{"practice_option_id":"option_ielts_speaking_part_3_full","scene_id":"scn_ielts_speaking_part_3","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_speaking_part_3_focus","scene_id":"scn_ielts_speaking_part_3","role_definition_id":"role_ielts_part3_examiner","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb,
        30
    );

ALTER TABLE coaching_scene_versions
    DROP COLUMN practice_experience,
    DROP COLUMN scene_category,
    ALTER COLUMN scene_family SET NOT NULL,
    ALTER COLUMN scene_model SET NOT NULL,
    ALTER COLUMN turn_policy_ref SET NOT NULL,
    ALTER COLUMN session_policy_ref SET NOT NULL,
    ALTER COLUMN evaluation_policy_ref SET NOT NULL,
    ADD CONSTRAINT coaching_scene_versions_family_model_check
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
        ),
    ADD CONSTRAINT coaching_scene_versions_turn_policy_ref_check
        CHECK (
            char_length(turn_policy_ref) BETWEEN 9 AND 128
            AND turn_policy_ref = btrim(turn_policy_ref)
            AND right(turn_policy_ref, 8) = '.turn.v1'
        ),
    ADD CONSTRAINT coaching_scene_versions_session_policy_ref_check
        CHECK (
            char_length(session_policy_ref) BETWEEN 12 AND 128
            AND session_policy_ref = btrim(session_policy_ref)
            AND right(session_policy_ref, 11) = '.session.v1'
        ),
    ADD CONSTRAINT coaching_scene_versions_evaluation_policy_ref_check
        CHECK (
            char_length(evaluation_policy_ref) BETWEEN 15 AND 128
            AND evaluation_policy_ref = btrim(evaluation_policy_ref)
            AND evaluation_policy_ref ~ '^[a-z][a-z0-9._-]{0,127}$'
            AND right(evaluation_policy_ref, 14) = '.evaluation.v1'
        ),
    ADD CONSTRAINT coaching_scene_versions_payload_check
        CHECK (
            coaching_scene_version_payload_is_valid_v1(
                scene_id,
                prompt,
                roles,
                practice_options
            )
        );

ALTER TABLE coaching_scene_versions
    ENABLE TRIGGER coaching_scene_versions_are_immutable;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_context_fields_check,
    DROP CONSTRAINT practice_sessions_experience_mode_check,
    DROP COLUMN practice_mode;

ALTER TABLE practice_sessions
    RENAME COLUMN practice_experience TO scene_family;

ALTER TABLE practice_sessions
    RENAME COLUMN scene_category TO scene_model;

ALTER TABLE practice_sessions
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            btrim(snapshot_id) <> ''
            AND btrim(scene_family) <> ''
            AND btrim(scene_model) <> ''
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

ALTER TABLE practice_session_snapshots
    DROP CONSTRAINT practice_session_snapshots_practice_mode_check;

ALTER TABLE practice_session_snapshots
    RENAME COLUMN practice_mode TO mode;

ALTER TABLE practice_session_snapshots
    ADD CONSTRAINT practice_session_snapshots_mode_check
        CHECK (btrim(mode) <> '');

ALTER TABLE practice_retry_turn_authorizations
    DROP CONSTRAINT practice_retry_turn_authorizations_scene_check,
    DROP COLUMN practice_mode;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN practice_experience TO scene_family;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN scene_category TO scene_model;

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

ALTER TABLE evaluation_formal_reports
    DROP CONSTRAINT evaluation_formal_reports_scene_check,
    DROP CONSTRAINT evaluation_formal_reports_payload_check,
    DROP COLUMN practice_experience,
    DROP COLUMN scene_category,
    DROP COLUMN practice_mode,
    ADD COLUMN scene_model text COLLATE "C" NOT NULL,
    ADD CONSTRAINT evaluation_formal_reports_scene_check
        CHECK (
            scene_type IN (
                'IELTS_SPEAKING',
                'INTERVIEW',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
            AND scene_model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    ADD CONSTRAINT evaluation_formal_reports_payload_check
        CHECK (
            schema_version = 'evaluation-report/v1'
            AND jsonb_typeof(report_payload) = 'object'
            AND report_payload ->> 'schema_version' = schema_version
            AND report_payload ->> 'scene_type' = scene_type
            AND report_payload ->> 'scene_model' = scene_model
            AND report_payload ->> 'scoreability_status' =
                scoreability_status
            AND octet_length(report_payload::text) <= 262144
        );

CREATE OR REPLACE FUNCTION preparation_plan_session_policy_is_valid_v1(
    payload jsonb
)
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
           'early_completion_rule',
           'retry_allowed',
           'question_translation_allowed'
       ]
       OR payload - ARRAY[
           'suggested_duration_seconds',
           'min_effective_turns',
           'max_effective_turns',
           'coverage_checkpoint_turn',
           'max_follow_ups_per_question',
           'early_completion_rule',
           'retry_allowed',
           'question_translation_allowed'
       ] <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'suggested_duration_seconds') <> 'number'
       OR jsonb_typeof(payload -> 'min_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'max_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'coverage_checkpoint_turn') <> 'number'
       OR jsonb_typeof(payload -> 'max_follow_ups_per_question') <> 'number'
       OR jsonb_typeof(payload -> 'retry_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'question_translation_allowed') <> 'boolean'
       OR (payload ->> 'suggested_duration_seconds') !~
           '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'min_effective_turns') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'max_effective_turns') !~ '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'coverage_checkpoint_turn') !~
           '^[1-9][0-9]{0,8}$'
       OR (payload ->> 'max_follow_ups_per_question') !~
           '^[0-9]{1,9}$'
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

CREATE OR REPLACE FUNCTION preparation_plan_ielts_assignment_is_valid_v1(
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

DROP FUNCTION ielts_assignment_is_valid_v1(text, jsonb, jsonb);

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
                   'evaluation_policy_ref', version.evaluation_policy_ref,
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

CREATE OR REPLACE FUNCTION evaluation_general_scene_result_shape_is_valid(
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    dimension jsonb;
    dimension_index integer;
    expected_dimension text;
    scoreability text;
BEGIN
    scoreability := payload ->> 'scoreability_status';
    IF jsonb_typeof(payload) IS DISTINCT FROM 'object'
       OR payload ->> 'schema_version'
            IS DISTINCT FROM 'general-scene-evaluation/v1'
       OR payload ->> 'scene_type' NOT IN (
            'IELTS_SPEAKING',
            'OVERSEAS_DAILY_LIFE',
            'OVERSEAS_WORKPLACE'
       )
       OR jsonb_typeof(payload -> 'scene_model')
            IS DISTINCT FROM 'string'
       OR octet_length(payload ->> 'scene_model') NOT BETWEEN 1 AND 128
       OR payload ->> 'scene_model' <> btrim(payload ->> 'scene_model')
       OR payload ->> 'scope' IS DISTINCT FROM 'SESSION'
       OR payload ->> 'channel' IS DISTINCT FROM 'SCENE'
       OR jsonb_typeof(payload -> 'snapshot_id')
            IS DISTINCT FROM 'string'
       OR octet_length(payload ->> 'snapshot_id') NOT BETWEEN 1 AND 128
       OR payload ->> 'snapshot_id' <> btrim(payload ->> 'snapshot_id')
       OR scoreability NOT IN ('PROVISIONAL', 'INSUFFICIENT')
       OR jsonb_typeof(payload -> 'dimensions') IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'dimensions') <> 4
       OR jsonb_typeof(payload -> 'priority_actions')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'priority_actions') > 3
       OR octet_length(payload::text) > 65536
       OR (
            scoreability = 'PROVISIONAL'
            AND jsonb_typeof(payload -> 'provider_lineage')
                IS DISTINCT FROM 'object'
       )
       OR (
            scoreability = 'INSUFFICIENT'
            AND payload ? 'provider_lineage'
       )
    THEN
        RETURN false;
    END IF;

    FOR dimension_index IN 0..3 LOOP
        expected_dimension := CASE dimension_index
            WHEN 0 THEN 'TASK_ACHIEVEMENT'
            WHEN 1 THEN 'CLARITY_COHERENCE'
            WHEN 2 THEN 'LANGUAGE_CONTROL'
            ELSE 'INTERACTION'
        END;
        dimension := payload -> 'dimensions' -> dimension_index;
        IF jsonb_typeof(dimension) IS DISTINCT FROM 'object'
           OR dimension ->> 'key' IS DISTINCT FROM expected_dimension
           OR dimension ->> 'scale' IS DISTINCT FROM 'PERCENTAGE_100'
           OR jsonb_typeof(dimension -> 'coverage')
                IS DISTINCT FROM 'number'
           OR (dimension ->> 'coverage')::numeric NOT BETWEEN 0 AND 1
           OR jsonb_typeof(dimension -> 'confidence')
                IS DISTINCT FROM 'number'
           OR (dimension ->> 'confidence')::numeric NOT BETWEEN 0 AND 1
           OR jsonb_typeof(dimension -> 'reason_codes')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(dimension -> 'evidence_ref_ids')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(dimension -> 'strengths')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(dimension -> 'strengths') > 3
           OR jsonb_typeof(dimension -> 'improvements')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(dimension -> 'improvements') > 3
           OR jsonb_typeof(dimension -> 'recommended_examples')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(dimension -> 'recommended_examples') > 3
           OR (
                scoreability = 'PROVISIONAL'
                AND (
                    jsonb_typeof(dimension -> 'score')
                        IS DISTINCT FROM 'number'
                    OR (dimension ->> 'score')::numeric
                        NOT BETWEEN 0 AND 100
                    OR jsonb_array_length(
                        dimension -> 'evidence_ref_ids'
                    ) < 1
                    OR jsonb_array_length(dimension -> 'strengths')
                        + jsonb_array_length(dimension -> 'improvements') < 1
                )
           )
           OR (
                scoreability = 'INSUFFICIENT'
                AND (
                    dimension ? 'score'
                    OR jsonb_array_length(
                        dimension -> 'evidence_ref_ids'
                    ) <> 0
                    OR jsonb_array_length(dimension -> 'strengths') <> 0
                    OR jsonb_array_length(dimension -> 'improvements') <> 0
                    OR jsonb_array_length(
                        dimension -> 'recommended_examples'
                    ) <> 0
                )
           )
        THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
EXCEPTION
    WHEN others THEN
        RETURN false;
END;
$$;

CREATE OR REPLACE FUNCTION evaluation_assert_general_scene_result_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    bound_record record;
BEGIN
    SELECT
        run.*,
        snapshot.canonical_payload #>>
            '{practice_context,scene_model}' AS scene_model
      INTO bound_record
      FROM evaluation_module_runs AS run
      JOIN evaluation_outbox AS outbox
        ON outbox.id = run.outbox_id
       AND outbox.evaluation_id = run.evaluation_id
       AND outbox.evaluation_revision_id = run.evaluation_revision_id
       AND outbox.owner_user_id = run.owner_user_id
       AND outbox.channel = run.channel
      JOIN identity_users AS owner
        ON owner.id = run.owner_user_id
      JOIN evaluation_revisions AS revision
        ON revision.evaluation_id = run.evaluation_id
       AND revision.id = run.evaluation_revision_id
       AND revision.owner_user_id = run.owner_user_id
      JOIN evaluation_revision_states AS state
        ON state.evaluation_id = run.evaluation_id
       AND state.revision_id = run.evaluation_revision_id
       AND state.owner_user_id = run.owner_user_id
      JOIN evaluation_evidence_snapshots AS snapshot
        ON snapshot.id = run.input_snapshot_id
       AND snapshot.owner_user_id = run.owner_user_id
     WHERE run.id = NEW.module_run_id
       AND run.run_status = 'RUNNING'
       AND run.scene_type IN (
            'IELTS_SPEAKING',
            'OVERSEAS_DAILY_LIFE',
            'OVERSEAS_WORKPLACE'
       )
       AND run.strategy_ref = 'general-scene-evaluation/v1'
       AND revision.pipeline_version = 'evaluation-pipeline/v1'
       AND outbox.delivery_status = 'PENDING'
       AND outbox.fencing_token = NEW.fencing_token
       AND outbox.lease_expires_at > clock_timestamp()
       AND owner.account_status = 'active'
       AND state.evaluation_status = 'RUNNING'
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_revisions AS later
           WHERE later.evaluation_id = revision.evaluation_id
             AND later.revision > revision.revision
       )
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_deletion_fences AS fence
           WHERE fence.owner_user_id = run.owner_user_id
       )
     FOR SHARE OF run, outbox, owner, revision, state, snapshot;

    IF NOT FOUND
       OR NEW.evaluation_id <> bound_record.evaluation_id
       OR NEW.evaluation_revision_id <>
            bound_record.evaluation_revision_id
       OR NEW.owner_user_id <> bound_record.owner_user_id
       OR NEW.channel <> bound_record.channel
       OR NEW.strategy_ref <> bound_record.strategy_ref
       OR NEW.practice_session_id <> bound_record.practice_session_id
       OR NEW.input_snapshot_id <> bound_record.input_snapshot_id
       OR NEW.input_revision <> bound_record.input_revision
       OR NEW.scene_type <> bound_record.scene_type
       OR NEW.snapshot_hash <> bound_record.snapshot_hash
       OR NEW.full_config_hash <> bound_record.full_config_hash
       OR NEW.prompt_version <> bound_record.prompt_version
       OR NEW.provider <> bound_record.provider
       OR NEW.model <> bound_record.model
       OR NEW.fencing_token <> bound_record.fencing_token
       OR NEW.result_payload ->> 'snapshot_id'
            IS DISTINCT FROM NEW.input_snapshot_id
       OR NEW.result_payload ->> 'scene_type'
            IS DISTINCT FROM NEW.scene_type
       OR NEW.result_payload ->> 'scene_model'
            IS DISTINCT FROM bound_record.scene_model
       OR NOT evaluation_interview_result_refs_are_consistent(
           NEW.input_snapshot_id,
           NEW.result_payload
       )
    THEN
        RAISE EXCEPTION 'invalid general Scene result binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

+
CREATE FUNCTION preparation_plan_ielts_assignment_is_valid_v2(
    scene_selection jsonb,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    expected_mode text;
    required_keys text[];
    part_1_questions integer;
    part_2_questions integer;
    part_3_questions integer;
BEGIN
    expected_mode := CASE scene_selection #>> '{scene,scene_model}'
        WHEN 'IELTS_SPEAKING_FULL_MOCK' THEN 'FULL_MOCK'
        WHEN 'IELTS_SPEAKING_PART_1' THEN 'PART_1'
        WHEN 'IELTS_SPEAKING_PART_2' THEN 'PART_2'
        WHEN 'IELTS_SPEAKING_PART_3' THEN 'PART_3'
        ELSE NULL
    END;

    IF expected_mode IS NULL OR expected_mode = 'FULL_MOCK' THEN
        RETURN preparation_plan_ielts_assignment_is_valid_v1(
            scene_selection,
            payload
        );
    END IF;

    IF scene_selection #>> '{scene,scene_family}' <> 'EXAM'
       OR payload IS NULL
       OR jsonb_typeof(payload) <> 'object' THEN
        RETURN false;
    END IF;

    required_keys := CASE expected_mode
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
       )
       OR payload -> 'turn_blueprints' IS DISTINCT FROM
           scene_selection #> '{scene,prompt,turn_blueprints}'
       OR jsonb_typeof(
           scene_selection #> '{scene,prompt,public_scene_brief}'
       ) IS DISTINCT FROM 'string'
       OR scene_selection #>> '{scene,prompt,public_scene_brief}' = ''
       OR scene_selection #>> '{scene,prompt,public_scene_brief}' <>
           btrim(scene_selection #>> '{scene,prompt,public_scene_brief}') THEN
        RETURN false;
    END IF;

    part_1_questions := (payload ->> 'part_1_questions')::integer;
    part_2_questions := (payload ->> 'part_2_questions')::integer;
    part_3_questions := (payload ->> 'part_3_questions')::integer;

    CASE expected_mode
        WHEN 'PART_1' THEN
            IF jsonb_typeof(payload -> 'part_1_set_id') <> 'string'
               OR payload ->> 'part_1_set_id' !~
                   '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
               OR part_1_questions NOT BETWEEN 1 AND 24
               OR part_2_questions <> 0
               OR part_3_questions <> 0
               OR jsonb_array_length(payload -> 'turn_blueprints') <>
                   part_1_questions THEN
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
               OR part_3_questions NOT BETWEEN 1 AND 6
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
               OR part_3_questions NOT BETWEEN 1 AND 6
               OR jsonb_array_length(payload -> 'turn_blueprints') <>
                   part_3_questions THEN
                RETURN false;
            END IF;
    END CASE;

    RETURN true;
END;
$$;

ALTER TABLE preparation_practice_plan_revisions
    DROP CONSTRAINT preparation_practice_plan_revisions_ielts_check,
    ADD CONSTRAINT preparation_practice_plan_revisions_ielts_check
        CHECK (
            preparation_plan_ielts_assignment_is_valid_v2(
                scene_selection,
                ielts_assignment
            )
        );

COMMIT;
