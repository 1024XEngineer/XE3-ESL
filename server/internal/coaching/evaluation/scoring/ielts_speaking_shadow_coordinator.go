package scoring

import (
	"context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	IELTSSpeakingShadowStrategyRef     = "ielts-speaking-full-mock-shadow/v1"
	IELTSSpeakingShadowPipelineVersion = "evaluation-pipeline-shadow/v1"
)

var ieltsSpeakingShadowStrategy = sceneShadowStrategy{
	sceneType:       evaluation.SceneIELTSSpeaking,
	strategyRef:     IELTSSpeakingShadowStrategyRef,
	pipelineVersion: IELTSSpeakingShadowPipelineVersion,
}

type IELTSSpeakingShadowCoordinator struct {
	shared *sceneShadowCoordinator
}

func NewIELTSSpeakingShadowCoordinator(
	evidence sceneShadowEvidenceFreezer,
	evaluations sceneShadowEvaluationCreator,
) (*IELTSSpeakingShadowCoordinator, error) {
	shared, err := newSceneShadowCoordinator(
		evidence,
		evaluations,
		ieltsSpeakingShadowStrategy,
	)
	if err != nil {
		return nil, err
	}
	return &IELTSSpeakingShadowCoordinator{shared: shared}, nil
}

func (coordinator *IELTSSpeakingShadowCoordinator) EnsureForCompletedIELTSSpeaking(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (evaluation.Evaluation, bool, error) {
	if coordinator == nil || coordinator.shared == nil {
		return evaluation.Evaluation{}, false, evaluation.ErrInvalidRequest
	}
	return coordinator.shared.ensureForCompletedSession(
		ctx,
		actor,
		practiceSessionID,
	)
}

func ValidateIELTSSpeakingShadowCreateRequest(
	request evaluation.CreateRequest,
) error {
	return validateSceneShadowCreateRequest(
		request,
		ieltsSpeakingShadowStrategy,
	)
}

func ValidateIELTSSpeakingShadowReevaluateRequest(
	request evaluation.ReevaluateRequest,
) error {
	return validateSceneShadowReevaluateRequest(
		request,
		ieltsSpeakingShadowStrategy,
	)
}
