package bootstrap

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
)

func TestAgentRunServiceOptionsUseProductionRegistry(t *testing.T) {
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

func TestAgentRunServiceOptionsRequireProductionRegistry(t *testing.T) {
	if _, err := agentRunServiceOptions(nil); err == nil {
		t.Fatal("agentRunServiceOptions() error = nil, want registry rejection")
	}
}
