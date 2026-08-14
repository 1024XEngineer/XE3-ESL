BEGIN;

ALTER TABLE evaluation_ielts_speaking_scene_results
    DROP CONSTRAINT evaluation_ielts_scene_results_v9_lineage_check;

DROP FUNCTION evaluation_ielts_v9_lineage_is_valid(jsonb);

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
                'ielts-speaking-full-mock-shadow-prompt/v7',
                'ielts-speaking-full-mock-shadow-prompt/v8'
            )
        );

COMMIT;
