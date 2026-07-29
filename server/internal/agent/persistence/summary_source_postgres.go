package persistence

import (
	"context"
	"unicode/utf8"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
)

func (r *PostgresRepository) ListMessagesForSummary(
	ctx context.Context,
	ownerID string,
	threadID string,
	sourceFromSequence int64,
	coveredThroughSequence int64,
) ([]Message, error) {
	if ctx == nil ||
		!ValidUUID(ownerID) ||
		!ValidUUID(threadID) ||
		sourceFromSequence < 1 ||
		coveredThroughSequence < sourceFromSequence ||
		coveredThroughSequence-sourceFromSequence+1 >
			MaxSummarySourceMessages {
		return nil, ErrInvalidRequest
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
		return nil, ErrRepository
	}
	defer rows.Close()

	expectedSequence := sourceFromSequence
	usedRunes := 0
	result := make(
		[]Message,
		0,
		coveredThroughSequence-sourceFromSequence+1,
	)
	for rows.Next() {
		var item Message
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
			return nil, ErrRepository
		}
		item.Role = MessageRole(role)
		item.Modality = MessageModality(modality)
		if item.Sequence != expectedSequence ||
			item.OwnerID != ownerID ||
			item.ThreadID != threadID ||
			(item.Role != MessageRoleUser &&
				item.Role != MessageRoleAssistant) ||
			(item.Modality != MessageModalityText &&
				item.Modality != MessageModalityVoice) ||
			!ValidMessageContent(item.Content) {
			return nil, ErrRepository
		}
		usedRunes += utf8.RuneCountInString(item.Content)
		if usedRunes > MaxSummarySourceRunes {
			return nil, ErrInvalidRequest
		}
		result = append(result, item)
		expectedSequence++
	}
	if err := rows.Err(); err != nil {
		return nil, ErrRepository
	}
	if len(result) == 0 ||
		expectedSequence != coveredThroughSequence+1 {
		return nil, ErrNotFound
	}
	return result, nil
}
