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
	var result conversation.Message
	var role string
	var modality string
	err := queryer.QueryRow(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    COALESCE(client_message_id, ''),
    COALESCE(produced_by_run_id::text, ''),
    modality,
    content,
    created_at
FROM agent_messages
WHERE id = $1
  AND owner_user_id = $2
  AND thread_id = $3`,
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
		return conversation.Message{}, mapConversationPostgresError(err)
	}
	result.Role = conversation.MessageRole(role)
	result.Modality = conversation.MessageModality(modality)
	return result, nil
}
