package postgres

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) FindMessage(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
) (conversation.Message, error) {
	return findMessage(ctx, r.database, ownerID, threadID, messageID)
}

func (r *Repository) FindOwnedMessage(
	ctx context.Context,
	ownerID string,
	messageID string,
) (conversation.Message, error) {
	return findOwnedMessage(ctx, r.database, ownerID, messageID)
}

type messageRowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findMessage(
	ctx context.Context,
	queryer messageRowQueryer,
	ownerID string,
	threadID string,
	messageID string,
) (conversation.Message, error) {
	return scanMessage(queryer.QueryRow(ctx, `
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
  AND thread.deleted_at IS NULL`, messageID, ownerID, threadID))
}

func findOwnedMessage(
	ctx context.Context,
	queryer messageRowQueryer,
	ownerID string,
	messageID string,
) (conversation.Message, error) {
	return scanMessage(queryer.QueryRow(ctx, `
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
  AND thread.deleted_at IS NULL`, messageID, ownerID))
}

func scanMessage(row pgx.Row) (conversation.Message, error) {
	var result conversation.Message
	var role string
	var modality string
	err := row.Scan(
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
		return conversation.Message{}, mapConversationPostgresError(err)
	}
	result.Role = conversation.MessageRole(role)
	result.Modality = conversation.MessageModality(modality)
	return result, nil
}
