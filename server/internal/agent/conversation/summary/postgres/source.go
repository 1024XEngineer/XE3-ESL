package postgres

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
)

func (r *Repository) ListMessagesForSummary(
	ctx context.Context,
	ownerID string,
	threadID string,
	fromSequence int64,
	throughSequence int64,
) ([]conversation.Message, error) {
	if ctx == nil || !conversation.ValidUUID(ownerID) ||
		!conversation.ValidUUID(threadID) || fromSequence < 1 ||
		throughSequence < fromSequence {
		return nil, conversation.ErrInvalidRequest
	}
	rows, err := r.database.Query(ctx, `
SELECT message.id::text, thread.user_id::text, message.thread_id::text,
       message.sequence_no, message.role,
       COALESCE(message.client_message_id, ''),
       COALESCE(message.produced_by_run_id::text, ''),
       message.modality, message.content, message.created_at
FROM agent_messages AS message
INNER JOIN agent_threads AS thread ON thread.id = message.thread_id
WHERE thread.user_id = $1 AND message.thread_id = $2
  AND thread.deleted_at IS NULL
  AND message.sequence_no BETWEEN $3 AND $4
ORDER BY message.sequence_no ASC
LIMIT $5`,
		ownerID,
		threadID,
		fromSequence,
		throughSequence,
		agentsummary.MaxSourceMessages+1,
	)
	if err != nil {
		return nil, conversation.ErrRepository
	}
	defer rows.Close()

	result := make([]conversation.Message, 0, agentsummary.MaxSourceMessages+1)
	for rows.Next() {
		var item conversation.Message
		var role string
		var modality string
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.ThreadID,
			&item.Sequence,
			&role,
			&item.ClientMessageID,
			&item.ProducedByRunID,
			&modality,
			&item.Content,
			&item.CreatedAt,
		); err != nil {
			return nil, conversation.ErrRepository
		}
		item.Role = conversation.MessageRole(role)
		item.Modality = conversation.MessageModality(modality)
		expectedSequence := fromSequence + int64(len(result))
		if item.Sequence != expectedSequence || item.OwnerID != ownerID ||
			item.ThreadID != threadID ||
			(item.Role != conversation.MessageRoleUser &&
				item.Role != conversation.MessageRoleAssistant) ||
			(item.Modality != conversation.MessageModalityText &&
				item.Modality != conversation.MessageModalityVoice &&
				item.Modality != conversation.MessageModalityMultimodal) ||
			!conversation.ValidMessageContent(item.Content) {
			return nil, conversation.ErrRepository
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, conversation.ErrRepository
	}
	if len(result) == 0 {
		return nil, conversation.ErrNotFound
	}
	return result, nil
}
