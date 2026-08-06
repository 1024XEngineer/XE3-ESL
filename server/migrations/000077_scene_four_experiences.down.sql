BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM preparation_practice_plan_revisions
        WHERE scene_selection #>> '{scene,practice_experience}' IN (
            'WORKPLACE', 'LIFE_AND_TRAVEL'
        )
    ) OR EXISTS (
        SELECT 1 FROM practice_sessions
        WHERE practice_experience IN ('WORKPLACE', 'LIFE_AND_TRAVEL')
    ) OR EXISTS (
        SELECT 1 FROM practice_retry_turn_authorizations
        WHERE practice_experience IN ('WORKPLACE', 'LIFE_AND_TRAVEL')
    ) OR EXISTS (
        SELECT 1 FROM evaluation_evidence_snapshots
        WHERE canonical_payload #>>
            '{practice_context,practice_experience}' IN (
                'WORKPLACE', 'LIFE_AND_TRAVEL'
            )
    ) OR EXISTS (
        SELECT 1 FROM evaluation_general_scene_results
        WHERE result_payload ->> 'practice_experience' IN (
            'WORKPLACE', 'LIFE_AND_TRAVEL'
        )
    ) OR EXISTS (
        SELECT 1 FROM evaluation_formal_reports
        WHERE practice_experience IN ('WORKPLACE', 'LIFE_AND_TRAVEL')
    ) THEN
        RAISE EXCEPTION
            'four-Experience practice data must be cleared before rollback';
    END IF;
END;
$$;

ALTER TABLE coaching_scene_versions
    DISABLE TRIGGER coaching_scene_versions_are_immutable;

ALTER TABLE coaching_scene_versions
    DROP CONSTRAINT coaching_scene_versions_experience_category_check;

UPDATE coaching_scene_versions
SET practice_experience = 'ROLEPLAY',
    scene_category = CASE scene_category
        WHEN 'WORKPLACE_GENERAL' THEN 'ROLEPLAY_WORKPLACE'
        WHEN 'LIFE_TRAVEL' THEN 'ROLEPLAY_TRAVEL'
        WHEN 'LIFE_DAILY' THEN 'ROLEPLAY_DAILY'
    END
WHERE (
        practice_experience = 'WORKPLACE'
        AND scene_category = 'WORKPLACE_GENERAL'
    ) OR (
        practice_experience = 'LIFE_AND_TRAVEL'
        AND scene_category IN ('LIFE_TRAVEL', 'LIFE_DAILY')
    );

ALTER TABLE coaching_scene_versions
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
        );

ALTER TABLE coaching_scene_versions
    ENABLE TRIGGER coaching_scene_versions_are_immutable;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_context_fields_check,
    DROP CONSTRAINT practice_sessions_experience_mode_check,
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            btrim(snapshot_id) <> ''
            AND practice_experience IN (
                'INTERVIEW', 'IELTS_SPEAKING', 'ROLEPLAY'
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
                'FULL_SIMULATION', 'FOCUS', 'FULL_MOCK',
                'PART_1', 'PART_2', 'PART_3'
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
                    'FULL_MOCK', 'PART_1', 'PART_2', 'PART_3'
                )
            )
            OR (
                practice_experience = 'ROLEPLAY'
                AND scene_category LIKE 'ROLEPLAY_%'
                AND practice_mode IN ('FULL_SIMULATION', 'FOCUS')
            )
        );

ALTER TABLE practice_retry_turn_authorizations
    DROP CONSTRAINT practice_retry_turn_authorizations_scene_check,
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
    ADD CONSTRAINT evaluation_formal_reports_scene_check
        CHECK (
            scene_type IN (
                'IELTS_SPEAKING', 'INTERVIEW',
                'OVERSEAS_DAILY_LIFE', 'OVERSEAS_WORKPLACE'
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
                        'FULL_MOCK', 'PART_1', 'PART_2', 'PART_3'
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

COMMIT;
