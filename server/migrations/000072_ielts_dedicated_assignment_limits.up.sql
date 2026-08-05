BEGIN;

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
