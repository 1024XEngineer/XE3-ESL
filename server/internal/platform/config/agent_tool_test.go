package config

import (
	"errors"
	"testing"
)

func TestLoadAgentToolDefaultsToRealMode(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "")
	t.Setenv("AGENT_TOOL_FIXTURES", "")
	t.Setenv("APP_ENV", "")

	config, err := LoadAgentTool()
	if err != nil {
		t.Fatalf("LoadAgentTool() error = %v", err)
	}
	if config.Mode != AgentToolModeReal {
		t.Fatalf("Mode = %q, want %q", config.Mode, AgentToolModeReal)
	}
	if config.FixturesEnabled {
		t.Fatal("FixturesEnabled = true, want false")
	}
}

func TestLoadAgentToolAllowsExplicitLocalMock(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", AgentToolModeMock)
	t.Setenv("AGENT_TOOL_FIXTURES", "")
	t.Setenv("APP_ENV", "development")

	config, err := LoadAgentTool()
	if err != nil {
		t.Fatalf("LoadAgentTool() error = %v", err)
	}
	if config.Mode != AgentToolModeMock {
		t.Fatalf("Mode = %q, want %q", config.Mode, AgentToolModeMock)
	}
}

func TestLoadAgentToolAllowsDevelopmentFixturesInRealMode(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", AgentToolModeReal)
	t.Setenv("AGENT_TOOL_FIXTURES", "enabled")
	t.Setenv("APP_ENV", "development")

	config, err := LoadAgentTool()
	if err != nil {
		t.Fatalf("LoadAgentTool() error = %v", err)
	}
	if config.Mode != AgentToolModeReal || !config.FixturesEnabled {
		t.Fatalf("config = %#v, want real mode with fixtures", config)
	}
}

func TestLoadAgentToolRejectsProductionMock(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", AgentToolModeMock)
	t.Setenv("AGENT_TOOL_FIXTURES", "")
	t.Setenv("APP_ENV", "production")

	_, err := LoadAgentTool()
	if !errors.Is(err, ErrInvalidAgentToolConfig) {
		t.Fatalf("LoadAgentTool() error = %v, want %v", err, ErrInvalidAgentToolConfig)
	}
}

func TestLoadAgentToolRejectsProductionFixtures(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", AgentToolModeReal)
	t.Setenv("AGENT_TOOL_FIXTURES", "1")
	t.Setenv("APP_ENV", "production")

	_, err := LoadAgentTool()
	if !errors.Is(err, ErrInvalidAgentToolConfig) {
		t.Fatalf("LoadAgentTool() error = %v, want %v", err, ErrInvalidAgentToolConfig)
	}
}

func TestLoadAgentToolRejectsInvalidFixturesFlag(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", AgentToolModeReal)
	t.Setenv("AGENT_TOOL_FIXTURES", "maybe")
	t.Setenv("APP_ENV", "development")

	_, err := LoadAgentTool()
	if !errors.Is(err, ErrInvalidAgentToolConfig) {
		t.Fatalf("LoadAgentTool() error = %v, want %v", err, ErrInvalidAgentToolConfig)
	}
}
