package media

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const cleanupFailureCode = "object_delete_failed"

type Service struct {
	repository Repository
	stores     Stores
	ids        IDGenerator
	clock      func() time.Time
	config     Config
}

func NewService(
	repository Repository,
	stores Stores,
	ids IDGenerator,
	config Config,
) (*Service, error) {
	if repository == nil ||
		(stores.Images == nil && stores.Audio == nil && stores.Documents == nil) ||
		ids == nil ||
		config.UploadLease < time.Second || config.UploadLease > 10*time.Minute ||
		config.CleanupLease < time.Second || config.CleanupLease > 30*time.Minute ||
		config.PlaybackTTL <= 0 || config.PlaybackTTL > 2*time.Minute ||
		config.CleanupBatch < 1 || config.CleanupBatch > 100 {
		return nil, ErrInvalidRequest
	}
	return &Service{
		repository: repository,
		stores:     stores,
		ids:        ids,
		clock:      func() time.Time { return time.Now().UTC() },
		config:     config,
	}, nil
}

func (service *Service) Upload(
	ctx context.Context,
	request Upload,
) (Asset, error) {
	if ctx == nil || !validUpload(request) {
		return Asset{}, ErrInvalidRequest
	}
	id, err := service.ids.NewID()
	if err != nil {
		return Asset{}, ErrRepository
	}
	now := service.clock()
	asset := Asset{
		ID:              id,
		UserID:          request.UserID,
		Kind:            request.Kind,
		UploadRequestID: request.IdempotencyKey,
		ObjectKey:       objectKey(request.Kind, id, request.ContentType),
		ContentType:     request.ContentType,
		Size:            request.Size,
		ChecksumSHA256:  request.ChecksumSHA256,
		Width:           request.Width,
		Height:          request.Height,
		Duration:        request.Duration,
		SampleRate:      request.SampleRate,
		Status:          StatusStaged,
		ExpiresAt:       request.ExpiresAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	stage, err := service.repository.Stage(ctx, asset)
	if err != nil {
		return Asset{}, err
	}
	asset = stage.Asset
	if !sameUpload(asset, request) {
		return Asset{}, ErrIdempotencyConflict
	}
	if asset.Status == StatusReady {
		return asset, nil
	}
	if asset.Status != StatusStaged {
		return Asset{}, ErrConflict
	}
	claim, acquired, err := service.repository.ClaimUpload(
		ctx, request.UserID, asset.ID, service.config.UploadLease,
	)
	if err != nil {
		return Asset{}, err
	}
	if !acquired {
		return claim.Asset, nil
	}
	deadline, ok := operationDeadline(
		service.clock(), claim.LeaseExpiresAt, service.config.UploadLease,
	)
	if !ok {
		return Asset{}, ErrConflict
	}
	if _, err := request.Body.Seek(0, 0); err != nil {
		return Asset{}, ErrInvalidRequest
	}
	putContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	store, err := service.store(request.Kind)
	if err != nil {
		return Asset{}, err
	}
	result, err := store.Put(
		putContext,
		objectstore.PutRequest{
			Key:            claim.Asset.ObjectKey,
			Body:           request.Body,
			Size:           request.Size,
			ContentType:    request.ContentType,
			ChecksumSHA256: request.ChecksumSHA256,
		},
	)
	if err != nil {
		return Asset{}, err
	}
	return service.repository.CommitUpload(
		ctx, request.UserID, asset.ID, claim.FencingToken, result.ETag,
	)
}

func (service *Service) FindOwned(
	ctx context.Context,
	userID string,
	assetID string,
) (Asset, error) {
	if ctx == nil || !ValidUUID(userID) || !ValidUUID(assetID) {
		return Asset{}, ErrNotFound
	}
	return service.repository.FindOwned(ctx, userID, assetID)
}

func (service *Service) SignedGet(
	ctx context.Context,
	userID string,
	assetID string,
) (objectstore.SignedGetResult, error) {
	asset, err := service.FindOwned(ctx, userID, assetID)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if asset.Status != StatusReady || asset.ETag == "" {
		return objectstore.SignedGetResult{}, ErrNotFound
	}
	store, err := service.store(asset.Kind)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	result, err := store.SignedGet(ctx, asset.ObjectKey)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	now := service.clock()
	if strings.TrimSpace(result.URL) == "" || !result.ExpiresAt.After(now) ||
		result.ExpiresAt.After(now.Add(service.config.PlaybackTTL)) {
		return objectstore.SignedGetResult{}, ErrRepository
	}
	return result, nil
}

// Open returns a ready owned document for trusted server-side processing.
func (service *Service) Open(
	ctx context.Context,
	userID string,
	assetID string,
) (io.ReadCloser, error) {
	asset, err := service.FindOwned(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	if asset.Kind != KindDocument || asset.Status != StatusReady ||
		asset.ETag == "" || service.stores.Documents == nil {
		return nil, ErrNotFound
	}
	return service.stores.Documents.Open(ctx, asset.ObjectKey)
}

// Delete removes an owned asset only after all business references have been
// detached. The media repository owns the deletion lease and object lifecycle.
func (service *Service) Delete(
	ctx context.Context,
	userID string,
	assetID string,
) error {
	if ctx == nil || !ValidUUID(userID) || !ValidUUID(assetID) {
		return ErrNotFound
	}
	asset, err := service.repository.BeginDeletion(
		ctx, userID, assetID, service.config.CleanupLease,
	)
	if err != nil {
		return err
	}
	return service.deleteClaim(ctx, CleanupClaim{
		AssetID: asset.ID, Kind: asset.Kind, ObjectKey: asset.ObjectKey,
		FencingToken:   asset.CleanupFencingToken,
		LeaseExpiresAt: asset.CleanupLeaseUntil,
	})
}

func (service *Service) Reclaim(
	ctx context.Context,
	limit int,
) (CleanupResult, error) {
	if ctx == nil {
		return CleanupResult{}, ErrInvalidRequest
	}
	if limit <= 0 || limit > service.config.CleanupBatch {
		limit = service.config.CleanupBatch
	}
	claims, err := service.repository.ClaimCleanup(
		ctx, service.config.CleanupLease, limit,
	)
	if err != nil {
		return CleanupResult{}, err
	}
	result := CleanupResult{}
	for _, claim := range claims {
		if err := service.deleteClaim(ctx, claim); err != nil {
			result.Failed++
			continue
		}
		result.Deleted++
	}
	return result, nil
}

func (service *Service) deleteClaim(
	ctx context.Context,
	claim CleanupClaim,
) error {
	store, err := service.store(claim.Kind)
	if err != nil {
		_ = service.repository.ReleaseCleanup(ctx, claim, cleanupFailureCode)
		return err
	}
	if err := store.Delete(ctx, claim.ObjectKey); err != nil {
		_ = service.repository.ReleaseCleanup(
			ctx, claim, cleanupFailureCode,
		)
		return err
	}
	if err := service.repository.FinishCleanup(ctx, claim); err != nil {
		_ = service.repository.ReleaseCleanup(
			ctx, claim, cleanupFailureCode,
		)
		return err
	}
	return nil
}

func (service *Service) store(kind Kind) (objectstore.Store, error) {
	if kind == KindImage {
		if service.stores.Images == nil {
			return nil, objectstore.ErrDisabled
		}
		return service.stores.Images, nil
	}
	if kind == KindAudio && service.stores.Audio != nil {
		return service.stores.Audio, nil
	}
	if kind == KindDocument && service.stores.Documents != nil {
		return service.stores.Documents, nil
	}
	return nil, objectstore.ErrDisabled
}

func validUpload(request Upload) bool {
	if !ValidUUID(request.UserID) || !ValidIdempotencyKey(request.IdempotencyKey) ||
		request.Body == nil || !ValidChecksum(request.ChecksumSHA256) ||
		request.Size < 1 || request.Size > 10*1024*1024 ||
		request.ExpiresAt.IsZero() {
		return false
	}
	switch request.Kind {
	case KindImage:
		return (request.ContentType == "image/jpeg" ||
			request.ContentType == "image/png" ||
			request.ContentType == "image/webp") &&
			request.Width > 0 && request.Height > 0 &&
			int64(request.Width)*int64(request.Height) <= 16_000_000 &&
			request.Duration == 0 && request.SampleRate == 0
	case KindAudio:
		return request.ContentType == "audio/wav" &&
			request.Size <= 7_400_000 &&
			request.Width == 0 && request.Height == 0 &&
			request.Duration > 0 &&
			request.Duration <= 122*time.Second &&
			request.SampleRate >= 8_000 && request.SampleRate <= 48_000
	case KindDocument:
		return request.ContentType == "application/pdf" &&
			request.Width == 0 && request.Height == 0 &&
			request.Duration == 0 && request.SampleRate == 0
	default:
		return false
	}
}

func sameUpload(asset Asset, request Upload) bool {
	return asset.UserID == request.UserID && asset.Kind == request.Kind &&
		asset.UploadRequestID == request.IdempotencyKey &&
		asset.ContentType == request.ContentType && asset.Size == request.Size &&
		asset.ChecksumSHA256 == request.ChecksumSHA256 &&
		asset.Width == request.Width && asset.Height == request.Height &&
		asset.Duration == request.Duration &&
		asset.SampleRate == request.SampleRate
}

func objectKey(kind Kind, id string, contentType string) string {
	if kind == KindAudio {
		return "audio/v1/media/" + id + ".wav"
	}
	if kind == KindDocument {
		return "resume/v1/media/" + id + ".pdf"
	}
	extension := ".webp"
	if contentType == "image/jpeg" {
		extension = ".jpg"
	} else if contentType == "image/png" {
		extension = ".png"
	}
	return "image/v1/media/" + id + extension
}

func operationDeadline(
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
