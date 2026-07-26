package agent

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollbackTimeout = 5 * time.Second

type PostgreSQL interface {
	Begin(context.Context) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct {
	database PostgreSQL
	ids      IDGenerator
}

func NewPostgresRepository(
	database PostgreSQL,
	ids IDGenerator,
) (*PostgresRepository, error) {
	if database == nil || ids == nil {
		return nil, ErrRepository
	}
	return &PostgresRepository{database: database, ids: ids}, nil
}

func (r *PostgresRepository) CreateThread(
	ctx context.Context,
	ownerID string,
	activeMatterID string,
) (Thread, error) {
	threadID, err := r.ids.NewID()
	if err != nil {
		return Thread{}, ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Thread{}, ErrRepository
	}
	defer rollback(tx)

	if activeMatterID != "" {
		if err := lockActiveMatter(
			ctx,
			tx,
			ownerID,
			activeMatterID,
		); err != nil {
			return Thread{}, err
		}
	}
	var result Thread
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
		return Thread{}, mapPostgresError(err)
	}

	if activeMatterID != "" {
		if _, err := tx.Exec(ctx, `
INSERT INTO agent_thread_matter_links (
    owner_user_id,
    thread_id,
    matter_id,
    is_active,
    linked_at,
    updated_at
) VALUES ($1, $2, $3, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			ownerID,
			threadID,
			activeMatterID,
		); err != nil {
			return Thread{}, mapPostgresError(err)
		}
		result.ActiveMatterID = activeMatterID
	}
	if err := tx.Commit(ctx); err != nil {
		return Thread{}, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) ListThreads(
	ctx context.Context,
	ownerID string,
) ([]Thread, error) {
	rows, err := r.database.Query(ctx, `
SELECT
    threads.id::text,
    threads.owner_user_id::text,
    COALESCE(active_link.matter_id::text, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM agent_threads AS threads
LEFT JOIN agent_thread_matter_links AS active_link
  ON active_link.thread_id = threads.id
 AND active_link.owner_user_id = threads.owner_user_id
 AND active_link.is_active
WHERE threads.owner_user_id = $1
ORDER BY threads.updated_at DESC, threads.id DESC`,
		ownerID,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()

	result := make([]Thread, 0)
	for rows.Next() {
		var item Thread
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.ActiveMatterID,
			&item.NextMessageSeq,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, ErrRepository
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) FindThread(
	ctx context.Context,
	ownerID string,
	threadID string,
) (Thread, error) {
	var result Thread
	err := r.database.QueryRow(ctx, `
SELECT
    threads.id::text,
    threads.owner_user_id::text,
    COALESCE(active_link.matter_id::text, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM agent_threads AS threads
LEFT JOIN agent_thread_matter_links AS active_link
  ON active_link.thread_id = threads.id
 AND active_link.owner_user_id = threads.owner_user_id
 AND active_link.is_active
WHERE threads.id = $1 AND threads.owner_user_id = $2`,
		threadID,
		ownerID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ActiveMatterID,
		&result.NextMessageSeq,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return Thread{}, mapPostgresError(err)
	}
	return result, nil
}

func (r *PostgresRepository) SetActiveMatter(
	ctx context.Context,
	ownerID string,
	threadID string,
	matterID string,
) (ThreadMatterLink, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return ThreadMatterLink{}, ErrRepository
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
		return ThreadMatterLink{}, mapPostgresError(err)
	}
	if err := lockActiveMatter(ctx, tx, ownerID, matterID); err != nil {
		return ThreadMatterLink{}, err
	}

	var current ThreadMatterLink
	err = tx.QueryRow(ctx, `
SELECT
    owner_user_id::text,
    thread_id::text,
    matter_id::text,
    is_active,
    linked_at,
    updated_at
FROM agent_thread_matter_links
WHERE thread_id = $1
  AND owner_user_id = $2
  AND is_active`,
		threadID,
		ownerID,
	).Scan(
		&current.OwnerID,
		&current.ThreadID,
		&current.MatterID,
		&current.Active,
		&current.LinkedAt,
		&current.UpdatedAt,
	)
	if err == nil && current.MatterID == matterID {
		if err := tx.Commit(ctx); err != nil {
			return ThreadMatterLink{}, ErrRepository
		}
		return current, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ThreadMatterLink{}, mapPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_thread_matter_links
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
		return ThreadMatterLink{}, mapPostgresError(err)
	}

	var result ThreadMatterLink
	if err := tx.QueryRow(ctx, `
INSERT INTO agent_thread_matter_links (
    owner_user_id,
    thread_id,
    matter_id,
    is_active,
    linked_at,
    updated_at
) VALUES ($1, $2, $3, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (thread_id, matter_id) DO UPDATE
SET
    is_active = true,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        agent_thread_matter_links.updated_at + INTERVAL '1 microsecond'
    )
WHERE agent_thread_matter_links.owner_user_id = EXCLUDED.owner_user_id
RETURNING
    owner_user_id::text,
    thread_id::text,
    matter_id::text,
    is_active,
    linked_at,
    updated_at`,
		ownerID,
		threadID,
		matterID,
	).Scan(
		&result.OwnerID,
		&result.ThreadID,
		&result.MatterID,
		&result.Active,
		&result.LinkedAt,
		&result.UpdatedAt,
	); err != nil {
		return ThreadMatterLink{}, mapPostgresError(err)
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
		return ThreadMatterLink{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ThreadMatterLink{}, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) AppendUserMessage(
	ctx context.Context,
	ownerID string,
	threadID string,
	clientMessageID string,
	content string,
) (Message, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Message{}, ErrRepository
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
		return Message{}, mapPostgresError(err)
	}

	existing, found, err := findMessageByClientID(
		ctx,
		tx,
		ownerID,
		threadID,
		clientMessageID,
	)
	if err != nil {
		return Message{}, err
	}
	if found {
		if existing.Content != content || existing.Role != MessageRoleUser {
			return Message{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return Message{}, ErrRepository
		}
		return existing, nil
	}

	messageID, err := r.ids.NewID()
	if err != nil {
		return Message{}, ErrRepository
	}
	var result Message
	var role string
	if err := tx.QueryRow(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content,
    created_at
) VALUES ($1, $2, $3, $4, 'user', $5, $6, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    client_message_id,
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
		&result.Content,
		&result.CreatedAt,
	); err != nil {
		return Message{}, mapPostgresError(err)
	}
	result.Role = MessageRole(role)
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
		return Message{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) ListMessages(
	ctx context.Context,
	ownerID string,
	threadID string,
) ([]Message, error) {
	rows, err := r.database.Query(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    COALESCE(client_message_id, ''),
    content,
    created_at
FROM agent_messages
WHERE owner_user_id = $1 AND thread_id = $2
ORDER BY sequence_no ASC`,
		ownerID,
		threadID,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()

	result := make([]Message, 0)
	for rows.Next() {
		var item Message
		var role string
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.ThreadID,
			&item.Sequence,
			&role,
			&item.ClientMessageID,
			&item.Content,
			&item.CreatedAt,
		); err != nil {
			return nil, ErrRepository
		}
		item.Role = MessageRole(role)
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return result, nil
}

func lockActiveMatter(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	matterID string,
) error {
	var active bool
	if err := tx.QueryRow(ctx, `
SELECT status = 'active'
FROM matters
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		matterID,
		ownerID,
	).Scan(&active); err != nil {
		return mapPostgresError(err)
	}
	if !active {
		return ErrConflict
	}
	return nil
}

func findMessageByClientID(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	clientMessageID string,
) (Message, bool, error) {
	var result Message
	var role string
	err := tx.QueryRow(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    client_message_id,
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
		&result.Content,
		&result.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, mapPostgresError(err)
	}
	result.Role = MessageRole(role)
	return result, true, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func mapPostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrNotFound
		case "23505":
			return ErrConflict
		case "23514":
			return ErrInvalidRequest
		}
	}
	return ErrRepository
}
