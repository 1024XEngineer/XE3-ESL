package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func (r *Repository) CreateCheckpoint(
	ctx context.Context,
	command agentsummary.CreateCheckpointCommand,
) (agentsummary.Checkpoint, error) {
	if ctx == nil || !command.Valid() {
		return agentsummary.Checkpoint{}, conversation.ErrInvalidRequest
	}
	checkpointID, err := r.ids.NewID()
	if err != nil {
		return agentsummary.Checkpoint{}, conversation.ErrRepository
	}
	content, err := json.Marshal(command.Content)
	if err != nil {
		return agentsummary.Checkpoint{}, conversation.ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentsummary.Checkpoint{}, conversation.ErrRepository
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
		return agentsummary.Checkpoint{}, mapSummaryPostgresError(err)
	}
	if command.CoveredThroughSequence >= nextMessageSequence {
		return agentsummary.Checkpoint{}, conversation.ErrInvalidRequest
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
			return agentsummary.Checkpoint{}, conversation.ErrConflict
		}
	case err != nil:
		return agentsummary.Checkpoint{}, mapSummaryPostgresError(err)
	default:
		if command.PreviousCheckpointID != latestID ||
			command.SourceFromSequence != latestCoveredThrough+1 ||
			command.CoveredThroughSequence <= latestCoveredThrough {
			return agentsummary.Checkpoint{}, conversation.ErrConflict
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
		return agentsummary.Checkpoint{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentsummary.Checkpoint{}, conversation.ErrRepository
	}
	return result, nil
}

func (r *Repository) FindLatestCheckpoint(
	ctx context.Context,
	ownerID string,
	threadID string,
	maxSequence int64,
) (agentsummary.Checkpoint, error) {
	if ctx == nil || !conversation.ValidUUID(ownerID) || !conversation.ValidUUID(threadID) ||
		maxSequence < 1 {
		return agentsummary.Checkpoint{}, conversation.ErrInvalidRequest
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
) (agentsummary.Checkpoint, error) {
	var result agentsummary.Checkpoint
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
		return agentsummary.Checkpoint{}, mapSummaryPostgresError(err)
	}
	if len(sourceChecksum) != len(result.SourceChecksum) {
		return agentsummary.Checkpoint{}, conversation.ErrRepository
	}
	copy(result.SourceChecksum[:], sourceChecksum)
	if err := json.Unmarshal(content, &result.Content); err != nil ||
		!result.Valid() {
		return agentsummary.Checkpoint{}, conversation.ErrRepository
	}
	return result, nil
}

func mapSummaryPostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return conversation.ErrNotFound
		case "23505":
			return conversation.ErrConflict
		case "23514":
			return conversation.ErrInvalidRequest
		}
	}
	return conversation.ErrRepository
}
