BEGIN;

CREATE FUNCTION evaluation_ielts_result_v4_shape_is_valid(payload jsonb)
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
    DROP CONSTRAINT evaluation_ielts_scene_results_payload_check;

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

COMMIT;
