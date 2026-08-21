package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const rollbackTimeout = 5 * time.Second

type database interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type IDGenerator interface {
	NewID() (string, error)
}

type rowScanner interface {
	Scan(...any) error
}

type Repository struct {
	database database
	ids      IDGenerator
	messages *conversationpostgres.Repository
}

func New(database database, ids IDGenerator) (*Repository, error) {
	if database == nil || ids == nil {
		return nil, agentvoice.ErrRepository
	}
	messages, err := conversationpostgres.New(database, ids)
	if err != nil {
		return nil, agentvoice.ErrRepository
	}
	return &Repository{database: database, ids: ids, messages: messages}, nil
}

const draftColumns = `
    draft.id::text,
    asset.user_id::text,
    draft.thread_id::text,
    asset.object_key,
    asset.content_type,
    asset.size_bytes,
    asset.checksum_sha256,
    asset.duration_ns,
    asset.sample_rate,
    asset.expires_at,
    draft.status,
    draft.asr_attempt,
    draft.version,
    draft.asr_lease_until,
    draft.asr_fencing_token,
    COALESCE(draft.asr_request_id, ''),
    COALESCE(draft.asr_provider, ''),
    COALESCE(draft.asr_model, ''),
    COALESCE(draft.transcript, ''),
    COALESCE(draft.language, ''),
    COALESCE(draft.emotion, ''),
    COALESCE(draft.finish_reason, ''),
    COALESCE(draft.failure_kind, ''),
    draft.failure_retryable,
    COALESCE(draft.confirmed_message_id::text, ''),
    COALESCE(draft.confirmed_run_id::text, ''),
    draft.created_at,
    draft.updated_at,
    draft.confirmed_at`

func (repository *Repository) FindDraft(
	ctx context.Context,
	ownerID string,
	draftID string,
) (agentvoice.Draft, error) {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(draftID) {
		return agentvoice.Draft{}, agentvoice.ErrNotFound
	}
	draft, err := scanDraft(repository.database.QueryRow(ctx, `
SELECT `+draftColumns+`
FROM agent_voice_drafts AS draft
JOIN media_assets AS asset ON asset.id = draft.id
JOIN agent_threads AS thread ON thread.id = draft.thread_id
JOIN users AS owner ON owner.id = thread.user_id
WHERE draft.id = $1
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id
  AND asset.kind = 'audio'
  AND asset.status = 'ready'
  AND thread.deleted_at IS NULL
  AND owner.status = 'active'`, draftID, ownerID))
	if err != nil {
		return agentvoice.Draft{}, mapError(err)
	}
	return draft, nil
}

func findDraftForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	draftID string,
) (agentvoice.Draft, error) {
	draft, err := scanDraft(tx.QueryRow(ctx, `
SELECT `+draftColumns+`
FROM agent_voice_drafts AS draft
JOIN media_assets AS asset ON asset.id = draft.id
JOIN agent_threads AS thread ON thread.id = draft.thread_id
WHERE draft.id = $1
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id
  AND asset.kind = 'audio'
  AND asset.status = 'ready'
  AND thread.deleted_at IS NULL
FOR UPDATE OF draft, asset`, draftID, ownerID))
	if err != nil {
		return agentvoice.Draft{}, mapError(err)
	}
	return draft, nil
}

func scanDraft(row rowScanner) (agentvoice.Draft, error) {
	var draft agentvoice.Draft
	var status string
	var expiresAt pgtype.Timestamptz
	var asrLease pgtype.Timestamptz
	var failureRetryable pgtype.Bool
	var confirmedAt pgtype.Timestamptz
	var duration int64
	if err := row.Scan(
		&draft.ID,
		&draft.OwnerID,
		&draft.ThreadID,
		&draft.ObjectKey,
		&draft.ContentType,
		&draft.Size,
		&draft.ChecksumSHA256,
		&duration,
		&draft.SampleRate,
		&expiresAt,
		&status,
		&draft.ASRAttempt,
		&draft.Version,
		&asrLease,
		&draft.ASRFencingToken,
		&draft.ASRRequestID,
		&draft.ASRProvider,
		&draft.ASRModel,
		&draft.Transcript,
		&draft.ASRLanguage,
		&draft.ASREmotion,
		&draft.ASRFinishReason,
		&draft.FailureKind,
		&failureRetryable,
		&draft.ConfirmedMessageID,
		&draft.ConfirmedRunID,
		&draft.CreatedAt,
		&draft.UpdatedAt,
		&confirmedAt,
	); err != nil {
		return agentvoice.Draft{}, err
	}
	draft.Status = agentvoice.DraftStatus(status)
	draft.Duration = time.Duration(duration)
	if expiresAt.Valid {
		draft.ExpiresAt = expiresAt.Time
	}
	if asrLease.Valid {
		draft.ASRLeaseUntil = asrLease.Time
	}
	if failureRetryable.Valid {
		draft.FailureRetryable = failureRetryable.Bool
	}
	if confirmedAt.Valid {
		draft.ConfirmedAt = confirmedAt.Time
	}
	return draft, nil
}

func (repository *Repository) FindAudioAttachment(
	ctx context.Context,
	ownerID string,
	audioID string,
) (conversation.AudioAttachment, error) {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(audioID) {
		return conversation.AudioAttachment{}, agentvoice.ErrNotFound
	}
	return scanAudioAttachment(repository.database.QueryRow(ctx, `
SELECT
    asset.id::text,
    message.id::text,
    asset.content_type,
    asset.size_bytes,
    asset.duration_ns,
    attachment.created_at
FROM agent_message_attachments AS attachment
JOIN media_assets AS asset ON asset.id = attachment.asset_id
JOIN agent_messages AS message ON message.id = attachment.message_id
JOIN agent_threads AS thread ON thread.id = message.thread_id
JOIN users AS owner ON owner.id = thread.user_id
WHERE asset.id = $1
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id
  AND asset.kind = 'audio'
  AND asset.status = 'ready'
  AND thread.deleted_at IS NULL
  AND owner.status = 'active'`, audioID, ownerID))
}

func findAudioAttachmentInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	audioID string,
) (conversation.AudioAttachment, error) {
	return scanAudioAttachment(tx.QueryRow(ctx, `
SELECT
    asset.id::text,
    message.id::text,
    asset.content_type,
    asset.size_bytes,
    asset.duration_ns,
    attachment.created_at
FROM agent_message_attachments AS attachment
JOIN media_assets AS asset ON asset.id = attachment.asset_id
JOIN agent_messages AS message ON message.id = attachment.message_id
JOIN agent_threads AS thread ON thread.id = message.thread_id
WHERE asset.id = $1
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id
  AND asset.kind = 'audio'
  AND asset.status = 'ready'
  AND thread.deleted_at IS NULL`, audioID, ownerID))
}

func scanAudioAttachment(row rowScanner) (conversation.AudioAttachment, error) {
	var attachment conversation.AudioAttachment
	var duration int64
	if err := row.Scan(
		&attachment.ID,
		&attachment.MessageID,
		&attachment.ContentType,
		&attachment.Size,
		&duration,
		&attachment.CreatedAt,
	); err != nil {
		return conversation.AudioAttachment{}, mapError(err)
	}
	attachment.Duration = time.Duration(duration)
	return attachment, nil
}

func (repository *Repository) FindMessageByID(
	ctx context.Context,
	ownerID string,
	messageID string,
) (conversation.Message, error) {
	message, err := repository.messages.FindOwnedMessage(ctx, ownerID, messageID)
	if err != nil {
		return conversation.Message{}, mapConversationError(err)
	}
	return message, nil
}

func lockActiveOwner(ctx context.Context, tx pgx.Tx, ownerID string) error {
	var lockedID string
	if err := tx.QueryRow(ctx, `
SELECT id::text
FROM users
WHERE id = $1 AND status = 'active'
FOR UPDATE`, ownerID).Scan(&lockedID); err != nil {
		return mapError(err)
	}
	return nil
}

func lockOwnedThread(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
) (int64, error) {
	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
FOR UPDATE`, threadID, ownerID).Scan(&nextSequence); err != nil {
		return 0, mapError(err)
	}
	return nextSequence, nil
}

type rowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func draftThreadID(
	ctx context.Context,
	queryer rowQueryer,
	ownerID string,
	draftID string,
) (string, error) {
	var threadID string
	if err := queryer.QueryRow(ctx, `
SELECT draft.thread_id::text
FROM agent_voice_drafts AS draft
JOIN media_assets AS asset ON asset.id = draft.id
JOIN agent_threads AS thread ON thread.id = draft.thread_id
WHERE draft.id = $1
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id`, draftID, ownerID).Scan(&threadID); err != nil {
		return "", mapError(err)
	}
	return threadID, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func mapConversationError(err error) error {
	switch {
	case errors.Is(err, conversation.ErrInvalidRequest):
		return agentvoice.ErrInvalidRequest
	case errors.Is(err, conversation.ErrNotFound):
		return agentvoice.ErrNotFound
	case errors.Is(err, conversation.ErrConflict):
		return agentvoice.ErrConflict
	case errors.Is(err, conversation.ErrIdempotencyConflict):
		return agentvoice.ErrIdempotencyConflict
	default:
		return agentvoice.ErrRepository
	}
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return agentvoice.ErrConflict
		case "23503":
			return agentvoice.ErrNotFound
		case "23514", "22P02", "22003":
			return agentvoice.ErrInvalidRequest
		}
	}
	return agentvoice.ErrRepository
}

func validFailure(kind string) bool {
	return kind == strings.TrimSpace(kind) && len(kind) >= 1 && len(kind) <= 64
}

var _ agentvoice.Repository = (*Repository)(nil)
