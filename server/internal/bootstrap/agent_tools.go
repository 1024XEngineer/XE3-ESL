package bootstrap

import (
	"errors"
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/mocktool"
	agentruntime "github.com/1024XEngineer/XE3-ESL/server/internal/agent/runtime"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

type agentToolRuntimeOptions struct {
	runOptions         []agentruntime.RunServiceOption
	productionRegistry *tool.Registry
}

func agentRunServiceOptions(
	productionRegistry *tool.Registry,
) (agentToolRuntimeOptions, error) {
	if productionRegistry == nil {
		return agentToolRuntimeOptions{},
			errors.New("bootstrap: production Agent tool registry is required")
	}
	toolConfig, err := config.LoadAgentTool()
	if err != nil {
		return agentToolRuntimeOptions{}, err
	}
	options := []agentruntime.RunServiceOption{
		agentruntime.WithRunLogger(slog.Default()),
	}
	if !toolConfig.FixturesEnabled {
		options = append(
			options,
			agentruntime.WithToolRegistry(productionRegistry),
		)
		return agentToolRuntimeOptions{
			runOptions:         options,
			productionRegistry: productionRegistry,
		}, nil
	}
	if toolConfig.Environment == "production" {
		return agentToolRuntimeOptions{},
			errors.New("bootstrap: Agent tool fixtures are disabled in production")
	}
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		return agentToolRuntimeOptions{}, err
	}
	slog.Info(
		"agent tool fixtures enabled",
		slog.Int("tool_count", len(registry.Definitions())),
	)
	options = append(options, agentruntime.WithToolRegistry(registry))
	return agentToolRuntimeOptions{runOptions: options}, nil
}
