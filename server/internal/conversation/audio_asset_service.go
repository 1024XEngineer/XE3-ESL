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
	defaultCleanupLimit = 4
	// Upload is bounded below its write lease. A claim-aware Repository keeps
	// cleanup and an active object write mutually exclusive with database time.
	audioUploadOperationTimeout = 2 * time.Minute
	defaultUploadLease          = 5 * time.Minute
	defaultCleanupLease         = 5 * time.Minute
	maxConflictReloads          = 3
	uploadClaimReloadAttempts   = 10
	uploadClaimReloadDelay      = 5 * time.Millisecond
	_                           = uint64(defaultUploadLease - audioUploadOperationTimeout - 1)
)

// AudioAssetRepository persists metadata only. Create maps an ID or
// (owner, upload request) uniqueness race to ErrAudioAssetConcurrentUpdate.
// Save atomically compares expectedVersion and maps a stale version to
// ErrAudioAssetConcurrentUpdate. Database uniqueness constraints must map
// either an (owner, Candidate) or (owner, Turn) binding race to
// ErrAudioAssetAlreadyBound.
type AudioAssetRepository interface {
	Create(context.Context, AudioAsset) error
	GetOwned(context.Context, string, string) (AudioAsset, error)
	GetByUploadRequest(context.Context, string, string) (AudioAsset, error)
	GetByCandidate(context.Context, string, string) (AudioAsset, error)
	GetByTurn(context.Context, string, string) (AudioAsset, error)
	Save(context.Context, AudioAsset, uint64) error
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
	Purged  int
	Pending bool
}

type AudioAssetService struct {
	repository AudioAssetLifecycleRepository
	store      objectstore.Store
	ids        AudioAssetIDGenerator
	clock      AudioAssetClock
	turns      AudioAssetTurnVerifier
	reclaimer  *AudioAssetReclaimer
	stagedTTL  time.Duration
}

func NewAudioAssetService(
	repository AudioAssetLifecycleRepository,
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
	reclaimer, err := NewAudioAssetReclaimer(repository, store, clock)
	if err != nil {
		return nil, err
	}
	return &AudioAssetService{
		repository: repository,
		store:      store,
		ids:        ids,
		clock:      clock,
		turns:      turns,
		reclaimer:  reclaimer,
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
	rawOwnerID := actor.UserID
	ownerID := strings.TrimSpace(rawOwnerID)
	requestID := strings.TrimSpace(request.RequestID)
	if rawOwnerID != ownerID ||
		!validAudioAssetIdentifier(ownerID) ||
		!validAudioAssetIdentifier(requestID) ||
		request.Body == nil {
		return AudioAsset{}, ErrAudioAssetInvalid
	}

	asset, err := s.repository.GetByUploadRequest(ctx, ownerID, requestID)
	switch {
	case err == nil:
		if !asset.sameUpload(request) {
			return AudioAsset{}, ErrAudioAssetIdempotencyConflict
		}
		switch asset.Status {
		case AudioAssetMetadataCommitted, AudioAssetReadable:
			return asset, nil
		case AudioAssetStaged:
		case AudioAssetDeleting, AudioAssetDeleted:
			return AudioAsset{}, ErrAudioAssetUploadTerminated
		default:
			return AudioAsset{}, ErrAudioAssetInvalidTransition
		}
	case errors.Is(err, ErrAudioAssetNotFound):
		asset, err = s.createStaged(ctx, ownerID, request)
		if err != nil {
			return AudioAsset{}, err
		}
		if !asset.sameUpload(request) {
			return AudioAsset{}, ErrAudioAssetIdempotencyConflict
		}
		switch asset.Status {
		case AudioAssetMetadataCommitted, AudioAssetReadable:
			return asset, nil
		case AudioAssetStaged:
		case AudioAssetDeleting, AudioAssetDeleted:
			return AudioAsset{}, ErrAudioAssetUploadTerminated
		default:
			return AudioAsset{}, ErrAudioAssetInvalidTransition
		}
	default:
		return AudioAsset{}, err
	}

	uploadCtx, cancelUpload := context.WithTimeout(
		ctx,
		audioUploadOperationTimeout,
	)
	defer cancelUpload()
	claim, err := s.repository.ClaimUpload(
		uploadCtx,
		ownerID,
		requestID,
		defaultUploadLease,
	)
	if err != nil {
		if errors.Is(err, ErrAudioAssetConcurrentUpdate) {
			return s.waitForConcurrentUpload(
				uploadCtx,
				ownerID,
				requestID,
				request,
			)
		}
		return AudioAsset{}, err
	}
	asset = claim.Asset
	if asset.OwnerID != ownerID ||
		asset.UploadRequestID != requestID ||
		!asset.sameUpload(request) ||
		asset.Status != AudioAssetStaged ||
		claim.FencingToken == 0 ||
		claim.LeaseExpiresAt.IsZero() {
		return AudioAsset{}, ErrAudioAssetInvalid
	}
	if err := uploadCtx.Err(); err != nil {
		return AudioAsset{}, err
	}

	putResult, err := s.store.Put(uploadCtx, objectstore.PutRequest{
		Key:            asset.ObjectKey,
		Body:           request.Body,
		Size:           asset.Size,
		ContentType:    asset.ContentType,
		ChecksumSHA256: asset.ChecksumSHA256,
	})
	if err != nil {
		if safeToReleaseUploadClaim(err) {
			releaseErr := s.repository.ReleaseUploadClaim(
				ctx,
				asset.OwnerID,
				asset.ID,
				claim.FencingToken,
			)
			return AudioAsset{}, errors.Join(err, releaseErr)
		}
		return AudioAsset{}, err
	}

	expectedVersion := asset.Version
	if err := asset.commitMetadata(putResult.ETag, s.clock.Now()); err != nil {
		return AudioAsset{}, err
	}
	if err := s.repository.CommitUploadClaim(
		ctx,
		asset,
		expectedVersion,
		claim.FencingToken,
	); err == nil {
		return asset, nil
	} else {
		if compensationErr := s.compensateLateUploadAfterDeletion(
			ctx,
			asset.OwnerID,
			asset.ID,
		); compensationErr != nil {
			return AudioAsset{}, errors.Join(err, compensationErr)
		}
		return AudioAsset{}, err
	}
}

func (s *AudioAssetService) waitForConcurrentUpload(
	ctx context.Context,
	ownerID string,
	requestID string,
	request UploadRecordingRequest,
) (AudioAsset, error) {
	for attempt := range uploadClaimReloadAttempts {
		current, err := s.repository.GetByUploadRequest(
			ctx,
			ownerID,
			requestID,
		)
		if err != nil {
			return AudioAsset{}, err
		}
		if !current.sameUpload(request) {
			return AudioAsset{}, ErrAudioAssetIdempotencyConflict
		}
		switch current.Status {
		case AudioAssetMetadataCommitted, AudioAssetReadable:
			return current, nil
		case AudioAssetDeleting, AudioAssetDeleted:
			return AudioAsset{}, ErrAudioAssetUploadTerminated
		case AudioAssetStaged:
		default:
			return AudioAsset{}, ErrAudioAssetInvalidTransition
		}
		if attempt == uploadClaimReloadAttempts-1 {
			break
		}
		timer := time.NewTimer(uploadClaimReloadDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return AudioAsset{}, ctx.Err()
		case <-timer.C:
		}
	}
	return AudioAsset{}, ErrAudioAssetConcurrentUpdate
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
	if !validAudioAssetIdentifier(candidateID) ||
		!validAudioAssetIdentifier(turnID) {
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
		asset, err = s.repository.GetOwned(ctx, asset.OwnerID, asset.ID)
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
	return s.reclaimer.ReclaimExpired(ctx, limit)
}

// CleanupAccountData is Conversation's module boundary for account-data
// cleanup. The #72 Identity orchestrator must first atomically mark the account
// deleting, revoke its Sessions, and prevent new Actor-backed writes; otherwise
// a new row could appear after the final Has query. Conversation neither reads
// nor writes Identity tables. The orchestrator retries this method while
// ErrAudioAssetCleanupPending is returned.
func (s *AudioAssetService) CleanupAccountData(
	ctx context.Context,
	actor AudioAssetActor,
	limit int,
) (AudioAssetCleanupResult, error) {
	rawOwnerID := actor.UserID
	ownerID := strings.TrimSpace(rawOwnerID)
	if rawOwnerID != ownerID ||
		!validAudioAssetIdentifier(ownerID) {
		return AudioAssetCleanupResult{}, ErrAudioAssetInvalid
	}
	if limit <= 0 {
		limit = defaultCleanupLimit
	}
	return s.reclaimer.cleanupAccountClaims(ctx, ownerID, limit)
}

func (s *AudioAssetService) ownedAsset(
	ctx context.Context,
	actor AudioAssetActor,
	assetID string,
) (AudioAsset, error) {
	rawOwnerID := actor.UserID
	ownerID := strings.TrimSpace(rawOwnerID)
	rawAssetID := assetID
	assetID = strings.TrimSpace(rawAssetID)
	if rawOwnerID != ownerID ||
		rawAssetID != assetID ||
		!validAudioAssetIdentifier(ownerID) ||
		!validAudioAssetIdentifier(assetID) {
		return AudioAsset{}, ErrAudioAssetInvalid
	}
	asset, err := s.repository.GetOwned(ctx, ownerID, assetID)
	if err != nil {
		return AudioAsset{}, err
	}
	if err := asset.ownedBy(ownerID); err != nil {
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
		asset, err = s.repository.GetOwned(ctx, asset.OwnerID, asset.ID)
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
		asset, err = s.repository.GetOwned(ctx, asset.OwnerID, asset.ID)
		if err != nil {
			return AudioAsset{}, err
		}
	}
	return AudioAsset{}, ErrAudioAssetConcurrentUpdate
}

func isExpiredUnconfirmed(asset AudioAsset, cutoff time.Time) bool {
	unconfirmed := asset.CandidateID == "" &&
		asset.TurnID == "" &&
		(asset.Status == AudioAssetStaged ||
			asset.Status == AudioAssetMetadataCommitted)
	return unconfirmed && !asset.StagedUntil.After(cutoff)
}

// compensateLateUploadAfterDeletion closes the race where cleanup deletes an
// absent object and commits its tombstone while Put is still in flight. A
// successful late Put makes that tombstone stale, so deleted is first reopened
// to deleting with a version check. This keeps a failed compensation visible to
// ClaimDeleting and therefore retryable by ReclaimExpired.
func (s *AudioAssetService) compensateLateUploadAfterDeletion(
	ctx context.Context,
	ownerID string,
	assetID string,
) error {
	asset, err := s.repository.GetOwned(ctx, ownerID, assetID)
	if err != nil {
		return fmt.Errorf("reload audio asset after upload commit failure: %w", err)
	}

	for range maxConflictReloads {
		switch asset.Status {
		case AudioAssetDeleting:
			if _, err := s.deleteAsset(ctx, asset); err != nil {
				return fmt.Errorf("compensate late audio upload: %w", err)
			}
			return nil
		case AudioAssetDeleted:
			expectedVersion := asset.Version
			if err := asset.resumeDeletingForLateObject(s.clock.Now()); err != nil {
				return fmt.Errorf("resume late audio upload cleanup: %w", err)
			}
			if err := s.repository.Save(ctx, asset, expectedVersion); err == nil {
				if _, err := s.deleteAsset(ctx, asset); err != nil {
					return fmt.Errorf("compensate late audio upload: %w", err)
				}
				return nil
			} else if !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
				return fmt.Errorf("persist late audio upload cleanup: %w", err)
			}
			asset, err = s.repository.GetOwned(ctx, asset.OwnerID, asset.ID)
			if err != nil {
				return fmt.Errorf("reload late audio upload cleanup: %w", err)
			}
		default:
			return nil
		}
	}
	return fmt.Errorf("persist late audio upload cleanup: %w", ErrAudioAssetConcurrentUpdate)
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

func safeToReleaseUploadClaim(err error) bool {
	return errors.Is(err, objectstore.ErrInvalidKey) ||
		errors.Is(err, objectstore.ErrInvalidObject)
}
