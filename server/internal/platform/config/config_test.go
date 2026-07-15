package config

import "testing"

func TestLoadUsesEnvironment(t *testing.T) {
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := Load()
	if cfg.Address() != "127.0.0.1:9000" || cfg.LogLevel != "debug" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}
