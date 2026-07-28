package config

import (
	"errors"
	"os"
	"strings"
)

const (
	AgentToolModeReal = "real"
	AgentToolModeMock = "mock"
)

var ErrInvalidAgentToolConfig = errors.New("agent tool configuration is invalid")

type AgentToolConfig struct {
	Mode        string
	Environment string
}

func LoadAgentTool() (AgentToolConfig, error) {
	config := AgentToolConfig{
		Mode:        valueOrDefault("AGENT_TOOL_MODE", AgentToolModeReal),
		Environment: strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))),
	}
	config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	switch config.Mode {
	case AgentToolModeReal, AgentToolModeMock:
	default:
		return AgentToolConfig{}, ErrInvalidAgentToolConfig
	}
	if config.Mode == AgentToolModeMock && config.Environment == "production" {
		return AgentToolConfig{}, ErrInvalidAgentToolConfig
	}
	return config, nil
}
