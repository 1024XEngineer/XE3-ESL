package config

import (
	"testing"
	"time"
)

func TestLoadSpatiusEnabledUsesProviderAndApplicationConfiguration(t *testing.T) {
	t.Setenv("SPATIUS_ENABLED", "true")
	t.Setenv("SPATIUS_REGION", "us-west")
	t.Setenv("SPATIUS_CONSOLE_BASE_URL", "")
	t.Setenv("SPATIUS_APP_ID", "test-app-id")
	t.Setenv("SPATIUS_API_KEY", "test-api-key")
	t.Setenv("SPATIUS_TOKEN_TTL", "")
	t.Setenv("SPATIUS_TIMEOUT", "")

	configuration, err := LoadSpatius()
	if err != nil {
		t.Fatalf("LoadSpatius: %v", err)
	}
	if !configuration.Enabled ||
		configuration.ConsoleBaseURL != spatiusConsoleBaseURLs[SpatiusRegionUSWest] ||
		configuration.AppID != "test-app-id" ||
		configuration.Region != SpatiusRegionUSWest ||
		configuration.APIKey.Reveal() != "test-api-key" ||
		configuration.TokenTTL != 10*time.Minute ||
		configuration.Timeout != 5*time.Second {
		t.Fatalf("configuration = %#v", configuration)
	}
}
