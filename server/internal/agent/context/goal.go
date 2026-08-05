package context

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// GoalContext is the trusted Goal projection selected for one Agent Run.
type GoalContext struct {
	ID      string
	Title   string
	Version int64
	Active  bool
}

// GoalReader supplies only the Goal fields needed to assemble model context.
type GoalReader interface {
	ReadGoalContext(
		context.Context,
		requestcontext.Actor,
		string,
	) (GoalContext, bool, error)
}
