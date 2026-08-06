package scoring

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

type CompletionIntakeConfiguration struct {
	MaxAttempts   int
	LeaseDuration time.Duration
	RetryDelay    time.Duration
}

func (configuration CompletionIntakeConfiguration) valid() bool {
	return configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10 &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.RetryDelay >= 0 &&
		configuration.RetryDelay <= time.Hour
}

type completedEvidenceFreezer interface {
	FreezeCompleted(
		context.Context,
		string,
		string,
		evaluation.Scope,
		evaluation.SceneType,
	) (evidence.EvidenceSnapshot, bool, error)
}

type completedEvaluationCreator interface {
	CreateCompleted(
		context.Context,
		string,
		evaluation.CreateRequest,
	) (evaluation.Evaluation, bool, error)
}

type CompletionIntake struct {
	completions   practice.CompletionHandoffRepository
	evidence      completedEvidenceFreezer
	evaluations   completedEvaluationCreator
	policies      *EvaluationPolicyRegistry
	configuration CompletionIntakeConfiguration
}

type CompletionIntakeSweepResult struct {
	Claimed   int
	Delivered int
	Retried   int
	Failed    int
}

func NewCompletionIntake(
	completions practice.CompletionHandoffRepository,
	evidence completedEvidenceFreezer,
	evaluations completedEvaluationCreator,
	policies *EvaluationPolicyRegistry,
	configuration CompletionIntakeConfiguration,
) (*CompletionIntake, error) {
	if completions == nil || evidence == nil || evaluations == nil ||
		policies == nil || !configuration.valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	return &CompletionIntake{
		completions:   completions,
		evidence:      evidence,
		evaluations:   evaluations,
		policies:      policies,
		configuration: configuration,
	}, nil
}

func (intake *CompletionIntake) ProcessPending(
	ctx context.Context,
	limit int,
) (CompletionIntakeSweepResult, error) {
	if intake == nil || intake.completions == nil || intake.evidence == nil ||
		intake.evaluations == nil || intake.policies == nil ||
		!intake.configuration.valid() ||
		ctx == nil || limit < 1 || limit > 20 {
		return CompletionIntakeSweepResult{}, evaluation.ErrInvalidRequest
	}
	var sweep CompletionIntakeSweepResult
	for sweep.Claimed < limit {
		claim, acquired, err := intake.completions.ClaimCompletionHandoff(
			ctx,
			intake.configuration.LeaseDuration,
			intake.configuration.MaxAttempts,
		)
		if err != nil {
			return sweep, err
		}
		if !acquired {
			return sweep, nil
		}
		sweep.Claimed++
		err = intake.process(ctx, claim)
		if err == nil {
			if err := intake.completions.CompleteCompletionHandoff(
				ctx,
				claim,
			); err != nil {
				return sweep, err
			}
			sweep.Delivered++
			continue
		}
		failure := completionIntakeFailure(err)
		if failErr := intake.completions.FailCompletionHandoff(
			ctx,
			claim,
			failure,
			intake.configuration.RetryDelay,
			intake.configuration.MaxAttempts,
		); failErr != nil {
			return sweep, errors.Join(err, failErr)
		}
		if failure.Retryable &&
			claim.AttemptCount < intake.configuration.MaxAttempts {
			sweep.Retried++
		} else {
			sweep.Failed++
		}
	}
	return sweep, nil
}

func (intake *CompletionIntake) process(
	ctx context.Context,
	claim practice.CompletionHandoffClaim,
) error {
	if !claim.Valid() {
		return practice.ErrCompletionHandoffInvalid
	}
	policy, err := intake.policies.resolve(claim.EvaluationPolicyRef)
	if err != nil {
		return err
	}
	snapshot, _, err := intake.evidence.FreezeCompleted(
		ctx,
		claim.OwnerUserID,
		claim.Completion.SessionID,
		evaluation.ScopeSession,
		policy.SceneType,
	)
	if err != nil {
		return err
	}
	if snapshot.InputRevision != claim.Completion.SessionVersion {
		return evaluation.ErrInvalidRequest
	}
	_, _, err = intake.evaluations.CreateCompleted(
		ctx,
		claim.OwnerUserID,
		evaluation.CreateRequest{
			PracticeSessionID: claim.Completion.SessionID,
			InputSnapshotID:   snapshot.ID,
			InputRevision:     snapshot.InputRevision,
			Scope:             evaluation.ScopeSession,
			SceneType:         policy.SceneType,
			Channels:          []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef:  policy.StrategyRef,
			PipelineVersion:   policy.PipelineVersion,
		},
	)
	return err
}

func completionIntakeFailure(err error) practice.CompletionHandoffFailure {
	switch {
	case errors.Is(err, ErrStrategyNotAvailable):
		return practice.CompletionHandoffFailure{
			Code:      "strategy_not_available",
			Retryable: false,
		}
	case errors.Is(err, evaluation.ErrInvalidRequest),
		errors.Is(err, practice.ErrCompletionHandoffInvalid):
		return practice.CompletionHandoffFailure{
			Code:      "invalid_completion",
			Retryable: false,
		}
	case errors.Is(err, evaluation.ErrNotFound):
		return practice.CompletionHandoffFailure{
			Code:      "source_not_found",
			Retryable: false,
		}
	default:
		return practice.CompletionHandoffFailure{
			Code:      "evaluation_unavailable",
			Retryable: true,
		}
	}
}
