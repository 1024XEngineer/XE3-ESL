package practice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

// PracticeCompleted is the durable handoff produced with the final Turn.
// Evaluation consumes this reference and reads confirmed evidence separately.
type PracticeCompleted struct {
	SessionID       string
	FinalTurnID     string
	SessionVersion  int
	CompletionToken string
	CreatedAt       time.Time
}

var (
	ErrCompletionHandoffInvalid = errors.New(
		"practice: invalid Evaluation handoff",
	)
	ErrCompletionHandoffClaimLost = errors.New(
		"practice: Evaluation handoff claim lost",
	)
)

type CompletionHandoffClaim struct {
	OwnerUserID    string
	Completion     PracticeCompleted
	SceneFamily    scene.SceneFamily
	SceneModel     scene.SceneModel
	AttemptCount   int
	FencingToken   int64
	LeaseExpiresAt time.Time
}

func (claim CompletionHandoffClaim) Valid() bool {
	return claim.OwnerUserID != "" &&
		claim.Completion.SessionID != "" &&
		claim.Completion.FinalTurnID != "" &&
		claim.Completion.SessionVersion > 1 &&
		strings.TrimSpace(claim.Completion.CompletionToken) != "" &&
		!claim.Completion.CreatedAt.IsZero() &&
		validCompletionScene(claim.SceneFamily, claim.SceneModel) &&
		claim.AttemptCount > 0 &&
		claim.FencingToken >= int64(claim.AttemptCount) &&
		!claim.LeaseExpiresAt.IsZero()
}

type CompletionHandoffFailure struct {
	Code      string
	Retryable bool
}

func (failure CompletionHandoffFailure) Valid() bool {
	if failure.Code == "" || failure.Code != strings.TrimSpace(failure.Code) ||
		len(failure.Code) > 128 ||
		failure.Code[0] < 'a' || failure.Code[0] > 'z' {
		return false
	}
	for _, character := range failure.Code[1:] {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '_', '.', ':', '-':
		default:
			return false
		}
	}
	return true
}

type CompletionHandoffRepository interface {
	ClaimCompletionHandoff(
		context.Context,
		time.Duration,
		int,
	) (CompletionHandoffClaim, bool, error)
	CompleteCompletionHandoff(
		context.Context,
		CompletionHandoffClaim,
	) error
	FailCompletionHandoff(
		context.Context,
		CompletionHandoffClaim,
		CompletionHandoffFailure,
		time.Duration,
		int,
	) error
}

func validCompletionScene(
	family scene.SceneFamily,
	model scene.SceneModel,
) bool {
	switch family {
	case scene.SceneFamilyInterview:
		return model == scene.SceneModelProjectExperienceDeepDive ||
			model == scene.SceneModelInterviewBasicDialogue
	case scene.SceneFamilyExam:
		return model == scene.SceneModelIELTSSpeakingPart1 ||
			model == scene.SceneModelIELTSSpeakingPart2 ||
			model == scene.SceneModelIELTSSpeakingPart3 ||
			model == scene.SceneModelIELTSSpeakingFullMock ||
			model == scene.SceneModelExamBasicDialogue
	case scene.SceneFamilyWorkplace:
		return model == scene.SceneModelProgressAndRiskUpdate ||
			model == scene.SceneModelWorkplaceBasicDialogue
	case scene.SceneFamilyDaily:
		return model == scene.SceneModelHotelCheckinAndIssueHandling ||
			model == scene.SceneModelDailyBasicDialogue
	default:
		return false
	}
}
