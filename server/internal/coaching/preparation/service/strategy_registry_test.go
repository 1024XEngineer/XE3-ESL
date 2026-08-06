package service

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
)

type registryStrategy struct{ kind model.PreparationKind }

func (strategy registryStrategy) Kind() model.PreparationKind { return strategy.kind }

func (strategy registryStrategy) Resolve(
	context.Context,
	port.ResolveCommand,
) (model.ResolvedContext, error) {
	return model.ResolvedContext{Kind: strategy.kind}, nil
}

func TestStrategyRegistryRejectsDuplicateKind(t *testing.T) {
	_, err := NewStrategyRegistry(
		registryStrategy{kind: model.PreparationKindScenario},
		registryStrategy{kind: model.PreparationKindScenario},
	)
	if !errors.Is(err, model.ErrUnsupportedKind) {
		t.Fatalf("NewStrategyRegistry error = %v", err)
	}
}

func TestStrategyRegistryReturnsStableUnsupportedError(t *testing.T) {
	registry, err := NewStrategyRegistry()
	if err != nil {
		t.Fatalf("NewStrategyRegistry: %v", err)
	}
	_, err = registry.Strategy(model.PreparationKind("ielts"))
	if !errors.Is(err, model.ErrUnsupportedKind) {
		t.Fatalf("Strategy error = %v", err)
	}
}
