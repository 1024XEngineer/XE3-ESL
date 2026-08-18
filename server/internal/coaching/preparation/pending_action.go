package preparation

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrPendingActionInvalid    = errors.New("preparation: invalid pending practice action")
	ErrPendingActionNotFound   = errors.New("preparation: pending practice action not found")
	ErrPendingActionConflict   = errors.New("preparation: pending practice action conflict")
	ErrPendingActionRepository = errors.New("preparation: pending practice action repository failure")
)

type PendingActionState string

const (
	PendingActionOpen       PendingActionState = "OPEN"
	PendingActionConfirming PendingActionState = "CONFIRMING"
	PendingActionConfirmed  PendingActionState = "CONFIRMED"
	PendingActionRejected   PendingActionState = "REJECTED"
	PendingActionSuperseded PendingActionState = "SUPERSEDED"
)

type PendingPracticeAction struct {
	ID                       string
	OwnerID                  string
	ThreadID                 string
	SourceRunID              string
	SourceInputMessageID     string
	SourceInputSequence      int64
	Proposal                 []byte
	ProposalFingerprint      [32]byte
	State                    PendingActionState
	ResolutionInputMessageID string
	ResolvedPlanID           string
	CreatedAt                time.Time
	ResolvedAt               *time.Time
}

func (value PendingPracticeAction) Valid() bool {
	if !ValidAggregateID(value.ID) || !ValidAggregateID(value.OwnerID) ||
		!ValidAggregateID(value.ThreadID) || !ValidAggregateID(value.SourceRunID) ||
		!ValidAggregateID(value.SourceInputMessageID) || value.SourceInputSequence < 1 ||
		len(value.Proposal) == 0 || value.CreatedAt.IsZero() {
		return false
	}
	switch value.State {
	case PendingActionOpen:
		return value.ResolutionInputMessageID == "" && value.ResolvedPlanID == "" &&
			value.ResolvedAt == nil
	case PendingActionConfirming:
		return ValidAggregateID(value.ResolutionInputMessageID) &&
			value.ResolvedPlanID == "" && value.ResolvedAt == nil
	case PendingActionConfirmed:
		return ValidAggregateID(value.ResolutionInputMessageID) &&
			ValidAggregateID(value.ResolvedPlanID) && value.ResolvedAt != nil
	case PendingActionRejected:
		return ValidAggregateID(value.ResolutionInputMessageID) &&
			value.ResolvedPlanID == "" && value.ResolvedAt != nil
	case PendingActionSuperseded:
		return value.ResolutionInputMessageID == "" && value.ResolvedPlanID == "" &&
			value.ResolvedAt != nil
	default:
		return false
	}
}

type CreatePendingActionCommand struct {
	ThreadID             string
	SourceRunID          string
	SourceInputMessageID string
	SourceInputSequence  int64
	Proposal             []byte
	ProposalFingerprint  [32]byte
}

type ResolvePendingActionCommand struct {
	ThreadID                 string
	ResolutionInputMessageID string
	ResolutionInputSequence  int64
	Confirm                  bool
}

type PendingActionRepository interface {
	HasOpenForReply(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		int64,
	) (bool, error)
	CreateOrReplay(
		context.Context,
		requestcontext.Actor,
		CreatePendingActionCommand,
	) (PendingPracticeAction, bool, error)
	ClaimForReply(
		context.Context,
		requestcontext.Actor,
		ResolvePendingActionCommand,
	) (PendingPracticeAction, bool, error)
	CompleteConfirmation(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		string,
	) (PendingPracticeAction, error)
}
