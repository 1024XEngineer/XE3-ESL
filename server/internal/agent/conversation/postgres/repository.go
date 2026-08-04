package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollbackTimeout = 5 * time.Second

type database interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct {
	database database
	ids      idGenerator
}

type idGenerator interface {
	NewID() (string, error)
}

type rowScanner interface {
	Scan(...any) error
}

func New(
	database database,
	ids idGenerator,
) (*Repository, error) {
	if database == nil || ids == nil {
		return nil, conversation.ErrRepository
	}
	return &Repository{database: database, ids: ids}, nil
}

func (r *Repository) CreateThread(
	ctx context.Context,
	ownerID string,
	activeGoalID string,
) (conversation.Thread, error) {
	threadID, err := r.ids.NewID()
	if err != nil {
		return conversation.Thread{}, conversation.ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return conversation.Thread{}, conversation.ErrRepository
	}
	defer rollback(tx)

	if activeGoalID != "" {
		if err := lockActiveGoal(
			ctx,
			tx,
			ownerID,
			activeGoalID,
		); err != nil {
			return conversation.Thread{}, err
		}
	}
	var result conversation.Thread
	if err := tx.QueryRow(ctx, `
INSERT INTO agent_threads (
    id,
    owner_user_id,
    next_message_sequence,
    created_at,
    updated_at
) VALUES ($1, $2, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    owner_user_id::text,
    next_message_sequence,
    created_at,
    updated_at`,
		threadID,
		ownerID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.NextMessageSeq,
		&result.CreatedAt,
		&result.UpdatedAt,
	); err != nil {
		return conversation.Thread{}, mapConversationPostgresError(err)
	}

	if activeGoalID != "" {
		if _, err := tx.Exec(ctx, `
INSERT INTO agent_thread_goal_links (
    owner_user_id,
    thread_id,
    goal_id,
    is_active,
    linked_at,
    updated_at
) VALUES ($1, $2, $3, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			ownerID,
			threadID,
			activeGoalID,
		); err != nil {
			return conversation.Thread{}, mapConversationPostgresError(err)
		}
		result.ActiveGoalID = activeGoalID
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.Thread{}, conversation.ErrRepository
	}
	return result, nil
}

func (r *Repository) ListThreads(
	ctx context.Context,
	ownerID string,
) ([]conversation.Thread, error) {
	rows, err := r.database.Query(ctx, `
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
WHERE threads.owner_user_id = $1
  AND threads.sidebar_deleted_at IS NULL
ORDER BY threads.updated_at DESC, threads.id DESC`,
		ownerID,
	)
	if err != nil {
		return nil, conversation.ErrRepository
	}
	defer rows.Close()

	result := make([]conversation.Thread, 0)
	for rows.Next() {
		var item conversation.Thread
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.Title,
			&item.ActiveGoalID,
			&item.NextMessageSeq,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, conversation.ErrRepository
		}
		item.Title = conversation.DeriveThreadTitle(item.Title)
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, conversation.ErrRepository
	}
	return result, nil
}

func (r *Repository) PageThreads(
	ctx context.Context,
	ownerID string,
	limit int,
	before *conversation.ThreadPageCursor,
) ([]conversation.Thread, error) {
	query := `
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
WHERE threads.owner_user_id = $1
  AND threads.sidebar_deleted_at IS NULL
ORDER BY threads.updated_at DESC, threads.id DESC
LIMIT $2`
	arguments := []any{ownerID, limit}
	if before != nil {
		query = `
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
WHERE threads.owner_user_id = $1
  AND threads.sidebar_deleted_at IS NULL
  AND (threads.updated_at, threads.id) < ($2, $3)
ORDER BY threads.updated_at DESC, threads.id DESC
LIMIT $4`
		arguments = []any{
			ownerID,
			before.UpdatedAt,
			before.ThreadID,
			limit,
		}
	}
	rows, err := r.database.Query(ctx, query, arguments...)
	if err != nil {
		return nil, conversation.ErrRepository
	}
	defer rows.Close()

	result := make([]conversation.Thread, 0, limit)
	for rows.Next() {
		var item conversation.Thread
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.Title,
			&item.ActiveGoalID,
			&item.NextMessageSeq,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, conversation.ErrRepository
		}
		item.Title = conversation.DeriveThreadTitle(item.Title)
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, conversation.ErrRepository
	}
	return result, nil
}

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
		return conversation.Thread{}, mapConversationPostgresError(err)
	}
	result.Title = conversation.DeriveThreadTitle(result.Title)
	return result, nil
}

func (r *Repository) FindFocusedThread(
	ctx context.Context,
	ownerID string,
) (conversation.Thread, bool, error) {
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
FROM agent_thread_focuses AS focus
JOIN agent_threads AS threads
  ON threads.id = focus.thread_id
 AND threads.owner_user_id = focus.owner_user_id
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
WHERE focus.owner_user_id = $1`,
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
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.Thread{}, false, nil
	}
	if err != nil {
		return conversation.Thread{}, false, mapConversationPostgresError(err)
	}
	result.Title = conversation.DeriveThreadTitle(result.Title)
	return result, true, nil
}

func (r *Repository) SetFocusedThread(
	ctx context.Context,
	ownerID string,
	threadID string,
) (conversation.Thread, error) {
	var result conversation.Thread
	err := r.database.QueryRow(ctx, `
WITH owned_thread AS (
    SELECT id, owner_user_id
    FROM agent_threads
    WHERE id = $1 AND owner_user_id = $2
),
selected AS (
    INSERT INTO agent_thread_focuses (
        owner_user_id,
        thread_id,
        updated_at
    )
    SELECT owner_user_id, id, CURRENT_TIMESTAMP
    FROM owned_thread
    ON CONFLICT (owner_user_id) DO UPDATE
    SET
        thread_id = EXCLUDED.thread_id,
        updated_at = CASE
            WHEN agent_thread_focuses.thread_id = EXCLUDED.thread_id
                THEN agent_thread_focuses.updated_at
            ELSE GREATEST(
                CURRENT_TIMESTAMP,
                agent_thread_focuses.updated_at + INTERVAL '1 microsecond'
            )
        END
    RETURNING owner_user_id, thread_id
)
SELECT
    threads.id::text,
    threads.owner_user_id::text,
    COALESCE(first_user.content, ''),
    COALESCE(active_link.goal_id::text, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM selected
JOIN agent_threads AS threads
  ON threads.id = selected.thread_id
 AND threads.owner_user_id = selected.owner_user_id
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
) AS first_user ON true`,
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
		return conversation.Thread{}, mapConversationPostgresError(err)
	}
	result.Title = conversation.DeriveThreadTitle(result.Title)
	return result, nil
}

func (r *Repository) ClearFocusedThread(
	ctx context.Context,
	ownerID string,
) error {
	var deleted int64
	if err := r.database.QueryRow(ctx, `
WITH deleted AS (
    DELETE FROM agent_thread_focuses
    WHERE owner_user_id = $1
    RETURNING 1
)
SELECT COUNT(*) FROM deleted`,
		ownerID,
	).Scan(&deleted); err != nil {
		return mapConversationPostgresError(err)
	}
	return nil
}

func (r *Repository) DeleteThread(
	ctx context.Context,
	ownerID string,
	threadID string,
) error {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return conversation.ErrRepository
	}
	defer rollback(tx)

	var lockedThreadID string
	if err := tx.QueryRow(ctx, `
SELECT id::text
FROM agent_threads
WHERE id = $1
  AND owner_user_id = $2
  AND sidebar_deleted_at IS NULL
FOR UPDATE`,
		threadID,
		ownerID,
	).Scan(&lockedThreadID); errors.Is(err, pgx.ErrNoRows) {
		return conversation.ErrNotFound
	} else if err != nil {
		return mapConversationPostgresError(err)
	}

	tag, err := tx.Exec(ctx, `
UPDATE agent_threads
SET sidebar_deleted_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND owner_user_id = $2
  AND sidebar_deleted_at IS NULL`,
		threadID,
		ownerID,
	)
	if err != nil {
		return mapConversationPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return conversation.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM agent_thread_focuses
WHERE owner_user_id = $1 AND thread_id = $2`,
		ownerID,
		threadID,
	); err != nil {
		return mapConversationPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.ErrRepository
	}
	return nil
}

func (r *Repository) SetActiveGoal(
	ctx context.Context,
	ownerID string,
	threadID string,
	goalID string,
) (conversation.ThreadGoalLink, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return conversation.ThreadGoalLink{}, conversation.ErrRepository
	}
	defer rollback(tx)

	var lockedThreadID string
	if err := tx.QueryRow(ctx, `
SELECT id::text
FROM agent_threads
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		threadID,
		ownerID,
	).Scan(&lockedThreadID); err != nil {
		return conversation.ThreadGoalLink{}, mapConversationPostgresError(err)
	}
	if err := lockActiveGoal(ctx, tx, ownerID, goalID); err != nil {
		return conversation.ThreadGoalLink{}, err
	}

	var current conversation.ThreadGoalLink
	err = tx.QueryRow(ctx, `
SELECT
    owner_user_id::text,
    thread_id::text,
    goal_id::text,
    is_active,
    linked_at,
    updated_at
FROM agent_thread_goal_links
WHERE thread_id = $1
  AND owner_user_id = $2
  AND is_active`,
		threadID,
		ownerID,
	).Scan(
		&current.OwnerID,
		&current.ThreadID,
		&current.GoalID,
		&current.Active,
		&current.LinkedAt,
		&current.UpdatedAt,
	)
	if err == nil && current.GoalID == goalID {
		if err := tx.Commit(ctx); err != nil {
			return conversation.ThreadGoalLink{}, conversation.ErrRepository
		}
		return current, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return conversation.ThreadGoalLink{}, mapConversationPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_thread_goal_links
SET
    is_active = false,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE thread_id = $1
  AND owner_user_id = $2
  AND is_active`,
		threadID,
		ownerID,
	); err != nil {
		return conversation.ThreadGoalLink{}, mapConversationPostgresError(err)
	}

	var result conversation.ThreadGoalLink
	if err := tx.QueryRow(ctx, `
INSERT INTO agent_thread_goal_links (
    owner_user_id,
    thread_id,
    goal_id,
    is_active,
    linked_at,
    updated_at
) VALUES ($1, $2, $3, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (thread_id, goal_id) DO UPDATE
SET
    is_active = true,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        agent_thread_goal_links.updated_at + INTERVAL '1 microsecond'
    )
WHERE agent_thread_goal_links.owner_user_id = EXCLUDED.owner_user_id
RETURNING
    owner_user_id::text,
    thread_id::text,
    goal_id::text,
    is_active,
    linked_at,
    updated_at`,
		ownerID,
		threadID,
		goalID,
	).Scan(
		&result.OwnerID,
		&result.ThreadID,
		&result.GoalID,
		&result.Active,
		&result.LinkedAt,
		&result.UpdatedAt,
	); err != nil {
		return conversation.ThreadGoalLink{}, mapConversationPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET updated_at = GREATEST(
    CURRENT_TIMESTAMP,
    updated_at + INTERVAL '1 microsecond'
)
WHERE id = $1 AND owner_user_id = $2`,
		threadID,
		ownerID,
	); err != nil {
		return conversation.ThreadGoalLink{}, mapConversationPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.ThreadGoalLink{}, conversation.ErrRepository
	}
	return result, nil
}

func (r *Repository) AppendUserMessage(
	ctx context.Context,
	ownerID string,
	threadID string,
	clientMessageID string,
	content string,
) (conversation.Message, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return conversation.Message{}, conversation.ErrRepository
	}
	defer rollback(tx)

	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		threadID,
		ownerID,
	).Scan(&nextSequence); err != nil {
		return conversation.Message{}, mapConversationPostgresError(err)
	}

	existing, found, err := findMessageByClientID(
		ctx,
		tx,
		ownerID,
		threadID,
		clientMessageID,
	)
	if err != nil {
		return conversation.Message{}, err
	}
	if found {
		if existing.Content != content ||
			existing.Role != conversation.MessageRoleUser ||
			existing.Modality != conversation.MessageModalityText {
			return conversation.Message{}, conversation.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return conversation.Message{}, conversation.ErrRepository
		}
		return existing, nil
	}

	messageID, err := r.ids.NewID()
	if err != nil {
		return conversation.Message{}, conversation.ErrRepository
	}
	var result conversation.Message
	var role string
	var modality string
	if err := tx.QueryRow(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at
) VALUES ($1, $2, $3, $4, 'user', $5, 'text', $6, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at`,
		messageID,
		ownerID,
		threadID,
		nextSequence,
		clientMessageID,
		content,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.Sequence,
		&role,
		&result.ClientMessageID,
		&modality,
		&result.Content,
		&result.CreatedAt,
	); err != nil {
		return conversation.Message{}, mapConversationPostgresError(err)
	}
	result.Role = conversation.MessageRole(role)
	result.Modality = conversation.MessageModality(modality)
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2`,
		threadID,
		ownerID,
	); err != nil {
		return conversation.Message{}, mapConversationPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.Message{}, conversation.ErrRepository
	}
	return result, nil
}

func (r *Repository) ListMessages(
	ctx context.Context,
	ownerID string,
	threadID string,
) ([]conversation.Message, error) {
	rows, err := r.database.Query(ctx, `
SELECT `+agentMessageWithAudioSelectColumns+`
FROM agent_messages AS message
LEFT JOIN agent_message_audios AS audio
  ON audio.message_id = message.id
 AND audio.owner_user_id = message.owner_user_id
 AND audio.thread_id = message.thread_id
WHERE message.owner_user_id = $1 AND message.thread_id = $2
ORDER BY message.sequence_no ASC`,
		ownerID,
		threadID,
	)
	if err != nil {
		return nil, conversation.ErrRepository
	}
	defer rows.Close()

	result := make([]conversation.Message, 0)
	for rows.Next() {
		item, err := scanMessageWithAudio(rows)
		if err != nil {
			return nil, conversation.ErrRepository
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, conversation.ErrRepository
	}
	return result, nil
}

func (r *Repository) PageMessages(
	ctx context.Context,
	ownerID string,
	threadID string,
	limit int,
	before *conversation.MessagePageCursor,
) ([]conversation.Message, error) {
	query := `
SELECT ` + agentMessageWithAudioSelectColumns + `
FROM agent_messages AS message
LEFT JOIN agent_message_audios AS audio
  ON audio.message_id = message.id
 AND audio.owner_user_id = message.owner_user_id
 AND audio.thread_id = message.thread_id
WHERE message.owner_user_id = $1 AND message.thread_id = $2
ORDER BY message.sequence_no DESC
LIMIT $3`
	arguments := []any{ownerID, threadID, limit}
	if before != nil {
		query = `
SELECT ` + agentMessageWithAudioSelectColumns + `
FROM agent_messages AS message
LEFT JOIN agent_message_audios AS audio
  ON audio.message_id = message.id
 AND audio.owner_user_id = message.owner_user_id
 AND audio.thread_id = message.thread_id
WHERE message.owner_user_id = $1
  AND message.thread_id = $2
  AND message.sequence_no < $3
ORDER BY message.sequence_no DESC
LIMIT $4`
		arguments = []any{
			ownerID,
			threadID,
			before.BeforeSequence,
			limit,
		}
	}
	rows, err := r.database.Query(ctx, query, arguments...)
	if err != nil {
		return nil, conversation.ErrRepository
	}
	defer rows.Close()

	result := make([]conversation.Message, 0, limit)
	for rows.Next() {
		item, err := scanMessageWithAudio(rows)
		if err != nil {
			return nil, conversation.ErrRepository
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, conversation.ErrRepository
	}
	return result, nil
}

func lockActiveGoal(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	goalID string,
) error {
	var active bool
	if err := tx.QueryRow(ctx, `
SELECT status = 'active'
FROM coaching_goals
WHERE goal_id = $1 AND owner_user_id = $2
FOR UPDATE`,
		goalID,
		ownerID,
	).Scan(&active); err != nil {
		return mapConversationPostgresError(err)
	}
	if !active {
		return conversation.ErrConflict
	}
	return nil
}

func findMessageByClientID(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	clientMessageID string,
) (conversation.Message, bool, error) {
	var result conversation.Message
	var role string
	var modality string
	err := tx.QueryRow(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at
FROM agent_messages
WHERE owner_user_id = $1
  AND thread_id = $2
  AND client_message_id = $3`,
		ownerID,
		threadID,
		clientMessageID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.Sequence,
		&role,
		&result.ClientMessageID,
		&modality,
		&result.Content,
		&result.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.Message{}, false, nil
	}
	if err != nil {
		return conversation.Message{}, false, mapConversationPostgresError(err)
	}
	result.Role = conversation.MessageRole(role)
	result.Modality = conversation.MessageModality(modality)
	return result, true, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func mapConversationPostgresError(err error) error {
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

var _ conversation.Repository = (*Repository)(nil)
