package conversation

import (
	"context"
	"time"
)

// AudioAssetCleanupClaim is an exclusive, time-bounded right to delete one
// object's bytes and advance its durable metadata. FencingToken rejects work
// from a worker whose lease expired and was subsequently taken over.
type AudioAssetCleanupClaim struct {
	Asset          AudioAsset
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

// AudioAssetCleanupRepository extends metadata persistence with worker-safe
// cleanup claims. Claim methods use database time, atomically transition
// eligible assets to deleting, and increment both Version and FencingToken.
//
// SaveCleanupClaim may finish a live claim or safely reconcile an identical
// retry after the first response was lost. ReleaseCleanupClaim makes a failed
// object deletion immediately retryable without waiting for lease expiry and
// is retry-idempotent for the same owner, asset, and fencing token. The regular
// AudioAssetRepository methods remain available for request-path operations.
type AudioAssetCleanupRepository interface {
	AudioAssetRepository
	ClaimExpiredUnconfirmed(
		ctx context.Context,
		leaseDuration time.Duration,
		limit int,
	) ([]AudioAssetCleanupClaim, error)
	ClaimDeleting(
		ctx context.Context,
		leaseDuration time.Duration,
		limit int,
	) ([]AudioAssetCleanupClaim, error)
	ClaimOwnerAssetsForAccountCleanup(
		ctx context.Context,
		ownerID string,
		leaseDuration time.Duration,
		limit int,
	) ([]AudioAssetCleanupClaim, error)
	SaveCleanupClaim(
		ctx context.Context,
		asset AudioAsset,
		expectedVersion uint64,
		fencingToken uint64,
	) error
	ReleaseCleanupClaim(
		ctx context.Context,
		ownerID string,
		audioAssetID string,
		fencingToken uint64,
	) error
	HasOwnerAssetsForAccountCleanup(
		ctx context.Context,
		ownerID string,
	) (bool, error)
	PurgeOwnerDeletedAssets(
		ctx context.Context,
		ownerID string,
		limit int,
	) (int, error)
}
