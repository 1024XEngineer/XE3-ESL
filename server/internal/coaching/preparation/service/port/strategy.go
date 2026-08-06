// Package port defines the boundaries consumed by Preparation application
// services. Implementations live in strategy and infrastructure packages.
package port

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type ResolveCommand struct {
	Actor            requestcontext.Actor
	Input            model.ContextInput
	ScenarioDefaults model.ScenarioDefaults
}

type PreparationStrategy interface {
	Kind() model.PreparationKind
	Resolve(context.Context, ResolveCommand) (model.ResolvedContext, error)
}
