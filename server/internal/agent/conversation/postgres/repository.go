package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
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
	if err := lockActiveConversationOwner(ctx, tx, ownerID); err != nil {
		return conversation.Thread{}, err
	}
	var result conversation.Thread
	if err := tx.QueryRow(ctx, `
INSERT INTO agent_threads (
    id,
    user_id,
    next_message_sequence,
    created_at,
    updated_at
) VALUES ($1, $2, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    user_id::text,
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
    threads.user_id::text,
    COALESCE(threads.title, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM agent_threads AS threads
WHERE threads.user_id = $1
  AND threads.deleted_at IS NULL
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
			&item.NextMessageSeq,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, conversation.ErrRepository
		}
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
    threads.user_id::text,
    COALESCE(threads.title, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM agent_threads AS threads
WHERE threads.user_id = $1
  AND threads.deleted_at IS NULL
ORDER BY threads.updated_at DESC, threads.id DESC
LIMIT $2`
	arguments := []any{ownerID, limit}
	if before != nil {
		query = `
SELECT
    threads.id::text,
    threads.user_id::text,
    COALESCE(threads.title, ''),
    threads.next_message_sequence,
    threads.created_at,
    threads.updated_at
FROM agent_threads AS threads
WHERE threads.user_id = $1
  AND threads.deleted_at IS NULL
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
			&item.NextMessageSeq,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, conversation.ErrRepository
		}
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
		return conversation.Thread{}, mapConversationPostgresError(err)
	}
	return result, nil
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
	if err := lockActiveConversationOwner(ctx, tx, ownerID); err != nil {
		return err
	}

	var lockedThreadID string
	if err := tx.QueryRow(ctx, `
SELECT id::text
FROM agent_threads
WHERE id = $1
  AND user_id = $2
  AND deleted_at IS NULL
FOR UPDATE`,
		threadID,
		ownerID,
	).Scan(&lockedThreadID); errors.Is(err, pgx.ErrNoRows) {
		return conversation.ErrNotFound
	} else if err != nil {
		return mapConversationPostgresError(err)
	}
	assetIDs, err := threadAssetIDs(ctx, tx, ownerID, threadID)
	if err != nil {
		return err
	}
	if err := mediapostgres.ScheduleDeletionInTransaction(
		ctx, tx, ownerID, assetIDs,
	); err != nil {
		return mapConversationMediaError(err)
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM agent_message_attachments AS attachment
USING agent_messages AS message
WHERE attachment.message_id = message.id
  AND message.thread_id = $1`, threadID); err != nil {
		return conversation.ErrRepository
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM agent_voice_drafts
WHERE thread_id = $1`, threadID); err != nil {
		return conversation.ErrRepository
	}

	tag, err := tx.Exec(ctx, `
UPDATE agent_threads
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND user_id = $2
  AND deleted_at IS NULL`,
		threadID,
		ownerID,
	)
	if err != nil {
		return mapConversationPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return conversation.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.ErrRepository
	}
	return nil
}

func threadAssetIDs(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT attachment.asset_id::text
FROM agent_message_attachments AS attachment
JOIN agent_messages AS message ON message.id = attachment.message_id
JOIN media_assets AS asset ON asset.id = attachment.asset_id
WHERE message.thread_id = $1 AND asset.user_id = $2
UNION
SELECT draft.id::text
FROM agent_voice_drafts AS draft
JOIN media_assets AS asset ON asset.id = draft.id
WHERE draft.thread_id = $1 AND asset.user_id = $2
ORDER BY 1`, threadID, ownerID)
	if err != nil {
		return nil, conversation.ErrRepository
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return nil, conversation.ErrRepository
		}
		result = append(result, assetID)
	}
	if rows.Err() != nil {
		return nil, conversation.ErrRepository
	}
	return result, nil
}

func mapConversationMediaError(err error) error {
	switch {
	case errors.Is(err, sharedmedia.ErrNotFound):
		return conversation.ErrNotFound
	case errors.Is(err, sharedmedia.ErrConflict):
		return conversation.ErrConflict
	default:
		return conversation.ErrRepository
	}
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
	if err := lockActiveConversationOwner(ctx, tx, ownerID); err != nil {
		return conversation.Message{}, err
	}

	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
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
    thread_id,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at
) VALUES ($1, $2, $3, 'user', $4, 'text', $5, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    thread_id::text,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at`,
		messageID,
		threadID,
		nextSequence,
		clientMessageID,
		content,
	).Scan(
		&result.ID,
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
	result.OwnerID = ownerID
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
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
SELECT `+agentMessageWithAttachmentSelectColumns+`
FROM agent_messages AS message
INNER JOIN agent_threads AS thread ON thread.id = message.thread_id
`+audioAttachmentJoin+`
WHERE thread.user_id = $1 AND message.thread_id = $2
  AND thread.deleted_at IS NULL
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
		item, err := scanMessageWithAttachment(rows)
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
SELECT ` + agentMessageWithAttachmentSelectColumns + `
FROM agent_messages AS message
INNER JOIN agent_threads AS thread ON thread.id = message.thread_id
` + audioAttachmentJoin + `
WHERE thread.user_id = $1 AND message.thread_id = $2
  AND thread.deleted_at IS NULL
ORDER BY message.sequence_no DESC
LIMIT $3`
	arguments := []any{ownerID, threadID, limit}
	if before != nil {
		query = `
SELECT ` + agentMessageWithAttachmentSelectColumns + `
FROM agent_messages AS message
INNER JOIN agent_threads AS thread ON thread.id = message.thread_id
` + audioAttachmentJoin + `
WHERE thread.user_id = $1
  AND message.thread_id = $2
  AND thread.deleted_at IS NULL
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
		item, err := scanMessageWithAttachment(rows)
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
    message.id::text,
    thread.user_id::text,
    message.thread_id::text,
    message.sequence_no,
    message.role,
    message.client_message_id,
    message.modality,
    message.content,
    message.created_at
FROM agent_messages AS message
INNER JOIN agent_threads AS thread ON thread.id = message.thread_id
WHERE thread.user_id = $1
  AND message.thread_id = $2
  AND message.client_message_id = $3
  AND thread.deleted_at IS NULL`,
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

func lockActiveConversationOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
) error {
	var status string
	if err := tx.QueryRow(ctx, `
SELECT status
FROM users
WHERE id = $1
FOR SHARE`, ownerID).Scan(&status); err != nil {
		return mapConversationPostgresError(err)
	}
	if status != "active" {
		return conversation.ErrNotFound
	}
	return nil
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
