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

func (r *Repository) FindSummary(
	ctx context.Context,
	ownerID string,
	threadID string,
	maxSequence int64,
) (agentsummary.State, error) {
	if ctx == nil || !conversation.ValidUUID(ownerID) ||
		!conversation.ValidUUID(threadID) || maxSequence < 1 {
		return agentsummary.State{}, conversation.ErrInvalidRequest
	}
	var result agentsummary.State
	var content []byte
	err := r.database.QueryRow(ctx, `
SELECT thread.user_id::text, thread.id::text,
       thread.summary_through_sequence, thread.summary_content
FROM agent_threads AS thread
WHERE thread.id = $1 AND thread.user_id = $2
  AND thread.deleted_at IS NULL
  AND thread.summary_content IS NOT NULL
  AND thread.summary_through_sequence <= $3`,
		threadID, ownerID, maxSequence,
	).Scan(
		&result.OwnerID,
		&result.ThreadID,
		&result.ThroughSequence,
		&content,
	)
	if err != nil {
		return agentsummary.State{}, mapSummaryPostgresError(err)
	}
	if err := json.Unmarshal(content, &result.Content); err != nil || !result.Valid() {
		return agentsummary.State{}, conversation.ErrRepository
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
