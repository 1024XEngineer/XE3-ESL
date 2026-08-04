BEGIN;

ALTER TABLE evaluation_ielts_speaking_scene_results
    DROP CONSTRAINT evaluation_ielts_scene_results_payload_check;

ALTER TABLE evaluation_ielts_speaking_scene_results
    ADD CONSTRAINT evaluation_ielts_scene_results_payload_check
        CHECK (
            evaluation_ielts_result_shape_is_valid(result_payload)
            AND NOT jsonb_path_exists(
                result_payload,
                '$.** ? (@.type() == "object").keyvalue() ? (
                    @.key like_regex
                    "^(object[-_]?key|signed[-_]?url|audio[-_]?url|url)$"
                    flag "i"
                )'
            )
        );

DROP FUNCTION evaluation_ielts_result_v4_shape_is_valid(jsonb);

COMMIT;
