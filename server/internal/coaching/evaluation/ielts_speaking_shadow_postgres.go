package evaluation

import (
	"context"
	"encoding/json"
)

var (
	ieltsDurableSceneJobSpec = durableSceneJobSpec{
		sceneType:       SceneIELTSSpeaking,
		strategyRef:     IELTSSpeakingShadowStrategyRef,
		pipelineVersion: IELTSSpeakingShadowPipelineVersion,
		promptVersion:   IELTSSpeakingShadowPromptVersion,
		resultTable:     "evaluation_ielts_speaking_scene_results",
	}
	_ IELTSSpeakingShadowRuntimeRepository = (*PostgresRepository)(nil)
)

func durableConfigurationFromIELTS(
	source IELTSSpeakingShadowRuntimeConfiguration,
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

func durableClaimFromIELTS(
	source IELTSSpeakingShadowClaim,
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

func ieltsClaimFromDurable(
	source durableSceneJobClaim,
) IELTSSpeakingShadowClaim {
	return IELTSSpeakingShadowClaim{
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

func (r *PostgresRepository) ClaimIELTSSpeakingShadow(
	ctx context.Context,
	configuration IELTSSpeakingShadowRuntimeConfiguration,
) (IELTSSpeakingShadowClaim, bool, error) {
	claim, acquired, err := r.claimDurableSceneJob(
		ctx,
		ieltsDurableSceneJobSpec,
		durableConfigurationFromIELTS(configuration),
	)
	return ieltsClaimFromDurable(claim), acquired, err
}

func (r *PostgresRepository) CompleteIELTSSpeakingShadow(
	ctx context.Context,
	claim IELTSSpeakingShadowClaim,
	result IELTSSpeakingShadowResult,
) error {
	if ctx == nil || !claim.Valid() ||
		ValidateIELTSSpeakingShadowResult(
			claim.Snapshot,
			result,
		) != nil ||
		!ieltsResultMatchesRuntime(claim, result) {
		return ErrInvalidRequest
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return ErrInvalidRequest
	}
	var providerRequestID any
	if result.Provider != nil {
		providerRequestID = result.Provider.RequestID
	}
	return r.completeDurableSceneJob(
		ctx,
		ieltsDurableSceneJobSpec,
		durableClaimFromIELTS(claim),
		payload,
		providerRequestID,
	)
}

func (r *PostgresRepository) FailIELTSSpeakingShadow(
	ctx context.Context,
	claim IELTSSpeakingShadowClaim,
	failure IELTSSpeakingShadowFailure,
	configuration IELTSSpeakingShadowRuntimeConfiguration,
) (IELTSSpeakingShadowRuntimeStatus, error) {
	status, err := r.failDurableSceneJob(
		ctx,
		ieltsDurableSceneJobSpec,
		durableClaimFromIELTS(claim),
		durableSceneJobFailure{
			Code:      failure.Code,
			Retryable: failure.Retryable,
		},
		durableConfigurationFromIELTS(configuration),
	)
	return IELTSSpeakingShadowRuntimeStatus(status), err
}

func (r *PostgresRepository) GetIELTSSpeakingShadowState(
	ctx context.Context,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) (IELTSSpeakingShadowReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validUUID(evaluationID) ||
		!validUUID(evaluationRevisionID) {
		return IELTSSpeakingShadowReadState{}, ErrInvalidRequest
	}
	state, _, err := getIELTSSpeakingShadowState(
		ctx,
		r.pool,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	)
	return state, err
}

func getIELTSSpeakingShadowState(
	ctx context.Context,
	db queryable,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) (
	IELTSSpeakingShadowReadState,
	*EvidenceSnapshot,
	error,
) {
	raw, snapshot, err := getDurableSceneJobState(
		ctx,
		db,
		ieltsDurableSceneJobSpec,
		ownerUserID,
		evaluationID,
		evaluationRevisionID,
	)
	if err != nil {
		return IELTSSpeakingShadowReadState{}, nil, err
	}
	state := IELTSSpeakingShadowReadState{
		ModuleStatus: IELTSSpeakingShadowRuntimeStatus(
			raw.ModuleStatus,
		),
		FullConfigHash: raw.FullConfigHash,
	}
	if raw.Failure != nil {
		state.Failure = &IELTSSpeakingShadowFailure{
			Code:      raw.Failure.Code,
			Retryable: raw.Failure.Retryable,
		}
	}
	if raw.ModuleStatus == durableSceneJobReady {
		if snapshot == nil {
			return IELTSSpeakingShadowReadState{}, nil,
				ErrIELTSSpeakingShadowConfigurationConflict
		}
		result, decodeErr := decodeIELTSSpeakingShadowResult(
			raw.ResultPayload,
		)
		if decodeErr != nil ||
			ValidateIELTSSpeakingShadowResult(
				*snapshot,
				result,
			) != nil {
			return IELTSSpeakingShadowReadState{}, nil,
				ErrIELTSSpeakingShadowConfigurationConflict
		}
		state.Result = &result
	}
	return state, snapshot, nil
}

func ieltsResultMatchesRuntime(
	claim IELTSSpeakingShadowClaim,
	result IELTSSpeakingShadowResult,
) bool {
	if result.Provider == nil {
		return result.Scoreability ==
			IELTSSpeakingScoreabilityInsufficient
	}
	return result.Provider.Provider == claim.Provider &&
		result.Provider.Model == claim.Model &&
		result.Provider.PromptVersion == claim.PromptVersion &&
		result.Provider.ResponseSchema ==
			IELTSSpeakingShadowProviderSchemaVersion &&
		result.Provider.RubricVersion ==
			IELTSSpeakingShadowRubricVersion
}
