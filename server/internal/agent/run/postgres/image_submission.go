package postgres

import (
	"context"
	"slices"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ImageSubmissionRepository atomically creates a multimodal user message,
// attaches its images, and creates the initial Run.
type ImageSubmissionRepository struct {
	database database
	ids      IDGenerator
}

func NewImageSubmissionRepository(
	database database,
	ids IDGenerator,
) (*ImageSubmissionRepository, error) {
	if database == nil || ids == nil {
		return nil, agentrun.ErrRepository
	}
	return &ImageSubmissionRepository{database: database, ids: ids}, nil
}

func (r *ImageSubmissionRepository) CreateInitialWithImages(
	ctx context.Context,
	ownerID string,
	threadID string,
	clientMessageID string,
	content string,
	imageAssetIDs []string,
	configuration agentrun.Configuration,
) (agentrun.Submission, error) {
	if ctx == nil || !conversation.ValidUUID(ownerID) ||
		!conversation.ValidUUID(threadID) ||
		!conversation.ValidClientMessageID(clientMessageID) ||
		!conversation.ValidMessageContent(content) ||
		!agentimage.ValidAssetIDs(imageAssetIDs) ||
		!agentrun.ValidConfiguration(configuration) {
		return agentrun.Submission{}, agentrun.ErrInvalidRequest
	}

	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	defer rollback(tx)

	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT threads.next_message_sequence
FROM agent_threads AS threads
INNER JOIN identity_users AS owner
  ON owner.id = threads.owner_user_id
 AND owner.account_status = 'active'
WHERE threads.id = $1
  AND threads.owner_user_id = $2
FOR UPDATE OF threads`,
		threadID,
		ownerID,
	).Scan(&nextSequence); err != nil {
		return agentrun.Submission{}, mapRunPostgresError(err)
	}

	message, found, err := findInputMessageByClientIDInTransaction(
		ctx,
		tx,
		ownerID,
		threadID,
		clientMessageID,
	)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if found {
		return replayImageSubmission(
			ctx,
			tx,
			ownerID,
			threadID,
			content,
			imageAssetIDs,
			message,
		)
	}

	if err := lockAttachableImageAssets(
		ctx,
		tx,
		ownerID,
		threadID,
		imageAssetIDs,
	); err != nil {
		return agentrun.Submission{}, err
	}
	messageID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	runID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	message, err = insertImageMessage(
		ctx,
		tx,
		messageID,
		ownerID,
		threadID,
		nextSequence,
		clientMessageID,
		content,
	)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if err := attachImageAssets(
		ctx,
		tx,
		ownerID,
		threadID,
		messageID,
		imageAssetIDs,
	); err != nil {
		return agentrun.Submission{}, err
	}
	command, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + interval '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2`,
		threadID,
		ownerID,
	)
	if err != nil {
		return agentrun.Submission{}, mapRunPostgresError(err)
	}
	if command.RowsAffected() != 1 {
		return agentrun.Submission{}, agentrun.ErrNotFound
	}
	run, err := insertPendingRun(
		ctx,
		tx,
		runID,
		ownerID,
		threadID,
		messageID,
		1,
		"",
		"",
		configuration,
	)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	return agentrun.Submission{
		Run:         run,
		UserMessage: message,
		Created:     true,
	}, nil
}

func replayImageSubmission(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	content string,
	imageAssetIDs []string,
	message conversation.Message,
) (agentrun.Submission, error) {
	if message.Content != content ||
		message.Role != conversation.MessageRoleUser ||
		message.Modality != conversation.MessageModalityMultimodal {
		return agentrun.Submission{}, agentrun.ErrIdempotencyConflict
	}
	existingIDs, err := findMessageImageIDs(ctx, tx, message.ID)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if !slices.Equal(existingIDs, imageAssetIDs) {
		return agentrun.Submission{}, agentrun.ErrIdempotencyConflict
	}
	run, found, err := findInitialRunByInput(
		ctx,
		tx,
		ownerID,
		threadID,
		message.ID,
	)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if !found {
		return agentrun.Submission{}, agentrun.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	return agentrun.Submission{
		Run:         run,
		UserMessage: message,
		Created:     false,
	}, nil
}

func lockAttachableImageAssets(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	imageAssetIDs []string,
) error {
	rows, err := tx.Query(ctx, `
SELECT
    image_asset_id::text,
    status,
    etag,
    upload_lease_until,
    expires_at
FROM agent_image_assets
WHERE owner_user_id = $1
  AND thread_id = $2
  AND image_asset_id = ANY($3::uuid[])
ORDER BY image_asset_id
FOR UPDATE`,
		ownerID,
		threadID,
		imageAssetIDs,
	)
	if err != nil {
		return mapRunPostgresError(err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var assetID string
		var status string
		var etag string
		var uploadLease pgtype.Timestamptz
		var expiresAt pgtype.Timestamptz
		if err := rows.Scan(
			&assetID,
			&status,
			&etag,
			&uploadLease,
			&expiresAt,
		); err != nil {
			return agentrun.ErrRepository
		}
		if status != string(agentimage.StatusStaged) ||
			etag == "" || uploadLease.Valid || !expiresAt.Valid ||
			!expiresAt.Time.After(time.Now().UTC()) {
			return agentrun.ErrConflict
		}
		count++
	}
	if rows.Err() != nil {
		return agentrun.ErrRepository
	}
	if count != len(imageAssetIDs) {
		return agentrun.ErrNotFound
	}
	return nil
}

func insertImageMessage(
	ctx context.Context,
	tx pgx.Tx,
	messageID string,
	ownerID string,
	threadID string,
	sequence int64,
	clientMessageID string,
	content string,
) (conversation.Message, error) {
	var message conversation.Message
	var role string
	var modality string
	err := tx.QueryRow(ctx, `
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
) VALUES ($1, $2, $3, $4, 'user', $5, 'multimodal', $6, CURRENT_TIMESTAMP)
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
		sequence,
		clientMessageID,
		content,
	).Scan(
		&message.ID,
		&message.OwnerID,
		&message.ThreadID,
		&message.Sequence,
		&role,
		&message.ClientMessageID,
		&modality,
		&message.Content,
		&message.CreatedAt,
	)
	if err != nil {
		return conversation.Message{}, mapRunPostgresError(err)
	}
	message.Role = conversation.MessageRole(role)
	message.Modality = conversation.MessageModality(modality)
	return message, nil
}

func attachImageAssets(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	messageID string,
	imageAssetIDs []string,
) error {
	for position, assetID := range imageAssetIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO agent_message_images (
    owner_user_id,
    thread_id,
    message_id,
    image_asset_id,
    position,
    created_at
) VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)`,
			ownerID,
			threadID,
			messageID,
			assetID,
			position,
		); err != nil {
			return mapRunPostgresError(err)
		}
		command, err := tx.Exec(ctx, `
UPDATE agent_image_assets
SET
    status = 'attached',
    attached_at = CURRENT_TIMESTAMP,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE image_asset_id = $1
  AND owner_user_id = $2
  AND thread_id = $3
  AND status = 'staged'
  AND etag <> ''`,
			assetID,
			ownerID,
			threadID,
		)
		if err != nil {
			return mapRunPostgresError(err)
		}
		if command.RowsAffected() != 1 {
			return agentrun.ErrConflict
		}
	}
	return nil
}

func findMessageImageIDs(
	ctx context.Context,
	tx pgx.Tx,
	messageID string,
) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT image_asset_id::text
FROM agent_message_images
WHERE message_id = $1
ORDER BY position`,
		messageID,
	)
	if err != nil {
		return nil, mapRunPostgresError(err)
	}
	defer rows.Close()

	result := make([]string, 0)
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return nil, agentrun.ErrRepository
		}
		result = append(result, assetID)
	}
	if rows.Err() != nil {
		return nil, agentrun.ErrRepository
	}
	return result, nil
}

var _ agentrun.ImageSubmissionRepository = (*ImageSubmissionRepository)(nil)
