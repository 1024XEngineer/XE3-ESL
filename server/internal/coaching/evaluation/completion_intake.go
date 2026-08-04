package evaluation

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
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
		Scope,
		SceneType,
	) (EvidenceSnapshot, bool, error)
}

type completedEvaluationCreator interface {
	CreateCompleted(
		context.Context,
		string,
		CreateRequest,
	) (Evaluation, bool, error)
}

type CompletionIntake struct {
	completions   practice.CompletionHandoffRepository
	evidence      completedEvidenceFreezer
	evaluations   completedEvaluationCreator
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
	configuration CompletionIntakeConfiguration,
) (*CompletionIntake, error) {
	if completions == nil || evidence == nil || evaluations == nil ||
		!configuration.valid() {
		return nil, ErrInvalidRequest
	}
	return &CompletionIntake{
		completions:   completions,
		evidence:      evidence,
		evaluations:   evaluations,
		configuration: configuration,
	}, nil
}

func (intake *CompletionIntake) ProcessPending(
	ctx context.Context,
	limit int,
) (CompletionIntakeSweepResult, error) {
	if intake == nil || intake.completions == nil || intake.evidence == nil ||
		intake.evaluations == nil || !intake.configuration.valid() ||
		ctx == nil || limit < 1 || limit > 20 {
		return CompletionIntakeSweepResult{}, ErrInvalidRequest
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
	route, err := completionEvaluationRoute(
		claim.SceneFamily,
		claim.SceneModel,
	)
	if err != nil {
		return err
	}
	snapshot, _, err := intake.evidence.FreezeCompleted(
		ctx,
		claim.OwnerUserID,
		claim.Completion.SessionID,
		ScopeSession,
		route.SceneType,
	)
	if err != nil {
		return err
	}
	if snapshot.InputRevision != claim.Completion.SessionVersion {
		return ErrInvalidRequest
	}
	_, _, err = intake.evaluations.CreateCompleted(
		ctx,
		claim.OwnerUserID,
		CreateRequest{
			PracticeSessionID: claim.Completion.SessionID,
			InputSnapshotID:   snapshot.ID,
			InputRevision:     snapshot.InputRevision,
			Scope:             ScopeSession,
			SceneType:         route.SceneType,
			Channels:          []Channel{ChannelScene},
			SceneStrategyRef:  route.StrategyRef,
			PipelineVersion:   route.PipelineVersion,
		},
	)
	return err
}

type completionEvaluationRouteSpec struct {
	SceneType       SceneType
	StrategyRef     string
	PipelineVersion string
}

func completionEvaluationRoute(
	family scene.SceneFamily,
	model scene.SceneModel,
) (completionEvaluationRouteSpec, error) {
	switch family {
	case scene.SceneFamilyInterview:
		if model == scene.SceneModelProjectExperienceDeepDive ||
			model == scene.SceneModelInterviewBasicDialogue {
			return completionEvaluationRouteSpec{
				SceneType:       SceneInterview,
				StrategyRef:     InterviewShadowStrategyRef,
				PipelineVersion: InterviewShadowPipelineVersion,
			}, nil
		}
	case scene.SceneFamilyExam:
		if model == scene.SceneModelIELTSSpeakingFullMock {
			return completionEvaluationRouteSpec{
				SceneType:       SceneIELTSSpeaking,
				StrategyRef:     IELTSSpeakingShadowStrategyRef,
				PipelineVersion: IELTSSpeakingShadowPipelineVersion,
			}, nil
		}
		if model == scene.SceneModelIELTSSpeakingPart1 ||
			model == scene.SceneModelIELTSSpeakingPart2 ||
			model == scene.SceneModelIELTSSpeakingPart3 ||
			model == scene.SceneModelExamBasicDialogue {
			return completionEvaluationRouteSpec{
				SceneType:       SceneIELTSSpeaking,
				StrategyRef:     GeneralSceneStrategyRef,
				PipelineVersion: GeneralScenePipelineVersion,
			}, nil
		}
	case scene.SceneFamilyDaily:
		if model == scene.SceneModelHotelCheckinAndIssueHandling ||
			model == scene.SceneModelDailyBasicDialogue {
			return completionEvaluationRouteSpec{
				SceneType:       SceneOverseasDaily,
				StrategyRef:     GeneralSceneStrategyRef,
				PipelineVersion: GeneralScenePipelineVersion,
			}, nil
		}
	case scene.SceneFamilyWorkplace:
		if model == scene.SceneModelProgressAndRiskUpdate ||
			model == scene.SceneModelWorkplaceBasicDialogue {
			return completionEvaluationRouteSpec{
				SceneType:       SceneOverseasWorkplace,
				StrategyRef:     GeneralSceneStrategyRef,
				PipelineVersion: GeneralScenePipelineVersion,
			}, nil
		}
	}
	return completionEvaluationRouteSpec{}, ErrStrategyNotAvailable
}

func completionIntakeFailure(err error) practice.CompletionHandoffFailure {
	switch {
	case errors.Is(err, ErrStrategyNotAvailable):
		return practice.CompletionHandoffFailure{
			Code:      "strategy_not_available",
			Retryable: false,
		}
	case errors.Is(err, ErrInvalidRequest),
		errors.Is(err, practice.ErrCompletionHandoffInvalid):
		return practice.CompletionHandoffFailure{
			Code:      "invalid_completion",
			Retryable: false,
		}
	case errors.Is(err, ErrNotFound):
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
