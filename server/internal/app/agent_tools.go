package app

import (
	"errors"
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

type agentRunOptions struct {
	runOptions         []agentrun.Option
	productionRegistry *capability.Registry
}

func agentRunServiceOptions(
	productionRegistry *capability.Registry,
) (agentRunOptions, error) {
	if productionRegistry == nil {
		return agentRunOptions{},
			errors.New("bootstrap: production Agent tool registry is required")
	}
	options := []agentrun.Option{
		agentrun.WithRunLogger(slog.Default()),
		agentrun.WithToolRegistry(productionRegistry),
	}
	return agentRunOptions{
		runOptions:         options,
		productionRegistry: productionRegistry,
	}, nil
}
