BEGIN;

CREATE TABLE evaluation_ielts_speaking_acoustic_snapshots (
    id text COLLATE "C" PRIMARY KEY,
    evaluation_id uuid NOT NULL UNIQUE,
    owner_user_id uuid NOT NULL,
    input_snapshot_id text COLLATE "C" NOT NULL,
    input_snapshot_hash bytea NOT NULL,
    schema_version text COLLATE "C" NOT NULL,
    resolution text COLLATE "C" NOT NULL,
    snapshot_hash bytea NOT NULL,
    canonical_payload text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT evaluation_ielts_acoustic_ledger_fkey
        FOREIGN KEY (evaluation_id, owner_user_id)
        REFERENCES evaluation_ledgers (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_ielts_acoustic_evidence_fkey
        FOREIGN KEY (input_snapshot_id, owner_user_id)
        REFERENCES evaluation_evidence_snapshots (id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT evaluation_ielts_acoustic_identity_unique
        UNIQUE (id, evaluation_id, owner_user_id),
    CONSTRAINT evaluation_ielts_acoustic_id_check
        CHECK (id ~ '^ielts_acoustic_[0-9a-f]{32}$'),
    CONSTRAINT evaluation_ielts_acoustic_schema_check
        CHECK (schema_version = 'ielts-speaking-acoustic-snapshot/v1'),
    CONSTRAINT evaluation_ielts_acoustic_resolution_check
        CHECK (resolution IN ('COMPLETE', 'PARTIAL', 'TEXT_ONLY')),
    CONSTRAINT evaluation_ielts_acoustic_base_hash_check
        CHECK (octet_length(input_snapshot_hash) = 32),
    CONSTRAINT evaluation_ielts_acoustic_hash_check
        CHECK (
            octet_length(snapshot_hash) = 32
            AND snapshot_hash = sha256(
                convert_to(canonical_payload, 'UTF8')
            )
        ),
    CONSTRAINT evaluation_ielts_acoustic_payload_check
        CHECK (
            jsonb_typeof(canonical_payload::jsonb) = 'object'
            AND canonical_payload::jsonb ->> 'schema_version'
                = schema_version
            AND canonical_payload::jsonb ->> 'evaluation_id'
                = evaluation_id::text
            AND canonical_payload::jsonb ->> 'owner_user_id'
                = owner_user_id::text
            AND canonical_payload::jsonb ->> 'input_snapshot_id'
                = input_snapshot_id
            AND canonical_payload::jsonb ->> 'input_snapshot_hash'
                = encode(input_snapshot_hash, 'hex')
            AND canonical_payload::jsonb ->> 'resolution'
                = resolution
            AND jsonb_typeof(canonical_payload::jsonb -> 'turns') = 'array'
            AND NOT jsonb_path_exists(
                canonical_payload::jsonb,
                '$.** ? (@.type() == "object").keyvalue() ? (
                    @.key like_regex
                    "^(provider[-_]?session|session[-_]?id|raw[-_]?result|object[-_]?key|signed[-_]?url|audio[-_]?url|url)$"
                    flag "i"
                )'
            )
        )
);

CREATE INDEX evaluation_ielts_acoustic_owner_created_idx
    ON evaluation_ielts_speaking_acoustic_snapshots (
        owner_user_id,
        created_at DESC,
        id DESC
    );

CREATE FUNCTION reject_evaluation_ielts_acoustic_snapshot_mutation()
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
    RAISE EXCEPTION 'IELTS acoustic snapshots are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER evaluation_ielts_acoustic_snapshots_immutable
BEFORE UPDATE OR DELETE ON evaluation_ielts_speaking_acoustic_snapshots
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_ielts_acoustic_snapshot_mutation();

ALTER TABLE evaluation_module_runs
    ADD COLUMN acoustic_snapshot_id text COLLATE "C",
    ADD COLUMN acoustic_snapshot_hash bytea,
    ADD COLUMN input_bundle_hash bytea,
    ADD CONSTRAINT evaluation_module_runs_acoustic_shape_check
        CHECK (
            (
                acoustic_snapshot_id IS NULL
                AND acoustic_snapshot_hash IS NULL
                AND input_bundle_hash IS NULL
            )
            OR
            (
                acoustic_snapshot_id ~ '^ielts_acoustic_[0-9a-f]{32}$'
                AND octet_length(acoustic_snapshot_hash) = 32
                AND octet_length(input_bundle_hash) = 32
            )
        ),
    ADD CONSTRAINT evaluation_module_runs_acoustic_fkey
        FOREIGN KEY (
            acoustic_snapshot_id,
            evaluation_id,
            owner_user_id
        )
        REFERENCES evaluation_ielts_speaking_acoustic_snapshots (
            id,
            evaluation_id,
            owner_user_id
        )
        ON DELETE RESTRICT;

CREATE OR REPLACE FUNCTION evaluation_assert_scene_job_run_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    bound_record record;
    expected_bundle_hash bytea;
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
        outbox.fencing_token,
        acoustic.id AS acoustic_snapshot_id,
        acoustic.snapshot_hash AS acoustic_snapshot_hash
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
      LEFT JOIN evaluation_ielts_speaking_acoustic_snapshots AS acoustic
        ON acoustic.evaluation_id = ledger.id
       AND acoustic.owner_user_id = ledger.owner_user_id
       AND acoustic.input_snapshot_id = ledger.input_snapshot_id
       AND acoustic.input_snapshot_hash = snapshot.snapshot_hash
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
       OR NEW.practice_session_id <> bound_record.practice_session_id
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

    IF NEW.scene_type = 'IELTS_SPEAKING'
       AND NEW.strategy_ref = 'ielts-speaking-full-mock-shadow/v1'
    THEN
        expected_bundle_hash := sha256(
            convert_to('ielts-speaking-input-bundle/v1', 'UTF8') ||
                decode('00', 'hex') || bound_record.snapshot_hash ||
                bound_record.acoustic_snapshot_hash
        );
        IF bound_record.acoustic_snapshot_id IS NULL
           OR NEW.acoustic_snapshot_id
                IS DISTINCT FROM bound_record.acoustic_snapshot_id
           OR NEW.acoustic_snapshot_hash
                IS DISTINCT FROM bound_record.acoustic_snapshot_hash
           OR NEW.input_bundle_hash IS DISTINCT FROM expected_bundle_hash
        THEN
            RAISE EXCEPTION 'invalid frozen IELTS acoustic binding'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.acoustic_snapshot_id IS NOT NULL
       OR NEW.acoustic_snapshot_hash IS NOT NULL
       OR NEW.input_bundle_hash IS NOT NULL
    THEN
        RAISE EXCEPTION 'unexpected IELTS acoustic binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION reject_evaluation_module_run_identity_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owner_status text;
    deletion_fenced boolean;
    legacy_acoustic_binding boolean := false;
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

    IF OLD.acoustic_snapshot_id IS NULL
       AND OLD.acoustic_snapshot_hash IS NULL
       AND OLD.input_bundle_hash IS NULL
       AND NEW.acoustic_snapshot_id IS NOT NULL
       AND NEW.acoustic_snapshot_hash IS NOT NULL
       AND NEW.input_bundle_hash IS NOT NULL
       AND OLD.run_status = 'RUNNING'
       AND NEW.run_status = 'RUNNING'
       AND NEW.scene_type = 'IELTS_SPEAKING'
       AND NEW.strategy_ref = 'ielts-speaking-full-mock-shadow/v1'
    THEN
        SELECT EXISTS (
            SELECT 1
            FROM evaluation_ielts_speaking_acoustic_snapshots AS acoustic
            WHERE acoustic.id = NEW.acoustic_snapshot_id
              AND acoustic.evaluation_id = NEW.evaluation_id
              AND acoustic.owner_user_id = NEW.owner_user_id
              AND acoustic.input_snapshot_id = NEW.input_snapshot_id
              AND acoustic.input_snapshot_hash = NEW.snapshot_hash
              AND acoustic.snapshot_hash = NEW.acoustic_snapshot_hash
              AND NEW.input_bundle_hash = sha256(
                  convert_to(
                      'ielts-speaking-input-bundle/v1',
                      'UTF8'
                  ) || decode('00', 'hex') || NEW.snapshot_hash ||
                      acoustic.snapshot_hash
              )
        ) INTO legacy_acoustic_binding;
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
       OR (
           (
               NEW.acoustic_snapshot_id
                    IS DISTINCT FROM OLD.acoustic_snapshot_id
               OR NEW.acoustic_snapshot_hash
                    IS DISTINCT FROM OLD.acoustic_snapshot_hash
               OR NEW.input_bundle_hash
                    IS DISTINCT FROM OLD.input_bundle_hash
           )
           AND NOT legacy_acoustic_binding
       )
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

UPDATE evaluation_revision_states AS state
SET evaluation_status = 'VALIDATING',
    updated_at = transaction_timestamp()
FROM evaluation_revisions AS revision
JOIN evaluation_ledgers AS ledger
  ON ledger.id = revision.evaluation_id
 AND ledger.owner_user_id = revision.owner_user_id
WHERE state.revision_id = revision.id
  AND state.evaluation_id = ledger.id
  AND state.owner_user_id = ledger.owner_user_id
  AND state.evaluation_status IN ('QUEUED', 'RUNNING')
  AND ledger.scope = 'SESSION'
  AND ledger.scene_type = 'IELTS_SPEAKING'
  AND revision.channels = ARRAY['SCENE']::text[]
  AND revision.scene_strategy_ref = 'ielts-speaking-full-mock-shadow/v1'
  AND revision.pipeline_version = 'evaluation-pipeline-shadow/v1'
  AND NOT EXISTS (
      SELECT 1
      FROM evaluation_revisions AS later
      WHERE later.evaluation_id = revision.evaluation_id
        AND later.revision > revision.revision
  )
;

COMMIT;
