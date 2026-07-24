package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseConfigRequiresURL(t *testing.T) {
	for _, databaseURL := range []string{"", " \t\n "} {
		t.Run(databaseURL, func(t *testing.T) {
			_, err := parseConfig(databaseURL)
			if !errors.Is(err, ErrURLRequired) {
				t.Fatalf("expected ErrURLRequired, got %v", err)
			}
		})
	}
}

func TestParseConfigRejectsInvalidURLWithoutLeakingIt(t *testing.T) {
	const databaseURL = "postgres://speakup:do-not-leak@%zz/speakup"

	_, err := parseConfig(databaseURL)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
	if strings.Contains(err.Error(), databaseURL) ||
		strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("error leaked database credentials: %v", err)
	}
}

func TestParseConfigAppliesBoundedPoolDefaults(t *testing.T) {
	cfg, err := parseConfig("postgres://speakup:secret@127.0.0.1:5432/speakup")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if cfg.MaxConns != maxConnections {
		t.Fatalf("expected MaxConns %d, got %d", maxConnections, cfg.MaxConns)
	}
	if cfg.MinConns != minConnections {
		t.Fatalf("expected MinConns %d, got %d", minConnections, cfg.MinConns)
	}
	if cfg.MaxConnLifetime != maxConnLifetime {
		t.Fatalf("expected MaxConnLifetime %s, got %s", maxConnLifetime, cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != maxConnIdleTime {
		t.Fatalf("expected MaxConnIdleTime %s, got %s", maxConnIdleTime, cfg.MaxConnIdleTime)
	}
	if cfg.HealthCheckPeriod != healthCheckPeriod {
		t.Fatalf("expected HealthCheckPeriod %s, got %s", healthCheckPeriod, cfg.HealthCheckPeriod)
	}
	if cfg.ConnConfig.ConnectTimeout != startupTimeout {
		t.Fatalf("expected ConnectTimeout %s, got %s", startupTimeout, cfg.ConnConfig.ConnectTimeout)
	}
}

func TestOpenHonorsCanceledContextWithoutLeakingURL(t *testing.T) {
	const databaseURL = "postgres://speakup:do-not-leak@127.0.0.1:5432/speakup"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Open(ctx, databaseURL)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), databaseURL) ||
		strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("error leaked database credentials: %v", err)
	}
}

func TestNilPoolReadinessIsUnavailable(t *testing.T) {
	var pool *Pool
	if err := pool.Ping(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}

	pool.Close()
	if native := pool.Native(); native != nil {
		t.Fatalf("expected nil native pool, got %v", native)
	}
}

func TestOpenIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	if pool.Native() == nil {
		t.Fatal("expected native pgx pool")
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
}
