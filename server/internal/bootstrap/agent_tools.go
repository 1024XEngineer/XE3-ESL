package bootstrap

import (
	"errors"
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agenttest/capabilityfixture"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
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
	toolConfig, err := config.LoadAgentTool()
	if err != nil {
		return agentRunOptions{}, err
	}
	options := []agentrun.Option{
		agentrun.WithRunLogger(slog.Default()),
	}
	if !toolConfig.FixturesEnabled {
		options = append(
			options,
			agentrun.WithToolRegistry(productionRegistry),
		)
		return agentRunOptions{
			runOptions:         options,
			productionRegistry: productionRegistry,
		}, nil
	}
	if toolConfig.Environment == "production" {
		return agentRunOptions{},
			errors.New("bootstrap: Agent tool fixtures are disabled in production")
	}
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		return agentRunOptions{}, err
	}
	slog.Info(
		"agent tool fixtures enabled",
		slog.Int("tool_count", len(registry.Definitions())),
	)
	options = append(options, agentrun.WithToolRegistry(registry))
	return agentRunOptions{runOptions: options}, nil
}
