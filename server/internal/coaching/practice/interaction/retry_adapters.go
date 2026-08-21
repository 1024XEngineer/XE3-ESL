package interaction

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type retryTurnAdapter struct {
	service *RetryTurnService
}

func (adapter *retryTurnAdapter) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	retryTurnID string,
) (RetryTurnDraft, error) {
	if adapter == nil || adapter.service == nil {
		return RetryTurnDraft{}, ErrInvalidContext
	}
	draft, err := adapter.service.Get(ctx, actor, retryTurnID)
	switch {
	case errors.Is(err, ErrRetryTurnInvalid):
		return RetryTurnDraft{}, ErrInvalidRequest
	case errors.Is(err, ErrRetryTurnNotFound):
		return RetryTurnDraft{}, ErrNotFound
	case errors.Is(err, ErrRetryTurnConflict),
		errors.Is(err, ErrRetryTurnNotReady):
		return RetryTurnDraft{}, ErrConflict
	default:
		return draft, err
	}
}
