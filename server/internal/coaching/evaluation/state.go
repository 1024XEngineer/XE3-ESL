package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"
)

var ErrClaimLost = errors.New("evaluation: claim lost")

type QueueCommand struct {
	UserID        string
	Kind          Kind
	SourceID      string
	ContextID     string
	InputSnapshot json.RawMessage
	InputHash     [sha256.Size]byte
	ConfigLineage json.RawMessage
	ConfigHash    [sha256.Size]byte
	AvailableAt   time.Time
}

func (command QueueCommand) Valid() bool {
	record := Record{
		ID:            "00000000-0000-4000-8000-000000000000",
		UserID:        command.UserID,
		Kind:          command.Kind,
		SourceID:      command.SourceID,
		ContextID:     command.ContextID,
		Status:        JobQueued,
		InputSnapshot: command.InputSnapshot,
		InputHash:     command.InputHash,
		ConfigLineage: command.ConfigLineage,
		ConfigHash:    command.ConfigHash,
		AvailableAt:   command.AvailableAt,
		CreatedAt:     command.AvailableAt,
		UpdatedAt:     command.AvailableAt,
	}
	return record.Valid()
}

type Claim struct {
	Record
	LeaseDuration time.Duration
}

func (claim Claim) Valid() bool {
	return claim.Record.Valid() && claim.Status == JobRunning &&
		claim.LeaseDuration > 0
}

type ClaimLane struct {
	Kinds         []Kind
	LeaseDuration time.Duration
	MaxAttempts   int
}

type Deferral struct {
	UserID      string
	ID          string
	LeaseToken  string
	AvailableAt time.Time
}

func (deferral Deferral) Valid() bool {
	return validUUID(deferral.UserID) && validUUID(deferral.ID) &&
		validUUID(deferral.LeaseToken) &&
		!deferral.AvailableAt.IsZero()
}

func (lane ClaimLane) Valid() bool {
	if len(lane.Kinds) == 0 || len(lane.Kinds) > 2 ||
		lane.LeaseDuration < time.Second || lane.LeaseDuration > 30*time.Minute ||
		lane.MaxAttempts < 1 || lane.MaxAttempts > 10 {
		return false
	}
	seen := make(map[Kind]struct{}, len(lane.Kinds))
	for _, kind := range lane.Kinds {
		if !kind.Valid() {
			return false
		}
		if _, exists := seen[kind]; exists {
			return false
		}
		seen[kind] = struct{}{}
	}
	return true
}

type Completion struct {
	UserID     string
	ID         string
	LeaseToken string
	Result     json.RawMessage
	Items      []FeedbackItemDraft
}

func (completion Completion) Valid() bool {
	if !validUUID(completion.UserID) || !validUUID(completion.ID) ||
		!validUUID(completion.LeaseToken) || !json.Valid(completion.Result) ||
		len(completion.Items) > 32 {
		return false
	}
	for _, item := range completion.Items {
		if !item.Valid() {
			return false
		}
	}
	return true
}

type Failure struct {
	UserID     string
	ID         string
	LeaseToken string
	Error      JobError
	// AutomaticRetryable controls worker requeueing independently from the
	// public/manual retry contract carried by Error.Retryable.
	AutomaticRetryable bool
	RetryAt            time.Time
	MaxAttempts        int
}

func (failure Failure) Valid() bool {
	return validUUID(failure.UserID) && validUUID(failure.ID) &&
		validUUID(failure.LeaseToken) && failure.Error.Valid() &&
		!failure.RetryAt.IsZero() && failure.MaxAttempts > 0 &&
		failure.MaxAttempts <= 10
}

type SnapshotCheckpoint struct {
	UserID        string
	ID            string
	LeaseToken    string
	InputSnapshot json.RawMessage
	InputHash     [sha256.Size]byte
}

type RetrySource struct {
	Evaluation Record
	Item       FeedbackItem
}

func (source RetrySource) Valid() bool {
	return source.Evaluation.Valid() && source.Item.Valid() &&
		source.Item.EvaluationID == source.Evaluation.ID &&
		source.Evaluation.Kind == KindPracticeTurnFeedback &&
		source.Evaluation.Status == JobReady
}

func (checkpoint SnapshotCheckpoint) Valid() bool {
	return validUUID(checkpoint.UserID) && validUUID(checkpoint.ID) &&
		validUUID(checkpoint.LeaseToken) &&
		len(checkpoint.InputSnapshot) > 0 &&
		sha256.Sum256(checkpoint.InputSnapshot) == checkpoint.InputHash
}

type Store interface {
	Queue(context.Context, QueueCommand) (Record, bool, error)
	GetRecordBySource(context.Context, string, Kind, string) (Record, error)
	ClaimNext(context.Context, ClaimLane) (Claim, error)
	DeferClaim(context.Context, Deferral) error
	CheckpointSnapshot(context.Context, SnapshotCheckpoint) (Record, error)
	CompleteClaim(context.Context, Completion) error
	FailClaim(context.Context, Failure) error
	ListFeedbackItems(context.Context, string, string) ([]FeedbackItem, error)
	GetFeedbackItem(context.Context, string, string) (FeedbackItem, error)
}
