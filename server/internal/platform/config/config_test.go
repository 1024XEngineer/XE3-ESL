package config

import "testing"

func TestLoadUsesEnvironment(t *testing.T) {
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DATABASE_URL", "postgres://speakup:secret@127.0.0.1:5432/speakup")

	cfg := Load()
	if cfg.Address() != "127.0.0.1:9000" ||
		cfg.LogLevel != "debug" ||
		cfg.DatabaseURL != "postgres://speakup:secret@127.0.0.1:5432/speakup" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadLeavesDatabaseURLEmptyWhenUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if cfg := Load(); cfg.DatabaseURL != "" {
		t.Fatalf("expected empty database URL, got %q", cfg.DatabaseURL)
	}
}
