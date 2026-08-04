package voice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const (
	// Every cleanup batch is bounded below its database lease. With the
	// production batch size of four, the OSS request timeout leaves margin for
	// all claimed objects to finish before another worker may take over.
	audioAssetCleanupOperationTimeout = 4 * time.Minute
	_                                 = uint64(defaultCleanupLease - audioAssetCleanupOperationTimeout - 1)
)

// AudioAssetExpiredReclaimer is the narrow production boundary used by the
// background cleanup worker. It intentionally has no authentication or Turn
// dependency.
type AudioAssetExpiredReclaimer interface {
	ReclaimExpired(context.Context, int) (AudioAssetCleanupResult, error)
}

// AudioAssetReclaimer owns only staged/deleting object cleanup. Keeping it
// separate lets production reclaim durable leftovers before #87 supplies the
// Turn verifier required by the complete AudioAssetService.
type AudioAssetReclaimer struct {
	repository AudioAssetCleanupRepository
	store      objectstore.Store
	clock      AudioAssetClock
}

func NewAudioAssetReclaimer(
	repository AudioAssetCleanupRepository,
	store objectstore.Store,
	clock AudioAssetClock,
) (*AudioAssetReclaimer, error) {
	if nilDependency(repository) ||
		nilDependency(store) ||
		nilDependency(clock) {
		return nil, ErrAudioAssetInvalidDependency
	}
	return &AudioAssetReclaimer{
		repository: repository,
		store:      store,
		clock:      clock,
	}, nil
}

type audioAssetSystemClock struct{}

// NewAudioAssetSystemClock returns the UTC production clock used by the
// composition root. Tests should continue to inject a deterministic clock.
func NewAudioAssetSystemClock() AudioAssetClock {
	return audioAssetSystemClock{}
}

func (audioAssetSystemClock) Now() time.Time {
	return time.Now().UTC()
}

// ReclaimExpired removes expired, Turn-less staged or metadata-committed
// uploads and retries prior failed deletes.
func (r *AudioAssetReclaimer) ReclaimExpired(
	ctx context.Context,
	limit int,
) (AudioAssetCleanupResult, error) {
	limit = boundedAudioAssetCleanupLimit(limit)
	cleanupCtx, cancel := context.WithTimeout(
		ctx,
		audioAssetCleanupOperationTimeout,
	)
	defer cancel()

	// Reserve at least half of every production batch for prior failed
	// deletions so a stream of newly expired uploads cannot starve retries.
	deletingLimit := (limit + 1) / 2
	deleting, err := r.repository.ClaimDeleting(
		cleanupCtx,
		defaultCleanupLease,
		deletingLimit,
	)
	if err != nil {
		return AudioAssetCleanupResult{}, err
	}
	result := r.processCleanupClaims(cleanupCtx, deleting, "")
	remaining := limit - len(deleting)
	if remaining <= 0 {
		return result, nil
	}
	expired, err := r.repository.ClaimExpiredUnconfirmed(
		cleanupCtx,
		defaultCleanupLease,
		remaining,
	)
	if err != nil {
		return result, err
	}
	reclaimed := r.processCleanupClaims(cleanupCtx, expired, "")
	result.Deleted += reclaimed.Deleted
	result.Failed += reclaimed.Failed
	return result, nil
}

func (r *AudioAssetReclaimer) cleanupAccountClaims(
	ctx context.Context,
	ownerID string,
	limit int,
) (AudioAssetCleanupResult, error) {
	limit = boundedAudioAssetCleanupLimit(limit)
	cleanupCtx, cancel := context.WithTimeout(
		ctx,
		audioAssetCleanupOperationTimeout,
	)
	defer cancel()

	claims, err := r.repository.ClaimOwnerAssetsForAccountCleanup(
		cleanupCtx,
		ownerID,
		defaultCleanupLease,
		limit,
	)
	if err != nil {
		return AudioAssetCleanupResult{}, err
	}
	result := r.processCleanupClaims(cleanupCtx, claims, ownerID)
	purged, err := r.repository.PurgeOwnerDeletedAssets(
		cleanupCtx,
		ownerID,
		limit,
	)
	if err != nil {
		return result, err
	}
	result.Purged = purged
	pending, err := r.repository.HasOwnerAssetsForAccountCleanup(
		cleanupCtx,
		ownerID,
	)
	if err != nil {
		return result, err
	}
	result.Pending = pending || result.Failed > 0
	if result.Pending {
		return result, ErrAudioAssetCleanupPending
	}
	return result, nil
}

func boundedAudioAssetCleanupLimit(limit int) int {
	if limit <= 0 || limit > defaultCleanupLimit {
		return defaultCleanupLimit
	}
	return limit
}

func (r *AudioAssetReclaimer) processCleanupClaims(
	ctx context.Context,
	claims []AudioAssetCleanupClaim,
	expectedOwnerID string,
) AudioAssetCleanupResult {
	result := AudioAssetCleanupResult{}
	for _, claim := range claims {
		if expectedOwnerID != "" && claim.Asset.OwnerID != expectedOwnerID {
			result.Failed++
			continue
		}
		deleted, err := r.processCleanupClaim(ctx, claim)
		if err != nil {
			result.Failed++
			continue
		}
		if deleted {
			result.Deleted++
		}
	}
	return result
}

func (r *AudioAssetReclaimer) processCleanupClaim(
	ctx context.Context,
	claim AudioAssetCleanupClaim,
) (bool, error) {
	asset := claim.Asset
	if asset.Status != AudioAssetDeleting ||
		claim.FencingToken == 0 ||
		claim.LeaseExpiresAt.IsZero() {
		return false, ErrAudioAssetInvalid
	}
	if err := r.store.Delete(ctx, asset.ObjectKey); err != nil {
		releaseErr := r.repository.ReleaseCleanupClaim(
			ctx,
			asset.OwnerID,
			asset.ID,
			claim.FencingToken,
		)
		return false, errors.Join(
			fmt.Errorf("delete audio object: %w", err),
			releaseErr,
		)
	}

	expectedVersion := asset.Version
	if err := asset.finishDeleting(r.clock.Now()); err != nil {
		return false, err
	}
	if err := r.repository.SaveCleanupClaim(
		ctx,
		asset,
		expectedVersion,
		claim.FencingToken,
	); err != nil {
		_ = r.repository.ReleaseCleanupClaim(
			ctx,
			asset.OwnerID,
			asset.ID,
			claim.FencingToken,
		)
		return false, err
	}
	return true, nil
}
