package conversation

import (
	"context"
	"time"
)

// AudioAssetUploadClaim is an exclusive, time-bounded right to write one
// staged object's bytes and commit its uploaded metadata. A later claimant
// increments FencingToken so an earlier worker cannot commit after takeover.
type AudioAssetUploadClaim struct {
	Asset          AudioAsset
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

// AudioAssetUploadRepository provides the PostgreSQL write barrier around an
// object-store Put. The Put context must have a stricter deadline than the
// lease so cleanup never overlaps a worker that may still legitimately commit.
// ReleaseUploadClaim is retry-idempotent for the same owner, asset, and fencing
// token; it advances Version only when it clears a held lease.
type AudioAssetUploadRepository interface {
	ClaimUpload(
		ctx context.Context,
		ownerID string,
		uploadRequestID string,
		leaseDuration time.Duration,
	) (AudioAssetUploadClaim, error)
	CommitUploadClaim(
		ctx context.Context,
		asset AudioAsset,
		expectedVersion uint64,
		fencingToken uint64,
	) error
	ReleaseUploadClaim(
		ctx context.Context,
		ownerID string,
		audioAssetID string,
		fencingToken uint64,
	) error
}

// AudioAssetLifecycleRepository is the only persistence port accepted by the
// service. Requiring both upload and cleanup claims prevents production wiring
// from silently falling back to unsafe List-then-Save lifecycle transitions.
type AudioAssetLifecycleRepository interface {
	AudioAssetCleanupRepository
	AudioAssetUploadRepository
}
