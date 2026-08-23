package app

import (
	"errors"
	"log/slog"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

type agentRunOptions struct {
	runOptions         []agentrun.Option
	productionRegistry *capability.Registry
}

type AgentRunRuntimeConfiguration struct {
	LoopTimeout time.Duration
}

func agentRunServiceOptions(
	productionRegistry *capability.Registry,
	runtimeConfiguration AgentRunRuntimeConfiguration,
) (agentRunOptions, error) {
	if productionRegistry == nil {
		return agentRunOptions{},
			errors.New("bootstrap: production Agent tool registry is required")
	}
	if runtimeConfiguration.LoopTimeout <= 0 {
		return agentRunOptions{},
			errors.New("bootstrap: Agent Run loop timeout is required")
	}
	options := []agentrun.Option{
		agentrun.WithRunLogger(slog.Default()),
		agentrun.WithToolRegistry(productionRegistry),
		agentrun.WithLoopLimits(agentrun.LoopLimits{
			LoopTimeout: runtimeConfiguration.LoopTimeout,
		}),
	}
	return agentRunOptions{
		runOptions:         options,
		productionRegistry: productionRegistry,
	}, nil
}
