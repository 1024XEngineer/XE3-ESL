package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

var _ scoring.GeneralSceneRuntimeRepository = (*PostgresRepository)(nil)

func generalSceneDurableJobSpec(
	sceneType evaluation.SceneType,
) (durableSceneJobSpec, bool) {
	if !generalSceneTypeSupported(sceneType) {
		return durableSceneJobSpec{}, false
	}
	return durableSceneJobSpec{
		sceneType:       sceneType,
		strategyRef:     scoring.GeneralSceneStrategyRef,
		pipelineVersion: scoring.GeneralScenePipelineVersion,
		promptVersion:   scoring.GeneralScenePromptVersion,
		resultTable:     "evaluation_general_scene_results",
	}, true
}

func durableConfigurationFromGeneralScene(
	source scoring.GeneralSceneRuntimeConfiguration,
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

func durableClaimFromGeneralScene(
	source scoring.GeneralSceneClaim,
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

func generalSceneClaimFromDurable(
	source durableSceneJobClaim,
) scoring.GeneralSceneClaim {
	return scoring.GeneralSceneClaim{
		OutboxID:             source.OutboxID,
		ModuleRunID:          source.ModuleRunID,
		EvaluationID:         source.EvaluationID,
		EvaluationRevisionID: source.EvaluationRevisionID,
		OwnerUserID:          source.OwnerUserID,
		Revision:             source.Revision,
		SceneType:            source.Snapshot.SceneType,
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

func (repository *PostgresRepository) ClaimGeneralScene(
	ctx context.Context,
	sceneType evaluation.SceneType,
	configuration scoring.GeneralSceneRuntimeConfiguration,
) (scoring.GeneralSceneClaim, bool, error) {
	spec, ok := generalSceneDurableJobSpec(sceneType)
	effective, valid := configuration.ForScene(sceneType)
	if !ok || !valid {
		return scoring.GeneralSceneClaim{}, false, evaluation.ErrInvalidRequest
	}
	claim, acquired, err := repository.claimDurableSceneJob(
		ctx,
		spec,
		durableConfigurationFromGeneralScene(effective),
	)
	return generalSceneClaimFromDurable(claim), acquired, err
}

func (repository *PostgresRepository) LoadGeneralSceneAtomicResults(
	ctx context.Context,
	claim scoring.GeneralSceneClaim,
) ([]scoring.GeneralSceneAtomicResult, error) {
	return repository.loadGeneralSceneAtomicResults(ctx, claim, true)
}

func (repository *PostgresRepository) loadGeneralSceneAtomicResults(
	ctx context.Context,
	claim scoring.GeneralSceneClaim,
	requireActiveLease bool,
) ([]scoring.GeneralSceneAtomicResult, error) {
	if repository == nil || repository.pool == nil || ctx == nil ||
		!claim.Valid() || claim.SceneType != evaluation.SceneIELTSSpeaking {
		return nil, evaluation.ErrInvalidRequest
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT attempt.result_payload
		FROM evaluation_general_scene_atomic_attempts AS attempt
		JOIN evaluation_module_runs AS run
		  ON run.id = attempt.module_run_id
		 AND run.owner_user_id = attempt.owner_user_id
		JOIN evaluation_outbox AS outbox
		  ON outbox.id = run.outbox_id
		 AND outbox.evaluation_id = run.evaluation_id
		 AND outbox.evaluation_revision_id = run.evaluation_revision_id
		 AND outbox.owner_user_id = run.owner_user_id
		WHERE attempt.module_run_id = $1
		  AND attempt.owner_user_id = $2
		  AND attempt.input_snapshot_id = $3
		  AND attempt.status = 'READY'
		  AND attempt.provider = $4
		  AND attempt.model = $5
		  AND attempt.prompt_version = $6
		  AND run.full_config_hash = $7
		  AND run.fencing_token = $8
		  AND outbox.fencing_token = $8
		  AND (
		    NOT ($9::boolean)
		    OR (
		      run.run_status = 'RUNNING'
		      AND outbox.delivery_status = 'PENDING'
		      AND outbox.lease_expires_at > clock_timestamp()
		    )
		  )
		ORDER BY
		  CASE attempt.part_id
		    WHEN 'PART_1' THEN 1 WHEN 'PART_2' THEN 2 ELSE 3
		  END,
		  CASE attempt.dimension_key
		    WHEN 'TASK_ACHIEVEMENT' THEN 1
		    WHEN 'CLARITY_COHERENCE' THEN 2
		    WHEN 'LANGUAGE_CONTROL' THEN 3
		    ELSE 4
		  END
	`, claim.ModuleRunID, claim.OwnerUserID, claim.Snapshot.ID,
		claim.Provider, claim.Model, scoring.GeneralSceneAtomicPromptVersion,
		claim.FullConfigHash[:], claim.FencingToken, requireActiveLease)
	if err != nil {
		return nil, fmt.Errorf("query general Scene atomic results: %w", err)
	}
	defer rows.Close()
	results := make([]scoring.GeneralSceneAtomicResult, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan general Scene atomic result: %w", err)
		}
		var result scoring.GeneralSceneAtomicResult
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
			scoring.ValidateGeneralSceneAtomicResult(claim.Snapshot, result) != nil {
			return nil, evaluation.ErrInvalidRequest
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate general Scene atomic results: %w", err)
	}
	return results, nil
}

func (repository *PostgresRepository) RecordGeneralSceneAtomicAttempt(
	ctx context.Context,
	claim scoring.GeneralSceneClaim,
	attempt scoring.GeneralSceneAtomicAttempt,
) error {
	if repository == nil || repository.pool == nil || ctx == nil ||
		!claim.Valid() || claim.SceneType != evaluation.SceneIELTSSpeaking ||
		attempt.AttemptCount != claim.AttemptCount ||
		scoring.ValidateGeneralSceneAtomicAttempt(claim.Snapshot, attempt) != nil {
		return evaluation.ErrInvalidRequest
	}
	var (
		payload           any
		providerRequestID any
		failureCode       any
		failureRetryable  any
	)
	if attempt.ProviderRequestID != "" {
		providerRequestID = attempt.ProviderRequestID
	}
	if attempt.Result != nil {
		encoded, err := json.Marshal(attempt.Result)
		if err != nil {
			return evaluation.ErrInvalidRequest
		}
		payload = encoded
	} else {
		failureCode = attempt.Failure.Code
		failureRetryable = attempt.Failure.Retryable
	}
	command, err := repository.pool.Exec(ctx, `
		INSERT INTO evaluation_general_scene_atomic_attempts (
			module_run_id,
			owner_user_id,
			input_snapshot_id,
			part_id,
			dimension_key,
			attempt_count,
			fencing_token,
			status,
			provider,
			model,
			provider_request_id,
			prompt_version,
			failure_code,
			failure_retryable,
			result_payload
		)
		SELECT
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15::jsonb
		WHERE EXISTS (
			SELECT 1
			FROM evaluation_module_runs AS run
			JOIN evaluation_outbox AS outbox
			  ON outbox.id = run.outbox_id
			 AND outbox.evaluation_id = run.evaluation_id
			 AND outbox.evaluation_revision_id = run.evaluation_revision_id
			 AND outbox.owner_user_id = run.owner_user_id
			WHERE run.id = $1
			  AND run.owner_user_id = $2
			  AND run.input_snapshot_id = $3
			  AND run.scene_type = 'IELTS_SPEAKING'
			  AND run.strategy_ref = $16
			  AND run.full_config_hash = $17
			  AND run.provider = $9
			  AND run.model = $10
			  AND run.attempt_count = $6
			  AND run.fencing_token = $7
			  AND run.run_status = 'RUNNING'
			  AND outbox.delivery_status = 'PENDING'
			  AND outbox.fencing_token = $7
			  AND outbox.lease_expires_at > clock_timestamp()
		)
		ON CONFLICT (
			module_run_id,
			part_id,
			dimension_key,
			attempt_count
		) DO NOTHING
	`, claim.ModuleRunID, claim.OwnerUserID, claim.Snapshot.ID,
		attempt.Key.Part, attempt.Key.Dimension, attempt.AttemptCount,
		claim.FencingToken, attempt.Status, claim.Provider, claim.Model,
		providerRequestID, scoring.GeneralSceneAtomicPromptVersion,
		failureCode, failureRetryable, payload, claim.StrategyRef,
		claim.FullConfigHash[:])
	if err != nil {
		return fmt.Errorf("insert general Scene atomic attempt: %w", err)
	}
	if command.RowsAffected() != 1 {
		return scoring.ErrRuntimeLeaseLost
	}
	return nil
}

func (repository *PostgresRepository) CompleteGeneralScene(
	ctx context.Context,
	claim scoring.GeneralSceneClaim,
	result scoring.GeneralSceneResult,
) error {
	spec, ok := generalSceneDurableJobSpec(claim.SceneType)
	if !ok || ctx == nil || !claim.Valid() ||
		scoring.ValidateGeneralSceneResult(claim.Snapshot, result) != nil {
		return evaluation.ErrInvalidRequest
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return evaluation.ErrInvalidRequest
	}
	if claim.SceneType == evaluation.SceneIELTSSpeaking &&
		result.ScoreabilityStatus == scoring.GeneralSceneScoreabilityProvisional {
		atoms, loadErr := repository.loadGeneralSceneAtomicResults(ctx, claim, false)
		if loadErr != nil {
			return loadErr
		}
		aggregated, aggregateErr := scoring.AggregateGeneralSceneAtoms(
			claim.Snapshot,
			atoms,
		)
		if aggregateErr != nil {
			return evaluation.ErrInvalidRequest
		}
		aggregatedPayload, marshalErr := json.Marshal(aggregated)
		if marshalErr != nil || !bytes.Equal(payload, aggregatedPayload) {
			return evaluation.ErrInvalidRequest
		}
	}
	report, err := report.ProjectGeneralSceneFormalReport(claim.Snapshot, result)
	if err != nil {
		return err
	}
	var providerRequestID any
	if result.Provider != nil && result.Provider.RequestID != "" {
		providerRequestID = result.Provider.RequestID
	}
	return repository.completeDurableSceneJob(
		ctx,
		spec,
		durableClaimFromGeneralScene(claim),
		payload,
		providerRequestID,
		report,
	)
}

func (repository *PostgresRepository) FailGeneralScene(
	ctx context.Context,
	claim scoring.GeneralSceneClaim,
	failure scoring.GeneralSceneFailure,
	configuration scoring.GeneralSceneRuntimeConfiguration,
) (scoring.GeneralSceneRuntimeStatus, error) {
	spec, ok := generalSceneDurableJobSpec(claim.SceneType)
	effective, valid := configuration.ForScene(claim.SceneType)
	if !ok || !valid {
		return "", evaluation.ErrInvalidRequest
	}
	status, err := repository.failDurableSceneJob(
		ctx,
		spec,
		durableClaimFromGeneralScene(claim),
		durableSceneJobFailure(failure),
		durableConfigurationFromGeneralScene(effective),
	)
	return scoring.GeneralSceneRuntimeStatus(status), err
}
