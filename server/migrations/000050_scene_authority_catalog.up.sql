BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

CREATE FUNCTION coaching_scene_nonempty_string_array_is_valid_v1(
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'array'
       OR jsonb_array_length(payload) = 0 THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements(payload) AS item(value)
        WHERE jsonb_typeof(value) <> 'string'
           OR value #>> '{}' = ''
           OR value #>> '{}' <> btrim(value #>> '{}')
    ) THEN
        RETURN false;
    END IF;

    RETURN (
        SELECT count(*) = count(DISTINCT value #>> '{}')
        FROM jsonb_array_elements(payload) AS item(value)
    );
END;
$$;

CREATE FUNCTION coaching_scene_practice_objectives_are_valid_v1(
    payload jsonb
)
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

CREATE TABLE coaching_scenes (
    scene_id text COLLATE "C" PRIMARY KEY,
    owner_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT coaching_scenes_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT coaching_scenes_scene_id_check
        CHECK (scene_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$')
);

CREATE INDEX coaching_scenes_owner_created_idx
    ON coaching_scenes (owner_user_id, created_at, scene_id)
    WHERE owner_user_id IS NOT NULL;

CREATE TABLE coaching_scene_versions (
    scene_id text COLLATE "C" NOT NULL,
    scene_version bigint NOT NULL,
    scene_family text NOT NULL,
    scene_model text NOT NULL,
    name text NOT NULL,
    status text NOT NULL,
    turn_policy_ref text COLLATE "C" NOT NULL,
    session_policy_ref text COLLATE "C" NOT NULL,
    prompt jsonb NOT NULL,
    roles jsonb NOT NULL,
    practice_options jsonb NOT NULL,
    display_order integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT coaching_scene_versions_pkey
        PRIMARY KEY (scene_id, scene_version),
    CONSTRAINT coaching_scene_versions_scene_id_fkey
        FOREIGN KEY (scene_id)
        REFERENCES coaching_scenes (scene_id)
        ON DELETE RESTRICT,
    CONSTRAINT coaching_scene_versions_scene_version_check
        CHECK (scene_version > 0),
    CONSTRAINT coaching_scene_versions_family_model_check
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
    CONSTRAINT coaching_scene_versions_name_check
        CHECK (
            char_length(name) BETWEEN 1 AND 200
            AND octet_length(name) <= 512
            AND name = btrim(name)
            AND name !~ '^[[:space:]]*$'
        ),
    CONSTRAINT coaching_scene_versions_status_check
        CHECK (status IN ('active', 'inactive')),
    CONSTRAINT coaching_scene_versions_turn_policy_ref_check
        CHECK (
            char_length(turn_policy_ref) BETWEEN 9 AND 128
            AND turn_policy_ref = btrim(turn_policy_ref)
            AND right(turn_policy_ref, 8) = '.turn.v1'
        ),
    CONSTRAINT coaching_scene_versions_session_policy_ref_check
        CHECK (
            char_length(session_policy_ref) BETWEEN 12 AND 128
            AND session_policy_ref = btrim(session_policy_ref)
            AND right(session_policy_ref, 11) = '.session.v1'
        ),
    CONSTRAINT coaching_scene_versions_payload_check
        CHECK (
            coaching_scene_version_payload_is_valid_v1(
                scene_id,
                prompt,
                roles,
                practice_options
            )
        ),
    CONSTRAINT coaching_scene_versions_display_order_check
        CHECK (display_order >= 0)
);

CREATE INDEX coaching_scene_versions_latest_idx
    ON coaching_scene_versions (scene_id, scene_version DESC);

CREATE FUNCTION reject_coaching_scene_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION
        'Scene version rows are immutable; insert a new scene_version instead'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER coaching_scene_versions_are_immutable
    BEFORE UPDATE OR DELETE ON coaching_scene_versions
    FOR EACH ROW
    EXECUTE FUNCTION reject_coaching_scene_version_mutation();

INSERT INTO coaching_scenes (scene_id, owner_user_id) VALUES
    ('scn_daily_airport_transport', NULL),
    ('scn_daily_complaint_help', NULL),
    ('scn_daily_custom', NULL),
    ('scn_daily_hotel_checkin_issue', NULL),
    ('scn_daily_medical_appointment', NULL),
    ('scn_daily_phone_call', NULL),
    ('scn_daily_rental_maintenance', NULL),
    ('scn_daily_restaurant_ordering', NULL),
    ('scn_daily_shopping_return', NULL),
    ('scn_daily_small_talk', NULL),
    ('scn_daily_social_invitation', NULL),
    ('scn_ielts_speaking_full', NULL),
    ('scn_ielts_speaking_part_1', NULL),
    ('scn_ielts_speaking_part_2', NULL),
    ('scn_ielts_speaking_part_3', NULL),
    ('scn_interview_behavioral', NULL),
    ('scn_interview_custom', NULL),
    ('scn_interview_hiring_manager', NULL),
    ('scn_interview_recruiter_screening', NULL),
    ('scn_interview_self_introduction', NULL),
    ('scn_interview_system_design_spoken', NULL),
    ('scn_programmer_interview', NULL),
    ('scn_speaking_exam_custom', NULL),
    ('scn_workplace_client_delay', NULL),
    ('scn_workplace_cross_team_alignment', NULL),
    ('scn_workplace_custom', NULL),
    ('scn_workplace_feedback_conflict', NULL),
    ('scn_workplace_meeting_disagreement', NULL),
    ('scn_workplace_negotiation', NULL),
    ('scn_workplace_progress_risk_update', NULL),
    ('scn_workplace_solution_presentation', NULL);

INSERT INTO coaching_scene_versions (
    scene_id,
    scene_version,
    scene_family,
    scene_model,
    name,
    status,
    turn_policy_ref,
    session_policy_ref,
    prompt,
    roles,
    practice_options,
    display_order
) VALUES
    ('scn_daily_airport_transport', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '机场与交通', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"向工作人员或司机问路、确认时间，并处理一个行程变化。","practice_goal":"清楚说明目的地和时间需求，确认交通或票务信息。","user_role":"旅客","ai_role":"交通工作人员","persona_summary":"A concise transport staff member who provides necessary information and clarifies destination, timing, or ticket details.","focus_areas":["destination","time","route","travel_change"],"turn_blueprints":["询问旅客的目的地或问题","澄清时间或票务信息","提供一个必要路线或选择","确认旅客理解下一步"],"suggested_duration_seconds":480}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_airport_transport_counterpart","scene_id":"scn_daily_airport_transport","role_type":"TRANSPORT_STAFF","display_name":"交通工作人员","responsibilities":"清楚说明目的地和时间需求，确认交通或票务信息。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"destination","description":"State and confirm the intended destination accurately."},{"objective_id":"time","description":"State and confirm the relevant departure, arrival, or appointment time."},{"objective_id":"route","description":"Ask about and confirm the correct route or transport option."},{"objective_id":"travel_change","description":"Explain a travel change and confirm its consequences."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_airport_transport_full","scene_id":"scn_daily_airport_transport","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_airport_transport_focus","scene_id":"scn_daily_airport_transport","role_definition_id":"role_daily_airport_transport_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 40),
    ('scn_daily_complaint_help', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '投诉与求助', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"向服务人员清楚说明问题、提出诉求并确认解决办法。","practice_goal":"有条理地说明事实与期望，理解可行范围。","user_role":"顾客或旅客","ai_role":"服务人员","persona_summary":"A calm service staff member who clarifies facts and realistic limits before offering a solution.","focus_areas":["problem","fact","request","resolution"],"turn_blueprints":["请用户说明遇到的问题","澄清一个关键事实","确认用户的具体诉求","提供并确认可行方案"],"suggested_duration_seconds":480}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_complaint_help_counterpart","scene_id":"scn_daily_complaint_help","role_type":"SERVICE_STAFF","display_name":"服务人员","responsibilities":"有条理地说明事实与期望，理解可行范围。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"problem","description":"Describe the problem, its context, and its immediate effect."},{"objective_id":"fact","description":"Describe observable facts without mixing in assumptions."},{"objective_id":"request","description":"Make a specific, actionable request."},{"objective_id":"resolution","description":"Agree on a concrete resolution that addresses the reported issue."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_complaint_help_full","scene_id":"scn_daily_complaint_help","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_complaint_help_focus","scene_id":"scn_daily_complaint_help","role_definition_id":"role_daily_complaint_help_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 100),
    ('scn_daily_custom', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '自定义日常交流', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"使用默认生活场景，练习其他日常地点、身份和交流目标。","practice_goal":"根据用户补充的信息保持自然日常角色和对话边界。","user_role":"日常沟通者","ai_role":"日常交流对象","persona_summary":"A natural everyday conversation partner who follows the stated place, relationship, and goal without shifting into a job interview or exam.","focus_areas":["place","relationship","goal","confirmation"],"turn_blueprints":["根据用户目标建立日常语境","回应用户刚才的实际表达","澄清一个必要细节","自然完成交流"],"suggested_duration_seconds":420}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_custom_counterpart","scene_id":"scn_daily_custom","role_type":"DAILY_COUNTERPART","display_name":"日常交流对象","responsibilities":"根据用户补充的信息保持自然日常角色和对话边界。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"place","description":"Establish where the conversation is happening and why it matters."},{"objective_id":"relationship","description":"Use language appropriate to the relationship between participants."},{"objective_id":"goal","description":"State the desired outcome of the conversation clearly."},{"objective_id":"confirmation","description":"Restate and confirm the agreed details accurately."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_custom_full","scene_id":"scn_daily_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_custom_focus","scene_id":"scn_daily_custom","role_definition_id":"role_daily_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 110),
    ('scn_daily_hotel_checkin_issue', 1, 'DAILY', 'HOTEL_CHECKIN_AND_ISSUE_HANDLING', '酒店入住与问题处理', 'active', 'daily.hotel_checkin_issue.turn.v1', 'daily.hotel_checkin_issue.session.v1', $scene_catalog${"public_scene_brief":"在酒店前台核验预订、办理入住并处理一个房间问题。","practice_goal":"清楚说明预订和问题，理解可行方案并确认最终安排。","user_role":"住客","ai_role":"酒店前台","persona_summary":"A professional hotel receptionist who verifies details, clarifies one issue, offers realistic options, and confirms the outcome.","focus_areas":["reservation_verification","issue_description","solution_clarification","outcome_confirmation"],"turn_blueprints":["核验住客姓名和预订信息","请住客说明入住或房间问题","澄清限制并提供可行方案","确认住客选择和最终安排"],"suggested_duration_seconds":480}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_hotel_front_desk","scene_id":"scn_daily_hotel_checkin_issue","role_type":"HOTEL_FRONT_DESK","display_name":"酒店前台","responsibilities":"Verify the booking, clarify the issue, offer realistic options, and confirm the final arrangement.","style":"Professional, calm, and service oriented.","practice_objectives":[{"objective_id":"reservation_verification","description":"Verify the reservation identity, dates, room, and booking details."},{"objective_id":"solution_clarification","description":"Clarify the proposed solution, its timing, and any remaining conditions."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_hotel_full_simulation","scene_id":"scn_daily_hotel_checkin_issue","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_hotel_front_desk_focus","scene_id":"scn_daily_hotel_checkin_issue","role_definition_id":"role_hotel_front_desk","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 50),
    ('scn_daily_medical_appointment', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '医疗预约', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"与医疗前台预约时间、描述基础情况并确认准备事项。","practice_goal":"完成基础预约沟通，不把练习当作医疗诊断。","user_role":"患者","ai_role":"医疗前台","persona_summary":"A medical receptionist who handles scheduling and basic preparation only, never diagnoses, and directs emergencies to real urgent care.","focus_areas":["appointment_reason","availability","time_confirmation","preparation"],"turn_blueprints":["询问预约原因和是否紧急","澄清可用时间","提供并确认一个预约时段","说明基础准备事项"],"suggested_duration_seconds":420}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_medical_appointment_counterpart","scene_id":"scn_daily_medical_appointment","role_type":"MEDICAL_RECEPTIONIST","display_name":"医疗前台","responsibilities":"完成基础预约沟通，不把练习当作医疗诊断。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"appointment_reason","description":"Explain the reason for the appointment with relevant details."},{"objective_id":"availability","description":"Ask about and confirm a workable time or service slot."},{"objective_id":"time_confirmation","description":"Repeat and confirm the final appointment time."},{"objective_id":"preparation","description":"Ask what preparation or documents are needed before the appointment."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_medical_appointment_full","scene_id":"scn_daily_medical_appointment","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_medical_appointment_focus","scene_id":"scn_daily_medical_appointment","role_definition_id":"role_daily_medical_appointment_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 70),
    ('scn_daily_phone_call', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '电话沟通', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"通过电话向客服、机构或联系人说明身份、目的并确认信息。","practice_goal":"在看不到对方时清楚表达，并自然请求重复或复述确认。","user_role":"来电者","ai_role":"电话接听者","persona_summary":"A clear phone contact who confirms identity and purpose, and naturally requests repetition when information is unclear.","focus_areas":["identity","purpose","clarification","confirmation"],"turn_blueprints":["接听电话并询问来电目的","澄清一项关键信息","必要时请求重复或复述","确认处理结果"],"suggested_duration_seconds":420}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_phone_call_counterpart","scene_id":"scn_daily_phone_call","role_type":"PHONE_CONTACT","display_name":"电话接听者","responsibilities":"在看不到对方时清楚表达，并自然请求重复或复述确认。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"identity","description":"Identify the caller and intended recipient clearly."},{"objective_id":"purpose","description":"State the purpose of the call or conversation early."},{"objective_id":"clarification","description":"Ask focused questions to resolve ambiguity."},{"objective_id":"confirmation","description":"Restate and confirm the agreed details accurately."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_phone_call_full","scene_id":"scn_daily_phone_call","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_phone_call_focus","scene_id":"scn_daily_phone_call","role_definition_id":"role_daily_phone_call_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 80),
    ('scn_daily_rental_maintenance', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '租房与维修', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"向房东、物业或维修人员描述故障、约定时间并确认责任。","practice_goal":"准确描述问题，协商可行时间和后续安排。","user_role":"租客","ai_role":"物业工作人员","persona_summary":"A practical property staff member who asks for fault details, has realistic scheduling limits, and confirms responsibility and follow-up.","focus_areas":["fault_description","availability","responsibility","follow_up"],"turn_blueprints":["请租客描述具体故障","澄清影响和可进入时间","提出一个现实的维修时段","确认责任和后续安排"],"suggested_duration_seconds":480}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_rental_maintenance_counterpart","scene_id":"scn_daily_rental_maintenance","role_type":"PROPERTY_STAFF","display_name":"物业工作人员","responsibilities":"准确描述问题，协商可行时间和后续安排。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"fault_description","description":"Describe the fault and its symptoms precisely."},{"objective_id":"availability","description":"Ask about and confirm a workable time or service slot."},{"objective_id":"responsibility","description":"Clarify who is responsible for handling the issue."},{"objective_id":"follow_up","description":"Agree on a specific follow-up action and timing."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_rental_maintenance_full","scene_id":"scn_daily_rental_maintenance","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_rental_maintenance_focus","scene_id":"scn_daily_rental_maintenance","role_definition_id":"role_daily_rental_maintenance_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 60),
    ('scn_daily_restaurant_ordering', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '餐厅点餐', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"在餐厅询问菜品、说明偏好、完成点餐并确认订单。","practice_goal":"理解服务员问题，清楚表达选择和饮食偏好。","user_role":"顾客","ai_role":"服务员","persona_summary":"A helpful restaurant server who follows a realistic ordering flow and may explain availability, flavors, or set-menu options.","focus_areas":["menu_question","preference","order","confirmation"],"turn_blueprints":["欢迎顾客并询问是否需要介绍","回应菜品问题或说明一个选择","确认偏好和具体点单","复述并确认订单"],"suggested_duration_seconds":480}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_restaurant_ordering_counterpart","scene_id":"scn_daily_restaurant_ordering","role_type":"RESTAURANT_SERVER","display_name":"服务员","responsibilities":"理解服务员问题，清楚表达选择和饮食偏好。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"menu_question","description":"Ask specific questions needed to understand menu choices."},{"objective_id":"preference","description":"Express preferences and any important constraints clearly."},{"objective_id":"order","description":"Place the order accurately with all required details."},{"objective_id":"confirmation","description":"Restate and confirm the agreed details accurately."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_restaurant_ordering_full","scene_id":"scn_daily_restaurant_ordering","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_restaurant_ordering_focus","scene_id":"scn_daily_restaurant_ordering","role_definition_id":"role_daily_restaurant_ordering_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 20),
    ('scn_daily_shopping_return', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '购物、换货与退款', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"在商店询价、比较商品，或说明换货退款问题并协商处理方式。","practice_goal":"清楚说明需求与事实，理解门店规则和可行选项。","user_role":"顾客","ai_role":"店员","persona_summary":"A realistic store assistant who asks relevant questions and offers options within a clear store policy.","focus_areas":["product_question","issue","store_policy","resolution"],"turn_blueprints":["询问顾客需要购买还是处理售后","澄清商品或问题细节","说明一个合理门店规则","确认可行处理方案"],"suggested_duration_seconds":480}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_shopping_return_counterpart","scene_id":"scn_daily_shopping_return","role_type":"STORE_ASSISTANT","display_name":"店员","responsibilities":"清楚说明需求与事实，理解门店规则和可行选项。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"product_question","description":"Ask focused questions about the product and available options."},{"objective_id":"issue","description":"Explain the product or service issue clearly."},{"objective_id":"store_policy","description":"Ask about and confirm the relevant return or exchange policy."},{"objective_id":"resolution","description":"Agree on a concrete resolution that addresses the reported issue."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_shopping_return_full","scene_id":"scn_daily_shopping_return","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_shopping_return_focus","scene_id":"scn_daily_shopping_return","role_definition_id":"role_daily_shopping_return_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 30),
    ('scn_daily_small_talk', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '自我介绍与寒暄', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"与新朋友、邻居或同学自然打招呼、介绍自己并延续简短话题。","practice_goal":"自然开启话题并完成一段轻松的双向交流。","user_role":"自己","ai_role":"新朋友","persona_summary":"A friendly new acquaintance who opens with a natural topic and responds conversationally without turning the exchange into an interview.","focus_areas":["greeting","self_introduction","shared_topic"],"turn_blueprints":["先打招呼并提供一个自然话题","回应用户的自我介绍","围绕共同点继续一句","自然结束简短寒暄"],"suggested_duration_seconds":360}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_small_talk_counterpart","scene_id":"scn_daily_small_talk","role_type":"NEW_ACQUAINTANCE","display_name":"新朋友","responsibilities":"自然开启话题并完成一段轻松的双向交流。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"greeting","description":"Open the conversation with a natural, context-appropriate greeting."},{"objective_id":"self_introduction","description":"Introduce yourself with concise, relevant personal information."},{"objective_id":"shared_topic","description":"Find and develop a topic that both participants can discuss naturally."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_small_talk_full","scene_id":"scn_daily_small_talk","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_small_talk_focus","scene_id":"scn_daily_small_talk","role_definition_id":"role_daily_small_talk_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 10),
    ('scn_daily_social_invitation', 1, 'DAILY', 'DAILY_BASIC_DIALOGUE', '社交邀请与礼貌拒绝', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"与朋友或同事发出、回应或礼貌拒绝一项邀请。","practice_goal":"表达意愿和原因，并在需要时协商替代时间。","user_role":"自己","ai_role":"朋友或同事","persona_summary":"A friendly contact who responds naturally to invitations and is open to an alternative time or activity.","focus_areas":["invitation","response","polite_reason","alternative"],"turn_blueprints":["提出或回应一个自然邀请","询问时间或意愿","回应接受或礼貌拒绝","协商替代安排"],"suggested_duration_seconds":360}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_daily_social_invitation_counterpart","scene_id":"scn_daily_social_invitation","role_type":"FRIEND_OR_COLLEAGUE","display_name":"朋友或同事","responsibilities":"表达意愿和原因，并在需要时协商替代时间。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"invitation","description":"Make or respond to an invitation with clear details."},{"objective_id":"response","description":"Respond directly to the other person’s invitation or concern."},{"objective_id":"polite_reason","description":"Give a clear reason using courteous, relationship-aware language."},{"objective_id":"alternative","description":"Offer a practical alternative that addresses the other party’s concern."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_daily_social_invitation_full","scene_id":"scn_daily_social_invitation","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_daily_social_invitation_focus","scene_id":"scn_daily_social_invitation","role_definition_id":"role_daily_social_invitation_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 90),
    ('scn_ielts_speaking_full', 2, 'EXAM', 'IELTS_SPEAKING_FULL_MOCK', 'IELTS 口语完整模拟', 'active', 'ielts.speaking_full_mock.turn.v1', 'ielts.speaking_full_mock.session.v1', $scene_catalog${"public_scene_brief":"按 Part 1、Part 2、Part 3 连续完成一轮 IELTS 口语完整模考。","practice_goal":"适应真实三段式流程，并在不同题型中保持连贯自然的表达。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who follows the frozen three-part mock-test sequence, asks exactly one item at a time, and never teaches or scores during the simulation.","focus_areas":["part_1_familiar_topics","part_2_long_turn","part_3_discussion","section_transition"],"turn_blueprints":["Part 1 question: Where is your hometown?","Part 1 question: Is there anything you do not like about your hometown?","Part 1 question: Would you say it is a good place for young people?","Part 1 question: Do you use artificial intelligence in your daily life?","Part 1 question: Has technology changed the way you learn things?","Part 1 question: Is there any technology you find difficult to use?","Part 1 question: What do you usually do in your free time?","Part 1 question: Do you prefer spending your free time alone or with other people?","Part 2 cue card: Describe a skill you would like to learn.\nYou should say:\n• What the skill is\n• Why you want to learn it\n• How you would learn it\n• And explain how learning this skill would benefit you","Part 3 question: What kinds of skills are most valuable in today's society?","Part 3 question: Some people say it is never too late to learn a new skill. Do you agree?","Part 3 question: Do you think schools should focus more on practical skills?","Part 3 question: How has technology changed the way people learn skills?","Part 3 question: Do you think some skills will become obsolete in the future?"],"suggested_duration_seconds":900}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_ielts_speaking_full_counterpart","scene_id":"scn_ielts_speaking_full","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Run the frozen Part 1, Part 2, and Part 3 sequence without coaching or scoring.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"part_1_familiar_topics","description":"Answer familiar-topic questions directly with relevant detail."},{"objective_id":"part_2_long_turn","description":"Deliver a coherent long turn that covers every cue-card point."},{"objective_id":"part_3_discussion","description":"Develop abstract ideas with reasons, examples, and comparisons."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_ielts_speaking_full_full","scene_id":"scn_ielts_speaking_full","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_speaking_full_focus","scene_id":"scn_ielts_speaking_full","role_definition_id":"role_ielts_speaking_full_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 40),
    ('scn_ielts_speaking_part_1', 1, 'EXAM', 'IELTS_SPEAKING_PART_1', 'IELTS Speaking Part 1', 'active', 'ielts.speaking_part1.turn.v1', 'ielts.speaking_part1.session.v1', $scene_catalog${"public_scene_brief":"连续回答三个熟悉话题中的八道 Part 1 问题。","practice_goal":"在正式 Part 1 节奏中自然、直接并适度展开回答。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who asks the frozen Part 1 questions one at a time without coaching or scoring.","focus_areas":["part_1_familiar_topics","direct_answer","natural_extension"],"turn_blueprints":["Part 1 question 1","Part 1 question 2","Part 1 question 3","Part 1 question 4","Part 1 question 5","Part 1 question 6","Part 1 question 7","Part 1 question 8"],"suggested_duration_seconds":300}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_ielts_part1_examiner","scene_id":"scn_ielts_speaking_part_1","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Ask the frozen familiar-topic questions in order.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"part_1_familiar_topics","description":"Answer familiar-topic questions directly with relevant detail."},{"objective_id":"natural_extension","description":"Extend short answers naturally with relevant reasons or examples."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_ielts_speaking_part_1_full","scene_id":"scn_ielts_speaking_part_1","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_speaking_part_1_focus","scene_id":"scn_ielts_speaking_part_1","role_definition_id":"role_ielts_part1_examiner","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 10),
    ('scn_ielts_speaking_part_2', 1, 'EXAM', 'IELTS_SPEAKING_PART_2', 'IELTS Speaking Part 2', 'active', 'ielts.speaking_part2.turn.v1', 'ielts.speaking_part2.session.v1', $scene_catalog${"public_scene_brief":"根据一张 Cue Card 进行连续表达，并回答考官的必要追问。","practice_goal":"围绕主题清楚展开观点、细节和理由。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who presents one task at a time and does not teach during the simulation.","focus_areas":["cue_card","topic_development","detail_and_reason","fluency_and_extension"],"turn_blueprints":["给出清楚的 Cue Card 并邀请作答","根据回答追问主体内容","追问一个细节或理由","检查表达的流利度和展开程度"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_ielts_examiner","scene_id":"scn_ielts_speaking_part_2","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Present the cue card and ask concise, neutral follow-up questions.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"topic_development","description":"Develop the topic with a clear progression of connected ideas."},{"objective_id":"fluency_and_extension","description":"Speak fluently and extend answers with relevant detail."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_ielts_full_simulation","scene_id":"scn_ielts_speaking_part_2","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_examiner_focus","scene_id":"scn_ielts_speaking_part_2","role_definition_id":"role_ielts_examiner","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 20),
    ('scn_ielts_speaking_part_3', 1, 'EXAM', 'IELTS_SPEAKING_PART_3', 'IELTS Speaking Part 3', 'active', 'ielts.speaking_part3.turn.v1', 'ielts.speaking_part3.session.v1', $scene_catalog${"public_scene_brief":"围绕对应 Part 2 主题完成五道深入讨论题。","practice_goal":"解释观点，并从更一般和抽象的角度分析、讨论和推测。","user_role":"考生","ai_role":"IELTS 口语考官","persona_summary":"A neutral IELTS speaking examiner who keeps every Part 3 question bound to the selected Part 2 topic.","focus_areas":["part_3_discussion","opinion_and_reason","analysis_and_speculation"],"turn_blueprints":["Part 3 question 1","Part 3 question 2","Part 3 question 3","Part 3 question 4","Part 3 question 5"],"suggested_duration_seconds":300}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_ielts_part3_examiner","scene_id":"scn_ielts_speaking_part_3","role_type":"IELTS_EXAMINER","display_name":"IELTS 口语考官","responsibilities":"Discuss only the issues bound to the selected Part 2 topic.","style":"Neutral, concise, and exam appropriate.","practice_objectives":[{"objective_id":"part_3_discussion","description":"Develop abstract ideas with reasons, examples, and comparisons."},{"objective_id":"analysis_and_speculation","description":"Separate supported analysis from speculation and explain both clearly."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_ielts_speaking_part_3_full","scene_id":"scn_ielts_speaking_part_3","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_ielts_speaking_part_3_focus","scene_id":"scn_ielts_speaking_part_3","role_definition_id":"role_ielts_part3_examiner","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 30),
    ('scn_interview_behavioral', 1, 'INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE', '行为面试', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"围绕一个真实的协作、冲突、失败或成长经历进行行为面试。","practice_goal":"用具体情境、行动、结果和反思说明个人能力。","user_role":"候选人","ai_role":"行为面试官","persona_summary":"A focused behavioral interviewer who explores one real example at a time and never invents candidate experience.","focus_areas":["situation","action","result","reflection"],"turn_blueprints":["给出一个明确的行为主题","澄清当时的情境与个人责任","追问采取的具体行动和结果","询问复盘与成长"],"suggested_duration_seconds":720}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_interview_behavioral_counterpart","scene_id":"scn_interview_behavioral","role_type":"BEHAVIORAL_INTERVIEWER","display_name":"行为面试官","responsibilities":"用具体情境、行动、结果和反思说明个人能力。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"situation","description":"Set the context with the essential people, constraints, and stakes."},{"objective_id":"action","description":"State the action taken and explain why it was appropriate."},{"objective_id":"result","description":"Describe the outcome and connect it to the action taken."},{"objective_id":"reflection","description":"Reflect on what was learned and what would be changed next time."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_interview_behavioral_full","scene_id":"scn_interview_behavioral","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_interview_behavioral_focus","scene_id":"scn_interview_behavioral","role_definition_id":"role_interview_behavioral_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 40),
    ('scn_interview_custom', 1, 'INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE', '自定义面试', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"使用默认的通用岗位背景练习一个暂未被正式子场景覆盖的面试目标。","practice_goal":"保持面试语境，围绕用户补充的一句话目标进行自然问答。","user_role":"候选人","ai_role":"通用面试官","persona_summary":"A professional interviewer who follows the user's stated practice goal without claiming to reproduce a specific company's real questions.","focus_areas":["custom_goal","evidence","clarification"],"turn_blueprints":["根据用户目标提出一个明确问题","回应用户刚才的实际回答","追问一个最相关的证据或细节","围绕目标自然收尾"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_interview_custom_counterpart","scene_id":"scn_interview_custom","role_type":"CUSTOM_INTERVIEWER","display_name":"通用面试官","responsibilities":"保持面试语境，围绕用户补充的一句话目标进行自然问答。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"custom_goal","description":"Keep the conversation aligned with the user’s stated practice goal."},{"objective_id":"evidence","description":"Support claims with concrete evidence."},{"objective_id":"clarification","description":"Ask focused questions to resolve ambiguity."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_interview_custom_full","scene_id":"scn_interview_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_interview_custom_focus","scene_id":"scn_interview_custom","role_definition_id":"role_interview_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 70),
    ('scn_interview_hiring_manager', 1, 'INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE', 'Hiring Manager 面试', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"与用人经理讨论岗位动机、业务影响、判断和跨团队协作方式。","practice_goal":"说明岗位匹配、高影响经历和做出判断的依据。","user_role":"候选人","ai_role":"用人经理","persona_summary":"A concise hiring manager who probes role fit, judgment, collaboration, and measurable business impact.","focus_areas":["role_fit","business_impact","judgment","collaboration"],"turn_blueprints":["询问候选人选择该岗位的原因","追问一段高影响经历","澄清关键判断和跨团队协作","确认可衡量的结果"],"suggested_duration_seconds":720}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_interview_hiring_manager_counterpart","scene_id":"scn_interview_hiring_manager","role_type":"HIRING_MANAGER","display_name":"用人经理","responsibilities":"说明岗位匹配、高影响经历和做出判断的依据。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"role_fit","description":"Connect relevant strengths and experience to the role’s needs."},{"objective_id":"business_impact","description":"Connect a decision or result to measurable business impact."},{"objective_id":"judgment","description":"Explain the reasoning behind a professional judgment."},{"objective_id":"collaboration","description":"Explain how people or teams worked together to reach the outcome."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_interview_hiring_manager_full","scene_id":"scn_interview_hiring_manager","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_interview_hiring_manager_focus","scene_id":"scn_interview_hiring_manager","role_definition_id":"role_interview_hiring_manager_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 60),
    ('scn_interview_recruiter_screening', 1, 'INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE', '招聘初筛', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"与招聘专员讨论求职动机、岗位理解、基本条件和候选人反问。","practice_goal":"清楚表达动机与岗位理解，并完成一轮双向确认。","user_role":"候选人","ai_role":"招聘专员","persona_summary":"A professional recruiter who keeps the screening focused on motivation, role understanding, practical conditions, and candidate questions.","focus_areas":["motivation","role_understanding","conditions","candidate_questions"],"turn_blueprints":["询问候选人的求职动机","澄清对岗位的理解","确认一项基本求职条件","邀请候选人提出一个问题"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_interview_recruiter_screening_counterpart","scene_id":"scn_interview_recruiter_screening","role_type":"RECRUITER","display_name":"招聘专员","responsibilities":"清楚表达动机与岗位理解，并完成一轮双向确认。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"motivation","description":"Explain authentic motivation for the role or opportunity."},{"objective_id":"role_understanding","description":"Explain an accurate understanding of the role and its expectations."},{"objective_id":"conditions","description":"Confirm the key conditions, expectations, and limitations of the role."},{"objective_id":"candidate_questions","description":"Ask thoughtful questions that clarify the role and company."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_interview_recruiter_screening_full","scene_id":"scn_interview_recruiter_screening","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_interview_recruiter_screening_focus","scene_id":"scn_interview_recruiter_screening","role_definition_id":"role_interview_recruiter_screening_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 20),
    ('scn_interview_self_introduction', 1, 'INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE', '英文自我介绍', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"向招聘方做一段 60～90 秒的英文自我介绍，并围绕亮点继续交流。","practice_goal":"说清背景、优势和岗位匹配，并自然回应一到两个追问。","user_role":"候选人","ai_role":"招聘方","persona_summary":"A warm recruiter who invites a concise introduction and follows up on one concrete strength without scoring the candidate.","focus_areas":["background","strength","role_fit"],"turn_blueprints":["邀请候选人做简短自我介绍","围绕一个具体亮点自然追问","澄清亮点与岗位的关联","请候选人简短总结匹配度"],"suggested_duration_seconds":480}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_interview_self_introduction_counterpart","scene_id":"scn_interview_self_introduction","role_type":"RECRUITER","display_name":"招聘方","responsibilities":"说清背景、优势和岗位匹配，并自然回应一到两个追问。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"background","description":"Summarize relevant experience and background for the role."},{"objective_id":"strength","description":"Describe a relevant strength and support it with an example."},{"objective_id":"role_fit","description":"Connect relevant strengths and experience to the role’s needs."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_interview_self_introduction_full","scene_id":"scn_interview_self_introduction","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_interview_self_introduction_focus","scene_id":"scn_interview_self_introduction","role_definition_id":"role_interview_self_introduction_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 10),
    ('scn_interview_system_design_spoken', 1, 'INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE', '系统设计口述', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"用英语口述一个系统设计方案，从需求澄清逐步讨论架构、瓶颈和取舍。","practice_goal":"有条理地澄清需求、组织方案并解释关键技术取舍。","user_role":"候选人","ai_role":"系统设计面试官","persona_summary":"A technical interviewer who asks one system-design question at a time and probes the candidate's own architecture and trade-offs.","focus_areas":["requirements","architecture","bottleneck","tradeoff"],"turn_blueprints":["给出一个边界清楚的系统设计任务","请候选人澄清需求和规模","围绕候选人的方案追问瓶颈","讨论一个关键技术取舍"],"suggested_duration_seconds":900}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_interview_system_design_spoken_counterpart","scene_id":"scn_interview_system_design_spoken","role_type":"SYSTEM_DESIGN_INTERVIEWER","display_name":"系统设计面试官","responsibilities":"有条理地澄清需求、组织方案并解释关键技术取舍。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"requirements","description":"Clarify functional and non-functional requirements before proposing a solution."},{"objective_id":"architecture","description":"Describe the system architecture and how its components interact."},{"objective_id":"bottleneck","description":"Identify the main system bottleneck and its impact."},{"objective_id":"tradeoff","description":"Explain what was gained and sacrificed by a decision."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_interview_system_design_spoken_full","scene_id":"scn_interview_system_design_spoken","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_interview_system_design_spoken_focus","scene_id":"scn_interview_system_design_spoken","role_definition_id":"role_interview_system_design_spoken_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 50),
    ('scn_programmer_interview', 1, 'INTERVIEW', 'PROJECT_EXPERIENCE_DEEP_DIVE', '项目经历深挖', 'active', 'interview.project_deep_dive.turn.v1', 'interview.project_deep_dive.session.v1', $scene_catalog${"public_scene_brief":"围绕一个真实项目说明个人职责、关键难点、技术取舍和结果。","practice_goal":"清楚表达个人贡献、决策依据、结果与反思。","user_role":"候选人","ai_role":"技术面试官","persona_summary":"A precise technical interviewer who probes evidence and trade-offs without inventing candidate experience.","focus_areas":["background_responsibility","key_challenge","technical_tradeoff","result_reflection"],"turn_blueprints":["澄清项目背景和候选人的具体职责","追问最关键的技术难点","讨论方案选择和技术取舍","核实结果、影响和复盘"],"suggested_duration_seconds":900}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_technical_interviewer","scene_id":"scn_programmer_interview","role_type":"TECHNICAL_INTERVIEWER","display_name":"技术面试官","responsibilities":"Probe technical depth, engineering trade-offs, and decision making.","style":"Precise and evidence seeking.","practice_objectives":[{"objective_id":"key_challenge","description":"Describe the hardest challenge and why it mattered."},{"objective_id":"technical_tradeoff","description":"Compare technical options and justify the chosen trade-off."}],"display_order":10},{"role_definition_id":"role_hr_interviewer","scene_id":"scn_programmer_interview","role_type":"HR_INTERVIEWER","display_name":"招聘专员","responsibilities":"Explore career motivation and communication clarity.","style":"Warm and structured.","practice_objectives":[{"objective_id":"motivation","description":"Explain authentic motivation for the role or opportunity."},{"objective_id":"communication","description":"Communicate ideas clearly and adapt them to the listener."}],"display_order":20},{"role_definition_id":"role_project_manager","scene_id":"scn_programmer_interview","role_type":"PROJECT_MANAGER","display_name":"项目经理","responsibilities":"Explore delivery ownership and cross-functional collaboration.","style":"Outcome oriented and collaborative.","practice_objectives":[{"objective_id":"delivery","description":"Explain the delivery plan, ownership, and expected outcome."},{"objective_id":"collaboration","description":"Explain how people or teams worked together to reach the outcome."}],"display_order":30},{"role_definition_id":"role_executive_interviewer","scene_id":"scn_programmer_interview","role_type":"EXECUTIVE_INTERVIEWER","display_name":"用人经理","responsibilities":"Explore leadership judgment and measurable impact for senior or management roles.","style":"Concise, high level, and optional for advanced roles.","practice_objectives":[{"objective_id":"impact","description":"Explain the concrete impact on people, work, or outcomes."},{"objective_id":"judgment","description":"Explain the reasoning behind a professional judgment."}],"display_order":40}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_full_simulation","scene_id":"scn_programmer_interview","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_technical_focus","scene_id":"scn_programmer_interview","role_definition_id":"role_technical_interviewer","practice_option_type":"FOCUS","display_name":"技术深挖","display_order":20},{"practice_option_id":"option_hr_focus","scene_id":"scn_programmer_interview","role_definition_id":"role_hr_interviewer","practice_option_type":"FOCUS","display_name":"动机与沟通","display_order":30},{"practice_option_id":"option_project_manager_focus","scene_id":"scn_programmer_interview","role_definition_id":"role_project_manager","practice_option_type":"FOCUS","display_name":"交付与协作","display_order":40},{"practice_option_id":"option_executive_focus","scene_id":"scn_programmer_interview","role_definition_id":"role_executive_interviewer","practice_option_type":"FOCUS","display_name":"领导力与影响","display_order":50}]$scene_catalog$::jsonb, 30),
    ('scn_speaking_exam_custom', 1, 'EXAM', 'EXAM_BASIC_DIALOGUE', '自定义口语考试', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"使用默认考试设定，练习老师指定或其他考试形式的口语问题。","practice_goal":"围绕用户补充的考试名称、题型或目标完成自定义练习。","user_role":"考生","ai_role":"口语考官","persona_summary":"A neutral speaking examiner who follows the user's stated format while clearly treating it as custom practice.","focus_areas":["custom_format","clear_answer","supporting_detail"],"turn_blueprints":["根据用户目标提出一个考试式问题","根据回答追问一个相关细节","保持中立考官行为","完成自定义练习收尾"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_speaking_exam_custom_counterpart","scene_id":"scn_speaking_exam_custom","role_type":"CUSTOM_EXAMINER","display_name":"口语考官","responsibilities":"围绕用户补充的考试名称、题型或目标完成自定义练习。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"custom_format","description":"Follow the user-defined response format consistently."},{"objective_id":"clear_answer","description":"Give a direct, well-structured answer to the question."},{"objective_id":"supporting_detail","description":"Add a specific reason, example, or detail that supports the answer."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_speaking_exam_custom_full","scene_id":"scn_speaking_exam_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_speaking_exam_custom_focus","scene_id":"scn_speaking_exam_custom","role_definition_id":"role_speaking_exam_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 50),
    ('scn_workplace_client_delay', 1, 'WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE', '客户延期说明与需求澄清', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"向客户解释交付限制、澄清需求并协商可行方案。","practice_goal":"说明影响和时间安排，避免含糊承诺。","user_role":"项目负责人","ai_role":"客户","persona_summary":"A concerned client who asks about impact, timing, and alternatives and does not accept vague commitments.","focus_areas":["constraint","impact","timeline","alternative"],"turn_blueprints":["询问延期或需求变化的具体情况","追问对业务和时间的影响","要求一个可行替代方案","确认新的承诺与边界"],"suggested_duration_seconds":720}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_workplace_client_delay_counterpart","scene_id":"scn_workplace_client_delay","role_type":"CLIENT","display_name":"客户","responsibilities":"说明影响和时间安排，避免含糊承诺。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"constraint","description":"Explain the relevant constraint and how it shapes the available options."},{"objective_id":"impact","description":"Explain the concrete impact on people, work, or outcomes."},{"objective_id":"timeline","description":"Provide a realistic timeline with key milestones or dates."},{"objective_id":"alternative","description":"Offer a practical alternative that addresses the other party’s concern."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_client_delay_full","scene_id":"scn_workplace_client_delay","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_client_delay_focus","scene_id":"scn_workplace_client_delay","role_definition_id":"role_workplace_client_delay_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 50),
    ('scn_workplace_cross_team_alignment', 1, 'WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE', '跨团队对齐与请求资源', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"与合作团队负责人对齐目标、优先级、责任、依赖和资源需求。","practice_goal":"在时间或资源约束下推动双方形成清楚安排。","user_role":"项目负责人","ai_role":"合作团队负责人","persona_summary":"A partner-team lead with a realistic capacity constraint who seeks clear priorities, ownership, and an executable agreement.","focus_areas":["goal","priority","dependency","resource_request"],"turn_blueprints":["带着一个时间或资源约束开场","澄清共同目标和优先级","确认责任与依赖","形成可执行的资源安排"],"suggested_duration_seconds":720}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_workplace_cross_team_alignment_counterpart","scene_id":"scn_workplace_cross_team_alignment","role_type":"PARTNER_TEAM_LEAD","display_name":"合作团队负责人","responsibilities":"在时间或资源约束下推动双方形成清楚安排。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"goal","description":"State the desired outcome of the conversation clearly."},{"objective_id":"priority","description":"Rank competing needs and explain the most important priority."},{"objective_id":"dependency","description":"Identify dependencies and confirm who owns each one."},{"objective_id":"resource_request","description":"Request the exact people, time, or resources needed."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_cross_team_alignment_full","scene_id":"scn_workplace_cross_team_alignment","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_cross_team_alignment_focus","scene_id":"scn_workplace_cross_team_alignment","role_definition_id":"role_workplace_cross_team_alignment_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 30),
    ('scn_workplace_custom', 1, 'WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE', '自定义职场沟通', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"使用默认工作关系和业务目标，练习其他真实工作交流。","practice_goal":"根据用户补充的对象、目标和边界保持专业对话。","user_role":"职场沟通者","ai_role":"工作对象","persona_summary":"A professional workplace counterpart who follows the user's stated relationship, business goal, and non-negotiable boundaries.","focus_areas":["relationship","business_goal","boundary","next_step"],"turn_blueprints":["根据用户目标建立工作语境","回应用户刚才的实际表达","澄清一个业务边界","推动形成具体下一步"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_workplace_custom_counterpart","scene_id":"scn_workplace_custom","role_type":"WORKPLACE_COUNTERPART","display_name":"工作对象","responsibilities":"根据用户补充的对象、目标和边界保持专业对话。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"relationship","description":"Use language appropriate to the relationship between participants."},{"objective_id":"business_goal","description":"Clarify the business outcome the conversation should achieve."},{"objective_id":"boundary","description":"State a clear non-negotiable boundary professionally."},{"objective_id":"next_step","description":"Agree on a specific owner, action, and timing for the next step."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_custom_full","scene_id":"scn_workplace_custom","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_custom_focus","scene_id":"scn_workplace_custom","role_definition_id":"role_workplace_custom_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 80),
    ('scn_workplace_feedback_conflict', 1, 'WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE', '提供反馈与处理冲突', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"向同事说明一个具体事实、影响和期望，并共同确认下一步。","practice_goal":"清楚表达反馈，同时保持专业关系。","user_role":"同事或负责人","ai_role":"需要反馈的同事","persona_summary":"A colleague who may accept, clarify, or defensively question feedback depending on the user's wording, while staying professional.","focus_areas":["fact","impact","expectation","next_step"],"turn_blueprints":["邀请用户说明需要讨论的事实","根据语气作出真实回应","澄清影响和期望","共同确认具体下一步"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_workplace_feedback_conflict_counterpart","scene_id":"scn_workplace_feedback_conflict","role_type":"COLLEAGUE","display_name":"需要反馈的同事","responsibilities":"清楚表达反馈，同时保持专业关系。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"fact","description":"Describe observable facts without mixing in assumptions."},{"objective_id":"impact","description":"Explain the concrete impact on people, work, or outcomes."},{"objective_id":"expectation","description":"State the expected behavior or outcome clearly."},{"objective_id":"next_step","description":"Agree on a specific owner, action, and timing for the next step."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_feedback_conflict_full","scene_id":"scn_workplace_feedback_conflict","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_feedback_conflict_focus","scene_id":"scn_workplace_feedback_conflict","role_definition_id":"role_workplace_feedback_conflict_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 40),
    ('scn_workplace_meeting_disagreement', 1, 'WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE', '会议发言与表达异议', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"在会议中针对当前方案清楚表达观点、不同意见和替代建议。","practice_goal":"说明立场与原因，并推动形成可执行的下一步。","user_role":"参会者","ai_role":"会议主持人","persona_summary":"A constructive meeting facilitator who responds to the user's position and asks for one clear reason or alternative.","focus_areas":["position","reason","alternative","next_step"],"turn_blueprints":["介绍当前讨论背景并邀请发言","追问用户立场背后的原因","请用户提出一个替代方案","确认下一步行动"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_workplace_meeting_disagreement_counterpart","scene_id":"scn_workplace_meeting_disagreement","role_type":"MEETING_FACILITATOR","display_name":"会议主持人","responsibilities":"说明立场与原因，并推动形成可执行的下一步。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"position","description":"State a clear position on the proposal under discussion."},{"objective_id":"reason","description":"Give a clear reason that supports the stated position."},{"objective_id":"alternative","description":"Offer a practical alternative that addresses the other party’s concern."},{"objective_id":"next_step","description":"Agree on a specific owner, action, and timing for the next step."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_meeting_disagreement_full","scene_id":"scn_workplace_meeting_disagreement","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_meeting_disagreement_focus","scene_id":"scn_workplace_meeting_disagreement","role_definition_id":"role_workplace_meeting_disagreement_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 20),
    ('scn_workplace_negotiation', 1, 'WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE', '条件协商', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"与对手方澄清双方利益与底线，并尝试用交换条件形成方案。","practice_goal":"识别优先级，提出清楚的条件式方案。","user_role":"协商方","ai_role":"对手方","persona_summary":"A realistic negotiation counterpart with one firm constraint who considers conditional trades but does not concede automatically.","focus_areas":["interest","priority","constraint","conditional_offer"],"turn_blueprints":["给出初始立场和一个明确约束","请用户澄清最重要的优先级","回应用户提出的交换条件","确认可接受的方案或分歧"],"suggested_duration_seconds":720}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_workplace_negotiation_counterpart","scene_id":"scn_workplace_negotiation","role_type":"NEGOTIATION_COUNTERPART","display_name":"对手方","responsibilities":"识别优先级，提出清楚的条件式方案。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"interest","description":"Identify the underlying interests behind each side’s position."},{"objective_id":"priority","description":"Rank competing needs and explain the most important priority."},{"objective_id":"constraint","description":"Explain the relevant constraint and how it shapes the available options."},{"objective_id":"conditional_offer","description":"Propose a clear if-then trade that respects both sides’ constraints."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_negotiation_full","scene_id":"scn_workplace_negotiation","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_negotiation_focus","scene_id":"scn_workplace_negotiation","role_definition_id":"role_workplace_negotiation_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 70),
    ('scn_workplace_progress_risk_update', 1, 'WORKPLACE', 'PROGRESS_AND_RISK_UPDATE', '进度与风险汇报', 'active', 'workplace.progress_risk_update.turn.v1', 'workplace.progress_risk_update.session.v1', $scene_catalog${"public_scene_brief":"向直属领导汇报项目进展、证据、风险和需要的支持。","practice_goal":"用结果导向的方式说明状态、风险、方案和决策请求。","user_role":"项目负责人","ai_role":"直属领导","persona_summary":"A direct manager who asks for evidence, mitigation, and a concrete decision or support request.","focus_areas":["status","evidence","risk_mitigation","decision_or_support"],"turn_blueprints":["请用户概括当前状态","核实进展证据和影响","追问主要风险与缓解方案","确认需要的决策或支持"],"suggested_duration_seconds":600}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_direct_manager","scene_id":"scn_workplace_progress_risk_update","role_type":"DIRECT_MANAGER","display_name":"直属领导","responsibilities":"Clarify delivery status, impact, mitigation, and the requested decision.","style":"Direct, outcome oriented, and constructive.","practice_objectives":[{"objective_id":"evidence","description":"Support claims with concrete evidence."},{"objective_id":"risk_mitigation","description":"Present a concrete mitigation plan for the main risk."},{"objective_id":"decision_or_support","description":"Make a specific decision request or ask for the support needed."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_full_simulation","scene_id":"scn_workplace_progress_risk_update","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_direct_manager_focus","scene_id":"scn_workplace_progress_risk_update","role_definition_id":"role_direct_manager","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 10),
    ('scn_workplace_solution_presentation', 1, 'WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE', '方案介绍与问答', 'active', 'generic.practice.turn.v1', 'generic.practice.session.v1', $scene_catalog${"public_scene_brief":"向领导、客户或评审者简洁介绍方案并回答价值、风险和落地问题。","practice_goal":"结构化介绍方案，并根据听众问题清楚回应。","user_role":"汇报人","ai_role":"方案评审者","persona_summary":"A pragmatic reviewer who listens to the user's proposal and asks the single most relevant question about value, risk, or execution.","focus_areas":["value","risk","execution","question_response"],"turn_blueprints":["邀请用户简洁介绍方案","根据介绍追问最相关的价值问题","澄清一个风险或落地条件","确认结论和下一步"],"suggested_duration_seconds":720}$scene_catalog$::jsonb, $scene_catalog$[{"role_definition_id":"role_workplace_solution_presentation_counterpart","scene_id":"scn_workplace_solution_presentation","role_type":"SOLUTION_REVIEWER","display_name":"方案评审者","responsibilities":"结构化介绍方案，并根据听众问题清楚回应。","style":"Natural, concise, and appropriate to the current role.","practice_objectives":[{"objective_id":"value","description":"Explain the proposal’s value in terms relevant to the listener."},{"objective_id":"risk","description":"Identify key risks and explain their likely consequences."},{"objective_id":"execution","description":"Explain how the proposal will be implemented in practical steps."},{"objective_id":"question_response","description":"Answer follow-up questions directly and with relevant evidence."}],"display_order":10}]$scene_catalog$::jsonb, $scene_catalog$[{"practice_option_id":"option_workplace_solution_presentation_full","scene_id":"scn_workplace_solution_presentation","practice_option_type":"FULL_SIMULATION","display_name":"完整模拟","display_order":10},{"practice_option_id":"option_workplace_solution_presentation_focus","scene_id":"scn_workplace_solution_presentation","role_definition_id":"role_workplace_solution_presentation_counterpart","practice_option_type":"FOCUS","display_name":"重点练习","display_order":20}]$scene_catalog$::jsonb, 60);

COMMIT;
