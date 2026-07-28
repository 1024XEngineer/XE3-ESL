package bootstrap

import (
	"errors"
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/mocktool"
	agentruntime "github.com/1024XEngineer/XE3-ESL/server/internal/agent/runtime"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func agentRunServiceOptions() ([]agentruntime.RunServiceOption, error) {
	toolConfig, err := config.LoadAgentTool()
	if err != nil {
		return nil, err
	}
	options := []agentruntime.RunServiceOption{
		agentruntime.WithRunLogger(slog.Default()),
	}
	if !toolConfig.FixturesEnabled {
		return options, nil
	}
	if toolConfig.Environment == "production" {
		return nil, errors.New("bootstrap: Agent tool fixtures are disabled in production")
	}
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		return nil, err
	}
	slog.Info(
		"agent tool fixtures enabled",
		slog.Int("tool_count", len(registry.Definitions())),
	)
	options = append(options, agentruntime.WithToolRegistry(registry))
	return options, nil
}
