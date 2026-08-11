BEGIN;

CREATE FUNCTION evaluation_general_scene_atomic_result_shape_is_valid(
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    dimension jsonb;
    lineage jsonb;
BEGIN
    dimension := payload -> 'dimension';
    lineage := payload -> 'provider_lineage';
    IF jsonb_typeof(payload) IS DISTINCT FROM 'object'
       OR payload ->> 'schema_version' IS DISTINCT FROM
            'general-scene-evaluation-atomic-result/v1'
       OR jsonb_typeof(payload -> 'snapshot_id') IS DISTINCT FROM 'string'
       OR octet_length(payload ->> 'snapshot_id') NOT BETWEEN 1 AND 128
       OR jsonb_typeof(payload -> 'key') IS DISTINCT FROM 'object'
       OR payload #>> '{key,part_id}' NOT IN ('PART_1', 'PART_2', 'PART_3')
       OR payload #>> '{key,dimension_id}' NOT IN (
            'TASK_ACHIEVEMENT',
            'CLARITY_COHERENCE',
            'LANGUAGE_CONTROL',
            'INTERACTION'
       )
       OR jsonb_typeof(dimension) IS DISTINCT FROM 'object'
       OR dimension ->> 'key' IS DISTINCT FROM
            payload #>> '{key,dimension_id}'
       OR jsonb_typeof(dimension -> 'score') IS DISTINCT FROM 'number'
       OR (dimension ->> 'score')::numeric NOT BETWEEN 0 AND 100
       OR dimension ->> 'scale' IS DISTINCT FROM 'PERCENTAGE_100'
       OR jsonb_typeof(dimension -> 'coverage') IS DISTINCT FROM 'number'
       OR (dimension ->> 'coverage')::numeric NOT BETWEEN 0 AND 1
       OR jsonb_typeof(dimension -> 'confidence') IS DISTINCT FROM 'number'
       OR (dimension ->> 'confidence')::numeric NOT BETWEEN 0 AND 1
       OR jsonb_typeof(dimension -> 'reason_codes') IS DISTINCT FROM 'array'
       OR jsonb_typeof(dimension -> 'evidence_ref_ids') IS DISTINCT FROM 'array'
       OR jsonb_typeof(dimension -> 'strengths') IS DISTINCT FROM 'array'
       OR jsonb_array_length(dimension -> 'strengths') > 1
       OR jsonb_typeof(dimension -> 'improvements') IS DISTINCT FROM 'array'
       OR jsonb_array_length(dimension -> 'improvements') > 1
       OR jsonb_typeof(dimension -> 'recommended_examples') IS DISTINCT FROM 'array'
       OR jsonb_array_length(dimension -> 'recommended_examples') > 1
       OR jsonb_array_length(dimension -> 'strengths')
            + jsonb_array_length(dimension -> 'improvements') < 1
       OR jsonb_typeof(lineage) IS DISTINCT FROM 'object'
       OR jsonb_typeof(lineage -> 'provider') IS DISTINCT FROM 'string'
       OR jsonb_typeof(lineage -> 'model') IS DISTINCT FROM 'string'
       OR jsonb_typeof(lineage -> 'request_id') IS DISTINCT FROM 'string'
       OR lineage ->> 'prompt_version' IS DISTINCT FROM
            'general-scene-evaluation-atomic-prompt/v1'
       OR lineage ->> 'response_schema' IS DISTINCT FROM
            'general-scene-evaluation-atomic-provider/v1'
       OR octet_length(payload::text) > 32768
    THEN
        RETURN false;
    END IF;
    RETURN true;
EXCEPTION
    WHEN others THEN
        RETURN false;
END;
$$;

CREATE TABLE evaluation_general_scene_atomic_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    module_run_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    input_snapshot_id text COLLATE "C" NOT NULL,
    part_id text COLLATE "C" NOT NULL,
    dimension_key text COLLATE "C" NOT NULL,
    attempt_count integer NOT NULL,
    fencing_token bigint NOT NULL,
    status text COLLATE "C" NOT NULL,
    provider text COLLATE "C" NOT NULL,
    model text COLLATE "C" NOT NULL,
    provider_request_id text COLLATE "C",
    prompt_version text COLLATE "C" NOT NULL,
    failure_code text COLLATE "C",
    failure_retryable boolean,
    result_payload jsonb,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT evaluation_general_scene_atomic_attempts_run_fkey
        FOREIGN KEY (module_run_id)
        REFERENCES evaluation_module_runs (id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_general_scene_atomic_attempts_identity_unique
        UNIQUE (module_run_id, part_id, dimension_key, attempt_count),
    CONSTRAINT evaluation_general_scene_atomic_attempts_part_check
        CHECK (part_id IN ('PART_1', 'PART_2', 'PART_3')),
    CONSTRAINT evaluation_general_scene_atomic_attempts_dimension_check
        CHECK (
            dimension_key IN (
                'TASK_ACHIEVEMENT',
                'CLARITY_COHERENCE',
                'LANGUAGE_CONTROL',
                'INTERACTION'
            )
        ),
    CONSTRAINT evaluation_general_scene_atomic_attempts_attempt_check
        CHECK (attempt_count > 0 AND fencing_token > 0),
    CONSTRAINT evaluation_general_scene_atomic_attempts_status_check
        CHECK (status IN ('READY', 'FAILED')),
    CONSTRAINT evaluation_general_scene_atomic_attempts_provider_check
        CHECK (
            octet_length(provider) BETWEEN 1 AND 128
            AND provider = btrim(provider)
            AND octet_length(model) BETWEEN 1 AND 256
            AND model = btrim(model)
        ),
    CONSTRAINT evaluation_general_scene_atomic_attempts_request_check
        CHECK (
            provider_request_id IS NULL
            OR (
                octet_length(provider_request_id) BETWEEN 1 AND 128
                AND provider_request_id = btrim(provider_request_id)
            )
        ),
    CONSTRAINT evaluation_general_scene_atomic_attempts_prompt_check
        CHECK (
            prompt_version = 'general-scene-evaluation-atomic-prompt/v1'
        ),
    CONSTRAINT evaluation_general_scene_atomic_attempts_failure_check
        CHECK (
            (status = 'READY'
             AND failure_code IS NULL
             AND failure_retryable IS NULL
             AND provider_request_id IS NOT NULL
             AND result_payload IS NOT NULL
             AND evaluation_general_scene_atomic_result_shape_is_valid(
                 result_payload
             ))
            OR
            (status = 'FAILED'
             AND failure_code ~ '^[a-z][a-z0-9_.:-]{0,127}$'
             AND failure_retryable IS NOT NULL
             AND result_payload IS NULL)
        ),
    CONSTRAINT evaluation_general_scene_atomic_attempts_payload_safety_check
        CHECK (
            result_payload IS NULL
            OR NOT jsonb_path_exists(
                result_payload,
                '$.** ? (@.type() == "object").keyvalue() ? (
                    @.key like_regex
                    "^(object[-_]?key|signed[-_]?url|audio[-_]?url|url)$"
                    flag "i"
                )'
            )
        )
);

CREATE UNIQUE INDEX evaluation_general_scene_atomic_attempts_ready_unique
    ON evaluation_general_scene_atomic_attempts (
        module_run_id,
        part_id,
        dimension_key
    )
    WHERE status = 'READY';

CREATE INDEX evaluation_general_scene_atomic_attempts_owner_created_idx
    ON evaluation_general_scene_atomic_attempts (
        owner_user_id,
        created_at DESC,
        id DESC
    );

CREATE FUNCTION evaluation_assert_general_scene_atomic_attempt_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    bound_record record;
BEGIN
    SELECT
        run.input_snapshot_id,
        run.provider,
        run.model
      INTO bound_record
      FROM evaluation_module_runs AS run
      JOIN evaluation_ledgers AS ledger
        ON ledger.id = run.evaluation_id
       AND ledger.owner_user_id = run.owner_user_id
      JOIN evaluation_revisions AS revision
        ON revision.id = run.evaluation_revision_id
       AND revision.evaluation_id = run.evaluation_id
       AND revision.owner_user_id = run.owner_user_id
      JOIN evaluation_outbox AS outbox
        ON outbox.id = run.outbox_id
       AND outbox.evaluation_id = run.evaluation_id
       AND outbox.evaluation_revision_id = run.evaluation_revision_id
       AND outbox.owner_user_id = run.owner_user_id
      JOIN evaluation_revision_states AS state
        ON state.evaluation_id = run.evaluation_id
       AND state.revision_id = run.evaluation_revision_id
       AND state.owner_user_id = run.owner_user_id
      JOIN evaluation_evidence_snapshots AS snapshot
        ON snapshot.id = run.input_snapshot_id
       AND snapshot.owner_user_id = run.owner_user_id
      JOIN identity_users AS owner
        ON owner.id = run.owner_user_id
     WHERE run.id = NEW.module_run_id
       AND run.owner_user_id = NEW.owner_user_id
       AND run.input_snapshot_id = NEW.input_snapshot_id
       AND run.scene_type = 'IELTS_SPEAKING'
       AND run.strategy_ref = 'general-scene-evaluation/v1'
       AND run.run_status = 'RUNNING'
       AND run.attempt_count = NEW.attempt_count
       AND run.fencing_token = NEW.fencing_token
       AND run.provider = NEW.provider
       AND run.model = NEW.model
       AND outbox.delivery_status = 'PENDING'
       AND outbox.attempt_count = NEW.attempt_count
       AND outbox.fencing_token = NEW.fencing_token
       AND outbox.lease_expires_at > clock_timestamp()
       AND state.evaluation_status = 'RUNNING'
       AND snapshot.scene_type = 'IELTS_SPEAKING'
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_revisions AS later
           WHERE later.evaluation_id = run.evaluation_id
             AND later.owner_user_id = run.owner_user_id
             AND later.revision > revision.revision
       )
       AND EXISTS (
           SELECT 1
           FROM jsonb_array_elements(
               snapshot.canonical_payload #>
                   '{practice_context,ielts_assignment,parts}'
           ) AS assignment_part
           WHERE assignment_part ->> 'part' = NEW.part_id
       )
       AND owner.account_status = 'active'
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_deletion_fences AS fence
           WHERE fence.owner_user_id = run.owner_user_id
       )
     FOR SHARE OF ledger, revision, run, outbox, state, snapshot, owner;

    IF NOT FOUND
       OR (
           NEW.status = 'READY'
           AND (
               NEW.result_payload ->> 'snapshot_id'
                    IS DISTINCT FROM NEW.input_snapshot_id
               OR NEW.result_payload #>> '{key,part_id}'
                    IS DISTINCT FROM NEW.part_id
               OR NEW.result_payload #>> '{key,dimension_id}'
                    IS DISTINCT FROM NEW.dimension_key
               OR NEW.result_payload #>> '{provider_lineage,provider}'
                    IS DISTINCT FROM NEW.provider
               OR NEW.result_payload #>> '{provider_lineage,model}'
                    IS DISTINCT FROM NEW.model
               OR NEW.result_payload #>> '{provider_lineage,request_id}'
                    IS DISTINCT FROM NEW.provider_request_id
               OR NOT evaluation_interview_result_refs_are_consistent(
                   NEW.input_snapshot_id,
                   NEW.result_payload
               )
           )
       )
    THEN
        RAISE EXCEPTION 'invalid general Scene atomic attempt binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_general_scene_atomic_attempts_binding
BEFORE INSERT ON evaluation_general_scene_atomic_attempts
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_general_scene_atomic_attempt_binding();

CREATE FUNCTION reject_evaluation_general_scene_atomic_attempt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owner_status text;
    deletion_fenced boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        SELECT account_status
          INTO owner_status
          FROM identity_users
         WHERE id = OLD.owner_user_id;
        IF owner_status IN ('deleting', 'deleted') THEN
            RETURN OLD;
        END IF;
        SELECT EXISTS (
            SELECT 1
            FROM evaluation_deletion_fences
            WHERE owner_user_id = OLD.owner_user_id
        ) INTO deletion_fenced;
        IF deletion_fenced THEN
            RETURN OLD;
        END IF;
    END IF;
    RAISE EXCEPTION 'evaluation general Scene atomic attempts are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER evaluation_general_scene_atomic_attempts_immutable
BEFORE UPDATE OR DELETE ON evaluation_general_scene_atomic_attempts
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_general_scene_atomic_attempt_mutation();

COMMIT;
