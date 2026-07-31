package config

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
)

func TestLoadISE(t *testing.T) {
	setISEEnvironment(t)
	t.Setenv("XFYUN_ISE_TIMEOUT", "45s")

	configuration, err := LoadISE()
	if err != nil {
		t.Fatalf("load ISE: %v", err)
	}
	if configuration.Endpoint != xfyun.DefaultISEEndpoint ||
		configuration.Timeout != 45*time.Second ||
		configuration.AppID.Reveal() != "test-app-id" ||
		configuration.APIKey.Reveal() != "test-api-key" ||
		configuration.APISecret.Reveal() != "test-api-secret" {
		t.Fatalf("unexpected ISE config: %#v", configuration)
	}
}

func TestLoadISERejectsIncompleteOrUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing app ID", key: "APPID", value: ""},
		{name: "missing API key", key: "APIKey", value: ""},
		{name: "missing API secret", key: "APISecret", value: ""},
		{name: "unsafe API secret", key: "APISecret", value: "secret value"},
		{name: "invalid timeout", key: "XFYUN_ISE_TIMEOUT", value: "soon"},
		{name: "excessive timeout", key: "XFYUN_ISE_TIMEOUT", value: "301s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setISEEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadISE(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func setISEEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("APPID", "test-app-id")
	t.Setenv("APIKey", "test-api-key")
	t.Setenv("APISecret", "test-api-secret")
	t.Setenv("XFYUN_ISE_ENDPOINT", "")
	t.Setenv("XFYUN_ISE_TIMEOUT", "")
}
