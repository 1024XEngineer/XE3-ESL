// Package service coordinates Preparation use cases through narrow ports.
package service

import (
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
)

type StrategyRegistry struct {
	strategies map[model.PreparationKind]port.PreparationStrategy
}

func NewStrategyRegistry(
	strategies ...port.PreparationStrategy,
) (*StrategyRegistry, error) {
	registry := &StrategyRegistry{
		strategies: make(map[model.PreparationKind]port.PreparationStrategy, len(strategies)),
	}
	for _, strategy := range strategies {
		if strategy == nil || !strategy.Kind().Valid() {
			return nil, model.ErrUnsupportedKind
		}
		if _, exists := registry.strategies[strategy.Kind()]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate %s strategy",
				model.ErrUnsupportedKind,
				strategy.Kind(),
			)
		}
		registry.strategies[strategy.Kind()] = strategy
	}
	return registry, nil
}

func (registry *StrategyRegistry) Strategy(
	kind model.PreparationKind,
) (port.PreparationStrategy, error) {
	if registry == nil {
		return nil, model.ErrUnsupportedKind
	}
	strategy, found := registry.strategies[kind]
	if !found {
		return nil, model.ErrUnsupportedKind
	}
	return strategy, nil
}
