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
	Mode            string
	Environment     string
	FixturesEnabled bool
}

func LoadAgentTool() (AgentToolConfig, error) {
	fixtures, err := parseAgentToolFixtures(os.Getenv("AGENT_TOOL_FIXTURES"))
	if err != nil {
		return AgentToolConfig{}, err
	}
	config := AgentToolConfig{
		Mode:            valueOrDefault("AGENT_TOOL_MODE", AgentToolModeReal),
		Environment:     strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))),
		FixturesEnabled: fixtures,
	}
	config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	switch config.Mode {
	case AgentToolModeReal, AgentToolModeMock:
	default:
		return AgentToolConfig{}, ErrInvalidAgentToolConfig
	}
	if (config.Mode == AgentToolModeMock || config.FixturesEnabled) &&
		config.Environment == "production" {
		return AgentToolConfig{}, ErrInvalidAgentToolConfig
	}
	return config, nil
}

func parseAgentToolFixtures(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false", "disabled":
		return false, nil
	case "1", "true", "enabled":
		return true, nil
	default:
		return false, ErrInvalidAgentToolConfig
	}
}
