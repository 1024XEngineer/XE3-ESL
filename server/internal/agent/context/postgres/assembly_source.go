package postgres

import (
	"context"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

func (r *Repository) ListMessagesForContext(
	ctx context.Context,
	ownerID string,
	threadID string,
	minSequenceExclusive int64,
	maxSequence int64,
	characterBudget int,
) ([]conversation.Message, int, error) {
	// Every committed message contains at least one Unicode character, so a
	// character budget also bounds the maximum row count read from the index.
	// conversation.Thread sequence numbers are contiguous and never reused, which makes the
	// omitted count derivable without counting the full history.
	rows, err := r.database.Query(ctx, `
WITH recent AS (
    SELECT
        id,
        owner_user_id,
        thread_id,
        sequence_no,
        role,
        client_message_id,
        produced_by_run_id,
        modality,
        content,
        created_at
    FROM agent_messages
    WHERE owner_user_id = $1
      AND thread_id = $2
      AND sequence_no > $3
      AND sequence_no <= $4
    ORDER BY sequence_no DESC
    LIMIT $5
),
eligible AS (
    SELECT
        recent.*,
        SUM(char_length(content)) OVER (
            ORDER BY sequence_no DESC
            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS cumulative_characters
    FROM recent
),
selected AS (
    SELECT *
    FROM eligible
    WHERE cumulative_characters <= $5
)
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
FROM selected
ORDER BY sequence_no ASC`,
		ownerID,
		threadID,
		minSequenceExclusive,
		maxSequence,
		characterBudget,
	)
	if err != nil {
		return nil, 0, agentcontext.ErrRepository
	}
	defer rows.Close()

	result := make([]conversation.Message, 0)
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
			return nil, 0, agentcontext.ErrRepository
		}
		item.Role = conversation.MessageRole(role)
		item.Modality = conversation.MessageModality(modality)
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, 0, agentcontext.ErrRepository
	}
	return result, int(maxSequence) - len(result), nil
}
