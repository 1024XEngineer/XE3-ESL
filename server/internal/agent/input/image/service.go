package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	defaultStagedTTL       = 24 * time.Hour
	defaultUploadLease     = 2 * time.Minute
	defaultCleanupLease    = 5 * time.Minute
	defaultCleanupBatch    = 8
	maxUploadRequestIDSize = 128
)

type Service struct {
	repository Repository
	store      objectstore.Store
	ids        IDGenerator
	clock      func() time.Time
	config     Config
	logger     *slog.Logger
}

type ServiceOption func(*Service) error

func WithLogger(logger *slog.Logger) ServiceOption {
	return func(service *Service) error {
		if logger == nil {
			return errors.New("agent image: logger is required")
		}
		service.logger = logger
		return nil
	}
}

func NewService(
	repository Repository,
	store objectstore.Store,
	ids IDGenerator,
	config Config,
	options ...ServiceOption,
) (*Service, error) {
	if repository == nil || store == nil || ids == nil {
		return nil, errors.New("agent image: dependencies are required")
	}
	if config.StagedTTL <= 0 {
		config.StagedTTL = defaultStagedTTL
	}
	if config.UploadLease <= 0 {
		config.UploadLease = defaultUploadLease
	}
	if config.StagedTTL < time.Minute || config.StagedTTL > 7*24*time.Hour ||
		config.UploadLease < time.Second ||
		config.UploadLease > 10*time.Minute {
		return nil, ErrInvalidRequest
	}
	service := &Service{
		repository: repository,
		store:      store,
		ids:        ids,
		clock:      func() time.Time { return time.Now().UTC() },
		config:     config,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("agent image: option is invalid")
		}
		if err := option(service); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func (s *Service) Upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
) (result Asset, resultErr error) {
	startedAt := s.clock()
	telemetry := imageUploadTelemetry{}
	defer func() {
		s.logUpload(ctx, telemetry, result, resultErr, startedAt)
	}()
	if ctx == nil || !actor.Valid() || !ValidUUID(request.ThreadID) ||
		!validUploadRequestID(request.IdempotencyKey) ||
		request.Body == nil {
		return Asset{}, ErrInvalidRequest
	}
	payload, err := io.ReadAll(io.LimitReader(
		request.Body,
		MaxBytes+1,
	))
	if err != nil {
		return Asset{}, ErrInvalid
	}
	if len(payload) == 0 {
		return Asset{}, ErrInvalid
	}
	if len(payload) > MaxBytes {
		return Asset{}, ErrTooLarge
	}
	normalized, err := normalizeImage(
		request.ContentType,
		payload,
	)
	if err != nil {
		return Asset{}, err
	}
	telemetry = imageUploadTelemetry{
		contentType: normalized.contentType,
		size:        int64(len(normalized.payload)),
		width:       normalized.width,
		height:      normalized.height,
	}
	checksumBytes := sha256.Sum256(normalized.payload)
	checksum := hex.EncodeToString(checksumBytes[:])
	assetID, err := s.ids.NewID()
	if err != nil {
		return Asset{}, ErrRepository
	}
	now := s.clock()
	stage, err := s.repository.StageAsset(ctx, Asset{
		ID:              assetID,
		OwnerID:         actor.UserID,
		ThreadID:        request.ThreadID,
		UploadRequestID: request.IdempotencyKey,
		ObjectKey:       ObjectPrefix + assetID + normalized.extension,
		ContentType:     normalized.contentType,
		Size:            int64(len(normalized.payload)),
		Width:           normalized.width,
		Height:          normalized.height,
		ChecksumSHA256:  checksum,
		Status:          StatusStaged,
		ExpiresAt:       now.Add(s.config.StagedTTL),
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return Asset{}, err
	}
	asset := stage.Asset
	if !sameUpload(
		asset,
		normalized.contentType,
		int64(len(normalized.payload)),
		normalized.width,
		normalized.height,
		checksum,
	) {
		return Asset{}, ErrIdempotencyConflict
	}
	if asset.Status != StatusStaged || asset.ETag != "" {
		return asset, nil
	}
	claim, acquired, err := s.repository.ClaimUpload(
		ctx,
		actor.UserID,
		asset.ID,
		s.config.UploadLease,
	)
	if err != nil {
		return Asset{}, err
	}
	if !acquired {
		return claim.Asset, nil
	}
	deadline, ok := uploadDeadline(
		s.clock(),
		claim.LeaseExpiresAt,
		s.config.UploadLease,
	)
	if !ok {
		return Asset{}, ErrConflict
	}
	putContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	put, err := s.store.Put(putContext, objectstore.PutRequest{
		Key:            claim.Asset.ObjectKey,
		Body:           bytes.NewReader(normalized.payload),
		Size:           claim.Asset.Size,
		ContentType:    claim.Asset.ContentType,
		ChecksumSHA256: claim.Asset.ChecksumSHA256,
	})
	if err != nil {
		return Asset{}, err
	}
	return s.repository.CommitUpload(
		ctx,
		actor.UserID,
		claim.Asset.ID,
		claim.FencingToken,
		put.ETag,
	)
}

type imageUploadTelemetry struct {
	contentType string
	size        int64
	width       int
	height      int
}

func (s *Service) logUpload(
	ctx context.Context,
	telemetry imageUploadTelemetry,
	asset Asset,
	err error,
	startedAt time.Time,
) {
	if s.logger == nil {
		return
	}
	attributes := []any{
		"duration_ms", s.clock().Sub(startedAt).Milliseconds(),
	}
	if telemetry.contentType != "" {
		attributes = append(
			attributes,
			"content_type", telemetry.contentType,
			"size_bytes", telemetry.size,
			"width", telemetry.width,
			"height", telemetry.height,
		)
	}
	if asset.ID != "" {
		attributes = append(
			attributes,
			"image_asset_id", asset.ID,
			"status", asset.Status,
		)
	}
	if err != nil {
		attributes = append(
			attributes,
			"error_category", imageErrorCategory(err),
		)
		s.logger.WarnContext(ctx, "agent.image.upload.failed", attributes...)
		return
	}
	s.logger.InfoContext(ctx, "agent.image.upload.succeeded", attributes...)
}

func imageErrorCategory(err error) string {
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

func (s *Service) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) (Asset, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(assetID) {
		return Asset{}, ErrNotFound
	}
	return s.repository.FindAsset(ctx, actor.UserID, assetID)
}

func (s *Service) Content(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) (objectstore.SignedGetResult, error) {
	asset, err := s.Get(ctx, actor, assetID)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if asset.ETag == "" ||
		(asset.Status != StatusStaged && asset.Status != StatusAttached) {
		return objectstore.SignedGetResult{}, ErrNotFound
	}
	result, err := s.store.SignedGet(ctx, asset.ObjectKey)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if strings.TrimSpace(result.URL) == "" ||
		!result.ExpiresAt.After(s.clock()) {
		return objectstore.SignedGetResult{}, ErrRepository
	}
	return result, nil
}

func (s *Service) MessageImages(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) ([]ContextImage, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(threadID) ||
		!ValidUUID(messageID) {
		return nil, ErrNotFound
	}
	assets, err := s.repository.ListMessageAssets(
		ctx,
		actor.UserID,
		threadID,
		messageID,
	)
	if err != nil {
		return nil, err
	}
	result := make([]ContextImage, 0, len(assets))
	for _, asset := range assets {
		if asset.Status != StatusAttached {
			continue
		}
		signed, err := s.store.SignedGet(ctx, asset.ObjectKey)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(signed.URL) == "" ||
			!signed.ExpiresAt.After(s.clock()) {
			return nil, ErrRepository
		}
		result = append(result, ContextImage{
			AssetID: asset.ID,
			URL:     signed.URL,
		})
	}
	return result, nil
}

func (s *Service) MessageAssets(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) ([]Asset, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(threadID) ||
		!ValidUUID(messageID) {
		return nil, ErrNotFound
	}
	return s.repository.ListMessageAssets(
		ctx,
		actor.UserID,
		threadID,
		messageID,
	)
}

func (s *Service) Attach(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
	assetIDs []string,
) ([]Asset, error) {
	if ctx == nil || !actor.Valid() {
		return nil, ErrInvalidRequest
	}
	return s.repository.AttachAssets(
		ctx,
		actor.UserID,
		threadID,
		messageID,
		assetIDs,
	)
}

func (s *Service) Delete(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) error {
	if ctx == nil || !actor.Valid() || !ValidUUID(assetID) {
		return ErrNotFound
	}
	asset, err := s.repository.BeginDeletion(
		ctx,
		actor.UserID,
		assetID,
	)
	if err != nil {
		return err
	}
	if asset.Status == StatusDeleted {
		return nil
	}
	if err := s.store.Delete(ctx, asset.ObjectKey); err != nil {
		return err
	}
	_, err = s.repository.FinishDeletion(
		ctx,
		actor.UserID,
		asset.ID,
	)
	return err
}

func (s *Service) Reclaim(
	ctx context.Context,
	limit int,
) (CleanupResult, error) {
	if ctx == nil {
		return CleanupResult{}, ErrInvalidRequest
	}
	if limit <= 0 || limit > 100 {
		limit = defaultCleanupBatch
	}
	claims, err := s.repository.ClaimCleanup(
		ctx,
		defaultCleanupLease,
		limit,
	)
	if err != nil {
		return CleanupResult{}, err
	}
	result := CleanupResult{}
	for _, claim := range claims {
		if err := s.store.Delete(ctx, claim.ObjectKey); err != nil {
			result.Failed++
			_ = s.repository.ReleaseCleanup(ctx, claim)
			continue
		}
		if err := s.repository.FinishCleanup(ctx, claim); err != nil {
			result.Failed++
			_ = s.repository.ReleaseCleanup(ctx, claim)
			continue
		}
		result.Deleted++
	}
	return result, nil
}

func validUploadRequestID(value string) bool {
	return len(value) >= 8 &&
		len(value) <= maxUploadRequestIDSize &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func sameUpload(
	asset Asset,
	contentType string,
	size int64,
	width int,
	height int,
	checksum string,
) bool {
	return asset.ContentType == contentType &&
		asset.Size == size &&
		asset.Width == width &&
		asset.Height == height &&
		asset.ChecksumSHA256 == checksum
}

func uploadDeadline(
	now time.Time,
	leaseExpiresAt time.Time,
	leaseDuration time.Duration,
) (time.Time, bool) {
	reserve := leaseDuration / 10
	if reserve < 500*time.Millisecond {
		reserve = 500 * time.Millisecond
	}
	if reserve > 5*time.Second {
		reserve = 5 * time.Second
	}
	deadline := leaseExpiresAt.Add(-reserve)
	return deadline, deadline.After(now)
}

var _ Application = (*Service)(nil)
var _ ContextReader = (*Service)(nil)
