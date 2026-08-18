package avatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Service struct {
	repository Repository
	media      *sharedmedia.Service
	config     Config
	httpClient *http.Client
}

func NewService(repository Repository, media *sharedmedia.Service, config Config) (*Service, error) {
	if repository == nil || media == nil || config.StagedTTL <= 0 {
		return nil, ErrInvalidRequest
	}
	return &Service{
		repository: repository,
		media:      media,
		config:     config,
		httpClient: &http.Client{
			Timeout: defaultReadTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
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
) (Content, error) {
	if ctx == nil || !actor.Valid() {
		return Content{}, ErrNotFound
	}
	assetID, err := service.repository.CurrentAssetID(ctx, actor.UserID)
	if err != nil {
		return Content{}, err
	}
	asset, err := service.media.FindOwned(ctx, actor.UserID, assetID)
	if err != nil || asset.Kind != sharedmedia.KindImage ||
		asset.Status != sharedmedia.StatusReady || asset.Size < 1 || asset.Size > MaxBytes {
		return Content{}, ErrNotFound
	}
	signed, err := service.media.SignedGet(ctx, actor.UserID, assetID)
	if err != nil {
		return Content{}, mapMediaError(err)
	}
	signedURL, err := url.Parse(signed.URL)
	if err != nil || signedURL.Scheme != "https" || signedURL.Host == "" {
		return Content{}, ErrRepository
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL.String(), nil)
	if err != nil {
		return Content{}, ErrRepository
	}
	response, err := service.httpClient.Do(request)
	if err != nil {
		return Content{}, ErrRepository
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK ||
		!strings.EqualFold(strings.TrimSpace(response.Header.Get("Content-Type")), asset.ContentType) {
		return Content{}, ErrRepository
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
	if err != nil || int64(len(payload)) != asset.Size || len(payload) > MaxBytes {
		return Content{}, ErrRepository
	}
	checksum := sha256.Sum256(payload)
	if hex.EncodeToString(checksum[:]) != asset.ChecksumSHA256 {
		return Content{}, ErrRepository
	}
	return Content{Payload: payload, ContentType: asset.ContentType}, nil
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
