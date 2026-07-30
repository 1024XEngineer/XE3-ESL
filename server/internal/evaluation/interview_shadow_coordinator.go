package evaluation

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	InterviewShadowStrategyRef     = "interview-scene-shadow/v1"
	InterviewShadowPipelineVersion = "evaluation-pipeline-shadow/v1"
)

var ErrStrategyNotAvailable = errors.New(
	"evaluation: strategy not available",
)

type interviewEvidenceFreezer interface {
	Freeze(
		context.Context,
		requestcontext.Actor,
		string,
		Scope,
		SceneType,
	) (EvidenceSnapshot, bool, error)
}

type interviewEvaluationCreator interface {
	Create(
		context.Context,
		requestcontext.Actor,
		CreateRequest,
	) (Evaluation, bool, error)
}

// InterviewShadowCoordinator is the server-owned completion boundary. It
// freezes the completed Interview session before creating its durable
// Evaluation; callers cannot choose a strategy, scope, scene, or channel.
type InterviewShadowCoordinator struct {
	evidence    interviewEvidenceFreezer
	evaluations interviewEvaluationCreator
}

func NewInterviewShadowCoordinator(
	evidence interviewEvidenceFreezer,
	evaluations interviewEvaluationCreator,
) (*InterviewShadowCoordinator, error) {
	if evidence == nil || evaluations == nil {
		return nil, ErrInvalidRequest
	}
	return &InterviewShadowCoordinator{
		evidence:    evidence,
		evaluations: evaluations,
	}, nil
}

func (coordinator *InterviewShadowCoordinator) EnsureForCompletedInterview(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (Evaluation, bool, error) {
	practiceSessionID = strings.TrimSpace(practiceSessionID)
	if coordinator == nil || coordinator.evidence == nil ||
		coordinator.evaluations == nil || ctx == nil ||
		!validActor(actor) || !validIdentifier(practiceSessionID) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	snapshot, _, err := coordinator.evidence.Freeze(
		ctx,
		actor,
		practiceSessionID,
		ScopeSession,
		SceneInterview,
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
			SceneType:         SceneInterview,
			Channels:          []Channel{ChannelScene},
			SceneStrategyRef:  InterviewShadowStrategyRef,
			PipelineVersion:   InterviewShadowPipelineVersion,
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
		result.SceneType != SceneInterview ||
		!slices.Equal(result.Revision.Channels, []Channel{ChannelScene}) ||
		result.Revision.SceneStrategyRef != InterviewShadowStrategyRef ||
		result.Revision.Core4DStrategyRef != "" ||
		result.Revision.PipelineVersion != InterviewShadowPipelineVersion {
		return Evaluation{}, false, ErrInvalidRequest
	}
	return result, replayed, nil
}

func ValidateInterviewShadowCreateRequest(request CreateRequest) error {
	if request.Scope != ScopeSession ||
		request.SceneType != SceneInterview ||
		!slices.Equal(request.Channels, []Channel{ChannelScene}) ||
		request.SceneStrategyRef != InterviewShadowStrategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion != InterviewShadowPipelineVersion {
		return ErrStrategyNotAvailable
	}
	return nil
}

func ValidateInterviewShadowReevaluateRequest(
	request ReevaluateRequest,
) error {
	if !slices.Equal(request.Channels, []Channel{ChannelScene}) ||
		request.SceneStrategyRef != InterviewShadowStrategyRef ||
		request.Core4DStrategyRef != "" ||
		request.PipelineVersion != InterviewShadowPipelineVersion {
		return ErrStrategyNotAvailable
	}
	return nil
}
