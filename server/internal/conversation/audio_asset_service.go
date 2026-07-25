package conversation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const (
	audioObjectPrefix   = "audio/v1/assets/"
	MaxPlaybackURLTTL   = 2 * time.Minute
	defaultStagedTTL    = 24 * time.Hour
	defaultCleanupLimit = 100
	maxConflictReloads  = 3
)

// AudioAssetRepository persists metadata only. Create maps an ID or
// (owner, upload request) uniqueness race to ErrAudioAssetConcurrentUpdate.
// Save atomically compares expectedVersion and maps a stale version to
// ErrAudioAssetConcurrentUpdate. Database uniqueness constraints must map
// either an (owner, Candidate) or (owner, Turn) binding race to
// ErrAudioAssetAlreadyBound.
type AudioAssetRepository interface {
	Create(context.Context, AudioAsset) error
	Get(context.Context, string) (AudioAsset, error)
	GetByUploadRequest(context.Context, string, string) (AudioAsset, error)
	GetByCandidate(context.Context, string, string) (AudioAsset, error)
	GetByTurn(context.Context, string, string) (AudioAsset, error)
	Save(context.Context, AudioAsset, uint64) error
	// ListExpiredUnconfirmed returns expired assets with no Turn whose status
	// is staged or metadata_committed.
	ListExpiredUnconfirmed(context.Context, time.Time, int) ([]AudioAsset, error)
	ListDeleting(context.Context, int) ([]AudioAsset, error)
	// ListOwnerAssetsForAccountCleanup returns a bounded batch of one owner's
	// assets in any state except deleted.
	ListOwnerAssetsForAccountCleanup(context.Context, string, int) ([]AudioAsset, error)
}

type AudioAssetIDGenerator interface {
	NewID() (string, error)
}

type AudioAssetClock interface {
	Now() time.Time
}

// AudioAssetTurnVerifier is the minimal boundary needed to verify that a Turn
// exists, belongs to the actor, and records the expected #90 Candidate.
type AudioAssetTurnVerifier interface {
	VerifyOwnedTurn(context.Context, string, string, string) error
}

// AudioAssetActor must be constructed from the trusted authentication context;
// ownership is never accepted from UploadRecordingRequest.
type AudioAssetActor struct {
	UserID string
}

type UploadRecordingRequest struct {
	RequestID      string
	Body           io.ReadSeeker
	Size           int64
	ContentType    string
	ChecksumSHA256 string
	Duration       time.Duration
}

type AudioAssetCleanupResult struct {
	Deleted int
	Failed  int
}

type AudioAssetService struct {
	repository AudioAssetRepository
	store      objectstore.Store
	ids        AudioAssetIDGenerator
	clock      AudioAssetClock
	turns      AudioAssetTurnVerifier
	stagedTTL  time.Duration
}

func NewAudioAssetService(
	repository AudioAssetRepository,
	store objectstore.Store,
	ids AudioAssetIDGenerator,
	clock AudioAssetClock,
	turns AudioAssetTurnVerifier,
	stagedTTL time.Duration,
) (*AudioAssetService, error) {
	if nilDependency(repository) ||
		nilDependency(store) ||
		nilDependency(ids) ||
		nilDependency(clock) ||
		nilDependency(turns) {
		return nil, ErrAudioAssetInvalidDependency
	}
	if stagedTTL <= 0 {
		stagedTTL = defaultStagedTTL
	}
	return &AudioAssetService{
		repository: repository,
		store:      store,
		ids:        ids,
		clock:      clock,
		turns:      turns,
		stagedTTL:  stagedTTL,
	}, nil
}

// Upload creates staged metadata before writing object bytes. A failed Put or
// metadata commit therefore remains discoverable by staged cleanup.
func (s *AudioAssetService) Upload(
	ctx context.Context,
	actor AudioAssetActor,
	request UploadRecordingRequest,
) (AudioAsset, error) {
	ownerID := strings.TrimSpace(actor.UserID)
	requestID := strings.TrimSpace(request.RequestID)
	if ownerID == "" || requestID == "" || request.Body == nil {
		return AudioAsset{}, ErrAudioAssetInvalid
	}

	asset, err := s.repository.GetByUploadRequest(ctx, ownerID, requestID)
	switch {
	case err == nil:
		if !asset.sameUpload(request) {
			return AudioAsset{}, ErrAudioAssetIdempotencyConflict
		}
		if asset.Status != AudioAssetStaged {
			return asset, nil
		}
	case errors.Is(err, ErrAudioAssetNotFound):
		asset, err = s.createStaged(ctx, ownerID, request)
		if err != nil {
			return AudioAsset{}, err
		}
		if !asset.sameUpload(request) {
			return AudioAsset{}, ErrAudioAssetIdempotencyConflict
		}
		if asset.Status != AudioAssetStaged {
			return asset, nil
		}
	default:
		return AudioAsset{}, err
	}

	putResult, err := s.store.Put(ctx, objectstore.PutRequest{
		Key:            asset.ObjectKey,
		Body:           request.Body,
		Size:           asset.Size,
		ContentType:    asset.ContentType,
		ChecksumSHA256: asset.ChecksumSHA256,
	})
	if err != nil && !errors.Is(err, objectstore.ErrAlreadyExists) {
		return AudioAsset{}, err
	}

	return s.commitUploadedMetadata(ctx, asset, request, putResult.ETag)
}

func (s *AudioAssetService) createStaged(
	ctx context.Context,
	ownerID string,
	request UploadRecordingRequest,
) (AudioAsset, error) {
	id, err := s.ids.NewID()
	if err != nil {
		return AudioAsset{}, err
	}
	now := s.clock.Now()
	asset, err := newStagedAudioAsset(
		id,
		ownerID,
		request.RequestID,
		audioObjectPrefix+id+".wav",
		request.ContentType,
		request.Size,
		request.ChecksumSHA256,
		request.Duration,
		now,
		now.Add(s.stagedTTL),
	)
	if err != nil {
		return AudioAsset{}, err
	}
	if err := s.repository.Create(ctx, asset); err != nil {
		if !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
			return AudioAsset{}, err
		}
		return s.repository.GetByUploadRequest(ctx, ownerID, request.RequestID)
	}
	return asset, nil
}

// Confirm binds one asset to exactly one Candidate and one Turn. Repeating the
// same asset/Candidate/Turn binding is a no-op.
func (s *AudioAssetService) Confirm(
	ctx context.Context,
	actor AudioAssetActor,
	assetID string,
	candidateID string,
	turnID string,
) (AudioAsset, error) {
	candidateID = strings.TrimSpace(candidateID)
	turnID = strings.TrimSpace(turnID)
	if candidateID == "" || turnID == "" {
		return AudioAsset{}, ErrAudioAssetInvalid
	}
	asset, err := s.ownedAsset(ctx, actor, assetID)
	if err != nil {
		return AudioAsset{}, err
	}
	if err := s.turns.VerifyOwnedTurn(ctx, asset.OwnerID, turnID, candidateID); err != nil {
		return AudioAsset{}, err
	}

	for range maxConflictReloads {
		if existing, lookupErr := s.repository.GetByCandidate(
			ctx,
			asset.OwnerID,
			candidateID,
		); lookupErr == nil {
			if existing.ID != asset.ID ||
				existing.OwnerID != asset.OwnerID ||
				existing.TurnID != turnID {
				return AudioAsset{}, ErrAudioAssetAlreadyBound
			}
			asset = existing
		} else if !errors.Is(lookupErr, ErrAudioAssetNotFound) {
			return AudioAsset{}, lookupErr
		}
		if existing, lookupErr := s.repository.GetByTurn(
			ctx,
			asset.OwnerID,
			turnID,
		); lookupErr == nil {
			if existing.ID != asset.ID ||
				existing.OwnerID != asset.OwnerID ||
				existing.CandidateID != candidateID {
				return AudioAsset{}, ErrAudioAssetAlreadyBound
			}
			asset = existing
		} else if !errors.Is(lookupErr, ErrAudioAssetNotFound) {
			return AudioAsset{}, lookupErr
		}

		expectedVersion := asset.Version
		if err := asset.bindTurn(candidateID, turnID, s.clock.Now()); err != nil {
			return AudioAsset{}, err
		}
		if asset.Version == expectedVersion {
			return asset, nil
		}
		if err := s.repository.Save(ctx, asset, expectedVersion); err == nil {
			return asset, nil
		} else if !errors.Is(err, ErrAudioAssetConcurrentUpdate) &&
			!errors.Is(err, ErrAudioAssetAlreadyBound) {
			return AudioAsset{}, err
		}
		asset, err = s.repository.Get(ctx, asset.ID)
		if err != nil {
			return AudioAsset{}, err
		}
	}
	return AudioAsset{}, ErrAudioAssetConcurrentUpdate
}

func (s *AudioAssetService) Playback(
	ctx context.Context,
	actor AudioAssetActor,
	assetID string,
) (objectstore.SignedGetResult, error) {
	asset, err := s.ownedAsset(ctx, actor, assetID)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if asset.Status != AudioAssetReadable {
		return objectstore.SignedGetResult{}, ErrAudioAssetInvalidTransition
	}
	result, err := s.store.SignedGet(ctx, asset.ObjectKey)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	now := s.clock.Now()
	if !result.ExpiresAt.After(now) || result.ExpiresAt.After(now.Add(MaxPlaybackURLTTL)) {
		return objectstore.SignedGetResult{}, ErrAudioAssetPlaybackTTL
	}
	signedURL, err := url.Parse(result.URL)
	if err != nil || !strings.EqualFold(signedURL.Scheme, "https") || signedURL.Host == "" {
		return objectstore.SignedGetResult{}, ErrAudioAssetPlaybackURL
	}
	return result, nil
}

// Delete persists deleting before touching object storage. If object deletion
// fails, the stored state remains deleting and the same call can be retried.
func (s *AudioAssetService) Delete(
	ctx context.Context,
	actor AudioAssetActor,
	assetID string,
) (AudioAsset, error) {
	asset, err := s.ownedAsset(ctx, actor, assetID)
	if err != nil {
		return AudioAsset{}, err
	}
	if asset.Status == AudioAssetDeleted {
		return asset, nil
	}
	return s.deleteAsset(ctx, asset)
}

// ReclaimExpired removes expired, Turn-less staged or metadata-committed
// uploads and retries prior failed deletes.
func (s *AudioAssetService) ReclaimExpired(
	ctx context.Context,
	limit int,
) (AudioAssetCleanupResult, error) {
	if limit <= 0 {
		limit = defaultCleanupLimit
	}
	expired, err := s.repository.ListExpiredUnconfirmed(ctx, s.clock.Now(), limit)
	if err != nil {
		return AudioAssetCleanupResult{}, err
	}
	deleting, err := s.repository.ListDeleting(ctx, limit)
	if err != nil {
		return AudioAssetCleanupResult{}, err
	}

	result := AudioAssetCleanupResult{}
	seen := make(map[string]struct{}, len(expired)+len(deleting))
	for _, asset := range append(expired, deleting...) {
		if _, found := seen[asset.ID]; found {
			continue
		}
		seen[asset.ID] = struct{}{}
		if _, err := s.deleteAsset(ctx, asset); err != nil {
			result.Failed++
			continue
		}
		result.Deleted++
	}
	return result, nil
}

// CleanupAccountData is Conversation's module boundary for account-data
// cleanup. Identity orchestration calls it with a trusted actor; this service
// owns only the deletion of that actor's AudioAssets.
func (s *AudioAssetService) CleanupAccountData(
	ctx context.Context,
	actor AudioAssetActor,
	limit int,
) (AudioAssetCleanupResult, error) {
	ownerID := strings.TrimSpace(actor.UserID)
	if ownerID == "" {
		return AudioAssetCleanupResult{}, ErrAudioAssetInvalid
	}
	if limit <= 0 {
		limit = defaultCleanupLimit
	}
	assets, err := s.repository.ListOwnerAssetsForAccountCleanup(ctx, ownerID, limit)
	if err != nil {
		return AudioAssetCleanupResult{}, err
	}

	result := AudioAssetCleanupResult{}
	for _, asset := range assets {
		// Defend the ownership boundary even if a Repository implementation
		// accidentally returns a row outside the requested owner.
		if asset.OwnerID != ownerID {
			result.Failed++
			continue
		}
		if _, err := s.deleteAsset(ctx, asset); err != nil {
			result.Failed++
			continue
		}
		result.Deleted++
	}
	return result, nil
}

func (s *AudioAssetService) ownedAsset(
	ctx context.Context,
	actor AudioAssetActor,
	assetID string,
) (AudioAsset, error) {
	if strings.TrimSpace(assetID) == "" {
		return AudioAsset{}, ErrAudioAssetInvalid
	}
	asset, err := s.repository.Get(ctx, assetID)
	if err != nil {
		return AudioAsset{}, err
	}
	if err := asset.ownedBy(strings.TrimSpace(actor.UserID)); err != nil {
		return AudioAsset{}, err
	}
	return asset, nil
}

func (s *AudioAssetService) deleteAsset(
	ctx context.Context,
	asset AudioAsset,
) (AudioAsset, error) {
	var err error
	for range maxConflictReloads {
		if asset.Status == AudioAssetDeleted {
			return asset, nil
		}
		if asset.Status == AudioAssetDeleting {
			break
		}
		expectedVersion := asset.Version
		if err := asset.beginDeleting(s.clock.Now()); err != nil {
			return AudioAsset{}, err
		}
		if err := s.repository.Save(ctx, asset, expectedVersion); err == nil {
			break
		} else if !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
			return AudioAsset{}, err
		}
		asset, err = s.repository.Get(ctx, asset.ID)
		if err != nil {
			return AudioAsset{}, err
		}
	}
	if asset.Status != AudioAssetDeleting {
		return AudioAsset{}, ErrAudioAssetConcurrentUpdate
	}
	if err := s.store.Delete(ctx, asset.ObjectKey); err != nil {
		return asset, fmt.Errorf("delete audio object: %w", err)
	}

	for range maxConflictReloads {
		if asset.Status == AudioAssetDeleted {
			return asset, nil
		}
		expectedVersion := asset.Version
		if err := asset.finishDeleting(s.clock.Now()); err != nil {
			return AudioAsset{}, err
		}
		if err := s.repository.Save(ctx, asset, expectedVersion); err == nil {
			return asset, nil
		} else if !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
			return AudioAsset{}, err
		}
		asset, err = s.repository.Get(ctx, asset.ID)
		if err != nil {
			return AudioAsset{}, err
		}
	}
	return AudioAsset{}, ErrAudioAssetConcurrentUpdate
}

func (s *AudioAssetService) commitUploadedMetadata(
	ctx context.Context,
	asset AudioAsset,
	request UploadRecordingRequest,
	etag string,
) (AudioAsset, error) {
	for range maxConflictReloads {
		if !asset.sameUpload(request) {
			return AudioAsset{}, ErrAudioAssetIdempotencyConflict
		}
		switch asset.Status {
		case AudioAssetMetadataCommitted, AudioAssetReadable:
			return asset, nil
		case AudioAssetStaged:
		default:
			return AudioAsset{}, ErrAudioAssetInvalidTransition
		}

		expectedVersion := asset.Version
		if err := asset.commitMetadata(etag, s.clock.Now()); err != nil {
			return AudioAsset{}, err
		}
		if err := s.repository.Save(ctx, asset, expectedVersion); err == nil {
			return asset, nil
		} else if !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
			return AudioAsset{}, err
		}
		var err error
		asset, err = s.repository.Get(ctx, asset.ID)
		if err != nil {
			return AudioAsset{}, err
		}
	}
	return AudioAsset{}, ErrAudioAssetConcurrentUpdate
}

func nilDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
