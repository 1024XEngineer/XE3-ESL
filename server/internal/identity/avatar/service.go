package avatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Service struct {
	repository Repository
	media      *sharedmedia.Service
	config     Config
}

func NewService(repository Repository, media *sharedmedia.Service, config Config) (*Service, error) {
	if repository == nil || media == nil || config.StagedTTL <= 0 {
		return nil, ErrInvalidRequest
	}
	return &Service{repository: repository, media: media, config: config}, nil
}

func (service *Service) Upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
) (identity.UserProfile, error) {
	if ctx == nil || !actor.Valid() || request.Body == nil ||
		!sharedmedia.ValidIdempotencyKey(request.IdempotencyKey) ||
		request.ExpectedProfileVersion < 1 {
		return identity.UserProfile{}, ErrInvalidRequest
	}
	payload, err := io.ReadAll(io.LimitReader(request.Body, MaxBytes+1))
	if err != nil || len(payload) == 0 {
		return identity.UserProfile{}, ErrInvalidRequest
	}
	if len(payload) > MaxBytes {
		return identity.UserProfile{}, agentimage.ErrTooLarge
	}
	normalized, contentType, width, height, err := agentimage.NormalizePayload(
		request.ContentType,
		payload,
	)
	if err != nil {
		return identity.UserProfile{}, err
	}
	checksum := sha256.Sum256(normalized)
	asset, err := service.media.Upload(ctx, sharedmedia.Upload{
		UserID: actor.UserID, Kind: sharedmedia.KindImage,
		IdempotencyKey: request.IdempotencyKey,
		ContentType:    contentType, Body: bytes.NewReader(normalized),
		Size: int64(len(normalized)), ChecksumSHA256: hex.EncodeToString(checksum[:]),
		Width: width, Height: height,
		ExpiresAt: time.Now().UTC().Add(service.config.StagedTTL),
	})
	if err != nil {
		return identity.UserProfile{}, mapMediaError(err)
	}
	if asset.Status != sharedmedia.StatusReady {
		return identity.UserProfile{}, ErrUploadInProgress
	}
	return service.repository.Attach(
		ctx, actor.UserID, request.IdempotencyKey, request.ExpectedProfileVersion,
	)
}

func (service *Service) UseDefault(
	ctx context.Context,
	actor requestcontext.Actor,
	expectedVersion int64,
) (identity.UserProfile, error) {
	if ctx == nil || !actor.Valid() || expectedVersion < 1 {
		return identity.UserProfile{}, ErrInvalidRequest
	}
	return service.repository.UseDefault(ctx, actor.UserID, expectedVersion)
}

func (service *Service) Content(
	ctx context.Context,
	actor requestcontext.Actor,
) (objectstore.SignedGetResult, error) {
	if ctx == nil || !actor.Valid() {
		return objectstore.SignedGetResult{}, ErrNotFound
	}
	assetID, err := service.repository.CurrentAssetID(ctx, actor.UserID)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	content, err := service.media.SignedGet(ctx, actor.UserID, assetID)
	if err != nil {
		return objectstore.SignedGetResult{}, mapMediaError(err)
	}
	return content, nil
}

func mapMediaError(err error) error {
	switch {
	case errors.Is(err, sharedmedia.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	case errors.Is(err, sharedmedia.ErrConflict):
		return ErrUploadInProgress
	case errors.Is(err, sharedmedia.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, sharedmedia.ErrInvalidRequest):
		return ErrInvalidRequest
	default:
		return ErrRepository
	}
}
