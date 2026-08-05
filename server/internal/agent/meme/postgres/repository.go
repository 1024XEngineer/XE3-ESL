package postgres

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/meme"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	database *pgxpool.Pool
}

func New(database *pgxpool.Pool) (*Repository, error) {
	if database == nil {
		return nil, meme.ErrRepository
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) RecentMemeIDs(
	ctx context.Context,
	ownerID string,
	threadID string,
	limit int,
) ([]string, error) {
	if repository == nil || ownerID == "" || threadID == "" || limit < 0 || limit > 100 {
		return nil, meme.ErrInvalidRequest
	}
	if limit == 0 {
		return nil, nil
	}
	rows, err := repository.database.Query(ctx, `
SELECT meme_id
FROM agent_message_memes
WHERE owner_user_id = $1 AND thread_id = $2
ORDER BY created_at DESC, position DESC, id DESC
LIMIT $3`, ownerID, threadID, limit)
	if err != nil {
		return nil, meme.ErrRepository
	}
	defer rows.Close()
	result := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, meme.ErrRepository
		}
		result = append(result, id)
	}
	if rows.Err() != nil {
		return nil, meme.ErrRepository
	}
	return result, nil
}

func (repository *Repository) MessageAttachments(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
) ([]meme.Attachment, error) {
	if repository == nil || ownerID == "" || threadID == "" || messageID == "" {
		return nil, meme.ErrInvalidRequest
	}
	rows, err := repository.database.Query(ctx, `
SELECT `+attachmentColumns+`
FROM agent_message_memes
WHERE owner_user_id = $1 AND thread_id = $2 AND message_id = $3
ORDER BY position ASC`, ownerID, threadID, messageID)
	if err != nil {
		return nil, meme.ErrRepository
	}
	defer rows.Close()
	result := make([]meme.Attachment, 0, 1)
	for rows.Next() {
		item, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, meme.ErrRepository
	}
	return result, nil
}

func (repository *Repository) FindAttachment(
	ctx context.Context,
	ownerID string,
	attachmentID string,
) (meme.Attachment, error) {
	if repository == nil || ownerID == "" || attachmentID == "" {
		return meme.Attachment{}, meme.ErrInvalidRequest
	}
	result, err := scanAttachment(repository.database.QueryRow(ctx, `
SELECT `+attachmentColumns+`
FROM agent_message_memes
WHERE owner_user_id = $1 AND id = $2`, ownerID, attachmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return meme.Attachment{}, meme.ErrNotFound
	}
	return result, err
}

const attachmentColumns = `
    id::text,
    owner_user_id::text,
    thread_id::text,
    message_id::text,
    run_id::text,
    meme_id,
    pack_id,
    pack_version,
    category,
    asset_key,
    content_type,
    size_bytes,
    width,
    height,
    checksum_sha256,
    weight,
    position,
    classification_policy_version,
    selection_policy_version,
    classifier_provider,
    classifier_model,
    created_at`

type scanner interface{ Scan(...any) error }

func scanAttachment(row scanner) (meme.Attachment, error) {
	var result meme.Attachment
	if err := row.Scan(
		&result.ID, &result.OwnerID, &result.ThreadID, &result.MessageID,
		&result.RunID, &result.MemeID, &result.PackID, &result.PackVersion,
		&result.Category, &result.AssetKey, &result.ContentType, &result.SizeBytes,
		&result.Width, &result.Height, &result.ChecksumSHA256, &result.Weight,
		&result.Position, &result.ClassificationPolicyVersion,
		&result.SelectionPolicyVersion, &result.ClassifierProvider,
		&result.ClassifierModel, &result.CreatedAt,
	); err != nil {
		return meme.Attachment{}, err
	}
	return result, nil
}

var _ meme.RecentReader = (*Repository)(nil)
var _ meme.AttachmentReader = (*Repository)(nil)
