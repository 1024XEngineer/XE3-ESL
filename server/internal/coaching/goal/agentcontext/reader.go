package agentcontext

import (
	"context"
	"errors"

	agentctx "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Reader struct {
	source goal.Reader
}

func New(source goal.Reader) (*Reader, error) {
	if source == nil {
		return nil, errors.New("goal: Agent Context source is required")
	}
	return &Reader{source: source}, nil
}

func (reader *Reader) ReadGoalContext(
	ctx context.Context,
	actor requestcontext.Actor,
	goalID string,
) (agentctx.GoalContext, bool, error) {
	item, err := reader.source.ReadOwned(ctx, actor, goalID)
	if errors.Is(err, goal.ErrNotFound) {
		return agentctx.GoalContext{}, false, nil
	}
	if err != nil {
		return agentctx.GoalContext{}, false, err
	}
	return agentctx.GoalContext{
		ID:      item.ID,
		Title:   item.Title,
		Version: item.Version,
		Active:  item.Status == goal.StatusActive,
	}, true, nil
}

var _ agentctx.GoalReader = (*Reader)(nil)
