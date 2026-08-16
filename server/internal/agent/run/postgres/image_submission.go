package postgres

import (
	"context"
	"errors"
	"slices"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/jackc/pgx/v5"
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
		!validSubmissionImageIDs(imageAssetIDs) ||
		!agentrun.ValidConfiguration(configuration) {
		return agentrun.Submission{}, agentrun.ErrInvalidRequest
	}

	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	defer rollback(tx)

	nextSequence, err := lockOwnedThread(ctx, tx, ownerID, threadID)
	if err != nil {
		return agentrun.Submission{}, err
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
		messageID,
		imageAssetIDs,
	); err != nil {
		return agentrun.Submission{}, err
	}
	command, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    title = COALESCE(title, NULLIF($3, '')),
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + interval '1 microsecond'
    )
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		threadID,
		ownerID,
		conversation.DeriveThreadTitle(content),
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
	imageAssetIDs []string,
) error {
	return mapImageMediaError(mediapostgres.LockAttachableInTransaction(
		ctx,
		tx,
		ownerID,
		sharedmedia.KindImage,
		imageAssetIDs,
	))
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
	_, err := tx.Exec(ctx, `
INSERT INTO agent_messages (
    id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at
) VALUES ($1, $2, $3, 'user', $4, 'multimodal', $5, CURRENT_TIMESTAMP)`,
		messageID,
		threadID,
		sequence,
		clientMessageID,
		content,
	)
	if err != nil {
		return conversation.Message{}, mapRunPostgresError(err)
	}
	message, err := conversationpostgres.FindMessageInTransaction(
		ctx, tx, ownerID, threadID, messageID,
	)
	if err != nil {
		return conversation.Message{}, mapConversationError(err)
	}
	return message, nil
}

func attachImageAssets(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	messageID string,
	imageAssetIDs []string,
) error {
	for position, assetID := range imageAssetIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO agent_message_attachments (
    message_id,
    asset_id,
    position,
    created_at
) VALUES ($1, $2, $3, CURRENT_TIMESTAMP)`,
			messageID,
			assetID,
			position,
		); err != nil {
			return mapRunPostgresError(err)
		}
	}
	return mapImageMediaError(mediapostgres.RetainInTransaction(
		ctx,
		tx,
		ownerID,
		sharedmedia.KindImage,
		imageAssetIDs,
	))
}

func mapImageMediaError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sharedmedia.ErrInvalidRequest):
		return agentrun.ErrInvalidRequest
	case errors.Is(err, sharedmedia.ErrNotFound):
		return agentrun.ErrNotFound
	case errors.Is(err, sharedmedia.ErrConflict):
		return agentrun.ErrConflict
	default:
		return agentrun.ErrRepository
	}
}

func findMessageImageIDs(
	ctx context.Context,
	tx pgx.Tx,
	messageID string,
) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT attachment.asset_id::text
FROM agent_message_attachments AS attachment
JOIN media_assets AS asset ON asset.id = attachment.asset_id
WHERE message_id = $1
  AND asset.kind = 'image'
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

func validSubmissionImageIDs(assetIDs []string) bool {
	if len(assetIDs) < 1 || len(assetIDs) > 4 {
		return false
	}
	seen := make(map[string]struct{}, len(assetIDs))
	for _, id := range assetIDs {
		if !agentrun.ValidUUID(id) {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

var _ agentrun.ImageSubmissionRepository = (*ImageSubmissionRepository)(nil)
