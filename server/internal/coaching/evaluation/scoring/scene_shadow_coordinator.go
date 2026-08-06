package scoring

import (
	"context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type sceneShadowEvidenceFreezer interface {
	Freeze(
		context.Context,
		requestcontext.Actor,
		string,
		evaluation.Scope,
		evaluation.SceneType,
	) (evidence.EvidenceSnapshot, bool, error)
}

type sceneShadowEvaluationCreator interface {
	Create(
		context.Context,
		requestcontext.Actor,
		evaluation.CreateRequest,
	) (evaluation.Evaluation, bool, error)
}

type sceneShadowStrategy struct {
	sceneType       evaluation.SceneType
	strategyRef     string
	pipelineVersion string
}

func (strategy sceneShadowStrategy) valid() bool {
	return validSceneType(strategy.sceneType) &&
		validVersion(strategy.strategyRef) &&
		validVersion(strategy.pipelineVersion)
}

type sceneShadowCoordinator struct {
	evidence    sceneShadowEvidenceFreezer
	evaluations sceneShadowEvaluationCreator
	strategy    sceneShadowStrategy
}

func newSceneShadowCoordinator(
	evidence sceneShadowEvidenceFreezer,
	evaluations sceneShadowEvaluationCreator,
	strategy sceneShadowStrategy,
) (*sceneShadowCoordinator, error) {
	if evidence == nil || evaluations == nil || !strategy.valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	return &sceneShadowCoordinator{
		evidence:    evidence,
		evaluations: evaluations,
		strategy:    strategy,
	}, nil
}

func (coordinator *sceneShadowCoordinator) ensureForCompletedSession(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (evaluation.Evaluation, bool, error) {
	practiceSessionID = strings.TrimSpace(practiceSessionID)
	if coordinator == nil || coordinator.evidence == nil ||
		coordinator.evaluations == nil ||
		!coordinator.strategy.valid() || ctx == nil ||
		!validActor(actor) || !validIdentifier(practiceSessionID) {
		return evaluation.Evaluation{}, false, evaluation.ErrInvalidRequest
	}
	snapshot, _, err := coordinator.evidence.Freeze(
		ctx,
		actor,
		practiceSessionID,
		evaluation.ScopeSession,
		coordinator.strategy.sceneType,
	)
	if err != nil {
		return evaluation.Evaluation{}, false, err
	}
	result, replayed, err := coordinator.evaluations.Create(
		ctx,
		actor,
		evaluation.CreateRequest{
			PracticeSessionID: practiceSessionID,
			InputSnapshotID:   snapshot.ID,
			InputRevision:     snapshot.InputRevision,
			Scope:             evaluation.ScopeSession,
			SceneType:         coordinator.strategy.sceneType,
			Channels:          []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef:  coordinator.strategy.strategyRef,
			PipelineVersion:   coordinator.strategy.pipelineVersion,
		},
	)
	if err != nil {
		return evaluation.Evaluation{}, false, err
	}
	if !result.Valid() ||
		result.OwnerUserID != actor.UserID ||
		result.PracticeSessionID != practiceSessionID ||
		result.InputSnapshotID != snapshot.ID ||
		result.InputRevision != snapshot.InputRevision ||
		result.Scope != evaluation.ScopeSession ||
		result.SceneType != coordinator.strategy.sceneType ||
		!slices.Equal(result.Revision.Channels, []evaluation.Channel{evaluation.ChannelScene}) ||
		result.Revision.SceneStrategyRef !=
			coordinator.strategy.strategyRef ||
		result.Revision.Core4DStrategyRef != "" ||
		result.Revision.PipelineVersion !=
			coordinator.strategy.pipelineVersion {
		return evaluation.Evaluation{}, false, evaluation.ErrInvalidRequest
	}
	return result, replayed, nil
}

func validateSceneShadowCreateRequest(
	request evaluation.CreateRequest,
	strategy sceneShadowStrategy,
) error {
	if !strategy.valid() ||
		request.Scope != evaluation.ScopeSession ||
		request.SceneType != strategy.sceneType ||
		!slices.Equal(request.Channels, []evaluation.Channel{evaluation.ChannelScene}) ||
		request.SceneStrategyRef != strategy.strategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion != strategy.pipelineVersion {
		return ErrStrategyNotAvailable
	}
	return nil
}

func validateSceneShadowReevaluateRequest(
	request evaluation.ReevaluateRequest,
	strategy sceneShadowStrategy,
) error {
	if !strategy.valid() ||
		!slices.Equal(request.Channels, []evaluation.Channel{evaluation.ChannelScene}) ||
		request.SceneStrategyRef != strategy.strategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion != strategy.pipelineVersion {
		return ErrStrategyNotAvailable
	}
	return nil
}
