BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM preparation_practice_plan_revisions
        WHERE preparation_snapshot ? 'preparation_context'
    ) THEN
        RAISE EXCEPTION
            'cannot remove typed Preparation Plan support while typed Plan revisions exist';
    END IF;
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

DROP FUNCTION preparation_resolved_context_is_valid_v1(jsonb);

COMMIT;
