BEGIN;

ALTER TABLE evaluation_module_runs
    DROP CONSTRAINT evaluation_module_runs_scene_check,
    ADD CONSTRAINT evaluation_module_runs_scene_check
        CHECK (
            scene_type IN (
                'INTERVIEW',
                'IELTS_SPEAKING',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
        );

CREATE OR REPLACE FUNCTION evaluation_assert_scene_job_run_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    bound_record record;
BEGIN
    SELECT
        ledger.practice_session_id,
        ledger.input_snapshot_id,
        ledger.input_revision,
        ledger.scope,
        ledger.scene_type,
        revision.scene_strategy_ref,
        revision.pipeline_version,
        snapshot.snapshot_hash,
        outbox.channel,
        outbox.attempt_count,
        outbox.fencing_token
      INTO bound_record
      FROM evaluation_ledgers AS ledger
      JOIN evaluation_revisions AS revision
        ON revision.evaluation_id = ledger.id
       AND revision.owner_user_id = ledger.owner_user_id
       AND revision.id = NEW.evaluation_revision_id
      JOIN evaluation_evidence_snapshots AS snapshot
        ON snapshot.id = ledger.input_snapshot_id
       AND snapshot.owner_user_id = ledger.owner_user_id
      JOIN evaluation_outbox AS outbox
        ON outbox.id = NEW.outbox_id
       AND outbox.evaluation_id = ledger.id
       AND outbox.evaluation_revision_id = revision.id
       AND outbox.owner_user_id = ledger.owner_user_id
      JOIN identity_users AS owner
        ON owner.id = ledger.owner_user_id
      JOIN evaluation_revision_states AS state
        ON state.evaluation_id = ledger.id
       AND state.revision_id = revision.id
       AND state.owner_user_id = ledger.owner_user_id
     WHERE ledger.id = NEW.evaluation_id
       AND ledger.owner_user_id = NEW.owner_user_id
       AND owner.account_status = 'active'
       AND revision.channels = ARRAY['SCENE']::text[]
       AND (
           (
               ledger.scene_type = 'INTERVIEW'
               AND revision.scene_strategy_ref =
                   'interview-scene-shadow/v1'
               AND revision.pipeline_version =
                   'evaluation-pipeline-shadow/v1'
           )
           OR (
               ledger.scene_type = 'IELTS_SPEAKING'
               AND revision.scene_strategy_ref =
                   'ielts-speaking-full-mock-shadow/v1'
               AND revision.pipeline_version =
                   'evaluation-pipeline-shadow/v1'
           )
           OR (
               ledger.scene_type IN (
                   'IELTS_SPEAKING',
                   'OVERSEAS_DAILY_LIFE',
                   'OVERSEAS_WORKPLACE'
               )
               AND revision.scene_strategy_ref =
                   'general-scene-evaluation/v1'
               AND revision.pipeline_version =
                   'evaluation-pipeline/v1'
           )
       )
       AND state.evaluation_status IN ('QUEUED', 'RUNNING')
       AND outbox.delivery_status = 'PENDING'
       AND outbox.lease_expires_at > clock_timestamp()
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_revisions AS later
           WHERE later.evaluation_id = revision.evaluation_id
             AND later.revision > revision.revision
       )
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_deletion_fences AS fence
           WHERE fence.owner_user_id = ledger.owner_user_id
       )
     FOR SHARE OF ledger, revision, snapshot, outbox, owner, state;

    IF NOT FOUND
       OR NEW.practice_session_id
            <> bound_record.practice_session_id
       OR NEW.input_snapshot_id <> bound_record.input_snapshot_id
       OR NEW.input_revision <> bound_record.input_revision
       OR NEW.scope <> bound_record.scope
       OR NEW.scene_type <> bound_record.scene_type
       OR NEW.channel <> bound_record.channel
       OR NEW.strategy_ref <> bound_record.scene_strategy_ref
       OR NEW.snapshot_hash <> bound_record.snapshot_hash
       OR NEW.attempt_count <> bound_record.attempt_count
       OR NEW.fencing_token <> bound_record.fencing_token
       OR NEW.deletion_generation <> 0
    THEN
        RAISE EXCEPTION 'invalid durable Scene job module run binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION evaluation_general_scene_result_shape_is_valid(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    dimension jsonb;
    dimension_index integer;
    expected_dimension text;
    scoreability text;
BEGIN
    scoreability := payload ->> 'scoreability_status';
    IF jsonb_typeof(payload) IS DISTINCT FROM 'object'
       OR payload ->> 'schema_version'
            IS DISTINCT FROM 'general-scene-evaluation/v1'
       OR payload ->> 'scene_type' NOT IN (
            'IELTS_SPEAKING',
            'OVERSEAS_DAILY_LIFE',
            'OVERSEAS_WORKPLACE'
       )
       OR jsonb_typeof(payload -> 'scene_model')
            IS DISTINCT FROM 'string'
       OR octet_length(payload ->> 'scene_model') NOT BETWEEN 1 AND 128
       OR payload ->> 'scene_model' <> btrim(payload ->> 'scene_model')
       OR payload ->> 'scope' IS DISTINCT FROM 'SESSION'
       OR payload ->> 'channel' IS DISTINCT FROM 'SCENE'
       OR jsonb_typeof(payload -> 'snapshot_id')
            IS DISTINCT FROM 'string'
       OR octet_length(payload ->> 'snapshot_id') NOT BETWEEN 1 AND 128
       OR payload ->> 'snapshot_id' <> btrim(payload ->> 'snapshot_id')
       OR scoreability NOT IN ('PROVISIONAL', 'INSUFFICIENT')
       OR jsonb_typeof(payload -> 'dimensions') IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'dimensions') <> 4
       OR jsonb_typeof(payload -> 'priority_actions')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'priority_actions') > 3
       OR octet_length(payload::text) > 65536
       OR (
            scoreability = 'PROVISIONAL'
            AND jsonb_typeof(payload -> 'provider_lineage')
                IS DISTINCT FROM 'object'
       )
       OR (
            scoreability = 'INSUFFICIENT'
            AND payload ? 'provider_lineage'
       )
    THEN
        RETURN false;
    END IF;

    FOR dimension_index IN 0..3 LOOP
        expected_dimension := CASE dimension_index
            WHEN 0 THEN 'TASK_ACHIEVEMENT'
            WHEN 1 THEN 'CLARITY_COHERENCE'
            WHEN 2 THEN 'LANGUAGE_CONTROL'
            ELSE 'INTERACTION'
        END;
        dimension := payload -> 'dimensions' -> dimension_index;
        IF jsonb_typeof(dimension) IS DISTINCT FROM 'object'
           OR dimension ->> 'key' IS DISTINCT FROM expected_dimension
           OR dimension ->> 'scale' IS DISTINCT FROM 'PERCENTAGE_100'
           OR jsonb_typeof(dimension -> 'coverage')
                IS DISTINCT FROM 'number'
           OR (dimension ->> 'coverage')::numeric NOT BETWEEN 0 AND 1
           OR jsonb_typeof(dimension -> 'confidence')
                IS DISTINCT FROM 'number'
           OR (dimension ->> 'confidence')::numeric NOT BETWEEN 0 AND 1
           OR jsonb_typeof(dimension -> 'reason_codes')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(dimension -> 'evidence_ref_ids')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(dimension -> 'strengths')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(dimension -> 'strengths') > 3
           OR jsonb_typeof(dimension -> 'improvements')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(dimension -> 'improvements') > 3
           OR jsonb_typeof(dimension -> 'recommended_examples')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(dimension -> 'recommended_examples') > 3
           OR (
                scoreability = 'PROVISIONAL'
                AND (
                    jsonb_typeof(dimension -> 'score')
                        IS DISTINCT FROM 'number'
                    OR (dimension ->> 'score')::numeric
                        NOT BETWEEN 0 AND 100
                    OR jsonb_array_length(
                        dimension -> 'evidence_ref_ids'
                    ) < 1
                    OR jsonb_array_length(dimension -> 'strengths')
                        + jsonb_array_length(dimension -> 'improvements') < 1
                )
           )
           OR (
                scoreability = 'INSUFFICIENT'
                AND (
                    dimension ? 'score'
                    OR jsonb_array_length(
                        dimension -> 'evidence_ref_ids'
                    ) <> 0
                    OR jsonb_array_length(dimension -> 'strengths') <> 0
                    OR jsonb_array_length(dimension -> 'improvements') <> 0
                    OR jsonb_array_length(
                        dimension -> 'recommended_examples'
                    ) <> 0
                )
           )
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

CREATE TABLE evaluation_general_scene_results (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    module_run_id uuid NOT NULL,
    evaluation_id uuid NOT NULL,
    evaluation_revision_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    channel text COLLATE "C" NOT NULL,
    strategy_ref text COLLATE "C" NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    input_snapshot_id text COLLATE "C" NOT NULL,
    input_revision integer NOT NULL,
    scene_type text COLLATE "C" NOT NULL,
    snapshot_hash bytea NOT NULL,
    full_config_hash bytea NOT NULL,
    prompt_version text COLLATE "C" NOT NULL,
    provider text COLLATE "C" NOT NULL,
    model text COLLATE "C" NOT NULL,
    provider_request_id text COLLATE "C",
    fencing_token bigint NOT NULL,
    result_payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT evaluation_general_scene_results_run_fkey
        FOREIGN KEY (
            module_run_id,
            evaluation_id,
            evaluation_revision_id,
            owner_user_id,
            channel,
            strategy_ref
        )
        REFERENCES evaluation_module_runs (
            id,
            evaluation_id,
            evaluation_revision_id,
            owner_user_id,
            channel,
            strategy_ref
        )
        ON DELETE CASCADE,
    CONSTRAINT evaluation_general_scene_results_logical_unique
        UNIQUE (evaluation_revision_id, channel, strategy_ref),
    CONSTRAINT evaluation_general_scene_results_run_unique
        UNIQUE (module_run_id),
    CONSTRAINT evaluation_general_scene_results_channel_check
        CHECK (channel = 'SCENE'),
    CONSTRAINT evaluation_general_scene_results_strategy_check
        CHECK (strategy_ref = 'general-scene-evaluation/v1'),
    CONSTRAINT evaluation_general_scene_results_scene_check
        CHECK (
            scene_type IN (
                'IELTS_SPEAKING',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
        ),
    CONSTRAINT evaluation_general_scene_results_revision_check
        CHECK (input_revision > 0),
    CONSTRAINT evaluation_general_scene_results_snapshot_hash_check
        CHECK (octet_length(snapshot_hash) = 32),
    CONSTRAINT evaluation_general_scene_results_config_hash_check
        CHECK (
            octet_length(full_config_hash) = 32
            AND full_config_hash <> decode(repeat('00', 32), 'hex')
        ),
    CONSTRAINT evaluation_general_scene_results_prompt_check
        CHECK (prompt_version = 'general-scene-evaluation-prompt/v1'),
    CONSTRAINT evaluation_general_scene_results_request_id_check
        CHECK (
            provider_request_id IS NULL
            OR (
                octet_length(provider_request_id) BETWEEN 1 AND 128
                AND provider_request_id = btrim(provider_request_id)
            )
        ),
    CONSTRAINT evaluation_general_scene_results_fencing_check
        CHECK (fencing_token > 0),
    CONSTRAINT evaluation_general_scene_results_payload_check
        CHECK (
            evaluation_general_scene_result_shape_is_valid(result_payload)
            AND NOT jsonb_path_exists(
                result_payload,
                '$.** ? (@.type() == "object").keyvalue() ? (
                    @.key like_regex
                    "^(object[-_]?key|signed[-_]?url|audio[-_]?url|url)$"
                    flag "i"
                )'
            )
        )
);

CREATE INDEX evaluation_general_scene_results_owner_created_idx
    ON evaluation_general_scene_results (
        owner_user_id,
        created_at DESC,
        id DESC
    );

CREATE FUNCTION evaluation_assert_general_scene_result_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    bound_record record;
BEGIN
    SELECT
        run.*,
        snapshot.canonical_payload #>>
            '{practice_context,scene_model}' AS scene_model
      INTO bound_record
      FROM evaluation_module_runs AS run
      JOIN evaluation_outbox AS outbox
        ON outbox.id = run.outbox_id
       AND outbox.evaluation_id = run.evaluation_id
       AND outbox.evaluation_revision_id = run.evaluation_revision_id
       AND outbox.owner_user_id = run.owner_user_id
       AND outbox.channel = run.channel
      JOIN identity_users AS owner
        ON owner.id = run.owner_user_id
      JOIN evaluation_revisions AS revision
        ON revision.evaluation_id = run.evaluation_id
       AND revision.id = run.evaluation_revision_id
       AND revision.owner_user_id = run.owner_user_id
      JOIN evaluation_revision_states AS state
        ON state.evaluation_id = run.evaluation_id
       AND state.revision_id = run.evaluation_revision_id
       AND state.owner_user_id = run.owner_user_id
      JOIN evaluation_evidence_snapshots AS snapshot
        ON snapshot.id = run.input_snapshot_id
       AND snapshot.owner_user_id = run.owner_user_id
     WHERE run.id = NEW.module_run_id
       AND run.run_status = 'RUNNING'
       AND run.scene_type IN (
            'IELTS_SPEAKING',
            'OVERSEAS_DAILY_LIFE',
            'OVERSEAS_WORKPLACE'
       )
       AND run.strategy_ref = 'general-scene-evaluation/v1'
       AND revision.pipeline_version = 'evaluation-pipeline/v1'
       AND outbox.delivery_status = 'PENDING'
       AND outbox.fencing_token = NEW.fencing_token
       AND outbox.lease_expires_at > clock_timestamp()
       AND owner.account_status = 'active'
       AND state.evaluation_status = 'RUNNING'
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_revisions AS later
           WHERE later.evaluation_id = revision.evaluation_id
             AND later.revision > revision.revision
       )
       AND NOT EXISTS (
           SELECT 1
           FROM evaluation_deletion_fences AS fence
           WHERE fence.owner_user_id = run.owner_user_id
       )
     FOR SHARE OF run, outbox, owner, revision, state, snapshot;

    IF NOT FOUND
       OR NEW.evaluation_id <> bound_record.evaluation_id
       OR NEW.evaluation_revision_id
            <> bound_record.evaluation_revision_id
       OR NEW.owner_user_id <> bound_record.owner_user_id
       OR NEW.channel <> bound_record.channel
       OR NEW.strategy_ref <> bound_record.strategy_ref
       OR NEW.practice_session_id <> bound_record.practice_session_id
       OR NEW.input_snapshot_id <> bound_record.input_snapshot_id
       OR NEW.input_revision <> bound_record.input_revision
       OR NEW.scene_type <> bound_record.scene_type
       OR NEW.snapshot_hash <> bound_record.snapshot_hash
       OR NEW.full_config_hash <> bound_record.full_config_hash
       OR NEW.prompt_version <> bound_record.prompt_version
       OR NEW.provider <> bound_record.provider
       OR NEW.model <> bound_record.model
       OR NEW.fencing_token <> bound_record.fencing_token
       OR NEW.result_payload ->> 'snapshot_id'
            IS DISTINCT FROM NEW.input_snapshot_id
       OR NEW.result_payload ->> 'scene_type'
            IS DISTINCT FROM NEW.scene_type
       OR NEW.result_payload ->> 'scene_model'
            IS DISTINCT FROM bound_record.scene_model
       OR NOT evaluation_interview_result_refs_are_consistent(
           NEW.input_snapshot_id,
           NEW.result_payload
       )
    THEN
        RAISE EXCEPTION 'invalid general Scene result binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_general_scene_results_binding
BEFORE INSERT ON evaluation_general_scene_results
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_general_scene_result_binding();

CREATE FUNCTION reject_evaluation_general_scene_result_mutation()
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
    RAISE EXCEPTION 'evaluation general Scene results are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER evaluation_general_scene_results_immutable
BEFORE UPDATE OR DELETE ON evaluation_general_scene_results
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_general_scene_result_mutation();

COMMIT;
