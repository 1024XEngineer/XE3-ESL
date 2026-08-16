package postgres

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
)

func (r *Repository) FindThread(
	ctx context.Context,
	ownerID string,
	threadID string,
) (conversation.Thread, error) {
	var result conversation.Thread
	err := r.database.QueryRow(ctx, `
SELECT
    threads.id::text,
    threads.user_id::text,
    COALESCE(threads.title, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM agent_threads AS threads
WHERE threads.id = $1 AND threads.user_id = $2
  AND threads.deleted_at IS NULL`,
		threadID,
		ownerID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.Title,
		&result.NextMessageSeq,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return conversation.Thread{}, mapSourcePostgresError(err)
	}
	return result, nil
}

func (r *Repository) FindMessage(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
) (conversation.Message, error) {
	var result conversation.Message
	var role string
	var modality string
	err := r.database.QueryRow(ctx, `
SELECT
    message.id::text,
    thread.user_id::text,
    message.thread_id::text,
    message.sequence_no,
    message.role,
    COALESCE(message.client_message_id, ''),
    COALESCE(message.produced_by_run_id::text, ''),
    message.modality,
    message.content,
    message.created_at
FROM agent_messages AS message
INNER JOIN agent_threads AS thread ON thread.id = message.thread_id
WHERE message.id = $1
  AND thread.user_id = $2
  AND message.thread_id = $3
  AND thread.deleted_at IS NULL`,
		messageID,
		ownerID,
		threadID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.Sequence,
		&role,
		&result.ClientMessageID,
		&result.ProducedByRunID,
		&modality,
		&result.Content,
		&result.CreatedAt,
	)
	if err != nil {
		return conversation.Message{}, mapSourcePostgresError(err)
	}
	result.Role = conversation.MessageRole(role)
	result.Modality = conversation.MessageModality(modality)
	return result, nil
}

func (r *Repository) FindSummary(
	ctx context.Context,
	ownerID string,
	threadID string,
	maxSequence int64,
) (agentsummary.State, error) {
	var result agentsummary.State
	var content []byte
	err := r.database.QueryRow(ctx, `
SELECT
	thread.user_id::text,
	thread.id::text,
	thread.summary_through_sequence,
	thread.summary_content
FROM agent_threads AS thread
WHERE thread.user_id = $1
  AND thread.id = $2
  AND thread.deleted_at IS NULL
	AND thread.summary_content IS NOT NULL
  AND thread.summary_through_sequence <= $3`,
		ownerID,
		threadID,
		maxSequence,
	).Scan(
		&result.OwnerID,
		&result.ThreadID,
		&result.ThroughSequence,
		&content,
	)
	if err != nil {
		return agentsummary.State{}, mapSourcePostgresError(err)
	}
	if err := json.Unmarshal(content, &result.Content); err != nil ||
		!result.Valid() {
		return agentsummary.State{}, conversation.ErrRepository
	}
	return result, nil
}
