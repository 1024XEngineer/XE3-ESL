package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInterviewShadowLeaseLost = errors.New(
		"evaluation: Interview Shadow lease lost",
	)
	ErrInterviewShadowConfigurationConflict = errors.New(
		"evaluation: Interview Shadow configuration conflict",
	)
)

const (
	interviewShadowConfigurationChangedCode = "runtime_configuration_changed"
	interviewShadowRevisionSupersededCode   = "revision_superseded"
)

var _ InterviewShadowRuntimeRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) ClaimInterviewShadow(
	ctx context.Context,
	configuration InterviewShadowRuntimeConfiguration,
) (InterviewShadowClaim, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!configuration.Valid() {
		return InterviewShadowClaim{}, false, ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"begin Interview Shadow claim: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exhausted, err := failOneExhaustedInterviewShadow(
		ctx,
		tx,
		configuration.MaxAttempts,
	)
	if err != nil {
		return InterviewShadowClaim{}, false, err
	}
	if exhausted {
		if err := tx.Commit(ctx); err != nil {
			return InterviewShadowClaim{}, false, fmt.Errorf(
				"commit exhausted Interview Shadow: %w",
				err,
			)
		}
		return InterviewShadowClaim{}, false, nil
	}

	var ownerUserID string
	var evaluationID string
	var evaluationRevisionID string
	var outboxID string
	err = tx.QueryRow(ctx, interviewShadowCandidateOwnerSQL,
		configuration.MaxAttempts).Scan(
		&ownerUserID,
		&evaluationID,
		&evaluationRevisionID,
		&outboxID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InterviewShadowClaim{}, false, nil
	}
	if err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"select Interview Shadow candidate owner: %w",
			err,
		)
	}
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		if errors.Is(err, ErrAccountUnavailable) {
			return InterviewShadowClaim{}, false, nil
		}
		return InterviewShadowClaim{}, false, err
	}
	if err := lockEvaluationLedgerAndRevisionRows(
		ctx,
		tx,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InterviewShadowClaim{}, false, nil
		}
		return InterviewShadowClaim{}, false, err
	}
	if err := lockEvaluationRevisionRuntimeRows(
		ctx,
		tx,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InterviewShadowClaim{}, false, nil
		}
		return InterviewShadowClaim{}, false, err
	}

	var claim InterviewShadowClaim
	var leaseExpiresAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		UPDATE evaluation_outbox AS outbox
		SET attempt_count = outbox.attempt_count + 1,
		    fencing_token = outbox.fencing_token + 1,
		    lease_expires_at =
		        clock_timestamp() + make_interval(secs => $6),
		    last_failure_code = NULL,
		    updated_at = transaction_timestamp()
		WHERE outbox.id = $7
		  AND outbox.evaluation_id = $8
		  AND outbox.evaluation_revision_id = $9
		  AND outbox.owner_user_id = $1
		  AND outbox.channel = 'SCENE'
		  AND outbox.delivery_status = 'PENDING'
		  AND outbox.available_at <= transaction_timestamp()
		  AND (
		      outbox.lease_expires_at IS NULL
		      OR outbox.lease_expires_at <= clock_timestamp()
		  )
		  AND outbox.attempt_count < $2
		  AND EXISTS (
		      SELECT 1
		      FROM evaluation_ledgers AS ledger
		      JOIN evaluation_revisions AS revision
		        ON revision.evaluation_id = ledger.id
		       AND revision.id = outbox.evaluation_revision_id
		       AND revision.owner_user_id = ledger.owner_user_id
		      JOIN evaluation_revision_states AS state
		        ON state.evaluation_id = ledger.id
		       AND state.revision_id = revision.id
		       AND state.owner_user_id = ledger.owner_user_id
		      JOIN evaluation_evidence_snapshots AS snapshot
		        ON snapshot.id = ledger.input_snapshot_id
		       AND snapshot.owner_user_id = ledger.owner_user_id
		      WHERE ledger.id = outbox.evaluation_id
		        AND ledger.owner_user_id = outbox.owner_user_id
		        AND ledger.scope = 'SESSION'
		        AND ledger.scene_type = 'INTERVIEW'
		        AND snapshot.practice_session_id =
		            ledger.practice_session_id
		        AND snapshot.scope = ledger.scope
		        AND snapshot.scene_type = ledger.scene_type
		        AND snapshot.input_revision = ledger.input_revision
		        AND revision.channels = ARRAY['SCENE']::text[]
		        AND revision.scene_strategy_ref = $3
		        AND revision.pipeline_version = $4
		        AND revision.schema_version = $5
		        AND state.evaluation_status IN ('QUEUED', 'RUNNING')
		        AND NOT EXISTS (
		            SELECT 1
		            FROM evaluation_revisions AS later
		            WHERE later.evaluation_id =
		                revision.evaluation_id
		              AND later.revision > revision.revision
		        )
		  )
		RETURNING
			outbox.id::text,
			outbox.evaluation_id::text,
			outbox.evaluation_revision_id::text,
			outbox.owner_user_id::text,
			outbox.attempt_count,
			outbox.fencing_token,
			outbox.lease_expires_at
	`, ownerUserID, configuration.MaxAttempts,
		InterviewShadowStrategyRef,
		InterviewShadowPipelineVersion,
		SchemaVersion,
		configuration.LeaseDuration.Seconds(),
		outboxID,
		evaluationID,
		evaluationRevisionID).Scan(
		&claim.OutboxID,
		&claim.EvaluationID,
		&claim.EvaluationRevisionID,
		&claim.OwnerUserID,
		&claim.AttemptCount,
		&claim.FencingToken,
		&leaseExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InterviewShadowClaim{}, false, nil
	}
	if err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"claim Interview Shadow outbox: %w",
			err,
		)
	}
	if !leaseExpiresAt.Valid {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"claim Interview Shadow outbox: missing lease",
		)
	}
	claim.LeaseExpiresAt = leaseExpiresAt.Time.UTC()

	stateUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_revision_states
		SET evaluation_status = 'RUNNING',
		    updated_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND revision_id = $2
		  AND owner_user_id = $3
		  AND evaluation_status IN ('QUEUED', 'RUNNING')
		  AND completed_at IS NULL
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID)
	if err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"mark Interview Shadow revision running: %w",
			err,
		)
	}
	if stateUpdate.RowsAffected() != 1 {
		return InterviewShadowClaim{}, false,
			ErrInterviewShadowLeaseLost
	}

	if err := tx.QueryRow(ctx, `
		SELECT revision, pipeline_version
		FROM evaluation_revisions
		WHERE evaluation_id = $1
		  AND id = $2
		  AND owner_user_id = $3
		  AND scene_strategy_ref = $4
		FOR SHARE
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID, InterviewShadowStrategyRef).Scan(
		&claim.Revision,
		&claim.PipelineVersion,
	); err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"read claimed Interview Shadow revision: %w",
			err,
		)
	}

	claim.Snapshot, err = scanEvidenceSnapshot(tx.QueryRow(
		ctx,
		evidenceSnapshotSelect+`
			FROM evaluation_evidence_snapshots AS snapshot
			JOIN evaluation_ledgers AS ledger
			  ON ledger.input_snapshot_id = snapshot.id
			 AND ledger.owner_user_id = snapshot.owner_user_id
			JOIN evaluation_revisions AS revision
			  ON revision.evaluation_id = ledger.id
			 AND revision.owner_user_id = ledger.owner_user_id
			WHERE ledger.id = $1
			  AND revision.id = $2
			  AND ledger.owner_user_id = $3
			  AND ledger.practice_session_id =
			      snapshot.practice_session_id
			  AND ledger.input_revision = snapshot.input_revision
			  AND ledger.scope = snapshot.scope
			  AND ledger.scene_type = snapshot.scene_type
			  AND ledger.scope = 'SESSION'
			  AND ledger.scene_type = 'INTERVIEW'
			  AND revision.channels = ARRAY['SCENE']::text[]
			  AND revision.scene_strategy_ref = $4
			  AND revision.pipeline_version = $5
			  AND revision.schema_version = $6
			  AND NOT EXISTS (
			      SELECT 1
			      FROM evaluation_revisions AS later
			      WHERE later.evaluation_id = revision.evaluation_id
			        AND later.revision > revision.revision
			  )
			FOR SHARE OF ledger, revision, snapshot
		`,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
		claim.OwnerUserID,
		InterviewShadowStrategyRef,
		InterviewShadowPipelineVersion,
		SchemaVersion,
	))
	if err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"read claimed Interview Shadow snapshot: %w",
			err,
		)
	}

	claim.StrategyRef = InterviewShadowStrategyRef
	claim.PromptVersion = configuration.PromptVersion
	claim.Provider = configuration.Provider
	claim.Model = configuration.Model
	claim.FullConfigHash = configuration.FullConfigHash

	err = tx.QueryRow(ctx, `
		INSERT INTO evaluation_module_runs (
			outbox_id,
			evaluation_id,
			evaluation_revision_id,
			owner_user_id,
			channel,
			strategy_ref,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scope,
			scene_type,
			snapshot_hash,
			full_config_hash,
			prompt_version,
			provider,
			model,
			attempt_count,
			fencing_token
		)
		VALUES (
			$1, $2, $3, $4, 'SCENE', $5, $6, $7, $8,
			'SESSION', 'INTERVIEW', $9, $10, $11, $12, $13,
			$14, $15
		)
		ON CONFLICT (
			evaluation_revision_id,
			channel,
			strategy_ref
		) DO UPDATE
		SET attempt_count = EXCLUDED.attempt_count,
		    fencing_token = EXCLUDED.fencing_token,
		    last_failure_code = NULL,
		    updated_at = transaction_timestamp()
		WHERE evaluation_module_runs.run_status = 'RUNNING'
		  AND evaluation_module_runs.outbox_id = EXCLUDED.outbox_id
		  AND evaluation_module_runs.evaluation_id =
		      EXCLUDED.evaluation_id
		  AND evaluation_module_runs.owner_user_id =
		      EXCLUDED.owner_user_id
		  AND evaluation_module_runs.practice_session_id =
		      EXCLUDED.practice_session_id
		  AND evaluation_module_runs.input_snapshot_id =
		      EXCLUDED.input_snapshot_id
		  AND evaluation_module_runs.input_revision =
		      EXCLUDED.input_revision
		  AND evaluation_module_runs.snapshot_hash =
		      EXCLUDED.snapshot_hash
		  AND evaluation_module_runs.full_config_hash =
		      EXCLUDED.full_config_hash
		  AND evaluation_module_runs.prompt_version =
		      EXCLUDED.prompt_version
		  AND evaluation_module_runs.provider = EXCLUDED.provider
		  AND evaluation_module_runs.model = EXCLUDED.model
		  AND evaluation_module_runs.attempt_count <
		      EXCLUDED.attempt_count
		  AND evaluation_module_runs.fencing_token <
		      EXCLUDED.fencing_token
		RETURNING id::text
	`, claim.OutboxID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		claim.AttemptCount, claim.FencingToken).Scan(
		&claim.ModuleRunID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if failErr := failInterviewShadowConfigurationChange(
			ctx,
			tx,
			claim,
		); failErr != nil {
			return InterviewShadowClaim{}, false, failErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return InterviewShadowClaim{}, false, fmt.Errorf(
				"commit Interview Shadow configuration failure: %w",
				commitErr,
			)
		}
		return InterviewShadowClaim{}, false,
			ErrInterviewShadowConfigurationConflict
	}
	if err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"upsert Interview Shadow module run: %w",
			err,
		)
	}
	if !claim.Valid() {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"validate claimed Interview Shadow: %w",
			ErrInvalidRequest,
		)
	}
	if err := tx.Commit(ctx); err != nil {
		return InterviewShadowClaim{}, false, fmt.Errorf(
			"commit Interview Shadow claim: %w",
			err,
		)
	}
	return claim, true, nil
}

func (r *PostgresRepository) CompleteInterviewShadow(
	ctx context.Context,
	claim InterviewShadowClaim,
	result InterviewShadowResult,
) error {
	if r == nil || r.pool == nil || ctx == nil || !claim.Valid() ||
		ValidateInterviewShadowResult(claim.Snapshot, result) != nil ||
		!interviewShadowResultMatchesRuntime(claim, result) {
		return ErrInvalidRequest
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Interview Shadow completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, claim.OwnerUserID); err != nil {
		return err
	}
	if err := lockEvaluationLedgerAndRevisionRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInterviewShadowLeaseLost
		}
		return err
	}
	if err := lockEvaluationRevisionRuntimeRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInterviewShadowLeaseLost
		}
		return err
	}

	var deliveryStatus string
	var persistedToken int64
	var leaseActive bool
	var runStatus string
	var revisionStatus string
	err = tx.QueryRow(ctx, `
		SELECT
			outbox.delivery_status,
			outbox.fencing_token,
			coalesce(
				outbox.lease_expires_at > clock_timestamp(),
				false
			),
			run.run_status,
			state.evaluation_status
		FROM evaluation_outbox AS outbox
		JOIN evaluation_module_runs AS run
		  ON run.outbox_id = outbox.id
		 AND run.evaluation_id = outbox.evaluation_id
		 AND run.evaluation_revision_id =
		     outbox.evaluation_revision_id
		 AND run.owner_user_id = outbox.owner_user_id
		JOIN evaluation_ledgers AS ledger
		  ON ledger.id = outbox.evaluation_id
		 AND ledger.owner_user_id = outbox.owner_user_id
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.id = outbox.evaluation_revision_id
		 AND revision.owner_user_id = ledger.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = ledger.id
		 AND state.revision_id = revision.id
		 AND state.owner_user_id = ledger.owner_user_id
		JOIN evaluation_evidence_snapshots AS snapshot
		  ON snapshot.id = ledger.input_snapshot_id
		 AND snapshot.owner_user_id = ledger.owner_user_id
		WHERE outbox.id = $1
		  AND outbox.evaluation_id = $2
		  AND outbox.evaluation_revision_id = $3
		  AND outbox.owner_user_id = $4
		  AND outbox.channel = 'SCENE'
		  AND run.id = $5
		  AND run.strategy_ref = $6
		  AND run.practice_session_id = $7
		  AND run.input_snapshot_id = $8
		  AND run.input_revision = $9
		  AND run.snapshot_hash = $10
		  AND run.full_config_hash = $11
		  AND run.prompt_version = $12
		  AND run.provider = $13
		  AND run.model = $14
		  AND snapshot.snapshot_hash = run.snapshot_hash
		  AND snapshot.practice_session_id =
		      ledger.practice_session_id
		  AND snapshot.input_revision = ledger.input_revision
		  AND snapshot.scope = 'SESSION'
		  AND snapshot.scene_type = 'INTERVIEW'
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $6
		  AND revision.pipeline_version = $15
		  AND revision.schema_version = $16
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.revision > revision.revision
		  )
		FOR UPDATE OF outbox, run, state
	`, claim.OutboxID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.ModuleRunID, claim.StrategyRef,
		claim.Snapshot.PracticeSessionID, claim.Snapshot.ID,
		claim.Snapshot.InputRevision, claim.Snapshot.SnapshotHash[:],
		claim.FullConfigHash[:], claim.PromptVersion,
		claim.Provider, claim.Model,
		InterviewShadowPipelineVersion, SchemaVersion).Scan(
		&deliveryStatus,
		&persistedToken,
		&leaseActive,
		&runStatus,
		&revisionStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInterviewShadowLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock Interview Shadow completion: %w", err)
	}
	if persistedToken != claim.FencingToken {
		return ErrInterviewShadowLeaseLost
	}
	if deliveryStatus == "DELIVERED" &&
		runStatus == "READY" &&
		revisionStatus == "READY" {
		same, err := sameInterviewShadowResult(
			ctx,
			tx,
			claim,
			result,
			payload,
		)
		if err != nil {
			return err
		}
		if !same {
			return ErrInterviewShadowConfigurationConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf(
				"commit Interview Shadow completion replay: %w",
				err,
			)
		}
		return nil
	}
	if deliveryStatus != "PENDING" ||
		runStatus != "RUNNING" ||
		revisionStatus != "RUNNING" ||
		!leaseActive {
		return ErrInterviewShadowLeaseLost
	}

	var providerRequestID any
	if result.Provider != nil {
		providerRequestID = result.Provider.RequestID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO evaluation_interview_scene_results (
			module_run_id,
			evaluation_id,
			evaluation_revision_id,
			owner_user_id,
			channel,
			strategy_ref,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scene_type,
			snapshot_hash,
			full_config_hash,
			prompt_version,
			provider,
			model,
			provider_request_id,
			fencing_token,
			result_payload
		)
		VALUES (
			$1, $2, $3, $4, 'SCENE', $5, $6, $7, $8,
			'INTERVIEW', $9, $10, $11, $12, $13, $14, $15,
			$16::jsonb
		)
	`, claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		providerRequestID, claim.FencingToken, payload); err != nil {
		return fmt.Errorf("insert Interview Shadow result: %w", err)
	}
	runUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_module_runs
		SET run_status = 'READY',
		    last_failure_code = NULL,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE id = $1
		  AND owner_user_id = $2
		  AND run_status = 'RUNNING'
		  AND fencing_token = $3
	`, claim.ModuleRunID, claim.OwnerUserID,
		claim.FencingToken)
	if err != nil {
		return fmt.Errorf("complete Interview Shadow run: %w", err)
	}
	if runUpdate.RowsAffected() != 1 {
		return ErrInterviewShadowLeaseLost
	}
	outboxUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_outbox
		SET delivery_status = 'DELIVERED',
		    lease_expires_at = NULL,
		    last_failure_code = NULL,
		    delivered_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE id = $1
		  AND owner_user_id = $2
		  AND delivery_status = 'PENDING'
		  AND fencing_token = $3
		  AND lease_expires_at > clock_timestamp()
	`, claim.OutboxID, claim.OwnerUserID, claim.FencingToken)
	if err != nil {
		return fmt.Errorf("deliver Interview Shadow outbox: %w", err)
	}
	if outboxUpdate.RowsAffected() != 1 {
		return ErrInterviewShadowLeaseLost
	}
	stateUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_revision_states
		SET evaluation_status = 'READY',
		    is_final = false,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND revision_id = $2
		  AND owner_user_id = $3
		  AND evaluation_status = 'RUNNING'
		  AND completed_at IS NULL
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID)
	if err != nil {
		return fmt.Errorf("ready Interview Shadow revision: %w", err)
	}
	if stateUpdate.RowsAffected() != 1 {
		return ErrInterviewShadowLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Interview Shadow completion: %w", err)
	}
	return nil
}

func (r *PostgresRepository) FailInterviewShadow(
	ctx context.Context,
	claim InterviewShadowClaim,
	failure InterviewShadowFailure,
	configuration InterviewShadowRuntimeConfiguration,
) (InterviewShadowRuntimeStatus, error) {
	if r == nil || r.pool == nil || ctx == nil || !claim.Valid() ||
		!failure.Valid() || !configuration.Valid() ||
		!claimMatchesRuntime(claim, configuration) {
		return "", ErrInvalidRequest
	}
	requeue := failure.Retryable &&
		claim.AttemptCount < configuration.MaxAttempts
	backoff := interviewShadowBackoff(claim.AttemptCount)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin Interview Shadow failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, claim.OwnerUserID); err != nil {
		return "", err
	}
	if err := lockEvaluationLedgerAndRevisionRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrInterviewShadowLeaseLost
		}
		return "", err
	}
	if err := lockEvaluationRevisionRuntimeRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrInterviewShadowLeaseLost
		}
		return "", err
	}

	var deliveryStatus string
	if requeue {
		deliveryStatus = "PENDING"
	} else {
		deliveryStatus = "FAILED"
	}
	var outboxID string
	err = tx.QueryRow(ctx, `
		UPDATE evaluation_outbox
		SET delivery_status = $4,
		    lease_expires_at = NULL,
		    available_at = CASE
		        WHEN $4 = 'PENDING'
		        THEN transaction_timestamp() +
		            make_interval(secs => $5)
		        ELSE available_at
		    END,
		    last_failure_code = $6,
		    failed_at = CASE
		        WHEN $4 = 'FAILED'
		        THEN transaction_timestamp()
		        ELSE NULL
		    END,
		    updated_at = transaction_timestamp()
		WHERE id = $1
		  AND owner_user_id = $2
		  AND delivery_status = 'PENDING'
		  AND fencing_token = $3
		  AND lease_expires_at > clock_timestamp()
		  AND EXISTS (
		      SELECT 1
		      FROM evaluation_module_runs AS run
		      JOIN evaluation_revisions AS revision
		        ON revision.evaluation_id = run.evaluation_id
		       AND revision.id = run.evaluation_revision_id
		       AND revision.owner_user_id = run.owner_user_id
		      JOIN evaluation_revision_states AS state
		        ON state.evaluation_id = run.evaluation_id
		       AND state.revision_id = run.evaluation_revision_id
		       AND state.owner_user_id = run.owner_user_id
		      WHERE run.id = $7
		        AND run.outbox_id = evaluation_outbox.id
		        AND run.evaluation_id = $8
		        AND run.evaluation_revision_id = $9
		        AND run.owner_user_id = $2
		        AND run.run_status = 'RUNNING'
		        AND run.fencing_token = $3
		        AND run.full_config_hash = $10
		        AND run.prompt_version = $11
		        AND run.provider = $12
		        AND run.model = $13
		        AND revision.scene_strategy_ref = $14
		        AND revision.pipeline_version = $15
		        AND revision.schema_version = $16
		        AND state.evaluation_status = 'RUNNING'
		        AND NOT EXISTS (
		            SELECT 1
		            FROM evaluation_revisions AS later
		            WHERE later.evaluation_id =
		                revision.evaluation_id
		              AND later.revision > revision.revision
		        )
		  )
		RETURNING id::text
	`, claim.OutboxID, claim.OwnerUserID,
		claim.FencingToken, deliveryStatus,
		backoff.Seconds(), failure.Code,
		claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		claim.StrategyRef, claim.PipelineVersion,
		SchemaVersion).Scan(&outboxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInterviewShadowLeaseLost
	}
	if err != nil {
		return "", fmt.Errorf("fail Interview Shadow outbox: %w", err)
	}

	runStatus := "RUNNING"
	if !requeue {
		runStatus = "FAILED"
	}
	runUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_module_runs
		SET run_status = $4,
		    last_failure_code = $5,
		    updated_at = transaction_timestamp(),
		    completed_at = CASE
		        WHEN $4 = 'FAILED'
		        THEN transaction_timestamp()
		        ELSE NULL
		    END
		WHERE id = $1
		  AND owner_user_id = $2
		  AND run_status = 'RUNNING'
		  AND fencing_token = $3
	`, claim.ModuleRunID, claim.OwnerUserID,
		claim.FencingToken, runStatus, failure.Code)
	if err != nil {
		return "", fmt.Errorf(
			"fail Interview Shadow module run: %w",
			err,
		)
	}
	if runUpdate.RowsAffected() != 1 {
		return "", ErrInterviewShadowLeaseLost
	}
	if !requeue {
		stateUpdate, err := tx.Exec(ctx, `
			UPDATE evaluation_revision_states
			SET evaluation_status = 'FAILED',
			    is_final = false,
			    updated_at = transaction_timestamp(),
			    completed_at = transaction_timestamp()
			WHERE evaluation_id = $1
			  AND revision_id = $2
			  AND owner_user_id = $3
			  AND evaluation_status = 'RUNNING'
			  AND completed_at IS NULL
		`, claim.EvaluationID, claim.EvaluationRevisionID,
			claim.OwnerUserID)
		if err != nil {
			return "", fmt.Errorf(
				"fail Interview Shadow revision: %w",
				err,
			)
		}
		if stateUpdate.RowsAffected() != 1 {
			return "", ErrInterviewShadowLeaseLost
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf(
			"commit Interview Shadow failure: %w",
			err,
		)
	}
	if requeue {
		return InterviewShadowRuntimePending, nil
	}
	return InterviewShadowRuntimeFailed, nil
}

func (r *PostgresRepository) GetInterviewShadowState(
	ctx context.Context,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) (InterviewShadowReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validUUID(evaluationID) ||
		!validUUID(evaluationRevisionID) {
		return InterviewShadowReadState{}, ErrInvalidRequest
	}
	var deliveryStatus string
	var leaseActive bool
	var moduleStatus string
	var revisionStatus string
	var failureCode string
	var payload []byte
	var inputSnapshotID string
	var fullConfigHash []byte
	err := r.pool.QueryRow(ctx, `
		SELECT
			outbox.delivery_status,
			coalesce(
				outbox.lease_expires_at > clock_timestamp(),
				false
			),
			coalesce(run.run_status, ''),
			state.evaluation_status,
			coalesce(
				run.last_failure_code,
				outbox.last_failure_code,
				''
			),
			coalesce(run.full_config_hash, ''::bytea),
			result.result_payload,
			ledger.input_snapshot_id
		FROM evaluation_ledgers AS ledger
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.owner_user_id = ledger.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = ledger.id
		 AND state.revision_id = revision.id
		 AND state.owner_user_id = ledger.owner_user_id
		JOIN evaluation_outbox AS outbox
		  ON outbox.evaluation_id = ledger.id
		 AND outbox.evaluation_revision_id = revision.id
		 AND outbox.owner_user_id = ledger.owner_user_id
		 AND outbox.channel = 'SCENE'
		LEFT JOIN evaluation_module_runs AS run
		  ON run.outbox_id = outbox.id
		 AND run.evaluation_id = ledger.id
		 AND run.evaluation_revision_id = revision.id
		 AND run.owner_user_id = ledger.owner_user_id
		 AND run.channel = outbox.channel
		LEFT JOIN evaluation_interview_scene_results AS result
		  ON result.module_run_id = run.id
		 AND result.evaluation_id = ledger.id
		 AND result.evaluation_revision_id = revision.id
		 AND result.owner_user_id = ledger.owner_user_id
		WHERE ledger.owner_user_id = $1
		  AND ledger.id = $2
		  AND revision.id = $3
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $4
		  AND revision.pipeline_version = $5
		  AND revision.schema_version = $6
	`, ownerUserID, evaluationID, evaluationRevisionID,
		InterviewShadowStrategyRef,
		InterviewShadowPipelineVersion,
		SchemaVersion).Scan(
		&deliveryStatus,
		&leaseActive,
		&moduleStatus,
		&revisionStatus,
		&failureCode,
		&fullConfigHash,
		&payload,
		&inputSnapshotID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InterviewShadowReadState{}, ErrNotFound
	}
	if err != nil {
		return InterviewShadowReadState{}, fmt.Errorf(
			"read Interview Shadow state: %w",
			err,
		)
	}
	var persistedConfigHash [32]byte
	if len(fullConfigHash) != 0 {
		if len(fullConfigHash) != len(persistedConfigHash) {
			return InterviewShadowReadState{},
				ErrInterviewShadowConfigurationConflict
		}
		copy(persistedConfigHash[:], fullConfigHash)
	}
	switch deliveryStatus {
	case "PENDING":
		if moduleStatus == "RUNNING" &&
			leaseActive {
			return InterviewShadowReadState{
				ModuleStatus:   InterviewShadowRuntimeRunning,
				FullConfigHash: persistedConfigHash,
			}, nil
		}
		if revisionStatus != "QUEUED" &&
			revisionStatus != "RUNNING" {
			return InterviewShadowReadState{},
				ErrInterviewShadowConfigurationConflict
		}
		return InterviewShadowReadState{
			ModuleStatus:   InterviewShadowRuntimePending,
			FullConfigHash: persistedConfigHash,
		}, nil
	case "FAILED":
		if moduleStatus != "FAILED" ||
			revisionStatus != "FAILED" ||
			!interviewShadowFailureCodePattern.MatchString(failureCode) {
			return InterviewShadowReadState{},
				ErrInterviewShadowConfigurationConflict
		}
		return InterviewShadowReadState{
			ModuleStatus:   InterviewShadowRuntimeFailed,
			FullConfigHash: persistedConfigHash,
			Failure: &InterviewShadowFailure{
				Code: failureCode,
			},
		}, nil
	case "DELIVERED":
		if moduleStatus != "READY" ||
			revisionStatus != "READY" ||
			len(payload) == 0 {
			return InterviewShadowReadState{},
				ErrInterviewShadowConfigurationConflict
		}
		var result InterviewShadowResult
		if err := json.Unmarshal(payload, &result); err != nil {
			return InterviewShadowReadState{}, fmt.Errorf(
				"decode Interview Shadow result: %w",
				err,
			)
		}
		snapshot, err := r.GetEvidenceSnapshot(
			ctx,
			ownerUserID,
			inputSnapshotID,
		)
		if err != nil {
			return InterviewShadowReadState{}, err
		}
		if err := ValidateInterviewShadowResult(
			snapshot,
			result,
		); err != nil {
			return InterviewShadowReadState{},
				ErrInterviewShadowConfigurationConflict
		}
		return InterviewShadowReadState{
			ModuleStatus:   InterviewShadowRuntimeReady,
			FullConfigHash: persistedConfigHash,
			Result:         &result,
		}, nil
	default:
		return InterviewShadowReadState{},
			ErrInterviewShadowConfigurationConflict
	}
}

func lockEvaluationLedgerAndRevisionRows(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) error {
	var locked bool
	if err := tx.QueryRow(ctx, `
		SELECT true
		FROM evaluation_ledgers
		WHERE id = $1
		  AND owner_user_id = $2
		FOR UPDATE
	`, evaluationID, ownerUserID).Scan(&locked); err != nil {
		return fmt.Errorf("lock Evaluation ledger runtime: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT true
		FROM evaluation_revisions
		WHERE evaluation_id = $1
		  AND id = $2
		  AND owner_user_id = $3
		FOR UPDATE
	`, evaluationID, evaluationRevisionID, ownerUserID).Scan(
		&locked,
	); err != nil {
		return fmt.Errorf("lock Evaluation revision runtime: %w", err)
	}
	return nil
}

func lockEvaluationRevisionRuntimeRows(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) error {
	var locked bool
	if err := tx.QueryRow(ctx, `
		SELECT true
		FROM evaluation_revision_states
		WHERE evaluation_id = $1
		  AND revision_id = $2
		  AND owner_user_id = $3
		FOR UPDATE
	`, evaluationID, evaluationRevisionID, ownerUserID).Scan(
		&locked,
	); err != nil {
		return fmt.Errorf("lock Evaluation revision state runtime: %w", err)
	}
	if err := lockEvaluationRuntimeIDs(ctx, tx, `
		SELECT id::text
		FROM evaluation_outbox
		WHERE evaluation_id = $1
		  AND evaluation_revision_id = $2
		  AND owner_user_id = $3
		ORDER BY channel, id
		FOR UPDATE
	`, evaluationID, evaluationRevisionID, ownerUserID); err != nil {
		return fmt.Errorf("lock Evaluation outbox runtime: %w", err)
	}
	if err := lockEvaluationRuntimeIDs(ctx, tx, `
		SELECT id::text
		FROM evaluation_module_runs
		WHERE evaluation_id = $1
		  AND evaluation_revision_id = $2
		  AND owner_user_id = $3
		ORDER BY id
		FOR UPDATE
	`, evaluationID, evaluationRevisionID, ownerUserID); err != nil {
		return fmt.Errorf("lock Evaluation module runtime: %w", err)
	}
	return nil
}

func lockEvaluationRuntimeIDs(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	arguments ...any,
) error {
	rows, err := tx.Query(ctx, query, arguments...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func cancelSupersededEvaluationRuntime(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE evaluation_module_runs
		SET run_status = 'FAILED',
		    last_failure_code = $4,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND evaluation_revision_id = $2
		  AND owner_user_id = $3
		  AND run_status = 'RUNNING'
	`, evaluationID, evaluationRevisionID, ownerUserID,
		interviewShadowRevisionSupersededCode); err != nil {
		return fmt.Errorf(
			"cancel superseded Evaluation module runtime: %w",
			err,
		)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE evaluation_outbox
		SET delivery_status = 'FAILED',
		    lease_expires_at = NULL,
		    last_failure_code = $4,
		    failed_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND evaluation_revision_id = $2
		  AND owner_user_id = $3
		  AND delivery_status = 'PENDING'
	`, evaluationID, evaluationRevisionID, ownerUserID,
		interviewShadowRevisionSupersededCode); err != nil {
		return fmt.Errorf(
			"cancel superseded Evaluation outbox runtime: %w",
			err,
		)
	}
	return nil
}

func failInterviewShadowConfigurationChange(
	ctx context.Context,
	tx pgx.Tx,
	claim InterviewShadowClaim,
) error {
	runUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_module_runs
		SET run_status = 'FAILED',
		    attempt_count = $5,
		    fencing_token = $6,
		    last_failure_code = $7,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE outbox_id = $1
		  AND evaluation_id = $2
		  AND evaluation_revision_id = $3
		  AND owner_user_id = $4
		  AND run_status = 'RUNNING'
	`, claim.OutboxID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.AttemptCount, claim.FencingToken,
		interviewShadowConfigurationChangedCode)
	if err != nil {
		return fmt.Errorf(
			"fail configuration-drifted Interview Shadow run: %w",
			err,
		)
	}
	if runUpdate.RowsAffected() != 1 {
		return ErrInterviewShadowConfigurationConflict
	}
	outboxUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_outbox
		SET delivery_status = 'FAILED',
		    lease_expires_at = NULL,
		    last_failure_code = $5,
		    failed_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE id = $1
		  AND evaluation_id = $2
		  AND evaluation_revision_id = $3
		  AND owner_user_id = $4
		  AND delivery_status = 'PENDING'
	`, claim.OutboxID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		interviewShadowConfigurationChangedCode)
	if err != nil {
		return fmt.Errorf(
			"fail configuration-drifted Interview Shadow outbox: %w",
			err,
		)
	}
	if outboxUpdate.RowsAffected() != 1 {
		return ErrInterviewShadowLeaseLost
	}
	stateUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_revision_states
		SET evaluation_status = 'FAILED',
		    is_final = false,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND revision_id = $2
		  AND owner_user_id = $3
		  AND evaluation_status = 'RUNNING'
		  AND completed_at IS NULL
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID)
	if err != nil {
		return fmt.Errorf(
			"fail configuration-drifted Interview Shadow revision: %w",
			err,
		)
	}
	if stateUpdate.RowsAffected() != 1 {
		return ErrInterviewShadowLeaseLost
	}
	return nil
}

const interviewShadowCandidateOwnerSQL = `
	SELECT
		outbox.owner_user_id::text,
		outbox.evaluation_id::text,
		outbox.evaluation_revision_id::text,
		outbox.id::text
	FROM evaluation_outbox AS outbox
	JOIN evaluation_ledgers AS ledger
	  ON ledger.id = outbox.evaluation_id
	 AND ledger.owner_user_id = outbox.owner_user_id
	JOIN evaluation_revisions AS revision
	  ON revision.evaluation_id = ledger.id
	 AND revision.id = outbox.evaluation_revision_id
	 AND revision.owner_user_id = ledger.owner_user_id
	JOIN evaluation_revision_states AS state
	  ON state.evaluation_id = ledger.id
	 AND state.revision_id = revision.id
	 AND state.owner_user_id = ledger.owner_user_id
	JOIN evaluation_evidence_snapshots AS snapshot
	  ON snapshot.id = ledger.input_snapshot_id
	 AND snapshot.owner_user_id = ledger.owner_user_id
	JOIN identity_users AS owner
	  ON owner.id = ledger.owner_user_id
	WHERE outbox.channel = 'SCENE'
	  AND outbox.delivery_status = 'PENDING'
	  AND outbox.available_at <= transaction_timestamp()
	  AND (
	      outbox.lease_expires_at IS NULL
	      OR outbox.lease_expires_at <= clock_timestamp()
	  )
	  AND outbox.attempt_count < $1
	  AND ledger.scope = 'SESSION'
	  AND ledger.scene_type = 'INTERVIEW'
	  AND snapshot.practice_session_id = ledger.practice_session_id
	  AND snapshot.scope = ledger.scope
	  AND snapshot.scene_type = ledger.scene_type
	  AND snapshot.input_revision = ledger.input_revision
	  AND revision.channels = ARRAY['SCENE']::text[]
	  AND revision.scene_strategy_ref = 'interview-scene-shadow/v1'
	  AND revision.pipeline_version = 'evaluation-pipeline-shadow/v1'
	  AND revision.schema_version = 'evaluation-schema/1.0.0'
	  AND state.evaluation_status IN ('QUEUED', 'RUNNING')
	  AND owner.account_status = 'active'
	  AND NOT EXISTS (
	      SELECT 1
	      FROM evaluation_deletion_fences AS fence
	      WHERE fence.owner_user_id = ledger.owner_user_id
	  )
	  AND NOT EXISTS (
	      SELECT 1
	      FROM evaluation_revisions AS later
	      WHERE later.evaluation_id = revision.evaluation_id
	        AND later.revision > revision.revision
	  )
	ORDER BY
		CASE
			WHEN outbox.lease_expires_at IS NOT NULL THEN 0
			ELSE 1
		END,
		outbox.available_at,
		outbox.created_at,
		outbox.id
	LIMIT 1
`

func failOneExhaustedInterviewShadow(
	ctx context.Context,
	tx pgx.Tx,
	maxAttempts int,
) (bool, error) {
	var ownerUserID string
	var evaluationID string
	var revisionID string
	var outboxID string
	err := tx.QueryRow(ctx, `
		SELECT
			outbox.owner_user_id::text,
			outbox.evaluation_id::text,
			outbox.evaluation_revision_id::text,
			outbox.id::text
		FROM evaluation_outbox AS outbox
		JOIN evaluation_ledgers AS ledger
		  ON ledger.id = outbox.evaluation_id
		 AND ledger.owner_user_id = outbox.owner_user_id
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.id = outbox.evaluation_revision_id
		 AND revision.owner_user_id = ledger.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = ledger.id
		 AND state.revision_id = revision.id
		 AND state.owner_user_id = ledger.owner_user_id
		JOIN identity_users AS owner
		  ON owner.id = ledger.owner_user_id
		WHERE outbox.channel = 'SCENE'
		  AND outbox.delivery_status = 'PENDING'
		  AND outbox.lease_expires_at <= clock_timestamp()
		  AND outbox.attempt_count >= $1
		  AND ledger.scope = 'SESSION'
		  AND ledger.scene_type = 'INTERVIEW'
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $2
		  AND revision.pipeline_version = $3
		  AND revision.schema_version = $4
		  AND state.evaluation_status = 'RUNNING'
		  AND owner.account_status = 'active'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_deletion_fences AS fence
		      WHERE fence.owner_user_id = ledger.owner_user_id
		  )
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.revision > revision.revision
		  )
		ORDER BY outbox.lease_expires_at, outbox.created_at, outbox.id
		LIMIT 1
	`, maxAttempts, InterviewShadowStrategyRef,
		InterviewShadowPipelineVersion, SchemaVersion).Scan(
		&ownerUserID,
		&evaluationID,
		&revisionID,
		&outboxID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"select exhausted Interview Shadow owner: %w",
			err,
		)
	}
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		if errors.Is(err, ErrAccountUnavailable) {
			return false, nil
		}
		return false, err
	}
	if err := lockEvaluationLedgerAndRevisionRows(
		ctx,
		tx,
		ownerUserID,
		evaluationID,
		revisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if err := lockEvaluationRevisionRuntimeRows(
		ctx,
		tx,
		ownerUserID,
		evaluationID,
		revisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	err = tx.QueryRow(ctx, `
		UPDATE evaluation_outbox AS outbox
		SET delivery_status = 'FAILED',
		    lease_expires_at = NULL,
		    last_failure_code = 'attempts_exhausted',
		    failed_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE outbox.id = $6
		  AND outbox.evaluation_id = $7
		  AND outbox.evaluation_revision_id = $8
		  AND outbox.owner_user_id = $1
		  AND outbox.channel = 'SCENE'
		  AND outbox.delivery_status = 'PENDING'
		  AND outbox.lease_expires_at <= clock_timestamp()
		  AND outbox.attempt_count >= $2
		  AND EXISTS (
		      SELECT 1
		      FROM evaluation_ledgers AS ledger
		      JOIN evaluation_revisions AS revision
		        ON revision.evaluation_id = ledger.id
		       AND revision.id = outbox.evaluation_revision_id
		       AND revision.owner_user_id = ledger.owner_user_id
		      JOIN evaluation_revision_states AS state
		        ON state.evaluation_id = ledger.id
		       AND state.revision_id = revision.id
		       AND state.owner_user_id = ledger.owner_user_id
		      WHERE ledger.id = outbox.evaluation_id
		        AND ledger.owner_user_id = outbox.owner_user_id
		        AND ledger.scope = 'SESSION'
		        AND ledger.scene_type = 'INTERVIEW'
		        AND revision.channels = ARRAY['SCENE']::text[]
		        AND revision.scene_strategy_ref = $3
		        AND revision.pipeline_version = $4
		        AND revision.schema_version = $5
		        AND state.evaluation_status = 'RUNNING'
		        AND NOT EXISTS (
		            SELECT 1
		            FROM evaluation_revisions AS later
		            WHERE later.evaluation_id =
		                revision.evaluation_id
		              AND later.revision > revision.revision
		        )
		  )
		RETURNING
			outbox.id::text,
			outbox.evaluation_id::text,
			outbox.evaluation_revision_id::text
	`, ownerUserID, maxAttempts, InterviewShadowStrategyRef,
		InterviewShadowPipelineVersion, SchemaVersion,
		outboxID, evaluationID, revisionID).Scan(
		&outboxID,
		&evaluationID,
		&revisionID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"fail exhausted Interview Shadow outbox: %w",
			err,
		)
	}
	runUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_module_runs
		SET run_status = 'FAILED',
		    last_failure_code = 'attempts_exhausted',
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE outbox_id = $1
		  AND owner_user_id = $2
		  AND run_status = 'RUNNING'
	`, outboxID, ownerUserID)
	if err != nil {
		return false, fmt.Errorf(
			"fail exhausted Interview Shadow run: %w",
			err,
		)
	}
	if runUpdate.RowsAffected() != 1 {
		return false, ErrInterviewShadowLeaseLost
	}
	stateUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_revision_states
		SET evaluation_status = 'FAILED',
		    is_final = false,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND revision_id = $2
		  AND owner_user_id = $3
		  AND evaluation_status = 'RUNNING'
		  AND completed_at IS NULL
	`, evaluationID, revisionID, ownerUserID)
	if err != nil {
		return false, fmt.Errorf(
			"fail exhausted Interview Shadow revision: %w",
			err,
		)
	}
	if stateUpdate.RowsAffected() != 1 {
		return false, ErrInterviewShadowLeaseLost
	}
	return true, nil
}

func sameInterviewShadowResult(
	ctx context.Context,
	tx pgx.Tx,
	claim InterviewShadowClaim,
	result InterviewShadowResult,
	payload []byte,
) (bool, error) {
	var providerRequestID any
	if result.Provider != nil {
		providerRequestID = result.Provider.RequestID
	}
	var same bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM evaluation_interview_scene_results
			WHERE module_run_id = $1
			  AND evaluation_id = $2
			  AND evaluation_revision_id = $3
			  AND owner_user_id = $4
			  AND channel = 'SCENE'
			  AND strategy_ref = $5
			  AND practice_session_id = $6
			  AND input_snapshot_id = $7
			  AND input_revision = $8
			  AND scene_type = 'INTERVIEW'
			  AND snapshot_hash = $9
			  AND full_config_hash = $10
			  AND prompt_version = $11
			  AND provider = $12
			  AND model = $13
			  AND provider_request_id IS NOT DISTINCT FROM $14
			  AND fencing_token = $15
			  AND result_payload = $16::jsonb
		)
	`, claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		providerRequestID, claim.FencingToken, payload).Scan(&same); err != nil {
		return false, fmt.Errorf(
			"read Interview Shadow completion replay: %w",
			err,
		)
	}
	return same, nil
}

func interviewShadowResultMatchesRuntime(
	claim InterviewShadowClaim,
	result InterviewShadowResult,
) bool {
	if result.Provider == nil {
		return result.Scoreability == InterviewScoreabilityInsufficient
	}
	return result.Provider.Provider == claim.Provider &&
		result.Provider.Model == claim.Model &&
		result.Provider.PromptVersion == claim.PromptVersion &&
		result.Provider.ResponseSchema ==
			InterviewShadowProviderSchemaVersion
}

func interviewShadowBackoff(attempt int) time.Duration {
	if attempt < 1 {
		return time.Second
	}
	backoff := time.Second << min(attempt-1, 8)
	if backoff > 5*time.Minute {
		return 5 * time.Minute
	}
	return backoff
}
