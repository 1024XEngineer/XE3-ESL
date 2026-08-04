package voice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrRetryTurnInvalid  = errors.New("conversation: invalid retry Turn")
	ErrRetryTurnNotFound = errors.New("conversation: retry Turn not found")
	ErrRetryTurnConflict = errors.New("conversation: retry Turn conflict")
	ErrRetryTurnNotReady = errors.New("conversation: retry Turn is not answering")
)

type RetryTurnStatus string

const (
	RetryTurnAnswering RetryTurnStatus = "ANSWERING"
	RetryTurnConfirmed RetryTurnStatus = "CONFIRMED"
)

// RetryTurnDraft is a real Conversation-owned Turn identity. The draft remains
// ANSWERING until a candidate for the same Session and Question is confirmed
// with RetryTurnID, at which point the confirmed RETRY Turn keeps this ID.
type RetryTurnDraft struct {
	TurnID            string
	RetryRequestID    string
	PracticeSessionID string
	OriginalTurnID    string
	QuestionID        string
	Status            RetryTurnStatus
	CandidateID       string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ConfirmedAt       *time.Time
}

type CreateRetryTurnCommand struct {
	RetryRequestID    string
	PracticeSessionID string
	OriginalTurnID    string
	QuestionID        string
}

type RetryTurnStore interface {
	CreateRetryTurn(
		context.Context,
		requestcontext.Actor,
		CreateRetryTurnCommand,
	) (RetryTurnDraft, error)
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
		return nil, errors.New(
			"conversation: retry Turn store is required",
		)
	}
	return &RetryTurnService{store: store}, nil
}

func (service *RetryTurnService) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreateRetryTurnCommand,
) (RetryTurnDraft, error) {
	if service == nil || service.store == nil || ctx == nil ||
		!actor.Valid() || !validRetryTurnCommand(command) {
		return RetryTurnDraft{}, ErrRetryTurnInvalid
	}
	if err := ctx.Err(); err != nil {
		return RetryTurnDraft{}, err
	}
	return service.store.CreateRetryTurn(ctx, actor, command)
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

func validRetryTurnCommand(command CreateRetryTurnCommand) bool {
	return validRetryUUID(command.RetryRequestID) &&
		validRetryResourceID(command.PracticeSessionID) &&
		validRetryResourceID(command.OriginalTurnID) &&
		validRetryResourceID(command.QuestionID)
}

func validRetryResourceID(value string) bool {
	if value == "" || len(value) > 128 ||
		value != strings.TrimSpace(value) {
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

func validRetryUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := range len(value) {
		switch index {
		case 8, 13, 18, 23:
			if value[index] != '-' {
				return false
			}
		default:
			character := value[index]
			if !((character >= '0' && character <= '9') ||
				(character >= 'a' && character <= 'f') ||
				(character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}
