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
            'Scene Experience and Policy boundary migration requires empty development and test Practice/Evaluation data and no private Scenes. RECREATE THE DEVELOPMENT OR TEST DATABASE, then rerun migrations.'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

CREATE FUNCTION coaching_scene_version_payload_is_valid_v2(
    expected_scene_id text,
    expected_experience text,
    prompt_payload jsonb,
    roles_payload jsonb,
    options_payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    option_payload jsonb;
    role_payload jsonb;
BEGIN
    IF prompt_payload IS NULL
       OR jsonb_typeof(prompt_payload) <> 'object'
       OR NOT prompt_payload ?& ARRAY[
           'public_scene_brief',
           'practice_goal',
           'user_role',
           'ai_role',
           'persona_summary',
           'focus_areas',
           'turn_blueprints'
       ]
       OR prompt_payload - ARRAY[
           'public_scene_brief',
           'practice_goal',
           'user_role',
           'ai_role',
           'persona_summary',
           'focus_areas',
           'turn_blueprints'
       ] <> '{}'::jsonb
       OR EXISTS (
           SELECT 1
           FROM unnest(ARRAY[
               'public_scene_brief',
               'practice_goal',
               'user_role',
               'ai_role',
               'persona_summary'
           ]) AS field(name)
           WHERE jsonb_typeof(prompt_payload -> field.name) <> 'string'
              OR prompt_payload ->> field.name = ''
              OR prompt_payload ->> field.name <>
                  btrim(prompt_payload ->> field.name)
       )
       OR NOT coaching_scene_nonempty_string_array_is_valid_v1(
           prompt_payload -> 'focus_areas'
       )
       OR NOT coaching_scene_nonempty_string_array_is_valid_v1(
           prompt_payload -> 'turn_blueprints'
       ) THEN
        RETURN false;
    END IF;

    IF roles_payload IS NULL
       OR jsonb_typeof(roles_payload) <> 'array'
       OR jsonb_array_length(roles_payload) = 0 THEN
        RETURN false;
    END IF;

    FOR role_payload IN
        SELECT value FROM jsonb_array_elements(roles_payload)
    LOOP
        IF jsonb_typeof(role_payload) <> 'object'
           OR NOT role_payload ?& ARRAY[
               'role_definition_id',
               'scene_id',
               'role_type',
               'display_name',
               'responsibilities',
               'style',
               'practice_objectives',
               'display_order'
           ]
           OR role_payload - ARRAY[
               'role_definition_id',
               'scene_id',
               'role_type',
               'display_name',
               'responsibilities',
               'style',
               'practice_objectives',
               'voice_config_ref',
               'display_order'
           ] <> '{}'::jsonb
           OR role_payload ->> 'role_definition_id' !~
               '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
           OR role_payload ->> 'scene_id' <> expected_scene_id
           OR role_payload ->> 'role_type' !~ '^[A-Z][A-Z0-9_]*$'
           OR jsonb_typeof(role_payload -> 'display_name') <> 'string'
           OR role_payload ->> 'display_name' = ''
           OR role_payload ->> 'display_name' <>
               btrim(role_payload ->> 'display_name')
           OR jsonb_typeof(role_payload -> 'responsibilities') <> 'string'
           OR role_payload ->> 'responsibilities' = ''
           OR role_payload ->> 'responsibilities' <>
               btrim(role_payload ->> 'responsibilities')
           OR jsonb_typeof(role_payload -> 'style') <> 'string'
           OR role_payload ->> 'style' = ''
           OR role_payload ->> 'style' <> btrim(role_payload ->> 'style')
           OR NOT coaching_scene_practice_objectives_are_valid_v1(
               role_payload -> 'practice_objectives'
           )
           OR jsonb_typeof(role_payload -> 'display_order') <> 'number'
           OR role_payload ->> 'display_order' !~ '^[0-9]{1,9}$'
           OR (
               role_payload ? 'voice_config_ref'
               AND (
                   jsonb_typeof(role_payload -> 'voice_config_ref') <>
                       'string'
                   OR role_payload ->> 'voice_config_ref' = ''
                   OR role_payload ->> 'voice_config_ref' <>
                       btrim(role_payload ->> 'voice_config_ref')
               )
           ) THEN
            RETURN false;
        END IF;
    END LOOP;

    IF (
        SELECT count(*) <>
            count(DISTINCT role.role_payload ->> 'role_definition_id')
        FROM jsonb_array_elements(roles_payload) AS role(role_payload)
    ) OR EXISTS (
        SELECT objective_item.objective ->> 'objective_id'
        FROM jsonb_array_elements(roles_payload) AS role(role_payload)
        CROSS JOIN LATERAL jsonb_array_elements(
            role.role_payload -> 'practice_objectives'
        ) AS objective_item(objective)
        GROUP BY objective_item.objective ->> 'objective_id'
        HAVING count(
            DISTINCT objective_item.objective ->> 'description'
        ) > 1
    ) THEN
        RETURN false;
    END IF;

    IF options_payload IS NULL
       OR jsonb_typeof(options_payload) <> 'array'
       OR jsonb_array_length(options_payload) = 0 THEN
        RETURN false;
    END IF;

    FOR option_payload IN
        SELECT value FROM jsonb_array_elements(options_payload)
    LOOP
        IF jsonb_typeof(option_payload) <> 'object'
           OR NOT option_payload ?& ARRAY[
               'practice_option_id',
               'scene_id',
               'practice_mode',
               'display_name',
               'suggested_duration_seconds',
               'turn_policy_ref',
               'session_policy_ref',
               'evaluation_policy_ref',
               'display_order'
           ]
           OR option_payload - ARRAY[
               'practice_option_id',
               'scene_id',
               'role_definition_id',
               'practice_mode',
               'display_name',
               'suggested_duration_seconds',
               'turn_policy_ref',
               'session_policy_ref',
               'evaluation_policy_ref',
               'display_order'
           ] <> '{}'::jsonb
           OR option_payload ->> 'practice_option_id' !~
               '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
           OR option_payload ->> 'scene_id' <> expected_scene_id
           OR option_payload ->> 'practice_mode' NOT IN (
               'FULL_SIMULATION',
               'FOCUS',
               'FULL_MOCK',
               'PART_1',
               'PART_2',
               'PART_3'
           )
           OR jsonb_typeof(option_payload -> 'display_name') <> 'string'
           OR option_payload ->> 'display_name' = ''
           OR option_payload ->> 'display_name' <>
               btrim(option_payload ->> 'display_name')
           OR jsonb_typeof(
               option_payload -> 'suggested_duration_seconds'
           ) <> 'number'
           OR option_payload ->> 'suggested_duration_seconds' !~
               '^[1-9][0-9]{0,8}$'
           OR option_payload ->> 'turn_policy_ref' !~
               '^[a-z][a-z0-9._-]{0,127}[.]turn[.]v1$'
           OR option_payload ->> 'session_policy_ref' !~
               '^[a-z][a-z0-9._-]{0,127}[.]session[.]v1$'
           OR option_payload ->> 'evaluation_policy_ref' !~
               '^[a-z][a-z0-9._-]{0,127}[.]evaluation[.]v1$'
           OR jsonb_typeof(option_payload -> 'display_order') <> 'number'
           OR option_payload ->> 'display_order' !~ '^[0-9]{1,9}$'
           OR (
               option_payload ->> 'practice_mode' = 'FOCUS'
               AND (
                   jsonb_typeof(option_payload -> 'role_definition_id') <>
                       'string'
                   OR NOT EXISTS (
                       SELECT 1
                       FROM jsonb_array_elements(roles_payload)
                           AS role(role_payload)
                       WHERE role.role_payload ->> 'role_definition_id' =
                           option_payload ->> 'role_definition_id'
                   )
               )
           )
           OR (
               option_payload ->> 'practice_mode' <> 'FOCUS'
               AND option_payload ? 'role_definition_id'
           ) THEN
            RETURN false;
        END IF;
    END LOOP;

    IF (
        SELECT count(*) <>
            count(
                DISTINCT practice_option.option_payload ->>
                    'practice_option_id'
            )
        FROM jsonb_array_elements(options_payload)
            AS practice_option(option_payload)
    ) THEN
        RETURN false;
    END IF;

    IF expected_experience = 'IELTS_SPEAKING' THEN
        RETURN jsonb_array_length(options_payload) = 4
           AND NOT EXISTS (
               SELECT required.mode
               FROM unnest(ARRAY[
                   'FULL_MOCK',
                   'PART_1',
                   'PART_2',
                   'PART_3'
               ]) AS required(mode)
               WHERE (
                   SELECT count(*)
                   FROM jsonb_array_elements(options_payload)
                       AS practice_option(option_payload)
                   WHERE practice_option.option_payload ->>
                       'practice_mode' = required.mode
               ) <> 1
           );
    END IF;

    IF (
        SELECT count(*)
        FROM jsonb_array_elements(options_payload)
            AS practice_option(option_payload)
        WHERE practice_option.option_payload ->> 'practice_mode' =
            'FULL_SIMULATION'
    ) <> 1 OR jsonb_array_length(options_payload) <>
        jsonb_array_length(roles_payload) + 1 THEN
        RETURN false;
    END IF;

    RETURN NOT EXISTS (
        SELECT 1
        FROM jsonb_array_elements(roles_payload) AS role(role_payload)
        WHERE (
            SELECT count(*)
            FROM jsonb_array_elements(options_payload)
                AS practice_option(option_payload)
            WHERE practice_option.option_payload ->> 'practice_mode' =
                    'FOCUS'
              AND practice_option.option_payload ->>
                    'role_definition_id' =
                  role.role_payload ->> 'role_definition_id'
        ) <> 1
    );
END;
$$;

ALTER TABLE coaching_scene_versions
    DISABLE TRIGGER coaching_scene_versions_are_immutable;

ALTER TABLE coaching_scene_versions
    DROP CONSTRAINT coaching_scene_versions_family_model_check,
    DROP CONSTRAINT coaching_scene_versions_turn_policy_ref_check,
    DROP CONSTRAINT coaching_scene_versions_session_policy_ref_check,
    DROP CONSTRAINT coaching_scene_versions_evaluation_policy_ref_check,
    DROP CONSTRAINT coaching_scene_versions_payload_check,
    ADD COLUMN practice_experience text,
    ADD COLUMN scene_category text;

DROP FUNCTION coaching_scene_version_payload_is_valid_v1(
    text,
    jsonb,
    jsonb,
    jsonb
);

UPDATE coaching_scene_versions AS version
SET practice_options = (
        SELECT jsonb_agg(
            (practice_option.value - 'practice_option_type') ||
            jsonb_build_object(
                'practice_mode',
                practice_option.value -> 'practice_option_type',
                'suggested_duration_seconds',
                version.prompt -> 'suggested_duration_seconds',
                'turn_policy_ref',
                version.turn_policy_ref,
                'session_policy_ref',
                version.session_policy_ref,
                'evaluation_policy_ref',
                version.evaluation_policy_ref
            )
            ORDER BY
                (practice_option.value ->> 'display_order')::integer,
                practice_option.value ->> 'practice_option_id'
        )
        FROM jsonb_array_elements(version.practice_options)
            AS practice_option(value)
    ),
    prompt = version.prompt - 'suggested_duration_seconds',
    practice_experience = CASE version.scene_family
        WHEN 'INTERVIEW' THEN 'INTERVIEW'
        WHEN 'EXAM' THEN 'IELTS_SPEAKING'
        ELSE 'ROLEPLAY'
    END,
    scene_category = CASE
        WHEN version.scene_id IN (
            'scn_interview_recruiter_screening',
            'scn_interview_self_introduction'
        ) THEN 'INTERVIEW_RECRUITER'
        WHEN version.scene_id = 'scn_interview_behavioral' THEN
            'INTERVIEW_BEHAVIORAL'
        WHEN version.scene_id IN (
            'scn_programmer_interview',
            'scn_interview_system_design_spoken'
        ) THEN 'INTERVIEW_PROFESSIONAL'
        WHEN version.scene_id = 'scn_interview_hiring_manager' THEN
            'INTERVIEW_HIRING_MANAGER'
        WHEN version.scene_id LIKE 'scn_ielts_speaking_%' THEN
            'IELTS_SPEAKING'
        WHEN version.scene_id LIKE 'scn_workplace_%' THEN
            'ROLEPLAY_WORKPLACE'
        WHEN version.scene_id IN (
            'scn_daily_airport_transport',
            'scn_daily_hotel_checkin_issue'
        ) THEN 'ROLEPLAY_TRAVEL'
        WHEN version.scene_id LIKE 'scn_daily_%' THEN 'ROLEPLAY_DAILY'
        ELSE NULL
    END;

DELETE FROM coaching_scene_versions
WHERE scene_id IN (
    'scn_daily_custom',
    'scn_interview_custom',
    'scn_speaking_exam_custom',
    'scn_workplace_custom',
    'scn_ielts_speaking_full',
    'scn_ielts_speaking_part_1',
    'scn_ielts_speaking_part_2',
    'scn_ielts_speaking_part_3'
);

DELETE FROM coaching_scenes
WHERE scene_id IN (
    'scn_daily_custom',
    'scn_interview_custom',
    'scn_speaking_exam_custom',
    'scn_workplace_custom',
    'scn_ielts_speaking_full',
    'scn_ielts_speaking_part_1',
    'scn_ielts_speaking_part_2',
    'scn_ielts_speaking_part_3'
);

ALTER TABLE coaching_scene_versions
    DROP COLUMN scene_family,
    DROP COLUMN scene_model,
    DROP COLUMN turn_policy_ref,
    DROP COLUMN session_policy_ref,
    DROP COLUMN evaluation_policy_ref,
    ALTER COLUMN practice_experience SET NOT NULL,
    ALTER COLUMN scene_category SET NOT NULL,
    ADD CONSTRAINT coaching_scene_versions_experience_category_check
        CHECK (
            (
                practice_experience = 'INTERVIEW'
                AND scene_category IN (
                    'INTERVIEW_RECRUITER',
                    'INTERVIEW_BEHAVIORAL',
                    'INTERVIEW_PROFESSIONAL',
                    'INTERVIEW_HIRING_MANAGER',
                    'INTERVIEW_CUSTOM'
                )
            )
            OR (
                practice_experience = 'IELTS_SPEAKING'
                AND scene_category = 'IELTS_SPEAKING'
            )
            OR (
                practice_experience = 'ROLEPLAY'
                AND scene_category IN (
                    'ROLEPLAY_WORKPLACE',
                    'ROLEPLAY_TRAVEL',
                    'ROLEPLAY_DAILY',
                    'ROLEPLAY_CUSTOM'
                )
            )
        ),
    ADD CONSTRAINT coaching_scene_versions_payload_check
        CHECK (
            coaching_scene_version_payload_is_valid_v2(
                scene_id,
                practice_experience,
                prompt,
                roles,
                practice_options
            )
        );

INSERT INTO coaching_scenes (scene_id, owner_user_id)
VALUES ('scn_ielts_speaking', NULL);

INSERT INTO coaching_scene_versions (
    scene_id,
    scene_version,
    practice_experience,
    scene_category,
    name,
    status,
    prompt,
    roles,
    practice_options,
    display_order
) VALUES (
    'scn_ielts_speaking',
    1,
    'IELTS_SPEAKING',
    'IELTS_SPEAKING',
    'IELTS 口语',
    'active',
    $scene_catalog${"public_scene_brief":"选择 Part 1、Part 2、Part 3 或完整模考，并从当前题库冻结本次练习题目。","practice_goal":"按所选 IELTS 口语模式完成真实节奏的连续表达。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who follows the frozen question sequence, asks exactly one item at a time, and never teaches or scores during the simulation.","focus_areas":["fluency_and_coherence","lexical_resource","grammatical_range_and_accuracy","pronunciation"],"turn_blueprints":["Freeze the selected published IELTS Speaking question set before practice starts."]}$scene_catalog$::jsonb,
    $scene_catalog$[{"role_definition_id":"role_ielts_speaking_examiner","scene_id":"scn_ielts_speaking","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Run the frozen IELTS Speaking question sequence without coaching or scoring.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"fluency_and_coherence","description":"Develop answers fluently with a clear and coherent progression."},{"objective_id":"lexical_resource","description":"Use vocabulary precisely and flexibly for the selected topic."},{"objective_id":"grammatical_range_and_accuracy","description":"Use a useful range of grammatical structures accurately."},{"objective_id":"pronunciation","description":"Speak clearly with intelligible rhythm, stress, and sounds."}],"display_order":10}]$scene_catalog$::jsonb,
    $scene_catalog$[{"practice_option_id":"option_ielts_speaking_full_mock","scene_id":"scn_ielts_speaking","practice_mode":"FULL_MOCK","display_name":"完整模考","suggested_duration_seconds":900,"turn_policy_ref":"ielts.speaking_full_mock.turn.v1","session_policy_ref":"ielts.speaking_full_mock.session.v1","evaluation_policy_ref":"ielts.speaking_full_mock.evaluation.v1","display_order":10},{"practice_option_id":"option_ielts_speaking_part_1","scene_id":"scn_ielts_speaking","practice_mode":"PART_1","display_name":"Part 1","suggested_duration_seconds":300,"turn_policy_ref":"ielts.speaking_part1.turn.v1","session_policy_ref":"ielts.speaking_part1.session.v1","evaluation_policy_ref":"ielts.speaking_practice.evaluation.v1","display_order":20},{"practice_option_id":"option_ielts_speaking_part_2","scene_id":"scn_ielts_speaking","practice_mode":"PART_2","display_name":"Part 2","suggested_duration_seconds":600,"turn_policy_ref":"ielts.speaking_part2.turn.v1","session_policy_ref":"ielts.speaking_part2.session.v1","evaluation_policy_ref":"ielts.speaking_practice.evaluation.v1","display_order":30},{"practice_option_id":"option_ielts_speaking_part_3","scene_id":"scn_ielts_speaking","practice_mode":"PART_3","display_name":"Part 3","suggested_duration_seconds":300,"turn_policy_ref":"ielts.speaking_part3.turn.v1","session_policy_ref":"ielts.speaking_part3.session.v1","evaluation_policy_ref":"ielts.speaking_practice.evaluation.v1","display_order":40}]$scene_catalog$::jsonb,
    10
);

ALTER TABLE coaching_scene_versions
    ENABLE TRIGGER coaching_scene_versions_are_immutable;

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
           'question_translation_allowed',
           'question_tips_allowed',
           'avatar_allowed',
           'speech_feedback_allowed'
       ]
       OR payload - ARRAY[
           'suggested_duration_seconds',
           'min_effective_turns',
           'max_effective_turns',
           'coverage_checkpoint_turn',
           'max_follow_ups_per_question',
           'early_completion_rule',
           'retry_allowed',
           'question_translation_allowed',
           'question_tips_allowed',
           'avatar_allowed',
           'speech_feedback_allowed'
       ] <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'suggested_duration_seconds') <> 'number'
       OR jsonb_typeof(payload -> 'min_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'max_effective_turns') <> 'number'
       OR jsonb_typeof(payload -> 'coverage_checkpoint_turn') <> 'number'
       OR jsonb_typeof(payload -> 'max_follow_ups_per_question') <> 'number'
       OR jsonb_typeof(payload -> 'retry_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'question_translation_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'question_tips_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'avatar_allowed') <> 'boolean'
       OR jsonb_typeof(payload -> 'speech_feedback_allowed') <> 'boolean'
       OR payload ->> 'suggested_duration_seconds' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'min_effective_turns' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'max_effective_turns' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'coverage_checkpoint_turn' !~ '^[1-9][0-9]{0,8}$'
       OR payload ->> 'max_follow_ups_per_question' !~ '^[0-9]{1,9}$'
       OR (payload ->> 'min_effective_turns')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR (payload ->> 'coverage_checkpoint_turn')::integer >
           (payload ->> 'max_effective_turns')::integer
       OR (payload ->> 'max_effective_turns')::integer > 64
       OR jsonb_typeof(payload -> 'early_completion_rule') <> 'string'
       OR payload ->> 'early_completion_rule' <>
           'COVERAGE_SATISFIED_AFTER_CHECKPOINT' THEN
        RETURN false;
    END IF;
    RETURN true;
END;
$$;

CREATE FUNCTION ielts_assignment_is_valid_v1(
    expected_mode text,
    expected_blueprints jsonb,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    expected_parts text[];
    part_payload jsonb;
    part_index integer;
    expected_part text;
    required_part_keys text[];
    concatenated_blueprints jsonb := '[]'::jsonb;
    part_2_source_id text;
    part_2_topic_title text;
BEGIN
    IF expected_mode IS NULL
       OR expected_mode NOT IN ('FULL_MOCK', 'PART_1', 'PART_2', 'PART_3')
       OR jsonb_typeof(expected_blueprints) IS DISTINCT FROM 'array' THEN
        RETURN false;
    END IF;
    IF payload IS NULL OR jsonb_typeof(payload) <> 'object' THEN
        RETURN false;
    END IF;

    expected_parts := CASE expected_mode
        WHEN 'FULL_MOCK' THEN ARRAY['PART_1', 'PART_2', 'PART_3']
        WHEN 'PART_1' THEN ARRAY['PART_1']
        WHEN 'PART_2' THEN ARRAY['PART_2', 'PART_3']
        WHEN 'PART_3' THEN ARRAY['PART_3']
    END;

    IF NOT payload ?& ARRAY['bank_id', 'season', 'mode', 'parts']
       OR payload - ARRAY['bank_id', 'season', 'mode', 'parts'] <>
           '{}'::jsonb
       OR payload ->> 'bank_id' !~
           '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
       OR jsonb_typeof(payload -> 'season') <> 'string'
       OR payload ->> 'season' = ''
       OR payload ->> 'season' <> btrim(payload ->> 'season')
       OR payload ->> 'mode' <> expected_mode
       OR jsonb_typeof(payload -> 'parts') <> 'array'
       OR jsonb_array_length(payload -> 'parts') <>
           cardinality(expected_parts) THEN
        RETURN false;
    END IF;

    FOR part_payload, part_index IN
        SELECT part.value, part.ordinality::integer
        FROM jsonb_array_elements(payload -> 'parts')
            WITH ORDINALITY AS part(value, ordinality)
        ORDER BY part.ordinality
    LOOP
        expected_part := expected_parts[part_index];
        required_part_keys := CASE expected_part
            WHEN 'PART_1' THEN
                ARRAY['part', 'source_id', 'turn_blueprints']
            WHEN 'PART_2' THEN
                ARRAY[
                    'part',
                    'source_id',
                    'topic_title',
                    'cue_card',
                    'turn_blueprints'
                ]
            WHEN 'PART_3' THEN
                ARRAY[
                    'part',
                    'source_id',
                    'topic_title',
                    'turn_blueprints'
                ]
        END;

        IF jsonb_typeof(part_payload) <> 'object'
           OR NOT part_payload ?& required_part_keys
           OR part_payload - required_part_keys <> '{}'::jsonb
           OR part_payload ->> 'part' <> expected_part
           OR part_payload ->> 'source_id' !~
               '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
           OR NOT coaching_scene_nonempty_string_array_is_valid_v1(
               part_payload -> 'turn_blueprints'
           ) THEN
            RETURN false;
        END IF;

        IF expected_part IN ('PART_2', 'PART_3')
           AND (
               jsonb_typeof(part_payload -> 'topic_title') <> 'string'
               OR part_payload ->> 'topic_title' = ''
               OR part_payload ->> 'topic_title' <>
                   btrim(part_payload ->> 'topic_title')
           ) THEN
            RETURN false;
        END IF;
        IF expected_part = 'PART_2'
           AND (
               jsonb_typeof(part_payload -> 'cue_card') <> 'string'
               OR part_payload ->> 'cue_card' = ''
               OR part_payload ->> 'cue_card' <>
                   btrim(part_payload ->> 'cue_card')
               OR jsonb_array_length(
                   part_payload -> 'turn_blueprints'
               ) <> 1
           ) THEN
            RETURN false;
        END IF;

        IF expected_part = 'PART_2' THEN
            part_2_source_id := part_payload ->> 'source_id';
            part_2_topic_title := part_payload ->> 'topic_title';
        ELSIF expected_part = 'PART_3' AND part_2_source_id IS NOT NULL
              AND (
                  part_payload ->> 'source_id' <> part_2_source_id
                  OR part_payload ->> 'topic_title' <>
                      part_2_topic_title
              ) THEN
            RETURN false;
        END IF;

        concatenated_blueprints := concatenated_blueprints ||
            (part_payload -> 'turn_blueprints');
    END LOOP;

    RETURN jsonb_array_length(concatenated_blueprints) BETWEEN 1 AND 64
       AND concatenated_blueprints IS NOT DISTINCT FROM
           expected_blueprints;
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
    expected_mode text;
    option_payload jsonb;
BEGIN
    SELECT practice_option.value
    INTO option_payload
    FROM jsonb_array_elements(
        scene_selection #> '{scene,practice_options}'
    ) AS practice_option(value)
    WHERE practice_option.value ->> 'practice_option_id' =
        scene_selection ->> 'practice_option_id';

    IF scene_selection #>> '{scene,practice_experience}' =
           'IELTS_SPEAKING'
       AND scene_selection #>> '{scene,scene_category}' =
           'IELTS_SPEAKING' THEN
        expected_mode := option_payload ->> 'practice_mode';
    END IF;

    IF expected_mode IS NULL
       OR expected_mode NOT IN ('FULL_MOCK', 'PART_1', 'PART_2', 'PART_3') THEN
        RETURN payload IS NULL;
    END IF;
    RETURN ielts_assignment_is_valid_v1(
        expected_mode,
        scene_selection #> '{scene,prompt,turn_blueprints}',
        payload
    );
END;
$$;

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
    assignment_turn_blueprints jsonb;
    assignment_turn_count integer;
BEGIN
    IF jsonb_typeof(NEW.scene_selection) <> 'object'
       OR NOT NEW.scene_selection ?& ARRAY[
           'scene',
           'selected_role_ids',
           'practice_option_id'
       ]
       OR NEW.scene_selection - ARRAY[
           'scene',
           'selected_role_ids',
           'practice_option_id'
       ] <> '{}'::jsonb
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

    IF NOT preparation_plan_ielts_assignment_is_valid_v1(
        NEW.scene_selection,
        NEW.ielts_assignment
    ) THEN
        RAISE EXCEPTION
            'Practice Plan revision contains an invalid IELTS Part assignment'
            USING
                ERRCODE = '23514',
                CONSTRAINT =
                    'preparation_practice_plan_revisions_ielts_check';
    END IF;

    IF NEW.ielts_assignment IS NOT NULL THEN
        SELECT jsonb_agg(
                   blueprint.value
                   ORDER BY part.ordinality, blueprint.ordinality
               )
        INTO assignment_turn_blueprints
        FROM jsonb_array_elements(NEW.ielts_assignment -> 'parts')
            WITH ORDINALITY AS part(value, ordinality)
        CROSS JOIN LATERAL jsonb_array_elements(
            part.value -> 'turn_blueprints'
        ) WITH ORDINALITY AS blueprint(value, ordinality);
        assignment_turn_count :=
            jsonb_array_length(assignment_turn_blueprints);

        IF (NEW.session_policy ->> 'min_effective_turns')::integer <>
               assignment_turn_count
           OR (NEW.session_policy ->> 'max_effective_turns')::integer <>
               assignment_turn_count
           OR (
               NEW.session_policy ->> 'coverage_checkpoint_turn'
           )::integer <> assignment_turn_count THEN
            RAISE EXCEPTION
                'IELTS Session policy must match the frozen Part turn sequence'
                USING
                    ERRCODE = '23514',
                    CONSTRAINT =
                        'preparation_practice_plan_revisions_ielts_check';
        END IF;
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
                   'practice_experience', version.practice_experience,
                   'scene_category', version.scene_category,
                   'name', version.name,
                   'scene_version', version.scene_version,
                   'status', version.status,
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
                    assignment_turn_blueprints
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
           NEW.scene_version
       OR NEW.scene_selection -> 'scene' <> expected_scene THEN
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
           option_payload ->> 'practice_mode' = 'FOCUS'
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
    DROP CONSTRAINT practice_sessions_context_fields_check,
    DROP CONSTRAINT practice_sessions_scene_model_check;

ALTER TABLE practice_sessions
    RENAME COLUMN scene_family TO practice_experience;

ALTER TABLE practice_sessions
    RENAME COLUMN scene_model TO scene_category;

ALTER TABLE practice_sessions
    ADD COLUMN practice_mode text NOT NULL,
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            btrim(snapshot_id) <> ''
            AND practice_experience IN (
                'INTERVIEW',
                'IELTS_SPEAKING',
                'ROLEPLAY'
            )
            AND scene_category IN (
                'INTERVIEW_RECRUITER',
                'INTERVIEW_BEHAVIORAL',
                'INTERVIEW_PROFESSIONAL',
                'INTERVIEW_HIRING_MANAGER',
                'INTERVIEW_CUSTOM',
                'IELTS_SPEAKING',
                'ROLEPLAY_WORKPLACE',
                'ROLEPLAY_TRAVEL',
                'ROLEPLAY_DAILY',
                'ROLEPLAY_CUSTOM'
            )
            AND practice_mode IN (
                'FULL_SIMULATION',
                'FOCUS',
                'FULL_MOCK',
                'PART_1',
                'PART_2',
                'PART_3'
            )
        ),
    ADD CONSTRAINT practice_sessions_experience_mode_check
        CHECK (
            (
                practice_experience = 'INTERVIEW'
                AND scene_category LIKE 'INTERVIEW_%'
                AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
            )
            OR (
                practice_experience = 'IELTS_SPEAKING'
                AND scene_category = 'IELTS_SPEAKING'
                AND practice_mode IN (
                    'FULL_MOCK',
                    'PART_1',
                    'PART_2',
                    'PART_3'
                )
            )
            OR (
                practice_experience = 'ROLEPLAY'
                AND scene_category LIKE 'ROLEPLAY_%'
                AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
            )
        );

ALTER TABLE practice_session_snapshots
    DROP CONSTRAINT practice_session_snapshots_mode_check;

ALTER TABLE practice_session_snapshots
    RENAME COLUMN mode TO practice_mode;

ALTER TABLE practice_session_snapshots
    ADD CONSTRAINT practice_session_snapshots_practice_mode_check
        CHECK (
            practice_mode IN (
                'FULL_SIMULATION',
                'FOCUS',
                'FULL_MOCK',
                'PART_1',
                'PART_2',
                'PART_3'
            )
        );

ALTER TABLE practice_retry_turn_authorizations
    DROP CONSTRAINT practice_retry_turn_authorizations_scene_check;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN scene_family TO practice_experience;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN scene_model TO scene_category;

ALTER TABLE practice_retry_turn_authorizations
    ADD COLUMN practice_mode text NOT NULL,
    ADD CONSTRAINT practice_retry_turn_authorizations_scene_check
        CHECK (
            practice_experience = 'ROLEPLAY'
            AND scene_category IN (
                'ROLEPLAY_WORKPLACE',
                'ROLEPLAY_TRAVEL',
                'ROLEPLAY_DAILY',
                'ROLEPLAY_CUSTOM'
            )
            AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
        );

ALTER TABLE evaluation_formal_reports
    DROP CONSTRAINT evaluation_formal_reports_scene_check,
    DROP CONSTRAINT evaluation_formal_reports_payload_check,
    DROP COLUMN scene_model,
    ADD COLUMN practice_experience text COLLATE "C" NOT NULL,
    ADD COLUMN scene_category text COLLATE "C" NOT NULL,
    ADD COLUMN practice_mode text COLLATE "C" NOT NULL,
    ADD CONSTRAINT evaluation_formal_reports_scene_check
        CHECK (
            scene_type IN (
                'IELTS_SPEAKING',
                'INTERVIEW',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
            AND (
                (
                    practice_experience = 'INTERVIEW'
                    AND scene_type = 'INTERVIEW'
                    AND scene_category LIKE 'INTERVIEW_%'
                    AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
                )
                OR (
                    practice_experience = 'IELTS_SPEAKING'
                    AND scene_type = 'IELTS_SPEAKING'
                    AND scene_category = 'IELTS_SPEAKING'
                    AND practice_mode IN (
                        'FULL_MOCK',
                        'PART_1',
                        'PART_2',
                        'PART_3'
                    )
                )
                OR (
                    practice_experience = 'ROLEPLAY'
                    AND scene_type = 'OVERSEAS_WORKPLACE'
                    AND scene_category = 'ROLEPLAY_WORKPLACE'
                    AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
                )
                OR (
                    practice_experience = 'ROLEPLAY'
                    AND scene_type = 'OVERSEAS_DAILY_LIFE'
                    AND scene_category IN (
                        'ROLEPLAY_TRAVEL',
                        'ROLEPLAY_DAILY',
                        'ROLEPLAY_CUSTOM'
                    )
                    AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
                )
            )
        ),
    ADD CONSTRAINT evaluation_formal_reports_payload_check
        CHECK (
            schema_version = 'evaluation-report/v1'
            AND jsonb_typeof(report_payload) = 'object'
            AND report_payload ->> 'schema_version' = schema_version
            AND report_payload ->> 'scene_type' = scene_type
            AND report_payload ->> 'practice_experience' =
                practice_experience
            AND report_payload ->> 'scene_category' = scene_category
            AND report_payload ->> 'practice_mode' = practice_mode
            AND report_payload ->> 'scoreability_status' =
                scoreability_status
            AND octet_length(report_payload::text) <= 262144
        );

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
    scene_type text;
    practice_experience text;
    scene_category text;
    practice_mode text;
BEGIN
    scoreability := payload ->> 'scoreability_status';
    scene_type := payload ->> 'scene_type';
    practice_experience := payload ->> 'practice_experience';
    scene_category := payload ->> 'scene_category';
    practice_mode := payload ->> 'practice_mode';
    IF jsonb_typeof(payload) IS DISTINCT FROM 'object'
       OR payload ->> 'schema_version'
            IS DISTINCT FROM 'general-scene-evaluation/v1'
       OR scene_type NOT IN (
            'IELTS_SPEAKING',
            'OVERSEAS_DAILY_LIFE',
            'OVERSEAS_WORKPLACE'
       )
       OR NOT (
            (
                scene_type = 'IELTS_SPEAKING'
                AND practice_experience = 'IELTS_SPEAKING'
                AND scene_category = 'IELTS_SPEAKING'
                AND practice_mode IN ('PART_1', 'PART_2', 'PART_3')
            )
            OR (
                scene_type = 'OVERSEAS_WORKPLACE'
                AND practice_experience = 'ROLEPLAY'
                AND scene_category = 'ROLEPLAY_WORKPLACE'
                AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
            )
            OR (
                scene_type = 'OVERSEAS_DAILY_LIFE'
                AND practice_experience = 'ROLEPLAY'
                AND scene_category IN (
                    'ROLEPLAY_TRAVEL',
                    'ROLEPLAY_DAILY',
                    'ROLEPLAY_CUSTOM'
                )
                AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
            )
       )
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
       ) THEN
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
           ) THEN
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
            '{practice_context,practice_experience}' AS practice_experience,
        snapshot.canonical_payload #>>
            '{practice_context,scene_category}' AS scene_category,
        snapshot.canonical_payload #>>
            '{practice_context,practice_mode}' AS practice_mode
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
       OR NEW.result_payload ->> 'practice_experience'
            IS DISTINCT FROM bound_record.practice_experience
       OR NEW.result_payload ->> 'scene_category'
            IS DISTINCT FROM bound_record.scene_category
       OR NEW.result_payload ->> 'practice_mode'
            IS DISTINCT FROM bound_record.practice_mode
       OR NOT evaluation_interview_result_refs_are_consistent(
           NEW.input_snapshot_id,
           NEW.result_payload
       ) THEN
        RAISE EXCEPTION 'invalid general Scene result binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

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
    current_part_rank integer;
    previous_part_rank integer := 0;
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
       OR jsonb_array_length(payload -> 'question_results')
            NOT BETWEEN 1 AND 64
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

    FOR question_index IN 0..jsonb_array_length(
        payload -> 'question_results'
    ) - 1 LOOP
        question := payload -> 'question_results' -> question_index;
        expected_part := question ->> 'part_id';
        current_part_rank := CASE expected_part
            WHEN 'PART_1' THEN 1
            WHEN 'PART_2' THEN 2
            WHEN 'PART_3' THEN 3
            ELSE 0
        END;
        IF current_part_rank = 0
           OR current_part_rank < previous_part_rank
           OR current_part_rank > previous_part_rank + 1 THEN
            RETURN false;
        END IF;
        previous_part_rank := current_part_rank;
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
    current_part_rank integer;
    previous_part_rank integer := 0;
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
       OR jsonb_array_length(payload -> 'question_results')
            NOT BETWEEN 1 AND 64
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

    FOR question_index IN 0..jsonb_array_length(
        payload -> 'question_results'
    ) - 1 LOOP
        question := payload -> 'question_results' -> question_index;
        expected_part := question ->> 'part_id';
        current_part_rank := CASE expected_part
            WHEN 'PART_1' THEN 1
            WHEN 'PART_2' THEN 2
            WHEN 'PART_3' THEN 3
            ELSE 0
        END;
        IF current_part_rank = 0
           OR current_part_rank < previous_part_rank
           OR current_part_rank > previous_part_rank + 1 THEN
            RETURN false;
        END IF;
        previous_part_rank := current_part_rank;
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

CREATE FUNCTION evaluation_assert_ielts_part_result_binding_v1()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    assignment jsonb;
    task_blueprints jsonb;
    opportunity_manifest jsonb;
    confirmed_turns jsonb;
    evidence_refs jsonb;
    expected_part_sequence jsonb;
    expected_question_texts jsonb;
BEGIN
    SELECT
        snapshot.canonical_payload #>
            '{practice_context,ielts_assignment}',
        snapshot.canonical_payload #>
            '{practice_context,task_blueprints}',
        snapshot.canonical_payload -> 'opportunity_manifest',
        snapshot.canonical_payload -> 'confirmed_turns',
        snapshot.canonical_payload -> 'evidence_refs'
    INTO
        assignment,
        task_blueprints,
        opportunity_manifest,
        confirmed_turns,
        evidence_refs
    FROM evaluation_evidence_snapshots AS snapshot
    WHERE snapshot.id = NEW.input_snapshot_id
      AND snapshot.owner_user_id = NEW.owner_user_id
      AND snapshot.practice_session_id = NEW.practice_session_id
      AND snapshot.scene_type = 'IELTS_SPEAKING'
      AND snapshot.scope = 'SESSION'
    FOR SHARE;

    IF NOT FOUND
       OR ielts_assignment_is_valid_v1(
            'FULL_MOCK',
            task_blueprints,
            assignment
          ) IS DISTINCT FROM true
       OR jsonb_typeof(opportunity_manifest) IS DISTINCT FROM 'array'
       OR jsonb_typeof(confirmed_turns) IS DISTINCT FROM 'array'
       OR jsonb_typeof(evidence_refs) IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'invalid IELTS Part result binding'
            USING ERRCODE = '23514';
    END IF;

    SELECT
        jsonb_agg(
            to_jsonb(part.value ->> 'part')
            ORDER BY part.ordinality, blueprint.ordinality
        ),
        jsonb_agg(
            to_jsonb(
                btrim(
                    regexp_replace(
                        blueprint.value #>> '{}',
                        '^[^:]+:[[:space:]]*',
                        ''
                    )
                )
            )
            ORDER BY part.ordinality, blueprint.ordinality
        )
    INTO
        expected_part_sequence,
        expected_question_texts
    FROM jsonb_array_elements(assignment -> 'parts')
        WITH ORDINALITY AS part(value, ordinality)
    CROSS JOIN LATERAL jsonb_array_elements(
        part.value -> 'turn_blueprints'
    ) WITH ORDINALITY AS blueprint(value, ordinality);

    IF jsonb_array_length(opportunity_manifest) <>
           jsonb_array_length(expected_part_sequence)
       OR jsonb_array_length(NEW.result_payload -> 'question_results') <>
           jsonb_array_length(expected_part_sequence)
       OR (
            SELECT count(DISTINCT opportunity.value ->> 'question_id')
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
          ) <> jsonb_array_length(opportunity_manifest)
       OR (
            SELECT count(*)
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          ) <> jsonb_array_length(confirmed_turns)
       OR (
            SELECT count(*)
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          ) <> jsonb_array_length(evidence_refs)
       OR (
            SELECT count(DISTINCT opportunity.value ->> 'response_turn_id')
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          ) <> (
            SELECT count(*)
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          )
       OR EXISTS (
            SELECT 1
            FROM jsonb_array_elements(
                NEW.result_payload -> 'question_results'
            ) WITH ORDINALITY AS question(value, ordinality)
            CROSS JOIN LATERAL (
                SELECT opportunity_manifest ->
                    (question.ordinality::integer - 1) AS opportunity
            ) AS expected
            CROSS JOIN LATERAL (
                SELECT coalesce(
                    jsonb_agg(
                        to_jsonb(
                            evidence_ref.value ->> 'evidence_ref_id'
                        )
                        ORDER BY evidence_ref.ordinality
                    ),
                    '[]'::jsonb
                ) AS evidence_ref_ids
                FROM jsonb_array_elements(evidence_refs)
                    WITH ORDINALITY AS evidence_ref(value, ordinality)
                WHERE evidence_ref.value ->> 'turn_id' =
                    expected.opportunity ->> 'response_turn_id'
            ) AS expected_refs
            WHERE expected.opportunity ->> 'sequence' IS DISTINCT FROM
                    question.ordinality::text
               OR question.value ->> 'index' IS DISTINCT FROM
                    question.ordinality::text
               OR question.value ->> 'part_id' IS DISTINCT FROM
                expected_part_sequence ->> (question.ordinality::integer - 1)
               OR question.value ->> 'question_id' IS DISTINCT FROM
                    expected.opportunity ->> 'question_id'
               OR expected.opportunity ->> 'question_text' IS DISTINCT FROM
                    expected_question_texts ->>
                        (question.ordinality::integer - 1)
               OR (
                    NOT (expected.opportunity ? 'response_turn_id')
                    AND (
                        question.value ->> 'opportunity_status'
                            IS DISTINCT FROM 'NOT_PROVIDED'
                        OR question.value ->> 'assessment_status'
                            IS DISTINCT FROM 'NOT_ASSESSED'
                        OR question.value ? 'response_turn_id'
                        OR question.value -> 'evidence_ref_ids'
                            IS DISTINCT FROM '[]'::jsonb
                    )
               )
               OR (
                    expected.opportunity ? 'response_turn_id'
                    AND (
                        question.value ->> 'opportunity_status'
                            IS DISTINCT FROM 'PROVIDED'
                        OR question.value ->> 'assessment_status'
                            IS DISTINCT FROM 'ASSESSED'
                        OR question.value ->> 'response_turn_id'
                            IS DISTINCT FROM
                                expected.opportunity ->> 'response_turn_id'
                        OR expected_refs.evidence_ref_ids = '[]'::jsonb
                        OR question.value -> 'evidence_ref_ids'
                            IS DISTINCT FROM
                                expected_refs.evidence_ref_ids
                        OR NOT EXISTS (
                            SELECT 1
                            FROM jsonb_array_elements(confirmed_turns)
                                AS confirmed_turn(value)
                            WHERE confirmed_turn.value ->> 'turn_id' =
                                expected.opportunity ->> 'response_turn_id'
                              AND confirmed_turn.value ->> 'question_id' =
                                expected.opportunity ->> 'question_id'
                              AND confirmed_turn.value ->> 'sequence' =
                                expected.opportunity ->> 'sequence'
                        )
                    )
               )
       ) THEN
        RAISE EXCEPTION 'invalid IELTS Part result binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_ielts_scene_results_part_binding
BEFORE INSERT ON evaluation_ielts_speaking_scene_results
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_ielts_part_result_binding_v1();

COMMIT;
