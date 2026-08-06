package service

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// ContextService is the only application entry point that selects a
// preparation-specific strategy. Callers do not switch on PreparationKind.
type ContextService struct {
	registry *StrategyRegistry
}

func (service *ContextService) ResolveContext(
	ctx context.Context,
	actor requestcontext.Actor,
	input model.ContextInput,
) (model.ResolvedContext, error) {
	return service.Resolve(ctx, port.ResolveCommand{Actor: actor, Input: input})
}

func NewContextService(registry *StrategyRegistry) (*ContextService, error) {
	if registry == nil {
		return nil, model.ErrUnsupportedKind
	}
	return &ContextService{registry: registry}, nil
}

func (service *ContextService) Resolve(
	ctx context.Context,
	command port.ResolveCommand,
) (model.ResolvedContext, error) {
	if service == nil || service.registry == nil || ctx == nil ||
		!command.Input.ValidShape() {
		return model.ResolvedContext{}, model.ErrInvalidContext
	}
	strategy, err := service.registry.Strategy(command.Input.Kind)
	if err != nil {
		return model.ResolvedContext{}, err
	}
	resolved, err := strategy.Resolve(ctx, command)
	if err != nil {
		return model.ResolvedContext{}, err
	}
	if !resolved.ValidShape() || resolved.Kind != command.Input.Kind {
		return model.ResolvedContext{}, model.ErrContextTypeMismatch
	}
	return resolved, nil
}
