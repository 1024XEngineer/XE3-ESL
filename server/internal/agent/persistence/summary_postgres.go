package persistence

import (
	"context"
	"encoding/json"
	"errors"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/jackc/pgx/v5"
)

const summaryCheckpointSelectColumns = `
    id::text,
    owner_user_id::text,
    thread_id::text,
    COALESCE(previous_checkpoint_id::text, ''),
    source_from_sequence,
    covered_through_sequence,
    summary_content,
    policy_version,
    prompt_version,
    provider,
    model,
    source_checksum,
    created_at`

func (r *PostgresRepository) CreateSummaryCheckpoint(
	ctx context.Context,
	command CreateThreadSummaryCheckpointCommand,
) (ThreadSummaryCheckpoint, error) {
	if ctx == nil || !command.Valid() {
		return ThreadSummaryCheckpoint{}, ErrInvalidRequest
	}
	checkpointID, err := r.ids.NewID()
	if err != nil {
		return ThreadSummaryCheckpoint{}, ErrRepository
	}
	content, err := json.Marshal(command.Content)
	if err != nil {
		return ThreadSummaryCheckpoint{}, ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return ThreadSummaryCheckpoint{}, ErrRepository
	}
	defer rollback(tx)

	var nextMessageSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		command.ThreadID,
		command.OwnerID,
	).Scan(&nextMessageSequence); err != nil {
		return ThreadSummaryCheckpoint{}, mapPostgresError(err)
	}
	if command.CoveredThroughSequence >= nextMessageSequence {
		return ThreadSummaryCheckpoint{}, ErrInvalidRequest
	}

	var latestID string
	var latestCoveredThrough int64
	err = tx.QueryRow(ctx, `
SELECT id::text, covered_through_sequence
FROM agent_thread_summary_checkpoints
WHERE owner_user_id = $1 AND thread_id = $2
ORDER BY covered_through_sequence DESC
LIMIT 1
FOR UPDATE`,
		command.OwnerID,
		command.ThreadID,
	).Scan(&latestID, &latestCoveredThrough)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if command.PreviousCheckpointID != "" ||
			command.SourceFromSequence != 1 {
			return ThreadSummaryCheckpoint{}, ErrConflict
		}
	case err != nil:
		return ThreadSummaryCheckpoint{}, mapPostgresError(err)
	default:
		if command.PreviousCheckpointID != latestID ||
			command.SourceFromSequence != latestCoveredThrough+1 ||
			command.CoveredThroughSequence <= latestCoveredThrough {
			return ThreadSummaryCheckpoint{}, ErrConflict
		}
	}

	previousCheckpointID := any(nil)
	if command.PreviousCheckpointID != "" {
		previousCheckpointID = command.PreviousCheckpointID
	}
	row := tx.QueryRow(ctx, `
INSERT INTO agent_thread_summary_checkpoints (
    id,
    owner_user_id,
    thread_id,
    previous_checkpoint_id,
    source_from_sequence,
    covered_through_sequence,
    summary_content,
    policy_version,
    prompt_version,
    provider,
    model,
    source_checksum,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, CURRENT_TIMESTAMP
)
RETURNING `+summaryCheckpointSelectColumns,
		checkpointID,
		command.OwnerID,
		command.ThreadID,
		previousCheckpointID,
		command.SourceFromSequence,
		command.CoveredThroughSequence,
		content,
		command.PolicyVersion,
		command.PromptVersion,
		command.Provider,
		command.Model,
		command.SourceChecksum[:],
	)
	result, err := scanSummaryCheckpoint(row)
	if err != nil {
		return ThreadSummaryCheckpoint{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ThreadSummaryCheckpoint{}, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) FindLatestSummaryCheckpoint(
	ctx context.Context,
	ownerID string,
	threadID string,
	maxSequence int64,
) (ThreadSummaryCheckpoint, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(threadID) ||
		maxSequence < 1 {
		return ThreadSummaryCheckpoint{}, ErrInvalidRequest
	}
	row := r.database.QueryRow(ctx, `
SELECT `+summaryCheckpointSelectColumns+`
FROM agent_thread_summary_checkpoints
WHERE owner_user_id = $1
  AND thread_id = $2
  AND covered_through_sequence <= $3
ORDER BY covered_through_sequence DESC
LIMIT 1`,
		ownerID,
		threadID,
		maxSequence,
	)
	return scanSummaryCheckpoint(row)
}

func scanSummaryCheckpoint(
	row rowScanner,
) (ThreadSummaryCheckpoint, error) {
	var result ThreadSummaryCheckpoint
	var content []byte
	var sourceChecksum []byte
	if err := row.Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.PreviousCheckpointID,
		&result.SourceFromSequence,
		&result.CoveredThroughSequence,
		&content,
		&result.PolicyVersion,
		&result.PromptVersion,
		&result.Provider,
		&result.Model,
		&sourceChecksum,
		&result.CreatedAt,
	); err != nil {
		return ThreadSummaryCheckpoint{}, mapPostgresError(err)
	}
	if len(sourceChecksum) != len(result.SourceChecksum) {
		return ThreadSummaryCheckpoint{}, ErrRepository
	}
	copy(result.SourceChecksum[:], sourceChecksum)
	if err := json.Unmarshal(content, &result.Content); err != nil ||
		!result.Valid() {
		return ThreadSummaryCheckpoint{}, ErrRepository
	}
	return result, nil
}
