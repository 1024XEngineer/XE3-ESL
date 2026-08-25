package config

import (
	"testing"
	"time"
)

func TestLoadISERelay(t *testing.T) {
	setISERelayEnvironment(t)
	configuration, err := LoadISERelay()
	if err != nil {
		t.Fatalf("load ISE relay: %v", err)
	}
	if configuration.Endpoint != "https://relay.example.test" ||
		configuration.PollInterval != defaultISERelayPollInterval ||
		configuration.Timeout != defaultISERelayTimeout {
		t.Fatalf("unexpected ISE relay config: %#v", configuration)
	}
}

func TestISERelayConfiguredRejectsPartialAndUnsafeConfiguration(t *testing.T) {
	for _, name := range []string{
		"ISE_RELAY_ENDPOINT",
		"ISE_RELAY_CA_FILE",
		"ISE_RELAY_CLIENT_CERT_FILE",
		"ISE_RELAY_CLIENT_KEY_FILE",
	} {
		t.Setenv(name, "")
	}
	if ISERelayConfigured() {
		t.Fatal("empty ISE relay configuration must be disabled")
	}
	t.Setenv("ISE_RELAY_ENDPOINT", "https://relay.example.test")
	if !ISERelayConfigured() {
		t.Fatal("partial relay configuration must be detected")
	}
	if _, err := LoadISERelay(); err == nil {
		t.Fatal("partial relay configuration must fail closed")
	}

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "plain HTTP", key: "ISE_RELAY_ENDPOINT", value: "http://relay.example.test"},
		{name: "endpoint path", key: "ISE_RELAY_ENDPOINT", value: "https://relay.example.test/api"},
		{name: "fast polling", key: "ISE_RELAY_POLL_INTERVAL", value: "10ms"},
		{name: "slow polling", key: "ISE_RELAY_POLL_INTERVAL", value: "11s"},
		{name: "excessive timeout", key: "ISE_RELAY_TIMEOUT", value: "301s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setISERelayEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadISERelay(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestLoadISERelayAcceptsExplicitDurations(t *testing.T) {
	setISERelayEnvironment(t)
	t.Setenv("ISE_RELAY_POLL_INTERVAL", "750ms")
	t.Setenv("ISE_RELAY_TIMEOUT", "2m")
	configuration, err := LoadISERelay()
	if err != nil {
		t.Fatalf("load ISE relay: %v", err)
	}
	if configuration.PollInterval != 750*time.Millisecond ||
		configuration.Timeout != 2*time.Minute {
		t.Fatalf("unexpected durations: %#v", configuration)
	}
}

func setISERelayEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("ISE_RELAY_ENDPOINT", "https://relay.example.test")
	t.Setenv("ISE_RELAY_CA_FILE", "/run/secrets/relay-ca.pem")
	t.Setenv("ISE_RELAY_CLIENT_CERT_FILE", "/run/secrets/relay-client.pem")
	t.Setenv("ISE_RELAY_CLIENT_KEY_FILE", "/run/secrets/relay-client-key.pem")
	t.Setenv("ISE_RELAY_POLL_INTERVAL", "")
	t.Setenv("ISE_RELAY_TIMEOUT", "")
}
