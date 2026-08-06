package conversation

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// GoalState is the only Goal projection needed by Agent Conversation.
type GoalState struct {
	ID     string
	Active bool
}

// GoalReader resolves an owned Goal without exposing Coaching's domain model.
type GoalReader interface {
	ReadGoalState(
		context.Context,
		requestcontext.Actor,
		string,
	) (GoalState, bool, error)
}
