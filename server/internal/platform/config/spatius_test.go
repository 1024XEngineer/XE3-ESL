package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadSpatiusUsesDisabledSafeDefaults(t *testing.T) {
	clearSpatiusEnvironment(t)

	configuration, err := LoadSpatius()
	if err != nil {
		t.Fatalf("LoadSpatius() error = %v", err)
	}
	if configuration.Enabled ||
		configuration.Region != SpatiusRegionChinaBeijing ||
		configuration.ConsoleBaseURL !=
			"https://console.cn-beijing.spatialwalk.top/v1/console" ||
		configuration.TokenTTL != 10*time.Minute ||
		configuration.Timeout != 5*time.Second ||
		configuration.APIKey.Reveal() != "" {
		t.Fatalf("unexpected defaults: %#v", configuration)
	}
}

func TestLoadSpatiusReadsEnabledConfiguration(t *testing.T) {
	clearSpatiusEnvironment(t)
	t.Setenv("SPATIUS_ENABLED", "true")
	t.Setenv("SPATIUS_REGION", SpatiusRegionAPNortheast)
	t.Setenv(
		"SPATIUS_CONSOLE_BASE_URL",
		"https://console.ap-northeast.spatius.ai/v1/console",
	)
	t.Setenv("SPATIUS_APP_ID", "app-test_1")
	t.Setenv("SPATIUS_AVATAR_ID", "avatar-test_1")
	t.Setenv("SPATIUS_API_KEY", "server-only-key")
	t.Setenv("SPATIUS_TOKEN_TTL", "8m")
	t.Setenv("SPATIUS_TIMEOUT", "8s")

	configuration, err := LoadSpatius()
	if err != nil {
		t.Fatalf("LoadSpatius() error = %v", err)
	}
	if !configuration.Enabled ||
		configuration.Region != SpatiusRegionAPNortheast ||
		configuration.AppID != "app-test_1" ||
		configuration.AvatarID != "avatar-test_1" ||
		configuration.APIKey.Reveal() != "server-only-key" ||
		configuration.TokenTTL != 8*time.Minute ||
		configuration.Timeout != 8*time.Second {
		t.Fatalf("unexpected configuration: %#v", configuration)
	}
	if strings.Contains(
		configuration.APIKey.String(),
		configuration.APIKey.Reveal(),
	) {
		t.Fatal("formatted API key exposed its value")
	}
}

func TestLoadSpatiusRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "enabled flag", key: "SPATIUS_ENABLED", value: "maybe"},
		{name: "region", key: "SPATIUS_REGION", value: "eu-central"},
		{
			name:  "region endpoint mismatch",
			key:   "SPATIUS_CONSOLE_BASE_URL",
			value: "https://console.us-west.spatius.ai/v1/console",
		},
		{
			name:  "endpoint query",
			key:   "SPATIUS_CONSOLE_BASE_URL",
			value: "https://console.cn-beijing.spatialwalk.top/v1/console?redirect=1",
		},
		{name: "short token ttl", key: "SPATIUS_TOKEN_TTL", value: "30s"},
		{name: "long token ttl", key: "SPATIUS_TOKEN_TTL", value: "11m"},
		{name: "long timeout", key: "SPATIUS_TIMEOUT", value: "31s"},
		{name: "app identifier", key: "SPATIUS_APP_ID", value: "bad/app"},
		{name: "avatar identifier", key: "SPATIUS_AVATAR_ID", value: "bad avatar"},
		{name: "api key whitespace", key: "SPATIUS_API_KEY", value: "bad key value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredSpatiusEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadSpatius(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestLoadSpatiusDisabledIgnoresStaleProviderOverrides(t *testing.T) {
	clearSpatiusEnvironment(t)
	t.Setenv("SPATIUS_ENABLED", "false")
	t.Setenv("SPATIUS_REGION", "stale-invalid-region")
	t.Setenv("SPATIUS_CONSOLE_BASE_URL", "http://unsafe.example.test")
	t.Setenv("SPATIUS_TOKEN_TTL", "24h")
	t.Setenv("SPATIUS_TIMEOUT", "2m")
	t.Setenv("SPATIUS_API_KEY", "stale secret with spaces")

	configuration, err := LoadSpatius()
	if err != nil {
		t.Fatalf("LoadSpatius() error = %v", err)
	}
	if configuration.Enabled ||
		configuration.Region != SpatiusRegionChinaBeijing ||
		configuration.TokenTTL != defaultSpatiusTokenTTL ||
		configuration.Timeout != defaultSpatiusTimeout ||
		configuration.APIKey.Reveal() != "" {
		t.Fatalf("unexpected disabled configuration: %#v", configuration)
	}
}

func clearSpatiusEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SPATIUS_ENABLED",
		"SPATIUS_CONSOLE_BASE_URL",
		"SPATIUS_APP_ID",
		"SPATIUS_AVATAR_ID",
		"SPATIUS_REGION",
		"SPATIUS_API_KEY",
		"SPATIUS_TOKEN_TTL",
		"SPATIUS_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}

func setRequiredSpatiusEnvironment(t *testing.T) {
	t.Helper()
	clearSpatiusEnvironment(t)
	t.Setenv("SPATIUS_ENABLED", "true")
	t.Setenv("SPATIUS_REGION", SpatiusRegionChinaBeijing)
	t.Setenv(
		"SPATIUS_CONSOLE_BASE_URL",
		"https://console.cn-beijing.spatialwalk.top/v1/console",
	)
	t.Setenv("SPATIUS_APP_ID", "app-test")
	t.Setenv("SPATIUS_AVATAR_ID", "avatar-test")
	t.Setenv("SPATIUS_API_KEY", "server-only-key")
}
