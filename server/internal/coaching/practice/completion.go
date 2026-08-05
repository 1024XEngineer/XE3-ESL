package practice

import (
	"context"
	"errors"
	"strings"
	"time"
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
	OwnerUserID         string
	Completion          PracticeCompleted
	EvaluationPolicyRef string
	AttemptCount        int
	FencingToken        int64
	LeaseExpiresAt      time.Time
}

func (claim CompletionHandoffClaim) Valid() bool {
	return claim.OwnerUserID != "" &&
		claim.Completion.SessionID != "" &&
		claim.Completion.FinalTurnID != "" &&
		claim.Completion.SessionVersion > 1 &&
		strings.TrimSpace(claim.Completion.CompletionToken) != "" &&
		!claim.Completion.CreatedAt.IsZero() &&
		validEvaluationPolicyRef(claim.EvaluationPolicyRef) &&
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

func validEvaluationPolicyRef(value string) bool {
	return len(value) <= 128 && value == strings.TrimSpace(value) &&
		strings.HasSuffix(value, ".evaluation.v1")
}
