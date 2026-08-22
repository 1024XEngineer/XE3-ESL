package config

import "testing"

func TestLoadUsesEnvironment(t *testing.T) {
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("METRICS_HOST", "127.0.0.2")
	t.Setenv("METRICS_PORT", "9091")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DATABASE_URL", "postgres://speakup:secret@127.0.0.1:5432/speakup")
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 2001:db8::/32")
	t.Setenv("TRUSTED_PROXY_HEADER", "x-forwarded-for")

	cfg := Load()
	if cfg.Address() != "127.0.0.1:9000" ||
		cfg.MetricsAddress() != "127.0.0.2:9091" ||
		cfg.LogLevel != "debug" ||
		cfg.DatabaseURL != "postgres://speakup:secret@127.0.0.1:5432/speakup" ||
		len(cfg.TrustedProxyCIDRs) != 2 ||
		cfg.TrustedProxyCIDRs[1] != "2001:db8::/32" ||
		cfg.TrustedProxyHeader != "x-forwarded-for" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadKeepsMetricsListenerOnLoopbackByDefault(t *testing.T) {
	t.Setenv("METRICS_HOST", "")
	t.Setenv("METRICS_PORT", "")

	if address := Load().MetricsAddress(); address != "127.0.0.1:9090" {
		t.Fatalf("metrics address = %q, want loopback default", address)
	}
}

func TestLoadLeavesDatabaseURLEmptyWhenUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if cfg := Load(); cfg.DatabaseURL != "" {
		t.Fatalf("expected empty database URL, got %q", cfg.DatabaseURL)
	}
}
