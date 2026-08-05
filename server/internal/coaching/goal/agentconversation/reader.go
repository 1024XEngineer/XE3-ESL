package agentconversation

import (
	"context"
	"errors"

	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Reader struct {
	source goal.Reader
}

func New(source goal.Reader) (*Reader, error) {
	if source == nil {
		return nil, errors.New("goal: Agent Conversation source is required")
	}
	return &Reader{source: source}, nil
}

func (reader *Reader) ReadGoalState(
	ctx context.Context,
	actor requestcontext.Actor,
	goalID string,
) (conversation.GoalState, bool, error) {
	item, err := reader.source.ReadOwned(ctx, actor, goalID)
	if errors.Is(err, goal.ErrNotFound) {
		return conversation.GoalState{}, false, nil
	}
	if err != nil {
		return conversation.GoalState{}, false, err
	}
	return conversation.GoalState{
		ID:     item.ID,
		Active: item.Status == goal.StatusActive,
	}, true, nil
}

var _ conversation.GoalReader = (*Reader)(nil)
