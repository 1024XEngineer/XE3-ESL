package store

import (
	"context"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
)

func (r *PostgresStore) ListMessagesForSummary(
	ctx context.Context,
	ownerID string,
	threadID string,
	sourceFromSequence int64,
	coveredThroughSequence int64,
) ([]conversation.Message, error) {
	if ctx == nil ||
		!conversation.ValidUUID(ownerID) ||
		!conversation.ValidUUID(threadID) {
		return nil, conversation.ErrInvalidRequest
	}
	sourceMessageCount, validRange := summarySourceMessageCount(
		sourceFromSequence,
		coveredThroughSequence,
	)
	if !validRange {
		return nil, conversation.ErrInvalidRequest
	}
	rows, err := r.database.Query(ctx, `
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
WHERE owner_user_id = $1
  AND thread_id = $2
  AND sequence_no BETWEEN $3 AND $4
ORDER BY sequence_no ASC`,
		ownerID,
		threadID,
		sourceFromSequence,
		coveredThroughSequence,
	)
	if err != nil {
		return nil, conversation.ErrRepository
	}
	defer rows.Close()

	usedRunes := 0
	result := make([]conversation.Message, 0, sourceMessageCount)
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
		if len(result) >= sourceMessageCount {
			return nil, conversation.ErrRepository
		}
		expectedSequence := sourceFromSequence + int64(len(result))
		if item.Sequence != expectedSequence ||
			item.OwnerID != ownerID ||
			item.ThreadID != threadID ||
			(item.Role != conversation.MessageRoleUser &&
				item.Role != conversation.MessageRoleAssistant) ||
			(item.Modality != conversation.MessageModalityText &&
				item.Modality != conversation.MessageModalityVoice &&
				item.Modality != conversation.MessageModalityMultimodal) ||
			!conversation.ValidMessageContent(item.Content) {
			return nil, conversation.ErrRepository
		}
		usedRunes += utf8.RuneCountInString(item.Content)
		if usedRunes > agentsummary.MaxSourceRunes {
			return nil, conversation.ErrInvalidRequest
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, conversation.ErrRepository
	}
	if len(result) != sourceMessageCount {
		return nil, conversation.ErrNotFound
	}
	return result, nil
}

func summarySourceMessageCount(
	sourceFromSequence int64,
	coveredThroughSequence int64,
) (int, bool) {
	if sourceFromSequence < 1 ||
		coveredThroughSequence < sourceFromSequence {
		return 0, false
	}
	sequenceSpan := coveredThroughSequence - sourceFromSequence
	if sequenceSpan >= int64(agentsummary.MaxSourceMessages) {
		return 0, false
	}
	return int(sequenceSpan) + 1, true
}

var _ agentsummary.Repository = (*PostgresStore)(nil)
