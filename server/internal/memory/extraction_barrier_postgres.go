package memory

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

func (repository *PostgresRepository) ReadExtractionBarrier(
	ctx context.Context,
	request ExtractionBarrierRequest,
) (ExtractionBarrierSnapshot, error) {
	if ctx == nil || !request.Valid() {
		return ExtractionBarrierSnapshot{}, ErrInvalidArgument
	}
	snapshot := ExtractionBarrierSnapshot{Cutoff: request.Cutoff}
	var latestSourceCompletedAt pgtype.Timestamptz
	var earliestNonTerminalSourceCompletedAt pgtype.Timestamptz
	err := repository.database.QueryRow(ctx, `
SELECT
    count(*),
    count(*) FILTER (WHERE status = 'pending'),
    count(*) FILTER (WHERE status = 'running'),
    count(*) FILTER (WHERE status = 'completed'),
    count(*) FILTER (WHERE status = 'failed'),
    count(*) FILTER (WHERE status = 'discarded'),
    max(source_completed_at),
    min(source_completed_at) FILTER (
        WHERE status IN ('pending', 'running')
    )
FROM agent_memory_extraction_jobs
WHERE owner_user_id = $1
  AND source_completed_at <= $2`,
		request.Actor.UserID,
		request.Cutoff,
	).Scan(
		&snapshot.JobCount,
		&snapshot.PendingCount,
		&snapshot.RunningCount,
		&snapshot.CompletedCount,
		&snapshot.FailedCount,
		&snapshot.DiscardedCount,
		&latestSourceCompletedAt,
		&earliestNonTerminalSourceCompletedAt,
	)
	if err != nil {
		return ExtractionBarrierSnapshot{}, ErrRepository
	}
	if latestSourceCompletedAt.Valid {
		snapshot.LatestSourceCompletedAt =
			latestSourceCompletedAt.Time.UTC()
	}
	if earliestNonTerminalSourceCompletedAt.Valid {
		snapshot.EarliestNonTerminalSourceCompletedAt =
			earliestNonTerminalSourceCompletedAt.Time.UTC()
	}
	if !snapshot.Valid() {
		return ExtractionBarrierSnapshot{}, ErrRepository
	}
	return snapshot, nil
}

var _ ExtractionBarrierReader = (*PostgresRepository)(nil)
