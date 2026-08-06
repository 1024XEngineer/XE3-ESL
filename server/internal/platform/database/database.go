package database

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	startupTimeout    = 5 * time.Second
	readinessTimeout  = 2 * time.Second
	maxConnections    = 10
	minConnections    = 0
	maxConnLifetime   = 30 * time.Minute
	maxConnIdleTime   = 5 * time.Minute
	healthCheckPeriod = time.Minute
)

var (
	ErrURLRequired          = errors.New("database URL is required")
	ErrInvalidConfiguration = errors.New("invalid database configuration")
	ErrUnavailable          = errors.New("database unavailable")
)

// Pool owns the process-wide PostgreSQL connection pool. Call Close during
// service shutdown. Native exposes the pool to infrastructure adapters without
// requiring business modules to parse configuration or create their own pool.
type Pool struct {
	pool *pgxpool.Pool
}

// Open validates the database URL, creates a bounded pool, and verifies the
// initial connection. Returned errors are deliberately stable and never
// include the supplied URL or a driver error that may contain credentials.
func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := parseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	pgxPool, err := pgxpool.NewWithConfig(startupCtx, cfg)
	if err != nil {
		return nil, ErrUnavailable
	}

	if err := pgxPool.Ping(startupCtx); err != nil {
		pgxPool.Close()
		return nil, ErrUnavailable
	}

	return &Pool{pool: pgxPool}, nil
}

// Ping performs a bounded readiness check. It returns only a stable,
// credential-safe error when PostgreSQL cannot be reached in time.
func (p *Pool) Ping(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return ErrUnavailable
	}

	readinessCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	if err := p.pool.Ping(readinessCtx); err != nil {
		return ErrUnavailable
	}
	return nil
}

// Close releases every connection owned by the pool.
func (p *Pool) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}

// Native returns the underlying pgx pool for PostgreSQL infrastructure
// adapters. Callers must not close it; lifecycle ownership remains with Pool.
func (p *Pool) Native() *pgxpool.Pool {
	if p == nil {
		return nil
	}
	return p.pool
}

func parseConfig(databaseURL string) (*pgxpool.Config, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, ErrURLRequired
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, ErrInvalidConfiguration
	}

	cfg.MaxConns = maxConnections
	cfg.MinConns = minConnections
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.MaxConnIdleTime = maxConnIdleTime
	cfg.HealthCheckPeriod = healthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = startupTimeout

	return cfg, nil
}
