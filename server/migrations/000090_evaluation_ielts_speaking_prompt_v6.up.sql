BEGIN;

ALTER TABLE evaluation_ielts_speaking_scene_results
    DROP CONSTRAINT evaluation_ielts_scene_results_prompt_check;

ALTER TABLE evaluation_ielts_speaking_scene_results
    ADD CONSTRAINT evaluation_ielts_scene_results_prompt_check
        CHECK (
            prompt_version IN (
                'ielts-speaking-full-mock-shadow-prompt/v1',
                'ielts-speaking-full-mock-shadow-prompt/v2',
                'ielts-speaking-full-mock-shadow-prompt/v3',
                'ielts-speaking-full-mock-shadow-prompt/v4',
                'ielts-speaking-full-mock-shadow-prompt/v5',
                'ielts-speaking-full-mock-shadow-prompt/v6'
            )
        );

CREATE FUNCTION evaluation_ielts_v6_lineage_is_valid(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    lineage jsonb;
    runs jsonb;
    run jsonb;
    attempts jsonb;
    attempt jsonb;
    run_index integer;
    attempt_index integer;
    expected_criterion text;
    request_id text;
    seen_request_ids text[] := ARRAY[]::text[];
BEGIN
    lineage := payload -> 'provider_lineage';
    IF jsonb_typeof(lineage) IS DISTINCT FROM 'object'
       OR lineage ? 'request_id'
       OR NOT (
            lineage ?& ARRAY[
                'provider',
                'model',
                'prompt_version',
                'response_schema',
                'rubric_version',
                'criterion_runs'
            ]
       )
       OR lineage - ARRAY[
            'provider',
            'model',
            'prompt_version',
            'response_schema',
            'rubric_version',
            'criterion_runs'
       ] <> '{}'::jsonb
       OR jsonb_typeof(lineage -> 'provider') IS DISTINCT FROM 'string'
       OR jsonb_typeof(lineage -> 'model') IS DISTINCT FROM 'string'
       OR lineage ->> 'prompt_version'
            IS DISTINCT FROM 'ielts-speaking-full-mock-shadow-prompt/v6'
       OR lineage ->> 'response_schema'
            IS DISTINCT FROM 'ielts-speaking-full-mock-shadow-provider/v3'
       OR lineage ->> 'rubric_version'
            IS DISTINCT FROM 'ielts-speaking-public-band-rubric/v2'
       OR jsonb_typeof(lineage -> 'criterion_runs')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(lineage -> 'criterion_runs') NOT IN (3, 4)
       OR jsonb_array_length(lineage -> 'criterion_runs') <> (
            CASE
                WHEN (
                    payload #>> '{criteria,3,scoreability_status}'
                ) = 'PROVISIONAL'
                THEN 4
                ELSE 3
            END
       )
    THEN
        RETURN false;
    END IF;

    runs := lineage -> 'criterion_runs';
    FOR run_index IN 0..jsonb_array_length(runs) - 1 LOOP
        run := runs -> run_index;
        expected_criterion := CASE run_index
            WHEN 0 THEN 'IELTS_FC'
            WHEN 1 THEN 'IELTS_LR'
            WHEN 2 THEN 'IELTS_GRA'
            ELSE 'IELTS_PR'
        END;
        IF jsonb_typeof(run) IS DISTINCT FROM 'object'
           OR NOT (run ?& ARRAY['criterion_id', 'attempts'])
           OR run - ARRAY['criterion_id', 'attempts'] <> '{}'::jsonb
           OR run ->> 'criterion_id' IS DISTINCT FROM expected_criterion
           OR jsonb_typeof(run -> 'attempts') IS DISTINCT FROM 'array'
           OR jsonb_array_length(run -> 'attempts') NOT BETWEEN 1 AND 2
        THEN
            RETURN false;
        END IF;

        attempts := run -> 'attempts';
        FOR attempt_index IN 0..jsonb_array_length(attempts) - 1 LOOP
            attempt := attempts -> attempt_index;
            request_id := attempt ->> 'request_id';
            IF jsonb_typeof(attempt) IS DISTINCT FROM 'object'
               OR NOT (
                    attempt ?& ARRAY[
                        'sequence',
                        'kind',
                        'request_id',
                        'outcome'
                    ]
               )
               OR attempt - ARRAY[
                    'sequence',
                    'kind',
                    'request_id',
                    'outcome',
                    'rejection_stage',
                    'rejection_code'
               ] <> '{}'::jsonb
               OR jsonb_typeof(attempt -> 'sequence')
                    IS DISTINCT FROM 'number'
               OR (attempt ->> 'sequence')::numeric <>
                    trunc((attempt ->> 'sequence')::numeric)
               OR (attempt ->> 'sequence')::integer <> attempt_index + 1
               OR jsonb_typeof(attempt -> 'kind')
                    IS DISTINCT FROM 'string'
               OR jsonb_typeof(attempt -> 'request_id')
                    IS DISTINCT FROM 'string'
               OR octet_length(request_id) NOT BETWEEN 1 AND 128
               OR request_id <> btrim(request_id)
               OR request_id ~ '^[[:space:]]'
               OR request_id ~ '[[:space:]]$'
               OR request_id = ANY(seen_request_ids)
               OR jsonb_typeof(attempt -> 'outcome')
                    IS DISTINCT FROM 'string'
            THEN
                RETURN false;
            END IF;
            seen_request_ids := array_append(seen_request_ids, request_id);

            IF jsonb_array_length(attempts) = 1 THEN
                IF attempt_index <> 0
                   OR attempt ->> 'kind' IS DISTINCT FROM 'INITIAL'
                   OR attempt ->> 'outcome' IS DISTINCT FROM 'ACCEPTED'
                   OR attempt ? 'rejection_stage'
                   OR attempt ? 'rejection_code'
                THEN
                    RETURN false;
                END IF;
            ELSIF attempt_index = 0 THEN
                IF attempt ->> 'kind' IS DISTINCT FROM 'INITIAL'
                   OR attempt ->> 'outcome' IS DISTINCT FROM 'REJECTED'
                   OR jsonb_typeof(attempt -> 'rejection_stage')
                        IS DISTINCT FROM 'string'
                   OR attempt ->> 'rejection_stage' NOT IN (
                        'json_decode',
                        'schema_validation',
                        'semantic_validation'
                   )
                   OR jsonb_typeof(attempt -> 'rejection_code')
                        IS DISTINCT FROM 'string'
                   OR attempt ->> 'rejection_code' NOT IN (
                        'invalid_json',
                        'invalid_shape',
                        'wrong_criterion',
                        'invalid_rubric_descriptor',
                        'invalid_finding_collections',
                        'no_primary_findings',
                        'missing_evidence',
                        'invalid_criterion'
                   )
                THEN
                    RETURN false;
                END IF;
            ELSIF attempt ->> 'kind' IS DISTINCT FROM 'REPAIR'
                  OR attempt ->> 'outcome' IS DISTINCT FROM 'ACCEPTED'
                  OR attempt ? 'rejection_stage'
                  OR attempt ? 'rejection_code'
            THEN
                RETURN false;
            END IF;
        END LOOP;
    END LOOP;
    RETURN true;
EXCEPTION
    WHEN others THEN
        RETURN false;
END;
$$;

ALTER TABLE evaluation_ielts_speaking_scene_results
    ADD CONSTRAINT evaluation_ielts_scene_results_v6_lineage_check
        CHECK (
            prompt_version <>
                'ielts-speaking-full-mock-shadow-prompt/v6'
            OR (
                provider_request_id IS NULL
                AND (
                    (
                        result_payload ->> 'scoreability_status' =
                            'INSUFFICIENT'
                        AND NOT result_payload ? 'provider_lineage'
                    )
                    OR (
                        result_payload ->> 'scoreability_status' =
                            'PROVISIONAL'
                        AND (
                            result_payload #>>
                                '{provider_lineage,provider}'
                        ) IS NOT DISTINCT FROM provider
                        AND (
                            result_payload #>>
                                '{provider_lineage,model}'
                        ) IS NOT DISTINCT FROM model
                        AND evaluation_ielts_v6_lineage_is_valid(
                            result_payload
                        )
                    )
                )
            )
        );

COMMIT;
