BEGIN;

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_model_configuration_check,
    ADD CONSTRAINT agent_runs_model_configuration_check CHECK (
        jsonb_typeof(model_configuration) = 'object'
        AND model_configuration ?& ARRAY[
            'provider', 'model', 'max_output_tokens', 'max_input_characters'
        ]
        AND model_configuration - ARRAY[
            'provider', 'model', 'max_output_tokens', 'max_input_characters'
        ] = '{}'::jsonb
        AND model_configuration->>'provider' ~ '^[a-z][a-z0-9_-]{0,63}$'
        AND model_configuration->>'model' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        AND (model_configuration->>'max_output_tokens')::integer BETWEEN 1 AND 1000000
        AND (model_configuration->>'max_input_characters')::integer BETWEEN 5000 AND 1000000
    ),
    DROP CONSTRAINT agent_runs_json_shape_check,
    ADD CONSTRAINT agent_runs_json_shape_check CHECK (
        (context_snapshot IS NULL OR jsonb_typeof(context_snapshot) = 'object')
        AND jsonb_typeof(tool_trace) = 'array'
        AND jsonb_array_length(tool_trace) <= 4
        AND octet_length(tool_trace::text) <= 524288
        AND (
            model_result IS NULL OR (
                jsonb_typeof(model_result) = 'object'
                AND model_result ?& ARRAY['completion_id', 'provider', 'model', 'finish_reason']
                AND model_result - ARRAY[
                    'completion_id', 'provider', 'model', 'finish_reason'
                ] = '{}'::jsonb
                AND model_result->>'completion_id' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
                AND model_result->>'provider' ~ '^[a-z][a-z0-9_-]{0,63}$'
                AND model_result->>'model' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
                AND model_result->>'finish_reason' IN ('stop', 'length')
            )
        )
        AND (
            usage IS NULL OR (
                jsonb_typeof(usage) = 'object'
                AND usage ?& ARRAY['input_tokens', 'output_tokens', 'total_tokens']
                AND usage - ARRAY['input_tokens', 'output_tokens', 'total_tokens'] = '{}'::jsonb
                AND (usage->>'input_tokens')::integer >= 0
                AND (usage->>'output_tokens')::integer >= 0
                AND (usage->>'total_tokens')::integer >= 0
            )
        )
        AND (
            error IS NULL OR (
                jsonb_typeof(error) = 'object'
                AND error ?& ARRAY['kind', 'retryable']
                AND error - ARRAY['kind', 'retryable'] = '{}'::jsonb
                AND error->>'kind' ~ '^[a-z][a-z0-9_]{0,63}$'
                AND jsonb_typeof(error->'retryable') = 'boolean'
            )
        )
    );

COMMIT;
