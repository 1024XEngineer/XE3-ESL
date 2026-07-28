package bootstrap

import "testing"

func TestAgentRunServiceOptionsAreDisabledByDefault(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "real")
	t.Setenv("AGENT_TOOL_FIXTURES", "")
	t.Setenv("APP_ENV", "development")

	options, err := agentRunServiceOptions()
	if err != nil {
		t.Fatalf("agentRunServiceOptions() error = %v", err)
	}
	if len(options) != 1 {
		t.Fatalf("options length = %d, want logger option only", len(options))
	}
}

func TestAgentRunServiceOptionsEnableDevelopmentFixtures(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "real")
	t.Setenv("AGENT_TOOL_FIXTURES", "1")
	t.Setenv("APP_ENV", "development")

	options, err := agentRunServiceOptions()
	if err != nil {
		t.Fatalf("agentRunServiceOptions() error = %v", err)
	}
	if len(options) != 2 {
		t.Fatalf("options length = %d, want logger and fixture options", len(options))
	}
}

func TestAgentRunServiceOptionsRejectProductionFixtures(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "real")
	t.Setenv("AGENT_TOOL_FIXTURES", "1")
	t.Setenv("APP_ENV", "production")

	if _, err := agentRunServiceOptions(); err == nil {
		t.Fatal("agentRunServiceOptions() error = nil, want production rejection")
	}
}
