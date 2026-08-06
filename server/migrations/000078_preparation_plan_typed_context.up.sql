BEGIN;

CREATE FUNCTION preparation_resolved_context_is_valid_v1(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    kind text;
    scenario jsonb;
    interview jsonb;
    job_target jsonb;
    resume jsonb;
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'object'
       OR octet_length(payload::text) > 65536
       OR jsonb_typeof(payload -> 'kind') <> 'string' THEN
        RETURN false;
    END IF;

    kind := payload ->> 'kind';
    IF kind = 'scenario' THEN
        IF payload - ARRAY['kind', 'scenario'] <> '{}'::jsonb
           OR NOT payload ?& ARRAY['kind', 'scenario']
           OR jsonb_typeof(payload -> 'scenario') <> 'object' THEN
            RETURN false;
        END IF;
        scenario := payload -> 'scenario';
        IF scenario - ARRAY[
               'situation',
               'user_role',
               'counterpart_role',
               'goal',
               'counterpart_persona'
           ] <> '{}'::jsonb
           OR NOT scenario ?& ARRAY[
               'situation',
               'user_role',
               'counterpart_role',
               'goal',
               'counterpart_persona'
           ] THEN
            RETURN false;
        END IF;
        RETURN (
            SELECT bool_and(
                jsonb_typeof(scenario -> field_name) = 'string'
                AND scenario ->> field_name <> ''
                AND scenario ->> field_name = btrim(
                    scenario ->> field_name
                )
                AND octet_length(scenario ->> field_name) <= 16384
            )
            FROM unnest(ARRAY[
                'situation',
                'user_role',
                'counterpart_role',
                'goal',
                'counterpart_persona'
            ]) AS field_name
        );
    END IF;

    IF kind <> 'interview'
       OR payload - ARRAY['kind', 'interview'] <> '{}'::jsonb
       OR NOT payload ?& ARRAY['kind', 'interview']
       OR jsonb_typeof(payload -> 'interview') <> 'object' THEN
        RETURN false;
    END IF;
    interview := payload -> 'interview';
    IF interview - ARRAY['resume', 'job_target'] <> '{}'::jsonb
       OR NOT interview ? 'job_target'
       OR jsonb_typeof(interview -> 'job_target') <> 'object' THEN
        RETURN false;
    END IF;
    job_target := interview -> 'job_target';
    IF job_target - ARRAY[
           'job_target_id',
           'confirmation_version'
       ] <> '{}'::jsonb
       OR NOT job_target ?& ARRAY[
           'job_target_id',
           'confirmation_version'
       ]
       OR jsonb_typeof(job_target -> 'job_target_id') <> 'string'
       OR job_target ->> 'job_target_id' !~
           '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
       OR jsonb_typeof(job_target -> 'confirmation_version') <> 'number'
       OR job_target ->> 'confirmation_version' !~
           '^[1-9][0-9]{0,8}$' THEN
        RETURN false;
    END IF;
    IF NOT interview ? 'resume' THEN
        RETURN true;
    END IF;
    IF jsonb_typeof(interview -> 'resume') <> 'object' THEN
        RETURN false;
    END IF;
    resume := interview -> 'resume';
    RETURN resume - ARRAY['resume_id', 'revision'] = '{}'::jsonb
       AND resume ?& ARRAY['resume_id', 'revision']
       AND jsonb_typeof(resume -> 'resume_id') = 'string'
       AND resume ->> 'resume_id' ~
           '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
       AND jsonb_typeof(resume -> 'revision') = 'number'
       AND resume ->> 'revision' ~ '^[1-9][0-9]{0,18}$';
END;
$$;

CREATE OR REPLACE FUNCTION preparation_plan_snapshot_is_valid_v1(
    expected_snapshot_id text,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    has_job_target boolean;
BEGIN
    IF payload IS NULL
       OR jsonb_typeof(payload) <> 'object'
       OR NOT payload ?& ARRAY[
           'preparation_snapshot_id',
           'source_profile_id',
           'source_version',
           'background_snapshot',
           'created_at'
       ]
       OR payload - ARRAY[
           'preparation_snapshot_id',
           'source_profile_id',
           'source_version',
           'preparation_context',
           'source_job_target_id',
           'source_job_target_confirmation_version',
           'job_target_input_snapshot',
           'job_target_candidate_snapshot',
           'resume_snapshot',
           'job_description_snapshot',
           'background_snapshot',
           'created_at'
       ] <> '{}'::jsonb
       OR jsonb_typeof(payload -> 'preparation_snapshot_id') <> 'string'
       OR payload ->> 'preparation_snapshot_id' <> expected_snapshot_id
       OR jsonb_typeof(payload -> 'source_profile_id') <> 'string'
       OR payload ->> 'source_profile_id' !~
           '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
       OR jsonb_typeof(payload -> 'source_version') <> 'number'
       OR (payload ->> 'source_version') !~ '^[1-9][0-9]{0,8}$'
       OR jsonb_typeof(payload -> 'background_snapshot') <> 'string'
       OR payload ->> 'background_snapshot' = ''
       OR payload ->> 'background_snapshot' <>
           btrim(payload ->> 'background_snapshot')
       OR jsonb_typeof(payload -> 'created_at') <> 'string'
       OR payload ->> 'created_at' = '' THEN
        RETURN false;
    END IF;

    IF payload ? 'preparation_context'
       AND NOT preparation_resolved_context_is_valid_v1(
           payload -> 'preparation_context'
       ) THEN
        RETURN false;
    END IF;

    IF payload ? 'resume_snapshot'
       AND (
           jsonb_typeof(payload -> 'resume_snapshot') <> 'string'
           OR payload ->> 'resume_snapshot' = ''
       ) THEN
        RETURN false;
    END IF;

    IF payload ? 'job_description_snapshot'
       AND (
           jsonb_typeof(payload -> 'job_description_snapshot') <> 'string'
           OR payload ->> 'job_description_snapshot' = ''
       ) THEN
        RETURN false;
    END IF;

    has_job_target := payload ? 'source_job_target_id';
    IF has_job_target <>
           (payload ? 'source_job_target_confirmation_version')
       OR has_job_target <> (payload ? 'job_target_input_snapshot')
       OR has_job_target <> (payload ? 'job_target_candidate_snapshot') THEN
        RETURN false;
    END IF;

    IF has_job_target
       AND (
           jsonb_typeof(payload -> 'source_job_target_id') <> 'string'
           OR payload ->> 'source_job_target_id' !~
               '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
           OR jsonb_typeof(
               payload -> 'source_job_target_confirmation_version'
           ) <> 'number'
           OR (payload ->> 'source_job_target_confirmation_version') !~
               '^[1-9][0-9]{0,8}$'
           OR jsonb_typeof(payload -> 'job_target_input_snapshot') <>
               'object'
           OR jsonb_typeof(payload -> 'job_target_candidate_snapshot') <>
               'object'
       ) THEN
        RETURN false;
    END IF;

    RETURN true;
END;
$$;

COMMIT;
