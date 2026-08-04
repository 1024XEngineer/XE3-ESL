package evaluation

import (
	"context"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type sceneShadowEvidenceFreezer interface {
	Freeze(
		context.Context,
		requestcontext.Actor,
		string,
		Scope,
		SceneType,
	) (EvidenceSnapshot, bool, error)
}

type sceneShadowEvaluationCreator interface {
	Create(
		context.Context,
		requestcontext.Actor,
		CreateRequest,
	) (Evaluation, bool, error)
}

type sceneShadowStrategy struct {
	sceneType       SceneType
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
		return nil, ErrInvalidRequest
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
) (Evaluation, bool, error) {
	practiceSessionID = strings.TrimSpace(practiceSessionID)
	if coordinator == nil || coordinator.evidence == nil ||
		coordinator.evaluations == nil ||
		!coordinator.strategy.valid() || ctx == nil ||
		!validActor(actor) || !validIdentifier(practiceSessionID) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	snapshot, _, err := coordinator.evidence.Freeze(
		ctx,
		actor,
		practiceSessionID,
		ScopeSession,
		coordinator.strategy.sceneType,
	)
	if err != nil {
		return Evaluation{}, false, err
	}
	result, replayed, err := coordinator.evaluations.Create(
		ctx,
		actor,
		CreateRequest{
			PracticeSessionID: practiceSessionID,
			InputSnapshotID:   snapshot.ID,
			InputRevision:     snapshot.InputRevision,
			Scope:             ScopeSession,
			SceneType:         coordinator.strategy.sceneType,
			Channels:          []Channel{ChannelScene},
			SceneStrategyRef:  coordinator.strategy.strategyRef,
			PipelineVersion:   coordinator.strategy.pipelineVersion,
		},
	)
	if err != nil {
		return Evaluation{}, false, err
	}
	if !result.Valid() ||
		result.OwnerUserID != actor.UserID ||
		result.PracticeSessionID != practiceSessionID ||
		result.InputSnapshotID != snapshot.ID ||
		result.InputRevision != snapshot.InputRevision ||
		result.Scope != ScopeSession ||
		result.SceneType != coordinator.strategy.sceneType ||
		!slices.Equal(result.Revision.Channels, []Channel{ChannelScene}) ||
		result.Revision.SceneStrategyRef !=
			coordinator.strategy.strategyRef ||
		result.Revision.Core4DStrategyRef != "" ||
		result.Revision.PipelineVersion !=
			coordinator.strategy.pipelineVersion {
		return Evaluation{}, false, ErrInvalidRequest
	}
	return result, replayed, nil
}

func validateSceneShadowCreateRequest(
	request CreateRequest,
	strategy sceneShadowStrategy,
) error {
	if !strategy.valid() ||
		request.Scope != ScopeSession ||
		request.SceneType != strategy.sceneType ||
		!slices.Equal(request.Channels, []Channel{ChannelScene}) ||
		request.SceneStrategyRef != strategy.strategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion != strategy.pipelineVersion {
		return ErrStrategyNotAvailable
	}
	return nil
}

func validateSceneShadowReevaluateRequest(
	request ReevaluateRequest,
	strategy sceneShadowStrategy,
) error {
	if !strategy.valid() ||
		!slices.Equal(request.Channels, []Channel{ChannelScene}) ||
		request.SceneStrategyRef != strategy.strategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion != strategy.pipelineVersion {
		return ErrStrategyNotAvailable
	}
	return nil
}
