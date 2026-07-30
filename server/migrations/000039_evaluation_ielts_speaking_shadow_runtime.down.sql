BEGIN;

DROP TRIGGER IF EXISTS evaluation_ielts_scene_results_immutable
    ON evaluation_ielts_speaking_scene_results;
DROP FUNCTION IF EXISTS
    reject_evaluation_ielts_scene_result_mutation();
DROP TRIGGER IF EXISTS evaluation_ielts_scene_results_binding
    ON evaluation_ielts_speaking_scene_results;
DROP FUNCTION IF EXISTS
    evaluation_assert_ielts_scene_result_binding();
DROP TABLE IF EXISTS evaluation_ielts_speaking_scene_results;
DROP FUNCTION IF EXISTS evaluation_ielts_result_shape_is_valid(jsonb);

DROP TRIGGER evaluation_module_runs_identity_immutable
    ON evaluation_module_runs;
DELETE FROM evaluation_module_runs
WHERE scene_type = 'IELTS_SPEAKING';
CREATE TRIGGER evaluation_module_runs_identity_immutable
BEFORE UPDATE OR DELETE ON evaluation_module_runs
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_module_run_identity_mutation();

DROP TRIGGER evaluation_module_runs_binding
    ON evaluation_module_runs;
DROP FUNCTION evaluation_assert_scene_job_run_binding();

ALTER TABLE evaluation_module_runs
    DROP CONSTRAINT evaluation_module_runs_scene_check,
    ADD CONSTRAINT evaluation_module_runs_scene_check
        CHECK (scene_type = 'INTERVIEW');

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

COMMIT;
