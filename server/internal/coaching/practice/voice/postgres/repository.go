// Package postgres implements Practice Voice's production PostgreSQL adapters.
package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
)

// Repository owns the durable Voice transaction boundary while reusing the
// authoritative Practice Session repository for Session and retry access.
type Repository struct {
	*practicepostgres.Repository
	pool               *pgxpool.Pool
	now                func() time.Time
	afterWriteFence    func()
	afterRecordingLock func()
}

func New(pool *pgxpool.Pool) (*Repository, error) {
	practiceRepository, err := practicepostgres.New(pool)
	if err != nil {
		return nil, err
	}
	return &Repository{
		Repository: practiceRepository,
		pool:       pool,
		now:        time.Now,
	}, nil
}
