package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	media    *sharedmedia.Service
	threads  *conversation.Service
	database *pgxpool.Pool
	clock    func() time.Time
	config   Config
	logger   *slog.Logger
}

func NewService(
	mediaService *sharedmedia.Service,
	threads *conversation.Service,
	database *pgxpool.Pool,
	config Config,
	logger *slog.Logger,
) (*Service, error) {
	if mediaService == nil || threads == nil || database == nil || logger == nil ||
		config.StagedTTL < time.Minute || config.StagedTTL > 7*24*time.Hour {
		return nil, ErrInvalidRequest
	}
	return &Service{
		media: mediaService, threads: threads, database: database,
		clock:  func() time.Time { return time.Now().UTC() },
		config: config, logger: logger,
	}, nil
}

func (service *Service) Upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
) (result Image, resultErr error) {
	startedAt := service.clock()
	defer func() {
		attributes := []any{
			"duration_ms", service.clock().Sub(startedAt).Milliseconds(),
		}
		if result.ID != "" {
			attributes = append(attributes, "media_asset_id", result.ID)
		}
		if resultErr != nil {
			service.logger.WarnContext(ctx, "agent.image.upload.failed", append(
				attributes, "error_category", errorCategory(resultErr),
			)...)
			return
		}
		service.logger.InfoContext(ctx, "agent.image.upload.succeeded", attributes...)
	}()
	if ctx == nil || !actor.Valid() || !sharedmedia.ValidUUID(request.ThreadID) ||
		!sharedmedia.ValidIdempotencyKey(request.IdempotencyKey) || request.Body == nil {
		return Image{}, ErrInvalidRequest
	}
	_, err := service.threads.GetThread(ctx, actor, request.ThreadID)
	if err != nil {
		return Image{}, mapConversationError(err)
	}
	payload, err := io.ReadAll(io.LimitReader(request.Body, MaxBytes+1))
	if err != nil || len(payload) == 0 {
		return Image{}, ErrInvalid
	}
	if len(payload) > MaxBytes {
		return Image{}, ErrTooLarge
	}
	normalized, err := normalizeImage(request.ContentType, payload)
	if err != nil {
		return Image{}, err
	}
	checksumBytes := sha256.Sum256(normalized.payload)
	asset, err := service.media.Upload(ctx, sharedmedia.Upload{
		UserID:         actor.UserID,
		Kind:           sharedmedia.KindImage,
		IdempotencyKey: request.IdempotencyKey,
		ContentType:    normalized.contentType,
		Body:           bytes.NewReader(normalized.payload),
		Size:           int64(len(normalized.payload)),
		ChecksumSHA256: hex.EncodeToString(checksumBytes[:]),
		Width:          normalized.width,
		Height:         normalized.height,
		ExpiresAt:      service.clock().Add(service.config.StagedTTL),
	})
	if err != nil {
		return Image{}, mapMediaError(err)
	}
	return imageFromAsset(asset, time.Time{}), nil
}

func (service *Service) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) (Image, error) {
	if ctx == nil || !actor.Valid() || !sharedmedia.ValidUUID(assetID) {
		return Image{}, ErrNotFound
	}
	asset, err := service.media.FindOwned(ctx, actor.UserID, assetID)
	if err != nil {
		return Image{}, mapMediaError(err)
	}
	if asset.Kind != sharedmedia.KindImage {
		return Image{}, ErrNotFound
	}
	return imageFromAsset(asset, time.Time{}), nil
}

func (service *Service) Content(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) (objectstore.SignedGetResult, error) {
	image, err := service.Get(ctx, actor, assetID)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	result, err := service.media.SignedGet(ctx, actor.UserID, image.ID)
	if err != nil {
		return objectstore.SignedGetResult{}, mapMediaError(err)
	}
	return result, nil
}

func (service *Service) Delete(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) error {
	if ctx == nil || !actor.Valid() || !sharedmedia.ValidUUID(assetID) {
		return ErrNotFound
	}
	image, err := service.Get(ctx, actor, assetID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := service.media.Delete(ctx, actor.UserID, image.ID); err != nil {
		return mapMediaError(err)
	}
	return nil
}

func (service *Service) MessageAssets(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) ([]Image, error) {
	if ctx == nil || !actor.Valid() || !sharedmedia.ValidUUID(threadID) ||
		!sharedmedia.ValidUUID(messageID) {
		return nil, ErrNotFound
	}
	rows, err := service.database.Query(ctx, `
SELECT
    asset.id::text,
    asset.content_type,
    asset.size_bytes,
    asset.width,
    asset.height,
    asset.status,
    asset.created_at,
    attachment.created_at
FROM agent_message_attachments AS attachment
JOIN media_assets AS asset ON asset.id = attachment.asset_id
JOIN agent_messages AS message ON message.id = attachment.message_id
JOIN agent_threads AS thread ON thread.id = message.thread_id
WHERE thread.user_id = $1
  AND thread.id = $2
  AND message.id = $3
  AND asset.kind = 'image'
  AND asset.status = 'ready'
ORDER BY attachment.position`, actor.UserID, threadID, messageID)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	images := make([]Image, 0, MaxPerMessage)
	for rows.Next() {
		var image Image
		if err := rows.Scan(
			&image.ID,
			&image.ContentType,
			&image.Size,
			&image.Width,
			&image.Height,
			&image.Status,
			&image.CreatedAt,
			&image.AttachedAt,
		); err != nil {
			return nil, ErrRepository
		}
		images = append(images, image)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return images, nil
}

func (service *Service) MessageImages(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) ([]ContextImage, error) {
	images, err := service.MessageAssets(ctx, actor, threadID, messageID)
	if err != nil {
		return nil, err
	}
	result := make([]ContextImage, 0, len(images))
	for _, image := range images {
		signed, err := service.media.SignedGet(ctx, actor.UserID, image.ID)
		if err != nil {
			return nil, mapMediaError(err)
		}
		result = append(result, ContextImage{AssetID: image.ID, URL: signed.URL})
	}
	return result, nil
}

func imageFromAsset(
	asset sharedmedia.Asset,
	attachedAt time.Time,
) Image {
	return Image{
		ID: asset.ID, ContentType: asset.ContentType,
		Size: asset.Size, Width: asset.Width, Height: asset.Height,
		Status: string(asset.Status), CreatedAt: asset.CreatedAt,
		AttachedAt: attachedAt,
	}
}

func mapConversationError(err error) error {
	if errors.Is(err, conversation.ErrNotFound) {
		return ErrNotFound
	}
	return ErrRepository
}

func mapMediaError(err error) error {
	switch {
	case errors.Is(err, sharedmedia.ErrInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, sharedmedia.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, sharedmedia.ErrConflict):
		return ErrConflict
	case errors.Is(err, sharedmedia.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	default:
		return err
	}
}

func errorCategory(err error) string {
	switch {
	case errors.Is(err, ErrTooLarge):
		return "image_too_large"
	case errors.Is(err, ErrUnsupported):
		return "unsupported_image_format"
	case errors.Is(err, ErrInvalid):
		return "invalid_image"
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request"
	case errors.Is(err, ErrIdempotencyConflict):
		return "idempotency_conflict"
	case errors.Is(err, ErrConflict):
		return "resource_conflict"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, objectstore.ErrDisabled),
		errors.Is(err, objectstore.ErrCredentials),
		errors.Is(err, objectstore.ErrOperationFailed):
		return "object_storage"
	default:
		return "repository"
	}
}

var _ Application = (*Service)(nil)
var _ ContextReader = (*Service)(nil)
