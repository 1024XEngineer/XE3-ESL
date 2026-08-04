package postgres

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
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

func (r *Repository) FindThread(
	ctx context.Context,
	ownerID string,
	threadID string,
) (conversation.Thread, error) {
	var result conversation.Thread
	err := r.database.QueryRow(ctx, `
SELECT
    threads.id::text,
    threads.owner_user_id::text,
    COALESCE(first_user.content, ''),
    COALESCE(active_link.goal_id::text, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM agent_threads AS threads
LEFT JOIN agent_thread_goal_links AS active_link
  ON active_link.thread_id = threads.id
 AND active_link.owner_user_id = threads.owner_user_id
 AND active_link.is_active
LEFT JOIN LATERAL (
    SELECT messages.content
    FROM agent_messages AS messages
    WHERE messages.owner_user_id = threads.owner_user_id
      AND messages.thread_id = threads.id
      AND messages.role = 'user'
    ORDER BY messages.sequence_no
    LIMIT 1
) AS first_user ON true
WHERE threads.id = $1 AND threads.owner_user_id = $2`,
		threadID,
		ownerID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.Title,
		&result.ActiveGoalID,
		&result.NextMessageSeq,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return conversation.Thread{}, mapSourcePostgresError(err)
	}
	result.Title = conversation.DeriveThreadTitle(result.Title)
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
		return conversation.Message{}, mapSourcePostgresError(err)
	}
	result.Role = conversation.MessageRole(role)
	result.Modality = conversation.MessageModality(modality)
	return result, nil
}

func (r *Repository) FindLatestCheckpoint(
	ctx context.Context,
	ownerID string,
	threadID string,
	maxSequence int64,
) (agentsummary.Checkpoint, error) {
	var result agentsummary.Checkpoint
	var content []byte
	var sourceChecksum []byte
	err := r.database.QueryRow(ctx, `
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
	).Scan(
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
	)
	if err != nil {
		return agentsummary.Checkpoint{}, mapSourcePostgresError(err)
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
