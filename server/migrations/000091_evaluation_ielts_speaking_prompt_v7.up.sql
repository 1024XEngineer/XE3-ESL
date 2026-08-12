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
                'ielts-speaking-full-mock-shadow-prompt/v6',
                'ielts-speaking-full-mock-shadow-prompt/v7'
            )
        );

CREATE FUNCTION evaluation_ielts_v7_lineage_is_valid(payload jsonb)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
STRICT
AS $$
    SELECT
        payload #>> '{provider_lineage,prompt_version}' =
            'ielts-speaking-full-mock-shadow-prompt/v7'
        AND evaluation_ielts_v6_lineage_is_valid(
            jsonb_set(
                payload,
                '{provider_lineage,prompt_version}',
                '"ielts-speaking-full-mock-shadow-prompt/v6"'::jsonb
            )
        );
$$;

ALTER TABLE evaluation_ielts_speaking_scene_results
    ADD CONSTRAINT evaluation_ielts_scene_results_v7_lineage_check
        CHECK (
            prompt_version <>
                'ielts-speaking-full-mock-shadow-prompt/v7'
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
                        AND evaluation_ielts_v7_lineage_is_valid(
                            result_payload
                        )
                    )
                )
            )
        );

COMMIT;
