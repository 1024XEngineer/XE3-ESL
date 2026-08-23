package app

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
)

func TestAgentRunServiceOptionsUseProductionRegistry(t *testing.T) {
	registry, err := capability.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	options, err := agentRunServiceOptions(
		registry,
		AgentRunRuntimeConfiguration{LoopTimeout: 150 * time.Second},
	)
	if err != nil {
		t.Fatalf("agentRunServiceOptions() error = %v", err)
	}
	if len(options.runOptions) != 3 ||
		options.productionRegistry != registry {
		t.Fatalf("real options = %#v", options)
	}
}

func TestAgentRunServiceOptionsRequireProductionRegistry(t *testing.T) {
	if _, err := agentRunServiceOptions(
		nil,
		AgentRunRuntimeConfiguration{LoopTimeout: 150 * time.Second},
	); err == nil {
		t.Fatal("agentRunServiceOptions() error = nil, want registry rejection")
	}
}

func TestAgentRunServiceOptionsRequireLoopTimeout(t *testing.T) {
	registry, err := capability.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentRunServiceOptions(
		registry,
		AgentRunRuntimeConfiguration{},
	); err == nil {
		t.Fatal("agentRunServiceOptions() error = nil, want timeout rejection")
	}
}
