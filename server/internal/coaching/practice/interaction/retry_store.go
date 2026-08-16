package interaction

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrRetryTurnInvalid  = errors.New("practice interaction: invalid retry Turn")
	ErrRetryTurnNotFound = errors.New("practice interaction: retry Turn not found")
	ErrRetryTurnConflict = errors.New("practice interaction: retry Turn conflict")
	ErrRetryTurnNotReady = errors.New("practice interaction: retry Turn is not ready")
)

type RetryTurnStatus string

const (
	RetryTurnAnswering RetryTurnStatus = "ANSWERING"
	RetryTurnReady     RetryTurnStatus = "READY"
	RetryTurnFailed    RetryTurnStatus = "FAILED"
	RetryTurnConfirmed RetryTurnStatus = "CONFIRMED"
)

// RetryTurnDraft is the actor-owned Practice Turn created synchronously by
// Review. Voice only reads and advances this same row; it never creates a
// parallel retry resource.
type RetryTurnDraft struct {
	TurnID                  string
	ClientRequestID         string
	PracticeSessionID       string
	OriginalTurnID          string
	QuestionID              string
	RespondentParticipantID string
	Status                  RetryTurnStatus
	CandidateID             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	ConfirmedAt             *time.Time
}

type RetryTurnStore interface {
	GetRetryTurn(
		context.Context,
		requestcontext.Actor,
		string,
	) (RetryTurnDraft, error)
}

type RetryTurnService struct {
	store RetryTurnStore
}

func NewRetryTurnService(store RetryTurnStore) (*RetryTurnService, error) {
	if store == nil {
		return nil, errors.New("practice interaction: retry Turn store is required")
	}
	return &RetryTurnService{store: store}, nil
}

func (service *RetryTurnService) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
) (RetryTurnDraft, error) {
	if service == nil || service.store == nil || ctx == nil ||
		!actor.Valid() || !validRetryResourceID(turnID) {
		return RetryTurnDraft{}, ErrRetryTurnInvalid
	}
	if err := ctx.Err(); err != nil {
		return RetryTurnDraft{}, err
	}
	return service.store.GetRetryTurn(ctx, actor, turnID)
}

func validRetryResourceID(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '.', '_', ':', '-':
		default:
			return false
		}
	}
	return true
}
