BEGIN;

UPDATE evaluation_revision_states AS state
SET evaluation_status = CASE
        WHEN EXISTS (
            SELECT 1
            FROM evaluation_module_runs AS run
            WHERE run.evaluation_id = state.evaluation_id
              AND run.evaluation_revision_id = state.revision_id
              AND run.owner_user_id = state.owner_user_id
              AND run.channel = 'SCENE'
              AND run.strategy_ref =
                  'ielts-speaking-full-mock-shadow/v1'
              AND run.run_status = 'RUNNING'
        )
        THEN 'RUNNING'
        ELSE 'QUEUED'
    END,
    updated_at = transaction_timestamp()
FROM evaluation_revisions AS revision
JOIN evaluation_ledgers AS ledger
  ON ledger.id = revision.evaluation_id
 AND ledger.owner_user_id = revision.owner_user_id
WHERE state.revision_id = revision.id
  AND state.evaluation_id = ledger.id
  AND state.owner_user_id = ledger.owner_user_id
  AND state.evaluation_status = 'VALIDATING'
  AND ledger.scope = 'SESSION'
  AND ledger.scene_type = 'IELTS_SPEAKING'
  AND revision.channels = ARRAY['SCENE']::text[]
  AND revision.scene_strategy_ref = 'ielts-speaking-full-mock-shadow/v1'
  AND revision.pipeline_version = 'evaluation-pipeline-shadow/v1';

ALTER TABLE evaluation_module_runs
    DROP CONSTRAINT evaluation_module_runs_acoustic_fkey,
    DROP CONSTRAINT evaluation_module_runs_acoustic_shape_check,
    DROP COLUMN input_bundle_hash,
    DROP COLUMN acoustic_snapshot_hash,
    DROP COLUMN acoustic_snapshot_id;

DROP TRIGGER evaluation_ielts_acoustic_snapshots_immutable
    ON evaluation_ielts_speaking_acoustic_snapshots;
DROP FUNCTION reject_evaluation_ielts_acoustic_snapshot_mutation();
DROP TABLE evaluation_ielts_speaking_acoustic_snapshots;

CREATE OR REPLACE FUNCTION reject_evaluation_module_run_identity_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owner_status text;
    deletion_fenced boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        SELECT account_status INTO owner_status
        FROM identity_users WHERE id = OLD.owner_user_id;
        IF owner_status IN ('deleting', 'deleted') THEN
            RETURN OLD;
        END IF;
        SELECT EXISTS (
            SELECT 1 FROM evaluation_deletion_fences
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
       OR NEW.evaluation_revision_id IS DISTINCT FROM OLD.evaluation_revision_id
       OR NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
       OR NEW.channel IS DISTINCT FROM OLD.channel
       OR NEW.strategy_ref IS DISTINCT FROM OLD.strategy_ref
       OR NEW.practice_session_id IS DISTINCT FROM OLD.practice_session_id
       OR NEW.input_snapshot_id IS DISTINCT FROM OLD.input_snapshot_id
       OR NEW.input_revision IS DISTINCT FROM OLD.input_revision
       OR NEW.scope IS DISTINCT FROM OLD.scope
       OR NEW.scene_type IS DISTINCT FROM OLD.scene_type
       OR NEW.snapshot_hash IS DISTINCT FROM OLD.snapshot_hash
       OR NEW.full_config_hash IS DISTINCT FROM OLD.full_config_hash
       OR NEW.prompt_version IS DISTINCT FROM OLD.prompt_version
       OR NEW.provider IS DISTINCT FROM OLD.provider
       OR NEW.model IS DISTINCT FROM OLD.model
       OR NEW.deletion_generation IS DISTINCT FROM OLD.deletion_generation
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
      JOIN identity_users AS owner ON owner.id = ledger.owner_user_id
      JOIN evaluation_revision_states AS state
        ON state.evaluation_id = ledger.id
       AND state.revision_id = revision.id
       AND state.owner_user_id = ledger.owner_user_id
     WHERE ledger.id = NEW.evaluation_id
       AND ledger.owner_user_id = NEW.owner_user_id
       AND owner.account_status = 'active'
       AND revision.channels = ARRAY['SCENE']::text[]
       AND (
           (ledger.scene_type = 'INTERVIEW'
            AND revision.scene_strategy_ref = 'interview-scene-shadow/v1'
            AND revision.pipeline_version = 'evaluation-pipeline-shadow/v1')
           OR
           (ledger.scene_type = 'IELTS_SPEAKING'
            AND revision.scene_strategy_ref = 'ielts-speaking-full-mock-shadow/v1'
            AND revision.pipeline_version = 'evaluation-pipeline-shadow/v1')
           OR
           (ledger.scene_type IN (
                'IELTS_SPEAKING',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
            AND revision.scene_strategy_ref = 'general-scene-evaluation/v1'
            AND revision.pipeline_version = 'evaluation-pipeline/v1')
       )
       AND state.evaluation_status IN ('QUEUED', 'RUNNING')
       AND outbox.delivery_status = 'PENDING'
       AND outbox.lease_expires_at > clock_timestamp()
       AND NOT EXISTS (
           SELECT 1 FROM evaluation_revisions AS later
           WHERE later.evaluation_id = revision.evaluation_id
             AND later.revision > revision.revision
       )
       AND NOT EXISTS (
           SELECT 1 FROM evaluation_deletion_fences AS fence
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
    RETURN NEW;
END;
$$;

COMMIT;
