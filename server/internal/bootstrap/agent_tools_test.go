package bootstrap

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
)

func TestAgentRunServiceOptionsAreDisabledByDefault(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "real")
	t.Setenv("AGENT_TOOL_FIXTURES", "")
	t.Setenv("APP_ENV", "development")

	registry, err := capability.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	options, err := agentRunServiceOptions(registry)
	if err != nil {
		t.Fatalf("agentRunServiceOptions() error = %v", err)
	}
	if len(options.runOptions) != 2 ||
		options.productionRegistry != registry {
		t.Fatalf("real options = %#v", options)
	}
}

func TestAgentRunServiceOptionsEnableDevelopmentFixtures(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "real")
	t.Setenv("AGENT_TOOL_FIXTURES", "1")
	t.Setenv("APP_ENV", "development")

	registry, err := capability.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	options, err := agentRunServiceOptions(registry)
	if err != nil {
		t.Fatalf("agentRunServiceOptions() error = %v", err)
	}
	if len(options.runOptions) != 2 || options.productionRegistry != nil {
		t.Fatalf("fixture options = %#v", options)
	}
}

func TestAgentRunServiceOptionsRejectProductionFixtures(t *testing.T) {
	t.Setenv("AGENT_TOOL_MODE", "real")
	t.Setenv("AGENT_TOOL_FIXTURES", "1")
	t.Setenv("APP_ENV", "production")

	registry, err := capability.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentRunServiceOptions(registry); err == nil {
		t.Fatal("agentRunServiceOptions() error = nil, want production rejection")
	}
}
