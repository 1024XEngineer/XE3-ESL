package config

import (
	"errors"
	"testing"
)

func TestLoadAgentToolDefaultsToRealMode(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "")
	t.Setenv("APP_ENV", "")

	config, err := LoadAgentTool()
	if err != nil {
		t.Fatalf("LoadAgentTool() error = %v", err)
	}
	if config.Mode != AgentToolModeReal {
		t.Fatalf("Mode = %q, want %q", config.Mode, AgentToolModeReal)
	}
}

func TestLoadAgentToolAllowsExplicitLocalMock(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", AgentToolModeMock)
	t.Setenv("APP_ENV", "development")

	config, err := LoadAgentTool()
	if err != nil {
		t.Fatalf("LoadAgentTool() error = %v", err)
	}
	if config.Mode != AgentToolModeMock {
		t.Fatalf("Mode = %q, want %q", config.Mode, AgentToolModeMock)
	}
}

func TestLoadAgentToolRejectsProductionMock(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", AgentToolModeMock)
	t.Setenv("APP_ENV", "production")

	_, err := LoadAgentTool()
	if !errors.Is(err, ErrInvalidAgentToolConfig) {
		t.Fatalf("LoadAgentTool() error = %v, want %v", err, ErrInvalidAgentToolConfig)
	}
}
