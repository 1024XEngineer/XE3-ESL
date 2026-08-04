package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	durableSceneJobConfigurationChangedCode = "runtime_configuration_changed"
	durableSceneJobRevisionSupersededCode   = "revision_superseded"
	interviewShadowConfigurationChangedCode = durableSceneJobConfigurationChangedCode
	interviewShadowRevisionSupersededCode   = durableSceneJobRevisionSupersededCode
)

var _ scoring.InterviewShadowRuntimeRepository = (*PostgresRepository)(nil)

var interviewDurableSceneJobSpec = durableSceneJobSpec{
	sceneType:       evaluation.SceneInterview,
	strategyRef:     scoring.InterviewShadowStrategyRef,
	pipelineVersion: scoring.InterviewShadowPipelineVersion,
	promptVersion:   scoring.InterviewShadowPromptVersion,
	resultTable:     "evaluation_interview_scene_results",
}

func durableConfigurationFromInterview(
	source scoring.InterviewShadowRuntimeConfiguration,
) durableSceneJobConfiguration {
	return durableSceneJobConfiguration{
		MaxAttempts:     source.MaxAttempts,
		LeaseDuration:   source.LeaseDuration,
		StrategyRef:     source.StrategyRef,
		PipelineVersion: source.PipelineVersion,
		FullConfigHash:  source.FullConfigHash,
		PromptVersion:   source.PromptVersion,
		Provider:        source.Provider,
		Model:           source.Model,
	}
}

func durableClaimFromInterview(
	source scoring.InterviewShadowClaim,
) durableSceneJobClaim {
	return durableSceneJobClaim{
		OutboxID:             source.OutboxID,
		ModuleRunID:          source.ModuleRunID,
		EvaluationID:         source.EvaluationID,
		EvaluationRevisionID: source.EvaluationRevisionID,
		OwnerUserID:          source.OwnerUserID,
		Revision:             source.Revision,
		StrategyRef:          source.StrategyRef,
		PipelineVersion:      source.PipelineVersion,
		AttemptCount:         source.AttemptCount,
		FencingToken:         source.FencingToken,
		LeaseExpiresAt:       source.LeaseExpiresAt,
		FullConfigHash:       source.FullConfigHash,
		PromptVersion:        source.PromptVersion,
		Provider:             source.Provider,
		Model:                source.Model,
		Snapshot:             source.Snapshot,
	}
}

func interviewClaimFromDurable(
	source durableSceneJobClaim,
) scoring.InterviewShadowClaim {
	return scoring.InterviewShadowClaim{
		OutboxID:             source.OutboxID,
		ModuleRunID:          source.ModuleRunID,
		EvaluationID:         source.EvaluationID,
		EvaluationRevisionID: source.EvaluationRevisionID,
		OwnerUserID:          source.OwnerUserID,
		Revision:             source.Revision,
		StrategyRef:          source.StrategyRef,
		PipelineVersion:      source.PipelineVersion,
		AttemptCount:         source.AttemptCount,
		FencingToken:         source.FencingToken,
		LeaseExpiresAt:       source.LeaseExpiresAt,
		FullConfigHash:       source.FullConfigHash,
		PromptVersion:        source.PromptVersion,
		Provider:             source.Provider,
		Model:                source.Model,
		Snapshot:             source.Snapshot,
	}
}

func (r *PostgresRepository) ClaimInterviewShadow(
	ctx context.Context,
	configuration scoring.InterviewShadowRuntimeConfiguration,
) (scoring.InterviewShadowClaim, bool, error) {
	claim, acquired, err := r.claimDurableSceneJob(
		ctx,
		interviewDurableSceneJobSpec,
		durableConfigurationFromInterview(configuration),
	)
	return interviewClaimFromDurable(claim), acquired, err
}

func (r *PostgresRepository) claimDurableSceneJob(
	ctx context.Context,
	spec durableSceneJobSpec,
	configuration durableSceneJobConfiguration,
) (durableSceneJobClaim, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!configuration.valid(spec) {
		return durableSceneJobClaim{}, false, evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"begin durable Scene job claim: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exhausted, err := failOneExhaustedDurableSceneJob(
		ctx,
		tx,
		spec,
		configuration.MaxAttempts,
	)
	if err != nil {
		return durableSceneJobClaim{}, false, err
	}
	if exhausted {
		if err := tx.Commit(ctx); err != nil {
			return durableSceneJobClaim{}, false, fmt.Errorf(
				"commit exhausted durable Scene job: %w",
				err,
			)
		}
		return durableSceneJobClaim{}, false, nil
	}

	var ownerUserID string
	var evaluationID string
	var evaluationRevisionID string
	var outboxID string
	err = tx.QueryRow(ctx, durableSceneJobCandidateOwnerSQL,
		configuration.MaxAttempts,
		spec.sceneType,
		spec.strategyRef,
		spec.pipelineVersion,
		evaluation.SchemaVersion,
		configuration.FullConfigHash[:],
		configuration.PromptVersion,
		configuration.Provider,
		configuration.Model).Scan(
		&ownerUserID,
		&evaluationID,
		&evaluationRevisionID,
		&outboxID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return durableSceneJobClaim{}, false, nil
	}
	if err != nil {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"select durable Scene job candidate owner: %w",
			err,
		)
	}
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		if errors.Is(err, evaluation.ErrAccountUnavailable) {
			return durableSceneJobClaim{}, false, nil
		}
		return durableSceneJobClaim{}, false, err
	}
	if err := lockEvaluationLedgerAndRevisionRows(
		ctx,
		tx,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return durableSceneJobClaim{}, false, nil
		}
		return durableSceneJobClaim{}, false, err
	}
	if err := lockEvaluationRevisionRuntimeRows(
		ctx,
		tx,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return durableSceneJobClaim{}, false, nil
		}
		return durableSceneJobClaim{}, false, err
	}

	var claim durableSceneJobClaim
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
			        AND ledger.scene_type = $10
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
		spec.strategyRef,
		spec.pipelineVersion,
		evaluation.SchemaVersion,
		configuration.LeaseDuration.Seconds(),
		outboxID,
		evaluationID,
		evaluationRevisionID,
		spec.sceneType).Scan(
		&claim.OutboxID,
		&claim.EvaluationID,
		&claim.EvaluationRevisionID,
		&claim.OwnerUserID,
		&claim.AttemptCount,
		&claim.FencingToken,
		&leaseExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return durableSceneJobClaim{}, false, nil
	}
	if err != nil {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"claim durable Scene job outbox: %w",
			err,
		)
	}
	if !leaseExpiresAt.Valid {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"claim durable Scene job outbox: missing lease",
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
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"mark durable Scene job revision running: %w",
			err,
		)
	}
	if stateUpdate.RowsAffected() != 1 {
		return durableSceneJobClaim{}, false,
			scoring.ErrRuntimeLeaseLost
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
		claim.OwnerUserID, spec.strategyRef).Scan(
		&claim.Revision,
		&claim.PipelineVersion,
	); err != nil {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"read claimed durable Scene job revision: %w",
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
			  AND ledger.scene_type = $7
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
		spec.strategyRef,
		spec.pipelineVersion,
		evaluation.SchemaVersion,
		spec.sceneType,
	))
	if err != nil {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"read claimed durable Scene job snapshot: %w",
			err,
		)
	}

	claim.StrategyRef = spec.strategyRef
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
			'SESSION', $16, $9, $10, $11, $12, $13,
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
		claim.AttemptCount, claim.FencingToken,
		spec.sceneType).Scan(
		&claim.ModuleRunID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if failErr := failDurableSceneJobConfigurationChange(
			ctx,
			tx,
			claim,
		); failErr != nil {
			return durableSceneJobClaim{}, false, failErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return durableSceneJobClaim{}, false, fmt.Errorf(
				"commit durable Scene job configuration failure: %w",
				commitErr,
			)
		}
		return durableSceneJobClaim{}, false,
			scoring.ErrRuntimeConfigurationConflict
	}
	if err != nil {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"upsert durable Scene job module run: %w",
			err,
		)
	}
	if !claim.valid(spec) {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"validate claimed durable Scene job: %w",
			evaluation.ErrInvalidRequest,
		)
	}
	if err := tx.Commit(ctx); err != nil {
		return durableSceneJobClaim{}, false, fmt.Errorf(
			"commit durable Scene job claim: %w",
			err,
		)
	}
	return claim, true, nil
}

func (r *PostgresRepository) CompleteInterviewShadow(
	ctx context.Context,
	claim scoring.InterviewShadowClaim,
	result scoring.InterviewShadowResult,
) error {
	if ctx == nil || !claim.Valid() ||
		scoring.ValidateInterviewShadowResult(claim.Snapshot, result) != nil ||
		!interviewShadowResultMatchesRuntime(claim, result) {
		return evaluation.ErrInvalidRequest
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return evaluation.ErrInvalidRequest
	}
	report, err := report.ProjectInterviewFormalReport(claim.Snapshot, result)
	if err != nil {
		return err
	}
	var providerRequestID any
	if result.Provider != nil {
		providerRequestID = result.Provider.RequestID
	}
	return r.completeDurableSceneJob(
		ctx,
		interviewDurableSceneJobSpec,
		durableClaimFromInterview(claim),
		payload,
		providerRequestID,
		report,
	)
}

func (r *PostgresRepository) completeDurableSceneJob(
	ctx context.Context,
	spec durableSceneJobSpec,
	claim durableSceneJobClaim,
	payload []byte,
	providerRequestID any,
	report report.FormalReport,
) error {
	if r == nil || r.pool == nil || ctx == nil ||
		!claim.valid(spec) || len(payload) == 0 || !report.Valid() ||
		report.SceneType != spec.sceneType {
		return evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin durable Scene job completion: %w", err)
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
			return scoring.ErrRuntimeLeaseLost
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
			return scoring.ErrRuntimeLeaseLost
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
		  AND snapshot.scene_type = $17
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
		spec.pipelineVersion, evaluation.SchemaVersion,
		spec.sceneType).Scan(
		&deliveryStatus,
		&persistedToken,
		&leaseActive,
		&runStatus,
		&revisionStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return scoring.ErrRuntimeLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock durable Scene job completion: %w", err)
	}
	if persistedToken != claim.FencingToken {
		return scoring.ErrRuntimeLeaseLost
	}
	if deliveryStatus == "DELIVERED" &&
		runStatus == "READY" &&
		revisionStatus == "READY" {
		same, err := sameDurableSceneJobResult(
			ctx,
			tx,
			spec,
			claim,
			providerRequestID,
			payload,
		)
		if err != nil {
			return err
		}
		if !same {
			return scoring.ErrRuntimeConfigurationConflict
		}
		if err := persistFormalReportAndLearningProfile(
			ctx,
			tx,
			claim,
			report,
		); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf(
				"commit durable Scene job completion replay: %w",
				err,
			)
		}
		return nil
	}
	if deliveryStatus != "PENDING" ||
		runStatus != "RUNNING" ||
		revisionStatus != "RUNNING" ||
		!leaseActive {
		return scoring.ErrRuntimeLeaseLost
	}

	insertResultSQL := fmt.Sprintf(`
		INSERT INTO %s (
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
			$17, $9, $10, $11, $12, $13, $14, $15,
			$16::jsonb
		)
	`, spec.resultTable)
	if _, err := tx.Exec(ctx, insertResultSQL,
		claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		providerRequestID, claim.FencingToken, payload,
		spec.sceneType); err != nil {
		return fmt.Errorf("insert durable Scene job result: %w", err)
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
		return fmt.Errorf("complete durable Scene job run: %w", err)
	}
	if runUpdate.RowsAffected() != 1 {
		return scoring.ErrRuntimeLeaseLost
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
		return fmt.Errorf("deliver durable Scene job outbox: %w", err)
	}
	if outboxUpdate.RowsAffected() != 1 {
		return scoring.ErrRuntimeLeaseLost
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
		return fmt.Errorf("ready durable Scene job revision: %w", err)
	}
	if stateUpdate.RowsAffected() != 1 {
		return scoring.ErrRuntimeLeaseLost
	}
	if err := persistFormalReportAndLearningProfile(
		ctx,
		tx,
		claim,
		report,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit durable Scene job completion: %w", err)
	}
	return nil
}

func (r *PostgresRepository) FailInterviewShadow(
	ctx context.Context,
	claim scoring.InterviewShadowClaim,
	failure scoring.InterviewShadowFailure,
	configuration scoring.InterviewShadowRuntimeConfiguration,
) (scoring.InterviewShadowRuntimeStatus, error) {
	status, err := r.failDurableSceneJob(
		ctx,
		interviewDurableSceneJobSpec,
		durableClaimFromInterview(claim),
		durableSceneJobFailure{
			Code:      failure.Code,
			Retryable: failure.Retryable,
		},
		durableConfigurationFromInterview(configuration),
	)
	return scoring.InterviewShadowRuntimeStatus(status), err
}

func (r *PostgresRepository) failDurableSceneJob(
	ctx context.Context,
	spec durableSceneJobSpec,
	claim durableSceneJobClaim,
	failure durableSceneJobFailure,
	configuration durableSceneJobConfiguration,
) (durableSceneJobStatus, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!failure.valid() ||
		!durableSceneJobConfigurationMatchesClaim(
			claim,
			configuration,
			spec,
		) {
		return "", evaluation.ErrInvalidRequest
	}
	requeue := failure.Retryable &&
		claim.AttemptCount < configuration.MaxAttempts
	backoff := durableSceneJobBackoff(claim.AttemptCount)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin durable Scene job failure: %w", err)
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
			return "", scoring.ErrRuntimeLeaseLost
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
			return "", scoring.ErrRuntimeLeaseLost
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
		evaluation.SchemaVersion).Scan(&outboxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", scoring.ErrRuntimeLeaseLost
	}
	if err != nil {
		return "", fmt.Errorf("fail durable Scene job outbox: %w", err)
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
			"fail durable Scene job module run: %w",
			err,
		)
	}
	if runUpdate.RowsAffected() != 1 {
		return "", scoring.ErrRuntimeLeaseLost
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
				"fail durable Scene job revision: %w",
				err,
			)
		}
		if stateUpdate.RowsAffected() != 1 {
			return "", scoring.ErrRuntimeLeaseLost
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf(
			"commit durable Scene job failure: %w",
			err,
		)
	}
	if requeue {
		return durableSceneJobPending, nil
	}
	return durableSceneJobFailed, nil
}

func (r *PostgresRepository) GetInterviewShadowState(
	ctx context.Context,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) (scoring.InterviewShadowReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validUUID(evaluationID) ||
		!validUUID(evaluationRevisionID) {
		return scoring.InterviewShadowReadState{}, evaluation.ErrInvalidRequest
	}
	state, _, err := getInterviewShadowState(
		ctx,
		r.pool,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	)
	return state, err
}

func getInterviewShadowState(
	ctx context.Context,
	db queryable,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) (scoring.InterviewShadowReadState, *evidence.EvidenceSnapshot, error) {
	raw, snapshot, err := getDurableSceneJobState(
		ctx,
		db,
		interviewDurableSceneJobSpec,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	)
	if err != nil {
		return scoring.InterviewShadowReadState{}, nil, err
	}
	state := scoring.InterviewShadowReadState{
		ModuleStatus: scoring.InterviewShadowRuntimeStatus(
			raw.ModuleStatus,
		),
		FullConfigHash: raw.FullConfigHash,
	}
	if raw.Failure != nil {
		state.Failure = &scoring.InterviewShadowFailure{
			Code:      raw.Failure.Code,
			Retryable: raw.Failure.Retryable,
		}
	}
	if raw.ModuleStatus == durableSceneJobReady {
		if snapshot == nil {
			return scoring.InterviewShadowReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		result, decodeErr := scoring.DecodeInterviewShadowResult(
			raw.ResultPayload,
		)
		if decodeErr != nil ||
			scoring.ValidateInterviewShadowResult(*snapshot, result) != nil {
			return scoring.InterviewShadowReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		state.Result = &result
	}
	return state, snapshot, nil
}

func getDurableSceneJobState(
	ctx context.Context,
	db queryable,
	spec durableSceneJobSpec,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) (durableSceneJobReadState, *evidence.EvidenceSnapshot, error) {
	if ctx == nil || db == nil || !spec.valid() ||
		!validUUID(ownerUserID) ||
		!validUUID(evaluationID) ||
		!validUUID(evaluationRevisionID) {
		return durableSceneJobReadState{}, nil, evaluation.ErrInvalidRequest
	}
	var deliveryStatus string
	var leaseActive bool
	var moduleStatus string
	var revisionStatus string
	var failureCode string
	var payload []byte
	var inputSnapshotID string
	var fullConfigHash []byte
	query := fmt.Sprintf(`
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
		LEFT JOIN %s AS result
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
		  AND ledger.scene_type = $7
	`, spec.resultTable)
	err := db.QueryRow(ctx, query,
		ownerUserID, evaluationID, evaluationRevisionID,
		spec.strategyRef,
		spec.pipelineVersion,
		evaluation.SchemaVersion,
		spec.sceneType).Scan(
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
		return durableSceneJobReadState{}, nil, evaluation.ErrNotFound
	}
	if err != nil {
		return durableSceneJobReadState{}, nil, fmt.Errorf(
			"read durable Scene job state: %w",
			err,
		)
	}
	var persistedConfigHash [32]byte
	if len(fullConfigHash) != 0 {
		if len(fullConfigHash) != len(persistedConfigHash) {
			return durableSceneJobReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		copy(persistedConfigHash[:], fullConfigHash)
	}
	switch deliveryStatus {
	case "PENDING":
		if moduleStatus == "RUNNING" &&
			leaseActive {
			return durableSceneJobReadState{
				ModuleStatus:   durableSceneJobRunning,
				FullConfigHash: persistedConfigHash,
			}, nil, nil
		}
		if revisionStatus != "QUEUED" &&
			revisionStatus != "RUNNING" {
			return durableSceneJobReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		return durableSceneJobReadState{
			ModuleStatus:   durableSceneJobPending,
			FullConfigHash: persistedConfigHash,
		}, nil, nil
	case "FAILED":
		if moduleStatus != "FAILED" ||
			revisionStatus != "FAILED" ||
			!durableSceneJobFailureCodePattern.MatchString(failureCode) {
			return durableSceneJobReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		return durableSceneJobReadState{
			ModuleStatus:   durableSceneJobFailed,
			FullConfigHash: persistedConfigHash,
			Failure: &durableSceneJobFailure{
				Code: failureCode,
			},
		}, nil, nil
	case "DELIVERED":
		if moduleStatus != "READY" ||
			revisionStatus != "READY" ||
			len(payload) == 0 {
			return durableSceneJobReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		snapshot, err := selectEvidenceSnapshotByID(
			ctx,
			db,
			ownerUserID,
			inputSnapshotID,
		)
		if err != nil {
			return durableSceneJobReadState{}, nil, err
		}
		return durableSceneJobReadState{
			ModuleStatus:   durableSceneJobReady,
			FullConfigHash: persistedConfigHash,
			ResultPayload:  payload,
		}, &snapshot, nil
	default:
		return durableSceneJobReadState{}, nil,
			scoring.ErrRuntimeConfigurationConflict
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
		durableSceneJobRevisionSupersededCode); err != nil {
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
		durableSceneJobRevisionSupersededCode); err != nil {
		return fmt.Errorf(
			"cancel superseded Evaluation outbox runtime: %w",
			err,
		)
	}
	return nil
}

func failDurableSceneJobConfigurationChange(
	ctx context.Context,
	tx pgx.Tx,
	claim durableSceneJobClaim,
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
		durableSceneJobConfigurationChangedCode)
	if err != nil {
		return fmt.Errorf(
			"fail configuration-drifted Interview Shadow run: %w",
			err,
		)
	}
	if runUpdate.RowsAffected() != 1 {
		return scoring.ErrRuntimeConfigurationConflict
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
		durableSceneJobConfigurationChangedCode)
	if err != nil {
		return fmt.Errorf(
			"fail configuration-drifted Interview Shadow outbox: %w",
			err,
		)
	}
	if outboxUpdate.RowsAffected() != 1 {
		return scoring.ErrRuntimeLeaseLost
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
		return scoring.ErrRuntimeLeaseLost
	}
	return nil
}

const durableSceneJobCandidateOwnerSQL = `
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
	  AND ledger.scene_type = $2
	  AND snapshot.practice_session_id = ledger.practice_session_id
	  AND snapshot.scope = ledger.scope
	  AND snapshot.scene_type = ledger.scene_type
	  AND snapshot.input_revision = ledger.input_revision
	  AND revision.channels = ARRAY['SCENE']::text[]
	  AND revision.scene_strategy_ref = $3
	  AND revision.pipeline_version = $4
	  AND revision.schema_version = $5
	  AND state.evaluation_status IN ('QUEUED', 'RUNNING')
	  AND (
	      NOT EXISTS (
	          SELECT 1
	          FROM evaluation_module_runs AS existing_run
	          WHERE existing_run.evaluation_id = outbox.evaluation_id
	            AND existing_run.evaluation_revision_id =
	                outbox.evaluation_revision_id
	            AND existing_run.owner_user_id = outbox.owner_user_id
	            AND existing_run.channel = outbox.channel
	            AND existing_run.strategy_ref = revision.scene_strategy_ref
	      )
	      OR EXISTS (
	          SELECT 1
	          FROM evaluation_module_runs AS existing_run
	          WHERE existing_run.evaluation_id = outbox.evaluation_id
	            AND existing_run.evaluation_revision_id =
	                outbox.evaluation_revision_id
	            AND existing_run.owner_user_id = outbox.owner_user_id
	            AND existing_run.channel = outbox.channel
	            AND existing_run.strategy_ref = revision.scene_strategy_ref
	            AND existing_run.run_status = 'RUNNING'
	            AND existing_run.full_config_hash = $6
	            AND existing_run.prompt_version = $7
	            AND existing_run.provider = $8
	            AND existing_run.model = $9
	      )
	  )
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

func failOneExhaustedDurableSceneJob(
	ctx context.Context,
	tx pgx.Tx,
	spec durableSceneJobSpec,
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
		  AND ledger.scene_type = $5
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
	`, maxAttempts, spec.strategyRef,
		spec.pipelineVersion, evaluation.SchemaVersion,
		spec.sceneType).Scan(
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
		if errors.Is(err, evaluation.ErrAccountUnavailable) {
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
		        AND ledger.scene_type = $9
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
	`, ownerUserID, maxAttempts, spec.strategyRef,
		spec.pipelineVersion, evaluation.SchemaVersion,
		outboxID, evaluationID, revisionID,
		spec.sceneType).Scan(
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
		return false, scoring.ErrRuntimeLeaseLost
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
		return false, scoring.ErrRuntimeLeaseLost
	}
	return true, nil
}

func sameDurableSceneJobResult(
	ctx context.Context,
	tx pgx.Tx,
	spec durableSceneJobSpec,
	claim durableSceneJobClaim,
	providerRequestID any,
	payload []byte,
) (bool, error) {
	var same bool
	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM %s
			WHERE module_run_id = $1
			  AND evaluation_id = $2
			  AND evaluation_revision_id = $3
			  AND owner_user_id = $4
			  AND channel = 'SCENE'
			  AND strategy_ref = $5
			  AND practice_session_id = $6
			  AND input_snapshot_id = $7
			  AND input_revision = $8
			  AND scene_type = $17
			  AND snapshot_hash = $9
			  AND full_config_hash = $10
			  AND prompt_version = $11
			  AND provider = $12
			  AND model = $13
			  AND provider_request_id IS NOT DISTINCT FROM $14
			  AND fencing_token = $15
			  AND result_payload = $16::jsonb
			)
	`, spec.resultTable)
	if err := tx.QueryRow(ctx, query,
		claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		providerRequestID, claim.FencingToken, payload,
		spec.sceneType).Scan(&same); err != nil {
		return false, fmt.Errorf(
			"read durable Scene job completion replay: %w",
			err,
		)
	}
	return same, nil
}

func interviewShadowResultMatchesRuntime(
	claim scoring.InterviewShadowClaim,
	result scoring.InterviewShadowResult,
) bool {
	if result.Provider == nil {
		return result.Scoreability == scoring.InterviewScoreabilityInsufficient
	}
	return result.Provider.Provider == claim.Provider &&
		result.Provider.Model == claim.Model &&
		result.Provider.PromptVersion == claim.PromptVersion &&
		result.Provider.ResponseSchema ==
			scoring.InterviewShadowProviderSchemaVersion
}

func durableSceneJobBackoff(attempt int) time.Duration {
	if attempt < 1 {
		return time.Second
	}
	backoff := time.Second << min(attempt-1, 8)
	if backoff > 5*time.Minute {
		return 5 * time.Minute
	}
	return backoff
}
