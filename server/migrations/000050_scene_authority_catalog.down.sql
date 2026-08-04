BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DO $$
BEGIN
    IF (SELECT count(*) FROM coaching_scenes) <> 31
       OR EXISTS (
           SELECT 1
           FROM coaching_scenes AS scene
           WHERE scene.owner_user_id IS NOT NULL
              OR NOT EXISTS (
                  SELECT 1
                  FROM (VALUES
                      ('scn_daily_airport_transport'),
                      ('scn_daily_complaint_help'),
                      ('scn_daily_custom'),
                      ('scn_daily_hotel_checkin_issue'),
                      ('scn_daily_medical_appointment'),
                      ('scn_daily_phone_call'),
                      ('scn_daily_rental_maintenance'),
                      ('scn_daily_restaurant_ordering'),
                      ('scn_daily_shopping_return'),
                      ('scn_daily_small_talk'),
                      ('scn_daily_social_invitation'),
                      ('scn_ielts_speaking_full'),
                      ('scn_ielts_speaking_part_1'),
                      ('scn_ielts_speaking_part_2'),
                      ('scn_ielts_speaking_part_3'),
                      ('scn_interview_behavioral'),
                      ('scn_interview_custom'),
                      ('scn_interview_hiring_manager'),
                      ('scn_interview_recruiter_screening'),
                      ('scn_interview_self_introduction'),
                      ('scn_interview_system_design_spoken'),
                      ('scn_programmer_interview'),
                      ('scn_speaking_exam_custom'),
                      ('scn_workplace_client_delay'),
                      ('scn_workplace_cross_team_alignment'),
                      ('scn_workplace_custom'),
                      ('scn_workplace_feedback_conflict'),
                      ('scn_workplace_meeting_disagreement'),
                      ('scn_workplace_negotiation'),
                      ('scn_workplace_progress_risk_update'),
                      ('scn_workplace_solution_presentation')
                  ) AS builtin(scene_id)
                  WHERE builtin.scene_id = scene.scene_id
              )
       )
       OR (SELECT count(*) FROM coaching_scene_versions) <> 31
       OR EXISTS (
           SELECT 1
           FROM coaching_scene_versions AS version
           WHERE NOT EXISTS (
               SELECT 1
               FROM (VALUES
                   ('scn_daily_airport_transport', 1::bigint),
                   ('scn_daily_complaint_help', 1::bigint),
                   ('scn_daily_custom', 1::bigint),
                   ('scn_daily_hotel_checkin_issue', 1::bigint),
                   ('scn_daily_medical_appointment', 1::bigint),
                   ('scn_daily_phone_call', 1::bigint),
                   ('scn_daily_rental_maintenance', 1::bigint),
                   ('scn_daily_restaurant_ordering', 1::bigint),
                   ('scn_daily_shopping_return', 1::bigint),
                   ('scn_daily_small_talk', 1::bigint),
                   ('scn_daily_social_invitation', 1::bigint),
                   ('scn_ielts_speaking_full', 2::bigint),
                   ('scn_ielts_speaking_part_1', 1::bigint),
                   ('scn_ielts_speaking_part_2', 1::bigint),
                   ('scn_ielts_speaking_part_3', 1::bigint),
                   ('scn_interview_behavioral', 1::bigint),
                   ('scn_interview_custom', 1::bigint),
                   ('scn_interview_hiring_manager', 1::bigint),
                   ('scn_interview_recruiter_screening', 1::bigint),
                   ('scn_interview_self_introduction', 1::bigint),
                   ('scn_interview_system_design_spoken', 1::bigint),
                   ('scn_programmer_interview', 1::bigint),
                   ('scn_speaking_exam_custom', 1::bigint),
                   ('scn_workplace_client_delay', 1::bigint),
                   ('scn_workplace_cross_team_alignment', 1::bigint),
                   ('scn_workplace_custom', 1::bigint),
                   ('scn_workplace_feedback_conflict', 1::bigint),
                   ('scn_workplace_meeting_disagreement', 1::bigint),
                   ('scn_workplace_negotiation', 1::bigint),
                   ('scn_workplace_progress_risk_update', 1::bigint),
                   ('scn_workplace_solution_presentation', 1::bigint)
               ) AS builtin(scene_id, scene_version)
               WHERE builtin.scene_id = version.scene_id
                 AND builtin.scene_version = version.scene_version
           )
       ) THEN
        RAISE EXCEPTION
            'Scene authority downgrade only removes the fixed builtin catalog; recreate the development or test database before reverting migration 000050'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TRIGGER coaching_scene_versions_are_immutable
    ON coaching_scene_versions;
DROP FUNCTION reject_coaching_scene_version_mutation();

DROP TABLE coaching_scene_versions;
DROP TABLE coaching_scenes;

DROP FUNCTION coaching_scene_version_payload_is_valid_v1(
    text,
    jsonb,
    jsonb,
    jsonb
);
DROP FUNCTION coaching_scene_practice_objectives_are_valid_v1(jsonb);
DROP FUNCTION coaching_scene_nonempty_string_array_is_valid_v1(jsonb);

COMMIT;
