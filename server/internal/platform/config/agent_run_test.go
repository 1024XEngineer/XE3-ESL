package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAgentRun(t *testing.T) {
	t.Setenv("AGENT_RUN_LOOP_TIMEOUT", "175s")

	configuration, err := LoadAgentRun(60 * time.Second)
	if err != nil {
		t.Fatalf("LoadAgentRun() error = %v", err)
	}
	if configuration.LoopTimeout != 175*time.Second {
		t.Fatalf(
			"LoopTimeout = %s, want %s",
			configuration.LoopTimeout,
			175*time.Second,
		)
	}
}

func TestLoadAgentRunUsesAuditedDefault(t *testing.T) {
	t.Setenv("AGENT_RUN_LOOP_TIMEOUT", "")

	configuration, err := LoadAgentRun(60 * time.Second)
	if err != nil {
		t.Fatalf("LoadAgentRun() error = %v", err)
	}
	if configuration.LoopTimeout != defaultAgentRunLoopTimeout {
		t.Fatalf(
			"LoopTimeout = %s, want %s",
			configuration.LoopTimeout,
			defaultAgentRunLoopTimeout,
		)
	}
}

func TestLoadAgentRunRejectsInvalidLoopTimeout(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		providerTimeout time.Duration
		want            string
	}{
		{name: "invalid", value: "soon", providerTimeout: time.Minute, want: "valid Go duration"},
		{name: "zero", value: "0s", providerTimeout: time.Minute, want: "greater than zero"},
		{name: "negative", value: "-1s", providerTimeout: time.Minute, want: "greater than zero"},
		{name: "equal to provider", value: "60s", providerTimeout: time.Minute, want: "greater than the text provider timeout"},
		{name: "below provider", value: "59s", providerTimeout: time.Minute, want: "greater than the text provider timeout"},
		{name: "default not above provider", value: "", providerTimeout: 150 * time.Second, want: "greater than the text provider timeout"},
		{name: "invalid provider", value: "150s", providerTimeout: 0, want: "text provider timeout must be greater than zero"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGENT_RUN_LOOP_TIMEOUT", test.value)
			_, err := LoadAgentRun(test.providerTimeout)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadAgentRun() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
