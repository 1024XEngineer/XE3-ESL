BEGIN;

DROP TRIGGER evaluation_module_runs_binding
    ON evaluation_module_runs;
DROP FUNCTION evaluation_assert_interview_shadow_run_binding();

ALTER TABLE evaluation_module_runs
    DROP CONSTRAINT evaluation_module_runs_scene_check,
    ADD CONSTRAINT evaluation_module_runs_scene_check
        CHECK (scene_type IN ('INTERVIEW', 'IELTS_SPEAKING'));

CREATE FUNCTION evaluation_assert_scene_job_run_binding()
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

CREATE TRIGGER evaluation_module_runs_binding
BEFORE INSERT ON evaluation_module_runs
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_scene_job_run_binding();

CREATE FUNCTION evaluation_ielts_result_shape_is_valid(payload jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    criterion jsonb;
    question jsonb;
    criterion_index integer;
    question_index integer;
    expected_criterion text;
    expected_part text;
    status text;
BEGIN
    IF jsonb_typeof(payload) IS DISTINCT FROM 'object'
       OR payload ->> 'schema_version'
            IS DISTINCT FROM 'ielts-speaking-full-mock-shadow/v1'
       OR payload ->> 'scene_type' IS DISTINCT FROM 'IELTS_SPEAKING'
       OR payload ->> 'scope' IS DISTINCT FROM 'SESSION'
       OR payload ->> 'channel' IS DISTINCT FROM 'SCENE'
       OR jsonb_typeof(payload -> 'snapshot_id')
            IS DISTINCT FROM 'string'
       OR octet_length(payload ->> 'snapshot_id') NOT BETWEEN 1 AND 128
       OR payload ->> 'snapshot_id' <> btrim(payload ->> 'snapshot_id')
       OR (
            payload ->> 'scoreability_status'
                IS DISTINCT FROM 'PROVISIONAL'
            AND payload ->> 'scoreability_status'
                IS DISTINCT FROM 'INSUFFICIENT'
       )
       OR (
            payload ->> 'scoreability_status' = 'PROVISIONAL'
            AND payload ->> 'gate_status'
                IS DISTINCT FROM 'FEEDBACK_ONLY'
       )
       OR (
            payload ->> 'scoreability_status' = 'INSUFFICIENT'
            AND payload ->> 'gate_status' IS DISTINCT FROM 'BLOCKED'
       )
       OR jsonb_typeof(payload -> 'reason_codes')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'reason_codes') < 1
       OR jsonb_typeof(payload -> 'criteria') IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'criteria') <> 4
       OR jsonb_typeof(payload -> 'question_results')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(payload -> 'question_results') <> 14
       OR payload ? 'overall'
       OR payload ? 'speaking_overall'
       OR jsonb_path_exists(
            payload,
            '$.** ? (@.type() == "object").keyvalue() ? (
                @.key like_regex
                "^(score|band|overall|speaking[-_]?overall|weight|weights|raw|display|interval|total)$"
                flag "i"
            )'
       )
    THEN
        RETURN false;
    END IF;

    FOR criterion_index IN 0..3 LOOP
        criterion := payload -> 'criteria' -> criterion_index;
        expected_criterion := CASE criterion_index
            WHEN 0 THEN 'IELTS_FC'
            WHEN 1 THEN 'IELTS_LR'
            WHEN 2 THEN 'IELTS_GRA'
            ELSE 'IELTS_PR'
        END;
        status := criterion ->> 'scoreability_status';
        IF jsonb_typeof(criterion) IS DISTINCT FROM 'object'
           OR criterion ->> 'criterion_id'
                IS DISTINCT FROM expected_criterion
           OR (
                status IS DISTINCT FROM 'PROVISIONAL'
                AND status IS DISTINCT FROM 'INSUFFICIENT'
           )
           OR (
                payload ->> 'scoreability_status' = 'PROVISIONAL'
                AND (
                    (
                        expected_criterion = 'IELTS_PR'
                        AND status IS DISTINCT FROM 'INSUFFICIENT'
                    )
                    OR (
                        expected_criterion <> 'IELTS_PR'
                        AND status IS DISTINCT FROM 'PROVISIONAL'
                    )
                )
           )
           OR (
                payload ->> 'scoreability_status' = 'INSUFFICIENT'
                AND status IS DISTINCT FROM 'INSUFFICIENT'
           )
           OR (
                status = 'PROVISIONAL'
                AND criterion ->> 'gate_status'
                    IS DISTINCT FROM 'FEEDBACK_ONLY'
           )
           OR (
                status = 'INSUFFICIENT'
                AND criterion ->> 'gate_status'
                    IS DISTINCT FROM 'BLOCKED'
           )
           OR jsonb_typeof(criterion -> 'reason_codes')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(criterion -> 'reason_codes') < 1
           OR jsonb_typeof(criterion -> 'evidence_ref_ids')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'strengths')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'improvements')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(criterion -> 'upgrade_examples')
                IS DISTINCT FROM 'array'
        THEN
            RETURN false;
        END IF;

        IF status = 'INSUFFICIENT' THEN
            IF criterion ? 'estimated_band'
               OR criterion ? 'band_descriptor'
               OR jsonb_array_length(
                    criterion -> 'evidence_ref_ids'
               ) <> 0
               OR jsonb_array_length(criterion -> 'strengths') <> 0
               OR jsonb_array_length(criterion -> 'improvements') <> 0
               OR jsonb_array_length(
                    criterion -> 'upgrade_examples'
               ) <> 0
            THEN
                RETURN false;
            END IF;
        ELSIF expected_criterion IN ('IELTS_LR', 'IELTS_GRA') THEN
            IF jsonb_typeof(criterion -> 'estimated_band')
                    IS DISTINCT FROM 'number'
               OR (criterion ->> 'estimated_band')::numeric
                    <> trunc((criterion ->> 'estimated_band')::numeric)
               OR (criterion ->> 'estimated_band')::integer
                    NOT BETWEEN 1 AND 9
               OR jsonb_typeof(criterion -> 'band_descriptor')
                    IS DISTINCT FROM 'string'
               OR octet_length(criterion ->> 'band_descriptor') < 1
               OR jsonb_array_length(
                    criterion -> 'evidence_ref_ids'
               ) < 1
            THEN
                RETURN false;
            END IF;
        ELSE
            IF criterion ? 'estimated_band'
               OR criterion ? 'band_descriptor'
               OR expected_criterion = 'IELTS_PR'
            THEN
                RETURN false;
            END IF;
        END IF;
    END LOOP;

    criterion := payload -> 'criteria' -> 3;
    IF criterion ->> 'scoreability_status'
            IS DISTINCT FROM 'INSUFFICIENT'
       OR criterion ->> 'gate_status' IS DISTINCT FROM 'BLOCKED'
       OR jsonb_array_length(criterion -> 'reason_codes') <> 1
       OR NOT (criterion -> 'reason_codes'
            @> '["PRONUNCIATION_ARTIFACT_UNAVAILABLE"]'::jsonb)
    THEN
        RETURN false;
    END IF;

    FOR question_index IN 0..13 LOOP
        question := payload -> 'question_results' -> question_index;
        expected_part := CASE
            WHEN question_index < 8 THEN 'PART_1'
            WHEN question_index = 8 THEN 'PART_2'
            ELSE 'PART_3'
        END;
        IF jsonb_typeof(question) IS DISTINCT FROM 'object'
           OR (question ->> 'index')::integer
                IS DISTINCT FROM question_index + 1
           OR question ->> 'part_id' IS DISTINCT FROM expected_part
           OR (
                question ->> 'opportunity_status'
                    IS DISTINCT FROM 'PROVIDED'
                AND question ->> 'opportunity_status'
                    IS DISTINCT FROM 'NOT_PROVIDED'
           )
           OR (
                question ->> 'opportunity_status' = 'PROVIDED'
                AND question ->> 'assessment_status'
                    IS DISTINCT FROM 'ASSESSED'
           )
           OR (
                question ->> 'opportunity_status' = 'NOT_PROVIDED'
                AND question ->> 'assessment_status'
                    IS DISTINCT FROM 'NOT_ASSESSED'
           )
           OR jsonb_typeof(question -> 'evidence_ref_ids')
                IS DISTINCT FROM 'array'
           OR jsonb_typeof(question -> 'criterion_findings')
                IS DISTINCT FROM 'array'
           OR jsonb_array_length(
                question -> 'criterion_findings'
           ) <> 4
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

CREATE TABLE evaluation_ielts_speaking_scene_results (
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
    CONSTRAINT evaluation_ielts_scene_results_run_fkey
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
    CONSTRAINT evaluation_ielts_scene_results_logical_unique
        UNIQUE (evaluation_revision_id, channel, strategy_ref),
    CONSTRAINT evaluation_ielts_scene_results_run_unique
        UNIQUE (module_run_id),
    CONSTRAINT evaluation_ielts_scene_results_channel_check
        CHECK (channel = 'SCENE'),
    CONSTRAINT evaluation_ielts_scene_results_strategy_check
        CHECK (
            strategy_ref = 'ielts-speaking-full-mock-shadow/v1'
        ),
    CONSTRAINT evaluation_ielts_scene_results_scene_check
        CHECK (scene_type = 'IELTS_SPEAKING'),
    CONSTRAINT evaluation_ielts_scene_results_revision_check
        CHECK (input_revision > 0),
    CONSTRAINT evaluation_ielts_scene_results_snapshot_hash_check
        CHECK (octet_length(snapshot_hash) = 32),
    CONSTRAINT evaluation_ielts_scene_results_config_hash_check
        CHECK (
            octet_length(full_config_hash) = 32
            AND full_config_hash <> decode(repeat('00', 32), 'hex')
        ),
    CONSTRAINT evaluation_ielts_scene_results_prompt_check
        CHECK (
            prompt_version =
                'ielts-speaking-full-mock-shadow-prompt/v1'
        ),
    CONSTRAINT evaluation_ielts_scene_results_request_id_check
        CHECK (
            provider_request_id IS NULL
            OR (
                octet_length(provider_request_id) BETWEEN 1 AND 128
                AND provider_request_id = btrim(provider_request_id)
            )
        ),
    CONSTRAINT evaluation_ielts_scene_results_fencing_check
        CHECK (fencing_token > 0),
    CONSTRAINT evaluation_ielts_scene_results_payload_check
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
        )
);

CREATE INDEX evaluation_ielts_scene_results_owner_created_idx
    ON evaluation_ielts_speaking_scene_results (
        owner_user_id,
        created_at DESC,
        id DESC
    );

CREATE FUNCTION evaluation_assert_ielts_scene_result_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    bound_run evaluation_module_runs%ROWTYPE;
BEGIN
    SELECT run.*
      INTO bound_run
      FROM evaluation_module_runs AS run
      JOIN evaluation_outbox AS outbox
        ON outbox.id = run.outbox_id
       AND outbox.evaluation_id = run.evaluation_id
       AND outbox.evaluation_revision_id =
            run.evaluation_revision_id
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
     WHERE run.id = NEW.module_run_id
       AND run.run_status = 'RUNNING'
       AND run.scene_type = 'IELTS_SPEAKING'
       AND run.strategy_ref =
            'ielts-speaking-full-mock-shadow/v1'
       AND revision.pipeline_version =
            'evaluation-pipeline-shadow/v1'
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
     FOR SHARE OF run, outbox, owner, revision, state;

    IF NOT FOUND
       OR NEW.evaluation_id <> bound_run.evaluation_id
       OR NEW.evaluation_revision_id
            <> bound_run.evaluation_revision_id
       OR NEW.owner_user_id <> bound_run.owner_user_id
       OR NEW.channel <> bound_run.channel
       OR NEW.strategy_ref <> bound_run.strategy_ref
       OR NEW.practice_session_id <> bound_run.practice_session_id
       OR NEW.input_snapshot_id <> bound_run.input_snapshot_id
       OR NEW.input_revision <> bound_run.input_revision
       OR NEW.scene_type <> bound_run.scene_type
       OR NEW.snapshot_hash <> bound_run.snapshot_hash
       OR NEW.full_config_hash <> bound_run.full_config_hash
       OR NEW.prompt_version <> bound_run.prompt_version
       OR NEW.provider <> bound_run.provider
       OR NEW.model <> bound_run.model
       OR NEW.fencing_token <> bound_run.fencing_token
       OR NEW.result_payload ->> 'snapshot_id'
            IS DISTINCT FROM NEW.input_snapshot_id
       OR NOT evaluation_interview_result_refs_are_consistent(
           NEW.input_snapshot_id,
           NEW.result_payload
       )
    THEN
        RAISE EXCEPTION 'invalid IELTS Speaking Shadow result binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_ielts_scene_results_binding
BEFORE INSERT ON evaluation_ielts_speaking_scene_results
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_ielts_scene_result_binding();

CREATE FUNCTION reject_evaluation_ielts_scene_result_mutation()
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
    RAISE EXCEPTION 'evaluation IELTS scene results are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER evaluation_ielts_scene_results_immutable
BEFORE UPDATE OR DELETE ON evaluation_ielts_speaking_scene_results
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_ielts_scene_result_mutation();

COMMIT;
