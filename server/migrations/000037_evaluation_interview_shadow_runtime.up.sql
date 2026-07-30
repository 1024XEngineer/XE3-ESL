BEGIN;

DROP INDEX evaluation_outbox_pending_idx;
ALTER TABLE evaluation_outbox
    DROP CONSTRAINT evaluation_outbox_delivery_shape_check,
    DROP CONSTRAINT evaluation_outbox_delivery_status_check;
ALTER TABLE evaluation_outbox
    ADD COLUMN lease_expires_at timestamptz,
    ADD COLUMN fencing_token bigint NOT NULL DEFAULT 0,
    ADD COLUMN last_failure_code text COLLATE "C",
    ADD COLUMN failed_at timestamptz,
    ADD COLUMN updated_at timestamptz NOT NULL
        DEFAULT transaction_timestamp(),
    ADD CONSTRAINT evaluation_outbox_delivery_status_check
        CHECK (delivery_status IN ('PENDING', 'DELIVERED', 'FAILED')),
    ADD CONSTRAINT evaluation_outbox_fencing_token_check
        CHECK (fencing_token >= 0),
    ADD CONSTRAINT evaluation_outbox_failure_code_check
        CHECK (
            last_failure_code IS NULL
            OR last_failure_code ~ '^[a-z][a-z0-9_.:-]{0,127}$'
        ),
    ADD CONSTRAINT evaluation_outbox_delivery_shape_check
        CHECK (
            (
                delivery_status = 'PENDING'
                AND delivered_at IS NULL
                AND failed_at IS NULL
            )
            OR (
                delivery_status = 'DELIVERED'
                AND delivered_at IS NOT NULL
                AND failed_at IS NULL
                AND lease_expires_at IS NULL
            )
            OR (
                delivery_status = 'FAILED'
                AND delivered_at IS NULL
                AND failed_at IS NOT NULL
                AND lease_expires_at IS NULL
            )
        ),
    ADD CONSTRAINT evaluation_outbox_runtime_timestamps_check
        CHECK (
            updated_at >= created_at
            AND (failed_at IS NULL OR failed_at >= created_at)
        );

CREATE INDEX evaluation_outbox_pending_idx
    ON evaluation_outbox (
        available_at,
        lease_expires_at,
        created_at,
        id
    )
    WHERE delivery_status = 'PENDING';

CREATE TABLE evaluation_module_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    outbox_id uuid NOT NULL,
    evaluation_id uuid NOT NULL,
    evaluation_revision_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    channel text COLLATE "C" NOT NULL,
    strategy_ref text COLLATE "C" NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    input_snapshot_id text COLLATE "C" NOT NULL,
    input_revision integer NOT NULL,
    scope text COLLATE "C" NOT NULL,
    scene_type text COLLATE "C" NOT NULL,
    snapshot_hash bytea NOT NULL,
    full_config_hash bytea NOT NULL,
    prompt_version text COLLATE "C" NOT NULL,
    provider text COLLATE "C" NOT NULL,
    model text COLLATE "C" NOT NULL,
    deletion_generation bigint NOT NULL DEFAULT 0,
    run_status text COLLATE "C" NOT NULL DEFAULT 'RUNNING',
    attempt_count integer NOT NULL,
    fencing_token bigint NOT NULL,
    last_failure_code text COLLATE "C",
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    CONSTRAINT evaluation_module_runs_logical_unique
        UNIQUE (evaluation_revision_id, channel, strategy_ref),
    CONSTRAINT evaluation_module_runs_outbox_unique
        UNIQUE (outbox_id),
    CONSTRAINT evaluation_module_runs_identity_unique
        UNIQUE (
            id,
            evaluation_id,
            evaluation_revision_id,
            owner_user_id,
            channel,
            strategy_ref
        ),
    CONSTRAINT evaluation_module_runs_channel_check
        CHECK (channel = 'SCENE'),
    CONSTRAINT evaluation_module_runs_strategy_check
        CHECK (
            strategy_ref ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'
        ),
    CONSTRAINT evaluation_module_runs_practice_session_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT evaluation_module_runs_snapshot_id_check
        CHECK (
            input_snapshot_id ~ '^[A-Za-z][A-Za-z0-9_-]{0,127}$'
        ),
    CONSTRAINT evaluation_module_runs_revision_check
        CHECK (input_revision > 0),
    CONSTRAINT evaluation_module_runs_scope_check
        CHECK (scope = 'SESSION'),
    CONSTRAINT evaluation_module_runs_scene_check
        CHECK (scene_type = 'INTERVIEW'),
    CONSTRAINT evaluation_module_runs_snapshot_hash_check
        CHECK (octet_length(snapshot_hash) = 32),
    CONSTRAINT evaluation_module_runs_config_hash_check
        CHECK (
            octet_length(full_config_hash) = 32
            AND full_config_hash <> decode(repeat('00', 32), 'hex')
        ),
    CONSTRAINT evaluation_module_runs_prompt_check
        CHECK (
            prompt_version ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'
        ),
    CONSTRAINT evaluation_module_runs_provider_check
        CHECK (
            octet_length(provider) BETWEEN 1 AND 128
            AND provider = btrim(provider)
        ),
    CONSTRAINT evaluation_module_runs_model_check
        CHECK (
            octet_length(model) BETWEEN 1 AND 128
            AND model = btrim(model)
        ),
    CONSTRAINT evaluation_module_runs_deletion_generation_check
        CHECK (deletion_generation >= 0),
    CONSTRAINT evaluation_module_runs_status_check
        CHECK (run_status IN ('RUNNING', 'READY', 'FAILED')),
    CONSTRAINT evaluation_module_runs_attempt_check
        CHECK (attempt_count > 0),
    CONSTRAINT evaluation_module_runs_fencing_token_check
        CHECK (fencing_token > 0),
    CONSTRAINT evaluation_module_runs_failure_code_check
        CHECK (
            last_failure_code IS NULL
            OR last_failure_code ~ '^[a-z][a-z0-9_.:-]{0,127}$'
        ),
    CONSTRAINT evaluation_module_runs_completion_check
        CHECK (
            (
                run_status = 'RUNNING'
                AND completed_at IS NULL
            )
            OR (
                run_status IN ('READY', 'FAILED')
                AND completed_at IS NOT NULL
            )
        ),
    CONSTRAINT evaluation_module_runs_timestamps_check
        CHECK (
            updated_at >= created_at
            AND (completed_at IS NULL OR completed_at >= created_at)
        )
);

CREATE INDEX evaluation_module_runs_owner_created_idx
    ON evaluation_module_runs (owner_user_id, created_at DESC, id DESC);

CREATE FUNCTION evaluation_assert_interview_shadow_run_binding()
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
        RAISE EXCEPTION 'invalid Interview Shadow module run binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_module_runs_binding
BEFORE INSERT ON evaluation_module_runs
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_interview_shadow_run_binding();

CREATE FUNCTION reject_evaluation_module_run_identity_mutation()
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
        RAISE EXCEPTION 'evaluation module runs cannot be deleted'
            USING ERRCODE = '55000';
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.outbox_id IS DISTINCT FROM OLD.outbox_id
       OR NEW.evaluation_id IS DISTINCT FROM OLD.evaluation_id
       OR NEW.evaluation_revision_id
            IS DISTINCT FROM OLD.evaluation_revision_id
       OR NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
       OR NEW.channel IS DISTINCT FROM OLD.channel
       OR NEW.strategy_ref IS DISTINCT FROM OLD.strategy_ref
       OR NEW.practice_session_id
            IS DISTINCT FROM OLD.practice_session_id
       OR NEW.input_snapshot_id IS DISTINCT FROM OLD.input_snapshot_id
       OR NEW.input_revision IS DISTINCT FROM OLD.input_revision
       OR NEW.scope IS DISTINCT FROM OLD.scope
       OR NEW.scene_type IS DISTINCT FROM OLD.scene_type
       OR NEW.snapshot_hash IS DISTINCT FROM OLD.snapshot_hash
       OR NEW.full_config_hash IS DISTINCT FROM OLD.full_config_hash
       OR NEW.prompt_version IS DISTINCT FROM OLD.prompt_version
       OR NEW.provider IS DISTINCT FROM OLD.provider
       OR NEW.model IS DISTINCT FROM OLD.model
       OR NEW.deletion_generation
            IS DISTINCT FROM OLD.deletion_generation
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.attempt_count < OLD.attempt_count
       OR NEW.fencing_token < OLD.fencing_token
       OR (
           OLD.run_status IN ('READY', 'FAILED')
           AND NEW.run_status IS DISTINCT FROM OLD.run_status
       )
    THEN
        RAISE EXCEPTION 'evaluation module run identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_module_runs_identity_immutable
BEFORE UPDATE OR DELETE ON evaluation_module_runs
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_module_run_identity_mutation();

CREATE FUNCTION evaluation_interview_result_refs_are_consistent(
    expected_snapshot_id text,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
STABLE
STRICT
AS $$
DECLARE
    reference_list jsonb;
    reference_id jsonb;
BEGIN
    IF jsonb_typeof(payload) <> 'object' THEN
        RETURN false;
    END IF;
    FOR reference_list IN
        SELECT reference_value
        FROM jsonb_path_query(
            payload,
            'lax $.**.evidence_ref_ids'
        ) AS reference_values(reference_value)
    LOOP
        IF jsonb_typeof(reference_list) <> 'array' THEN
            RETURN false;
        END IF;
        FOR reference_id IN
            SELECT value
            FROM jsonb_array_elements(reference_list)
        LOOP
            IF jsonb_typeof(reference_id) <> 'string'
               OR NOT EXISTS (
                   SELECT 1
                   FROM evaluation_evidence_snapshots AS snapshot
                   CROSS JOIN LATERAL jsonb_array_elements(
                       snapshot.canonical_payload -> 'evidence_refs'
                   ) AS evidence_ref
                   WHERE snapshot.id = expected_snapshot_id
                     AND evidence_ref ->> 'evidence_ref_id'
                         = reference_id #>> '{}'
               )
            THEN
                RETURN false;
            END IF;
        END LOOP;
    END LOOP;
    FOR reference_id IN
        SELECT reference_value
        FROM jsonb_path_query(
            payload,
            'lax $.**.evidence_ref_id'
        ) AS reference_values(reference_value)
    LOOP
        IF jsonb_typeof(reference_id) <> 'string'
           OR NOT EXISTS (
               SELECT 1
               FROM evaluation_evidence_snapshots AS snapshot
               CROSS JOIN LATERAL jsonb_array_elements(
                   snapshot.canonical_payload -> 'evidence_refs'
               ) AS evidence_ref
               WHERE snapshot.id = expected_snapshot_id
                 AND evidence_ref ->> 'evidence_ref_id'
                     = reference_id #>> '{}'
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

CREATE TABLE evaluation_interview_scene_results (
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
    CONSTRAINT evaluation_interview_scene_results_run_fkey
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
    CONSTRAINT evaluation_interview_scene_results_logical_unique
        UNIQUE (evaluation_revision_id, channel, strategy_ref),
    CONSTRAINT evaluation_interview_scene_results_run_unique
        UNIQUE (module_run_id),
    CONSTRAINT evaluation_interview_scene_results_channel_check
        CHECK (channel = 'SCENE'),
    CONSTRAINT evaluation_interview_scene_results_scene_check
        CHECK (scene_type = 'INTERVIEW'),
    CONSTRAINT evaluation_interview_scene_results_revision_check
        CHECK (input_revision > 0),
    CONSTRAINT evaluation_interview_scene_results_snapshot_hash_check
        CHECK (octet_length(snapshot_hash) = 32),
    CONSTRAINT evaluation_interview_scene_results_config_hash_check
        CHECK (
            octet_length(full_config_hash) = 32
            AND full_config_hash <> decode(repeat('00', 32), 'hex')
        ),
    CONSTRAINT evaluation_interview_scene_results_request_id_check
        CHECK (
            provider_request_id IS NULL
            OR (
                octet_length(provider_request_id) BETWEEN 1 AND 128
                AND provider_request_id = btrim(provider_request_id)
            )
        ),
    CONSTRAINT evaluation_interview_scene_results_fencing_check
        CHECK (fencing_token > 0),
    CONSTRAINT evaluation_interview_scene_results_payload_check
        CHECK (
            jsonb_typeof(result_payload) = 'object'
            AND NOT jsonb_path_exists(
                result_payload,
                '$.** ? (@.type() == "object").keyvalue() ? (
                    @.key like_regex
                    "^(raw|display|interval|total|overall|weights)$"
                    flag "i"
                )'
            )
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

CREATE INDEX evaluation_interview_scene_results_owner_created_idx
    ON evaluation_interview_scene_results (
        owner_user_id,
        created_at DESC,
        id DESC
    );

CREATE FUNCTION evaluation_assert_interview_scene_result_binding()
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
       OR NOT evaluation_interview_result_refs_are_consistent(
           NEW.input_snapshot_id,
           NEW.result_payload
       )
    THEN
        RAISE EXCEPTION 'invalid Interview Shadow result binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_interview_scene_results_binding
BEFORE INSERT ON evaluation_interview_scene_results
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_interview_scene_result_binding();

CREATE FUNCTION reject_evaluation_interview_scene_result_mutation()
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
    RAISE EXCEPTION 'evaluation Interview scene results are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER evaluation_interview_scene_results_immutable
BEFORE UPDATE OR DELETE ON evaluation_interview_scene_results
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_interview_scene_result_mutation();

COMMIT;
