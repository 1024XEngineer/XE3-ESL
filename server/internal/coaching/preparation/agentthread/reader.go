package agentthread

import (
	"context"
	"errors"
	"fmt"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type threadReader interface {
	GetThread(
		context.Context,
		requestcontext.Actor,
		string,
	) (agentconversation.Thread, error)
}

type Reader struct {
	threads threadReader
}

func New(threads threadReader) (*Reader, error) {
	if threads == nil {
		return nil, errors.New("Agent Thread reader is required")
	}
	return &Reader{threads: threads}, nil
}

func (reader *Reader) ReadOwnedThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) (preparation.SourceThread, error) {
	if reader == nil || reader.threads == nil || ctx == nil || !actor.Valid() {
		return preparation.SourceThread{}, preparation.ErrPlanInvalid
	}
	thread, err := reader.threads.GetThread(ctx, actor, threadID)
	if err != nil {
		return preparation.SourceThread{}, mapError(err)
	}
	if thread.ID != threadID || thread.OwnerID != actor.UserID {
		return preparation.SourceThread{}, preparation.ErrPlanNotFound
	}
	return preparation.SourceThread{ID: thread.ID}, nil
}

func mapError(err error) error {
	switch {
	case errors.Is(err, agentconversation.ErrInvalidRequest):
		return preparation.ErrPlanInvalid
	case errors.Is(err, agentconversation.ErrNotFound):
		return preparation.ErrPlanNotFound
	case errors.Is(err, agentconversation.ErrConflict):
		return preparation.ErrPlanConflict
	default:
		return fmt.Errorf("read Agent Thread for Preparation: %w", err)
	}
}

var _ preparation.SourceThreadReader = (*Reader)(nil)
