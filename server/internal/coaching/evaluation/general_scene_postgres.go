package evaluation

import (
	"context"
	"encoding/json"
)

var _ GeneralSceneRuntimeRepository = (*PostgresRepository)(nil)

func generalSceneDurableJobSpec(
	sceneType SceneType,
) (durableSceneJobSpec, bool) {
	if !generalSceneTypeSupported(sceneType) {
		return durableSceneJobSpec{}, false
	}
	return durableSceneJobSpec{
		sceneType:       sceneType,
		strategyRef:     GeneralSceneStrategyRef,
		pipelineVersion: GeneralScenePipelineVersion,
		promptVersion:   GeneralScenePromptVersion,
		resultTable:     "evaluation_general_scene_results",
	}, true
}

func durableConfigurationFromGeneralScene(
	source GeneralSceneRuntimeConfiguration,
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
	source GeneralSceneClaim,
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
) GeneralSceneClaim {
	return GeneralSceneClaim{
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
	sceneType SceneType,
	configuration GeneralSceneRuntimeConfiguration,
) (GeneralSceneClaim, bool, error) {
	spec, ok := generalSceneDurableJobSpec(sceneType)
	if !ok {
		return GeneralSceneClaim{}, false, ErrInvalidRequest
	}
	claim, acquired, err := repository.claimDurableSceneJob(
		ctx,
		spec,
		durableConfigurationFromGeneralScene(configuration),
	)
	return generalSceneClaimFromDurable(claim), acquired, err
}

func (repository *PostgresRepository) CompleteGeneralScene(
	ctx context.Context,
	claim GeneralSceneClaim,
	result GeneralSceneResult,
) error {
	spec, ok := generalSceneDurableJobSpec(claim.SceneType)
	if !ok || ctx == nil || !claim.Valid() ||
		ValidateGeneralSceneResult(claim.Snapshot, result) != nil {
		return ErrInvalidRequest
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return ErrInvalidRequest
	}
	report, err := ProjectGeneralSceneFormalReport(claim.Snapshot, result)
	if err != nil {
		return err
	}
	var providerRequestID any
	if result.Provider != nil {
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
	claim GeneralSceneClaim,
	failure GeneralSceneFailure,
	configuration GeneralSceneRuntimeConfiguration,
) (GeneralSceneRuntimeStatus, error) {
	spec, ok := generalSceneDurableJobSpec(claim.SceneType)
	if !ok {
		return "", ErrInvalidRequest
	}
	status, err := repository.failDurableSceneJob(
		ctx,
		spec,
		durableClaimFromGeneralScene(claim),
		durableSceneJobFailure(failure),
		durableConfigurationFromGeneralScene(configuration),
	)
	return GeneralSceneRuntimeStatus(status), err
}
