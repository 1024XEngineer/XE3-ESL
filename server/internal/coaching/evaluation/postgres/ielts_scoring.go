package postgres

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

var (
	ieltsDurableSceneJobSpec = durableSceneJobSpec{
		sceneType:       evaluation.SceneIELTSSpeaking,
		strategyRef:     scoring.IELTSSpeakingShadowStrategyRef,
		pipelineVersion: scoring.IELTSSpeakingShadowPipelineVersion,
		promptVersion:   scoring.IELTSSpeakingShadowPromptVersion,
		resultTable:     "evaluation_ielts_speaking_scene_results",
	}
	_ scoring.IELTSSpeakingShadowRuntimeRepository = (*PostgresRepository)(nil)
)

func durableConfigurationFromIELTS(
	source scoring.IELTSSpeakingShadowRuntimeConfiguration,
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
	source scoring.IELTSSpeakingShadowClaim,
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
		AcousticSnapshot:     &source.AcousticSnapshot,
		InputBundleHash:      source.InputBundleHash,
	}
}

func ieltsClaimFromDurable(
	source durableSceneJobClaim,
) scoring.IELTSSpeakingShadowClaim {
	result := scoring.IELTSSpeakingShadowClaim{
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
		InputBundleHash:      source.InputBundleHash,
	}
	if source.AcousticSnapshot != nil {
		result.AcousticSnapshot = *source.AcousticSnapshot
	}
	return result
}

func (r *PostgresRepository) ClaimIELTSSpeakingShadow(
	ctx context.Context,
	configuration scoring.IELTSSpeakingShadowRuntimeConfiguration,
) (scoring.IELTSSpeakingShadowClaim, bool, error) {
	claim, acquired, err := r.claimDurableSceneJob(
		ctx,
		ieltsDurableSceneJobSpec,
		durableConfigurationFromIELTS(configuration),
	)
	return ieltsClaimFromDurable(claim), acquired, err
}

func (r *PostgresRepository) CompleteIELTSSpeakingShadow(
	ctx context.Context,
	claim scoring.IELTSSpeakingShadowClaim,
	result scoring.IELTSSpeakingShadowResult,
) error {
	if ctx == nil || !claim.Valid() ||
		scoring.ValidateIELTSSpeakingShadowResult(
			claim.Snapshot,
			result,
		) != nil ||
		!ieltsResultMatchesRuntime(claim, result) {
		return evaluation.ErrInvalidRequest
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return evaluation.ErrInvalidRequest
	}
	report, err := report.ProjectIELTSFormalReport(claim.Snapshot, result)
	if err != nil {
		return err
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
		report,
	)
}

func (r *PostgresRepository) FailIELTSSpeakingShadow(
	ctx context.Context,
	claim scoring.IELTSSpeakingShadowClaim,
	failure scoring.IELTSSpeakingShadowFailure,
	configuration scoring.IELTSSpeakingShadowRuntimeConfiguration,
) (scoring.IELTSSpeakingShadowRuntimeStatus, error) {
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
	return scoring.IELTSSpeakingShadowRuntimeStatus(status), err
}

func (r *PostgresRepository) GetIELTSSpeakingShadowState(
	ctx context.Context,
	ownerUserID string,
	evaluationID string,
	evaluationRevisionID string,
) (scoring.IELTSSpeakingShadowReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validUUID(evaluationID) ||
		!validUUID(evaluationRevisionID) {
		return scoring.IELTSSpeakingShadowReadState{}, evaluation.ErrInvalidRequest
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
	scoring.IELTSSpeakingShadowReadState,
	*evidence.EvidenceSnapshot,
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
		return scoring.IELTSSpeakingShadowReadState{}, nil, err
	}
	state := scoring.IELTSSpeakingShadowReadState{
		ModuleStatus: scoring.IELTSSpeakingShadowRuntimeStatus(
			raw.ModuleStatus,
		),
		FullConfigHash: raw.FullConfigHash,
	}
	if raw.Failure != nil {
		state.Failure = &scoring.IELTSSpeakingShadowFailure{
			Code:      raw.Failure.Code,
			Retryable: raw.Failure.Retryable,
		}
	}
	if raw.ModuleStatus == durableSceneJobReady {
		if snapshot == nil {
			return scoring.IELTSSpeakingShadowReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		result, decodeErr := scoring.DecodeIELTSSpeakingShadowResult(
			raw.ResultPayload,
		)
		if decodeErr != nil ||
			scoring.ValidateIELTSSpeakingShadowResult(
				*snapshot,
				result,
			) != nil {
			return scoring.IELTSSpeakingShadowReadState{}, nil,
				scoring.ErrRuntimeConfigurationConflict
		}
		state.Result = &result
	}
	return state, snapshot, nil
}

func ieltsResultMatchesRuntime(
	claim scoring.IELTSSpeakingShadowClaim,
	result scoring.IELTSSpeakingShadowResult,
) bool {
	if result.Provider == nil {
		return result.Scoreability ==
			scoring.IELTSSpeakingScoreabilityInsufficient
	}
	return result.Provider.Provider == claim.Provider &&
		result.Provider.Model == claim.Model &&
		result.Provider.PromptVersion == claim.PromptVersion &&
		result.Provider.ResponseSchema ==
			scoring.IELTSSpeakingShadowProviderSchemaVersion &&
		result.Provider.RubricVersion ==
			scoring.IELTSSpeakingShadowRubricVersion
}
