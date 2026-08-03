ALTER TABLE evaluation_ielts_speaking_scene_results
    DROP CONSTRAINT evaluation_ielts_scene_results_prompt_check;

ALTER TABLE evaluation_ielts_speaking_scene_results
    ADD CONSTRAINT evaluation_ielts_scene_results_prompt_check
        CHECK (
            prompt_version =
                'ielts-speaking-full-mock-shadow-prompt/v1'
        );
