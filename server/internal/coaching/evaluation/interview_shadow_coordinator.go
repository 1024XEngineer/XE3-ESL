package evaluation

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	InterviewShadowStrategyRef     = "interview-scene-shadow/v1"
	InterviewShadowPipelineVersion = "evaluation-pipeline-shadow/v1"
)

var ErrStrategyNotAvailable = errors.New(
	"evaluation: strategy not available",
)

type interviewEvidenceFreezer = sceneShadowEvidenceFreezer
type interviewEvaluationCreator = sceneShadowEvaluationCreator

var interviewShadowStrategy = sceneShadowStrategy{
	sceneType:       SceneInterview,
	strategyRef:     InterviewShadowStrategyRef,
	pipelineVersion: InterviewShadowPipelineVersion,
}

// InterviewShadowCoordinator is the server-owned completion boundary. It
// freezes the completed Interview session before creating its durable
// Evaluation; callers cannot choose a strategy, scope, scene, or channel.
type InterviewShadowCoordinator struct {
	shared *sceneShadowCoordinator
}

func NewInterviewShadowCoordinator(
	evidence interviewEvidenceFreezer,
	evaluations interviewEvaluationCreator,
) (*InterviewShadowCoordinator, error) {
	shared, err := newSceneShadowCoordinator(
		evidence,
		evaluations,
		interviewShadowStrategy,
	)
	if err != nil {
		return nil, err
	}
	return &InterviewShadowCoordinator{shared: shared}, nil
}

func (coordinator *InterviewShadowCoordinator) EnsureForCompletedInterview(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (Evaluation, bool, error) {
	if coordinator == nil || coordinator.shared == nil {
		return Evaluation{}, false, ErrInvalidRequest
	}
	return coordinator.shared.ensureForCompletedSession(
		ctx,
		actor,
		practiceSessionID,
	)
}

func ValidateInterviewShadowCreateRequest(request CreateRequest) error {
	return validateSceneShadowCreateRequest(
		request,
		interviewShadowStrategy,
	)
}

func ValidateInterviewShadowReevaluateRequest(
	request ReevaluateRequest,
) error {
	return validateSceneShadowReevaluateRequest(
		request,
		interviewShadowStrategy,
	)
}
