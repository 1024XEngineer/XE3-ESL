// Package postgres implements Practice Interaction's production PostgreSQL adapters.
package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
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
	ids                practice.PracticeResourceIDGenerator
}

func New(
	pool *pgxpool.Pool,
	completion practicepostgres.CompletionScheduler,
	turnFeedback practicepostgres.TurnFeedbackScheduler,
	ids practice.PracticeResourceIDGenerator,
) (*Repository, error) {
	practiceRepository, err := practicepostgres.New(
		pool,
		completion,
		turnFeedback,
		ids,
	)
	if err != nil {
		return nil, err
	}
	return &Repository{
		Repository: practiceRepository,
		pool:       pool,
		now:        time.Now,
		ids:        ids,
	}, nil
}
