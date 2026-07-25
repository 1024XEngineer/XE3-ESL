package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

const (
	audioTestOwnerA = "10000000-0000-4000-8000-000000000001"
	audioTestOwnerB = "10000000-0000-4000-8000-000000000002"
)

func TestNewAudioAssetRepositoryRejectsNilPool(t *testing.T) {
	t.Parallel()

	if _, err := NewAudioAssetRepository(nil); !errors.Is(
		err,
		conversation.ErrAudioAssetInvalidDependency,
	) {
		t.Fatalf("NewAudioAssetRepository(nil) error = %v", err)
	}
}

func TestAudioAssetIdentifierValidationUsesUTF8Bytes(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		value string
		valid bool
	}{
		"ASCII boundary": {value: strings.Repeat("a", 128), valid: true},
		"ASCII too long": {value: strings.Repeat("a", 129), valid: false},
		"UTF-8 boundary": {value: strings.Repeat("界", 42), valid: true},
		"UTF-8 too long": {value: strings.Repeat("界", 43), valid: false},
		"invalid UTF-8":  {value: string([]byte{0xff}), valid: false},
		"outer spaces":   {value: " asset-id ", valid: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := validAudioAssetIdentifier(testCase.value); got != testCase.valid {
				t.Fatalf(
					"validAudioAssetIdentifier(%q) = %t, want %t",
					testCase.value,
					got,
					testCase.valid,
				)
			}
		})
	}
}

func TestAudioAssetRepositoryRejectsPaddedOpaqueIdentifiers(t *testing.T) {
	repository, _ := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	asset := newTestAudioAsset(
		"asset-exact-identifiers",
		audioTestOwnerA,
		"upload-exact-identifiers",
		now,
		now.Add(time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create exact-identifier fixture: %v", err)
	}

	paddedOwner := " " + audioTestOwnerA
	paddedAsset := asset.ID + " "
	paddedRequest := " " + asset.UploadRequestID
	paddedCandidate := " candidate-exact "
	paddedTurn := " turn-exact "
	tests := map[string]func() error{
		"GetOwned owner": func() error {
			_, err := repository.GetOwned(ctx, paddedOwner, asset.ID)
			return err
		},
		"GetOwned asset": func() error {
			_, err := repository.GetOwned(ctx, audioTestOwnerA, paddedAsset)
			return err
		},
		"GetByUploadRequest owner": func() error {
			_, err := repository.GetByUploadRequest(
				ctx,
				paddedOwner,
				asset.UploadRequestID,
			)
			return err
		},
		"GetByUploadRequest request": func() error {
			_, err := repository.GetByUploadRequest(
				ctx,
				audioTestOwnerA,
				paddedRequest,
			)
			return err
		},
		"GetByCandidate owner": func() error {
			_, err := repository.GetByCandidate(
				ctx,
				paddedOwner,
				"candidate-exact",
			)
			return err
		},
		"GetByCandidate candidate": func() error {
			_, err := repository.GetByCandidate(
				ctx,
				audioTestOwnerA,
				paddedCandidate,
			)
			return err
		},
		"GetByTurn owner": func() error {
			_, err := repository.GetByTurn(
				ctx,
				paddedOwner,
				"turn-exact",
			)
			return err
		},
		"GetByTurn turn": func() error {
			_, err := repository.GetByTurn(
				ctx,
				audioTestOwnerA,
				paddedTurn,
			)
			return err
		},
		"ClaimUpload owner": func() error {
			_, err := repository.ClaimUpload(
				ctx,
				paddedOwner,
				asset.UploadRequestID,
				time.Minute,
			)
			return err
		},
		"ClaimUpload request": func() error {
			_, err := repository.ClaimUpload(
				ctx,
				audioTestOwnerA,
				paddedRequest,
				time.Minute,
			)
			return err
		},
		"ReleaseUpload owner": func() error {
			return repository.ReleaseUploadClaim(
				ctx,
				paddedOwner,
				asset.ID,
				1,
			)
		},
		"ReleaseUpload asset": func() error {
			return repository.ReleaseUploadClaim(
				ctx,
				audioTestOwnerA,
				paddedAsset,
				1,
			)
		},
		"ClaimOwnerAssets": func() error {
			_, err := repository.ClaimOwnerAssetsForAccountCleanup(
				ctx,
				paddedOwner,
				time.Minute,
				1,
			)
			return err
		},
		"ReleaseCleanup owner": func() error {
			return repository.ReleaseCleanupClaim(
				ctx,
				paddedOwner,
				asset.ID,
				1,
			)
		},
		"ReleaseCleanup asset": func() error {
			return repository.ReleaseCleanupClaim(
				ctx,
				audioTestOwnerA,
				paddedAsset,
				1,
			)
		},
		"HasOwnerAssets": func() error {
			_, err := repository.HasOwnerAssetsForAccountCleanup(
				ctx,
				paddedOwner,
			)
			return err
		},
		"PurgeOwnerAssets": func() error {
			_, err := repository.PurgeOwnerDeletedAssets(
				ctx,
				paddedOwner,
				1,
			)
			return err
		},
	}
	for name, invoke := range tests {
		t.Run(name, func(t *testing.T) {
			if err := invoke(); !errors.Is(
				err,
				conversation.ErrAudioAssetInvalid,
			) {
				t.Fatalf("padded identifier error = %v", err)
			}
		})
	}

	persisted, err := repository.GetOwned(ctx, audioTestOwnerA, asset.ID)
	if err != nil {
		t.Fatalf("load fixture after padded calls: %v", err)
	}
	if persisted.Status != conversation.AudioAssetStaged ||
		persisted.Version != 1 {
		t.Fatalf("padded calls mutated fixture = %#v", persisted)
	}

	paddedObjectKey := asset
	paddedObjectKey.ID = "asset-padded-object-key"
	paddedObjectKey.UploadRequestID = "upload-padded-object-key"
	paddedObjectKey.ObjectKey = " " + paddedObjectKey.ObjectKey
	if err := repository.Create(ctx, paddedObjectKey); !errors.Is(
		err,
		conversation.ErrAudioAssetInvalid,
	) {
		t.Fatalf("padded object key Create error = %v", err)
	}
}

func TestAudioAssetRepositoryPersistsLifecycleAndOwnerScope(
	t *testing.T,
) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)

	alice := newTestAudioAsset(
		"asset-alice-primary",
		audioTestOwnerA,
		"upload-primary",
		databaseNow,
		databaseNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, alice); err != nil {
		t.Fatalf("Create Alice asset: %v", err)
	}

	restarted, err := NewAudioAssetRepository(pool)
	if err != nil {
		t.Fatalf("create restarted repository: %v", err)
	}
	recovered, err := restarted.GetByUploadRequest(
		ctx,
		audioTestOwnerA,
		alice.UploadRequestID,
	)
	if err != nil {
		t.Fatalf("recover asset after repository restart: %v", err)
	}
	if recovered.ID != alice.ID ||
		recovered.OwnerID != audioTestOwnerA ||
		recovered.Status != conversation.AudioAssetStaged ||
		recovered.ObjectKey != alice.ObjectKey {
		t.Fatalf("recovered asset = %#v", recovered)
	}
	if _, err := restarted.GetOwned(
		ctx,
		audioTestOwnerB,
		alice.ID,
	); !errors.Is(err, conversation.ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner GetOwned error = %v", err)
	}
	if _, err := restarted.GetByUploadRequest(
		ctx,
		audioTestOwnerB,
		alice.UploadRequestID,
	); !errors.Is(err, conversation.ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner GetByUploadRequest error = %v", err)
	}

	duplicateRequest := newTestAudioAsset(
		"asset-alice-duplicate-request",
		audioTestOwnerA,
		alice.UploadRequestID,
		databaseNow,
		databaseNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, duplicateRequest); !errors.Is(
		err,
		conversation.ErrAudioAssetConcurrentUpdate,
	) {
		t.Fatalf("duplicate upload request error = %v", err)
	}
	duplicateObject := newTestAudioAsset(
		"asset-bob-duplicate-object",
		audioTestOwnerB,
		"upload-bob-object",
		databaseNow,
		databaseNow.Add(time.Hour),
	)
	duplicateObject.ObjectKey = alice.ObjectKey
	if err := repository.Create(ctx, duplicateObject); !errors.Is(
		err,
		conversation.ErrAudioAssetConcurrentUpdate,
	) {
		t.Fatalf("duplicate object key error = %v", err)
	}

	alice.Status = conversation.AudioAssetMetadataCommitted
	alice.ETag = "etag-alice"
	alice.UpdatedAt = databaseNow.Add(time.Minute)
	alice.Version++
	if err := repository.Save(ctx, alice, 1); err != nil {
		t.Fatalf("commit uploaded metadata: %v", err)
	}
	if err := repository.Save(ctx, alice, 1); !errors.Is(
		err,
		conversation.ErrAudioAssetConcurrentUpdate,
	) {
		t.Fatalf("stale metadata Save error = %v", err)
	}

	alice.CandidateID = "candidate-shared"
	alice.TurnID = "turn-shared"
	alice.Status = conversation.AudioAssetReadable
	alice.UpdatedAt = databaseNow.Add(2 * time.Minute)
	alice.Version++
	if err := repository.Save(ctx, alice, 2); err != nil {
		t.Fatalf("bind readable asset: %v", err)
	}

	byTurn, err := restarted.GetByTurn(
		ctx,
		audioTestOwnerA,
		alice.TurnID,
	)
	if err != nil || byTurn.ID != alice.ID {
		t.Fatalf("GetByTurn = %#v, %v", byTurn, err)
	}
	if _, err := restarted.GetByTurn(
		ctx,
		audioTestOwnerB,
		alice.TurnID,
	); !errors.Is(err, conversation.ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner GetByTurn error = %v", err)
	}

	conflictingBinding := newTestAudioAsset(
		"asset-alice-binding-conflict",
		audioTestOwnerA,
		"upload-binding-conflict",
		databaseNow,
		databaseNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, conflictingBinding); err != nil {
		t.Fatalf("Create binding-conflict fixture: %v", err)
	}
	conflictingBinding.Status = conversation.AudioAssetMetadataCommitted
	conflictingBinding.ETag = "etag-conflict"
	conflictingBinding.UpdatedAt = databaseNow.Add(time.Minute)
	conflictingBinding.Version++
	if err := repository.Save(ctx, conflictingBinding, 1); err != nil {
		t.Fatalf("commit binding-conflict fixture: %v", err)
	}
	conflictingBinding.CandidateID = alice.CandidateID
	conflictingBinding.TurnID = "turn-other"
	conflictingBinding.Status = conversation.AudioAssetReadable
	conflictingBinding.UpdatedAt = databaseNow.Add(2 * time.Minute)
	conflictingBinding.Version++
	if err := repository.Save(
		ctx,
		conflictingBinding,
		2,
	); !errors.Is(err, conversation.ErrAudioAssetAlreadyBound) {
		t.Fatalf("duplicate candidate binding error = %v", err)
	}

	bob := newTestAudioAsset(
		"asset-bob-same-business-ids",
		audioTestOwnerB,
		"upload-primary",
		databaseNow,
		databaseNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, bob); err != nil {
		t.Fatalf("same request ID under Bob: %v", err)
	}
	bob.Status = conversation.AudioAssetMetadataCommitted
	bob.ETag = "etag-bob"
	bob.UpdatedAt = databaseNow.Add(time.Minute)
	bob.Version++
	if err := repository.Save(ctx, bob, 1); err != nil {
		t.Fatalf("commit Bob metadata: %v", err)
	}
	bob.CandidateID = alice.CandidateID
	bob.TurnID = alice.TurnID
	bob.Status = conversation.AudioAssetReadable
	bob.UpdatedAt = databaseNow.Add(2 * time.Minute)
	bob.Version++
	if err := repository.Save(ctx, bob, 2); err != nil {
		t.Fatalf("owner-scoped binding IDs should coexist: %v", err)
	}
}

func TestAudioAssetRepositoryConcurrentCreateIsExactlyOnce(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	restarted, err := NewAudioAssetRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	databaseNow := audioAssetDatabaseNow(t, pool)
	asset := newTestAudioAsset(
		"asset-concurrent-create",
		audioTestOwnerA,
		"upload-concurrent-create",
		databaseNow,
		databaseNow.Add(time.Hour),
	)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for _, candidateRepository := range []*AudioAssetRepository{
		repository,
		restarted,
	} {
		group.Add(1)
		go func(repository *AudioAssetRepository) {
			defer group.Done()
			<-start
			errs <- repository.Create(context.Background(), asset)
		}(candidateRepository)
	}
	close(start)
	group.Wait()
	close(errs)

	var created int
	var conflicted int
	for err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate):
			conflicted++
		default:
			t.Errorf("concurrent Create error = %v", err)
		}
	}
	if created != 1 || conflicted != 1 {
		t.Fatalf("concurrent Create outcomes = created %d, conflicted %d", created, conflicted)
	}

	var count int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*)
		 FROM conversation_audio_assets
		 WHERE owner_user_id = $1 AND upload_request_id = $2`,
		asset.OwnerID,
		asset.UploadRequestID,
	).Scan(&count); err != nil {
		t.Fatalf("count concurrent assets: %v", err)
	}
	if count != 1 {
		t.Fatalf("concurrent asset count = %d, want 1", count)
	}
}

func TestAudioAssetCreateUsesDatabaseClockAndBoundsStagedTTL(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseBefore := audioAssetDatabaseNow(t, pool)
	applicationNow := databaseBefore.Add(-48 * time.Hour)
	asset := newTestAudioAsset(
		"asset-database-clock-create",
		audioTestOwnerA,
		"upload-database-clock-create",
		applicationNow,
		applicationNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create behind-clock asset: %v", err)
	}
	databaseAfter := audioAssetDatabaseNow(t, pool)
	persisted, err := repository.GetOwned(ctx, audioTestOwnerA, asset.ID)
	if err != nil {
		t.Fatalf("load behind-clock asset: %v", err)
	}
	if persisted.CreatedAt.Before(databaseBefore) ||
		persisted.CreatedAt.After(databaseAfter) ||
		!persisted.UpdatedAt.Equal(persisted.CreatedAt) ||
		persisted.StagedUntil.Sub(persisted.CreatedAt) != time.Hour {
		t.Fatalf("database-clock timestamps = %#v", persisted)
	}
	expired, err := repository.ClaimExpiredUnconfirmed(ctx, time.Minute, 1)
	if err != nil {
		t.Fatalf("ClaimExpiredUnconfirmed behind-clock asset: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("new behind-clock asset was immediately expired: %#v", expired)
	}
	uploadClaim, err := repository.ClaimUpload(
		ctx,
		audioTestOwnerA,
		asset.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimUpload behind-clock asset: %v", err)
	}
	if !uploadClaim.Asset.CreatedAt.Equal(persisted.CreatedAt) ||
		!uploadClaim.Asset.StagedUntil.Equal(persisted.StagedUntil) {
		t.Fatalf("ClaimUpload did not return database timestamps: %#v", uploadClaim)
	}

	subMicrosecond := newTestAudioAsset(
		"asset-sub-microsecond-ttl",
		audioTestOwnerA,
		"upload-sub-microsecond-ttl",
		databaseAfter,
		databaseAfter.Add(time.Nanosecond),
	)
	if err := repository.Create(ctx, subMicrosecond); !errors.Is(
		err,
		conversation.ErrAudioAssetInvalid,
	) {
		t.Fatalf("sub-microsecond staged TTL error = %v", err)
	}
	overMaximum := newTestAudioAsset(
		"asset-over-maximum-ttl",
		audioTestOwnerA,
		"upload-over-maximum-ttl",
		databaseAfter,
		databaseAfter.Add(maxAudioAssetStagedTTL+time.Microsecond),
	)
	if err := repository.Create(ctx, overMaximum); !errors.Is(
		err,
		conversation.ErrAudioAssetInvalid,
	) {
		t.Fatalf("over-maximum staged TTL error = %v", err)
	}
	atMaximum := newTestAudioAsset(
		"asset-at-maximum-ttl",
		audioTestOwnerA,
		"upload-at-maximum-ttl",
		databaseAfter,
		databaseAfter.Add(maxAudioAssetStagedTTL),
	)
	if err := repository.Create(ctx, atMaximum); err != nil {
		t.Fatalf("maximum staged TTL Create: %v", err)
	}
	maximumPersisted, err := repository.GetOwned(
		ctx,
		audioTestOwnerA,
		atMaximum.ID,
	)
	if err != nil ||
		maximumPersisted.StagedUntil.Sub(maximumPersisted.CreatedAt) !=
			maxAudioAssetStagedTTL {
		t.Fatalf("maximum staged TTL persisted = %#v, %v", maximumPersisted, err)
	}
}

func TestAudioAssetUploadClaimFencesWritesAndBlocksCleanup(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	asset := newTestAudioAsset(
		"asset-upload-lease",
		audioTestOwnerA,
		"upload-write-barrier",
		databaseNow.Add(-2*time.Hour),
		databaseNow.Add(-time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create upload-lease fixture: %v", err)
	}

	first, err := repository.ClaimUpload(
		ctx,
		audioTestOwnerA,
		asset.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("first ClaimUpload: %v", err)
	}
	if first.Asset.ID != asset.ID ||
		first.Asset.Status != conversation.AudioAssetStaged ||
		first.Asset.Version != 2 ||
		first.FencingToken != 1 ||
		!first.LeaseExpiresAt.After(databaseNow) {
		t.Fatalf("first upload claim = %#v", first)
	}
	if _, err := repository.ClaimUpload(
		ctx,
		audioTestOwnerA,
		asset.UploadRequestID,
		time.Minute,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("active upload lease ClaimUpload error = %v", err)
	}

	pending, err := repository.HasOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerA,
	)
	if err != nil || !pending {
		t.Fatalf("Alice pending cleanup = %t, %v", pending, err)
	}
	pending, err = repository.HasOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerB,
	)
	if err != nil || pending {
		t.Fatalf("Bob pending cleanup = %t, %v", pending, err)
	}
	expiredClaims, err := repository.ClaimExpiredUnconfirmed(
		ctx,
		time.Minute,
		10,
	)
	if err != nil || len(expiredClaims) != 0 {
		t.Fatalf("cleanup overlapped active upload = %#v, %v", expiredClaims, err)
	}
	ownerClaims, err := repository.ClaimOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerA,
		time.Minute,
		10,
	)
	if err != nil || len(ownerClaims) != 0 {
		t.Fatalf("account cleanup overlapped active upload = %#v, %v", ownerClaims, err)
	}

	ordinaryCommit := first.Asset
	ordinaryCommit.Status = conversation.AudioAssetMetadataCommitted
	ordinaryCommit.ETag = "etag-ordinary-save"
	ordinaryCommit.UpdatedAt = audioAssetDatabaseNow(t, pool)
	ordinaryCommit.Version++
	if err := repository.Save(
		ctx,
		ordinaryCommit,
		first.Asset.Version,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("ordinary Save bypassed active upload lease: %v", err)
	}
	if err := repository.CommitUploadClaim(
		ctx,
		ordinaryCommit,
		first.Asset.Version,
		first.FencingToken+1,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("wrong upload fence commit error = %v", err)
	}
	if err := repository.ReleaseUploadClaim(
		ctx,
		audioTestOwnerB,
		first.Asset.ID,
		first.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner ReleaseUploadClaim error = %v", err)
	}
	if err := repository.ReleaseUploadClaim(
		ctx,
		audioTestOwnerA,
		first.Asset.ID,
		first.FencingToken+1,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("wrong upload fence release error = %v", err)
	}
	if err := repository.ReleaseUploadClaim(
		ctx,
		audioTestOwnerA,
		first.Asset.ID,
		first.FencingToken,
	); err != nil {
		t.Fatalf("release definitely-not-started Put claim: %v", err)
	}
	releasedUpload, err := repository.GetOwned(
		ctx,
		audioTestOwnerA,
		first.Asset.ID,
	)
	if err != nil {
		t.Fatalf("load released upload claim: %v", err)
	}
	if err := repository.ReleaseUploadClaim(
		ctx,
		audioTestOwnerA,
		first.Asset.ID,
		first.FencingToken,
	); err != nil {
		t.Fatalf("retry after lost ReleaseUploadClaim response: %v", err)
	}
	replayedUploadRelease, err := repository.GetOwned(
		ctx,
		audioTestOwnerA,
		first.Asset.ID,
	)
	if err != nil {
		t.Fatalf("load replayed upload release: %v", err)
	}
	if replayedUploadRelease.Version != releasedUpload.Version ||
		!replayedUploadRelease.UpdatedAt.Equal(releasedUpload.UpdatedAt) {
		t.Fatalf(
			"upload release replay mutated asset from %#v to %#v",
			releasedUpload,
			replayedUploadRelease,
		)
	}
	staleAfterRelease := first.Asset
	staleAfterRelease.Status = conversation.AudioAssetMetadataCommitted
	staleAfterRelease.ETag = "etag-stale-after-release"
	staleAfterRelease.UpdatedAt = audioAssetDatabaseNow(t, pool)
	staleAfterRelease.Version++
	if err := repository.Save(
		ctx,
		staleAfterRelease,
		first.Asset.Version,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("upload release did not fence stale Save: %v", err)
	}

	second, err := repository.ClaimUpload(
		ctx,
		audioTestOwnerA,
		asset.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("second ClaimUpload after release: %v", err)
	}
	if second.FencingToken != first.FencingToken+1 ||
		second.Asset.Version != first.Asset.Version+2 {
		t.Fatalf("second upload claim = %#v, first = %#v", second, first)
	}
	secondCommit := second.Asset
	secondCommit.Status = conversation.AudioAssetMetadataCommitted
	secondCommit.ETag = "etag-second"
	secondCommit.UpdatedAt = audioAssetDatabaseNow(t, pool)
	secondCommit.Version++
	if err := repository.CommitUploadClaim(
		ctx,
		secondCommit,
		second.Asset.Version,
		first.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("old upload fencing token error = %v", err)
	}

	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET upload_lease_until = transaction_timestamp() - interval '1 second'
		 WHERE audio_asset_id = $1`,
		asset.ID,
	); err != nil {
		t.Fatalf("expire ambiguous upload lease: %v", err)
	}
	if err := repository.CommitUploadClaim(
		ctx,
		secondCommit,
		second.Asset.Version,
		second.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("expired upload lease commit error = %v", err)
	}

	restarted, err := NewAudioAssetRepository(pool)
	if err != nil {
		t.Fatalf("create restarted upload repository: %v", err)
	}
	third, err := restarted.ClaimUpload(
		ctx,
		audioTestOwnerA,
		asset.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("take over expired upload after restart: %v", err)
	}
	if third.FencingToken != second.FencingToken+1 ||
		third.Asset.Version != second.Asset.Version+1 {
		t.Fatalf("upload takeover = %#v, second = %#v", third, second)
	}
	completed := third.Asset
	completed.Status = conversation.AudioAssetMetadataCommitted
	completed.ETag = "etag-completed"
	completed.UpdatedAt = audioAssetDatabaseNow(t, pool)
	completed.Version++
	if err := restarted.CommitUploadClaim(
		ctx,
		completed,
		third.Asset.Version,
		third.FencingToken,
	); err != nil {
		t.Fatalf("CommitUploadClaim after takeover: %v", err)
	}
	if err := restarted.CommitUploadClaim(
		ctx,
		completed,
		third.Asset.Version,
		third.FencingToken,
	); err != nil {
		t.Fatalf("retry after lost CommitUploadClaim response: %v", err)
	}
	persisted, err := restarted.GetOwned(ctx, audioTestOwnerA, asset.ID)
	if err != nil ||
		persisted.Status != conversation.AudioAssetMetadataCommitted ||
		persisted.ETag != completed.ETag ||
		persisted.Version != completed.Version {
		t.Fatalf("committed upload persisted = %#v, %v", persisted, err)
	}
	var uploadLease *time.Time
	var uploadFence int64
	if err := pool.QueryRow(
		ctx,
		`SELECT upload_lease_until, upload_fencing_token
		 FROM conversation_audio_assets
		 WHERE audio_asset_id = $1`,
		asset.ID,
	).Scan(&uploadLease, &uploadFence); err != nil {
		t.Fatalf("inspect committed upload barrier: %v", err)
	}
	if uploadLease != nil || uploadFence != int64(third.FencingToken) {
		t.Fatalf("committed upload lease/fence = %v/%d", uploadLease, uploadFence)
	}
}

func TestExpiredAmbiguousUploadCanBeClaimedForCleanup(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	asset := newTestAudioAsset(
		"asset-ambiguous-put",
		audioTestOwnerA,
		"upload-ambiguous-put",
		databaseNow.Add(-2*time.Hour),
		databaseNow.Add(-time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create ambiguous-Put fixture: %v", err)
	}
	setAudioAssetStagedUntil(
		t,
		pool,
		asset.ID,
		databaseNow.Add(-time.Hour),
	)
	uploadClaim, err := repository.ClaimUpload(
		ctx,
		audioTestOwnerA,
		asset.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimUpload ambiguous-Put fixture: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET upload_lease_until = transaction_timestamp() - interval '1 second'
		 WHERE audio_asset_id = $1`,
		asset.ID,
	); err != nil {
		t.Fatalf("expire ambiguous-Put lease: %v", err)
	}

	cleanupClaims, err := repository.ClaimExpiredUnconfirmed(
		ctx,
		time.Minute,
		1,
	)
	if err != nil {
		t.Fatalf("claim expired ambiguous Put for cleanup: %v", err)
	}
	if len(cleanupClaims) != 1 ||
		cleanupClaims[0].Asset.ID != asset.ID ||
		cleanupClaims[0].Asset.Status != conversation.AudioAssetDeleting ||
		cleanupClaims[0].Asset.Version != uploadClaim.Asset.Version+1 {
		t.Fatalf("ambiguous-Put cleanup claim = %#v", cleanupClaims)
	}
	var uploadLease *time.Time
	if err := pool.QueryRow(
		ctx,
		`SELECT upload_lease_until
		 FROM conversation_audio_assets
		 WHERE audio_asset_id = $1`,
		asset.ID,
	).Scan(&uploadLease); err != nil {
		t.Fatalf("inspect cleared ambiguous upload lease: %v", err)
	}
	if uploadLease != nil {
		t.Fatalf("cleanup transition retained expired upload lease %s", *uploadLease)
	}

	requestDelete := newTestAudioAsset(
		"asset-request-delete-after-upload-timeout",
		audioTestOwnerA,
		"upload-request-delete-after-timeout",
		databaseNow.Add(-time.Hour),
		databaseNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, requestDelete); err != nil {
		t.Fatalf("Create request-delete fixture: %v", err)
	}
	requestDeleteClaim, err := repository.ClaimUpload(
		ctx,
		audioTestOwnerA,
		requestDelete.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimUpload request-delete fixture: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET upload_lease_until = transaction_timestamp() - interval '1 second'
		 WHERE audio_asset_id = $1`,
		requestDelete.ID,
	); err != nil {
		t.Fatalf("expire request-delete upload lease: %v", err)
	}
	requestDeleting := requestDeleteClaim.Asset
	requestDeleting.Status = conversation.AudioAssetDeleting
	requestDeleting.UpdatedAt = audioAssetDatabaseNow(t, pool)
	requestDeleting.Version++
	if err := repository.Save(
		ctx,
		requestDeleting,
		requestDeleteClaim.Asset.Version,
	); err != nil {
		t.Fatalf("request Delete after upload lease expiry: %v", err)
	}
	var requestDeleteUploadFence int64
	if err := pool.QueryRow(
		ctx,
		`SELECT upload_lease_until, upload_fencing_token
		 FROM conversation_audio_assets
		 WHERE audio_asset_id = $1`,
		requestDelete.ID,
	).Scan(&uploadLease, &requestDeleteUploadFence); err != nil {
		t.Fatalf("inspect request-delete upload lease: %v", err)
	}
	if uploadLease != nil {
		t.Fatalf("request Delete retained expired upload lease %s", *uploadLease)
	}
	if requestDeleteUploadFence != int64(requestDeleteClaim.FencingToken+1) {
		t.Fatalf(
			"request Delete upload fence = %d, want %d",
			requestDeleteUploadFence,
			requestDeleteClaim.FencingToken+1,
		)
	}
}

func TestAudioAssetCleanupClaimsUseSkipLockedDatabaseClockAndFencing(
	t *testing.T,
) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)

	locked := newTestAudioAsset(
		"asset-a-locked",
		audioTestOwnerA,
		"upload-a-locked",
		databaseNow.Add(-3*time.Hour),
		databaseNow.Add(-2*time.Hour),
	)
	available := newTestAudioAsset(
		"asset-b-available",
		audioTestOwnerA,
		"upload-b-available",
		databaseNow.Add(-3*time.Hour),
		databaseNow.Add(-2*time.Hour),
	)
	future := newTestAudioAsset(
		"asset-c-future",
		audioTestOwnerA,
		"upload-c-future",
		databaseNow.Add(-time.Hour),
		databaseNow.Add(time.Hour),
	)
	bob := newTestAudioAsset(
		"asset-d-bob",
		audioTestOwnerB,
		"upload-d-bob",
		databaseNow.Add(-3*time.Hour),
		databaseNow.Add(-2*time.Hour),
	)
	for _, asset := range []conversation.AudioAsset{locked, available, future, bob} {
		if err := repository.Create(ctx, asset); err != nil {
			t.Fatalf("Create cleanup fixture %s: %v", asset.ID, err)
		}
		if !asset.StagedUntil.After(databaseNow) {
			setAudioAssetStagedUntil(
				t,
				pool,
				asset.ID,
				asset.StagedUntil,
			)
		}
	}

	lockTransaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin row-lock transaction: %v", err)
	}
	defer func() { _ = lockTransaction.Rollback(ctx) }()
	if _, err := lockTransaction.Exec(
		ctx,
		`SELECT audio_asset_id
		 FROM conversation_audio_assets
		 WHERE audio_asset_id = $1
		 FOR UPDATE`,
		locked.ID,
	); err != nil {
		t.Fatalf("lock first expired row: %v", err)
	}

	claimContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	claims, err := repository.ClaimExpiredUnconfirmed(
		claimContext,
		time.Minute,
		1,
	)
	if err != nil {
		t.Fatalf("ClaimExpiredUnconfirmed while first row locked: %v", err)
	}
	if len(claims) != 1 || claims[0].Asset.ID != available.ID {
		t.Fatalf("SKIP LOCKED claims = %#v, want %s", claims, available.ID)
	}
	availableClaim := claims[0]
	if availableClaim.Asset.Status != conversation.AudioAssetDeleting ||
		availableClaim.Asset.Version != 2 ||
		availableClaim.FencingToken != 1 ||
		!availableClaim.LeaseExpiresAt.After(databaseNow) {
		t.Fatalf("first cleanup claim = %#v", availableClaim)
	}
	if err := lockTransaction.Rollback(ctx); err != nil {
		t.Fatalf("release first-row lock: %v", err)
	}

	claims, err = repository.ClaimExpiredUnconfirmed(
		ctx,
		time.Minute,
		1,
	)
	if err != nil {
		t.Fatalf("claim unlocked expired row: %v", err)
	}
	if len(claims) != 1 || claims[0].Asset.ID != locked.ID {
		t.Fatalf("unlocked claims = %#v, want %s", claims, locked.ID)
	}
	firstLockedClaim := claims[0]

	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET cleanup_lease_until = transaction_timestamp() - interval '1 second'
		 WHERE audio_asset_id = $1`,
		locked.ID,
	); err != nil {
		t.Fatalf("expire cleanup lease: %v", err)
	}
	expiredCompletion := firstLockedClaim.Asset
	expiredCompletion.Status = conversation.AudioAssetDeleted
	expiredCompletion.DeletedAt = audioAssetDatabaseNow(t, pool)
	expiredCompletion.UpdatedAt = expiredCompletion.DeletedAt
	expiredCompletion.Version++
	if err := repository.SaveCleanupClaim(
		ctx,
		expiredCompletion,
		firstLockedClaim.Asset.Version,
		firstLockedClaim.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("expired cleanup lease completion error = %v", err)
	}

	restarted, err := NewAudioAssetRepository(pool)
	if err != nil {
		t.Fatalf("create restarted cleanup repository: %v", err)
	}
	claims, err = restarted.ClaimDeleting(ctx, time.Minute, 10)
	if err != nil {
		t.Fatalf("take over expired deleting claim: %v", err)
	}
	if len(claims) != 1 || claims[0].Asset.ID != locked.ID {
		t.Fatalf("deleting takeover claims = %#v", claims)
	}
	takenOver := claims[0]
	if takenOver.FencingToken != firstLockedClaim.FencingToken+1 ||
		takenOver.Asset.Version != firstLockedClaim.Asset.Version+1 {
		t.Fatalf("cleanup takeover = %#v, first = %#v", takenOver, firstLockedClaim)
	}

	wrongFenceCompletion := takenOver.Asset
	wrongFenceCompletion.Status = conversation.AudioAssetDeleted
	wrongFenceCompletion.DeletedAt = audioAssetDatabaseNow(t, pool)
	wrongFenceCompletion.UpdatedAt = wrongFenceCompletion.DeletedAt
	wrongFenceCompletion.Version++
	if err := restarted.SaveCleanupClaim(
		ctx,
		wrongFenceCompletion,
		takenOver.Asset.Version,
		firstLockedClaim.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("stale cleanup fencing token error = %v", err)
	}

	completed := takenOver.Asset
	completed.Status = conversation.AudioAssetDeleted
	completed.DeletedAt = audioAssetDatabaseNow(t, pool)
	completed.UpdatedAt = completed.DeletedAt
	completed.Version++
	if err := restarted.SaveCleanupClaim(
		ctx,
		completed,
		takenOver.Asset.Version,
		takenOver.FencingToken,
	); err != nil {
		t.Fatalf("current cleanup completion: %v", err)
	}
	if err := restarted.SaveCleanupClaim(
		ctx,
		completed,
		takenOver.Asset.Version,
		takenOver.FencingToken,
	); err != nil {
		t.Fatalf("retry after lost SaveCleanupClaim response: %v", err)
	}
	persisted, err := repository.GetOwned(ctx, audioTestOwnerA, locked.ID)
	if err != nil ||
		persisted.Status != conversation.AudioAssetDeleted ||
		persisted.Version != completed.Version {
		t.Fatalf("completed cleanup persisted = %#v, %v", persisted, err)
	}
	mismatchedReplay := completed
	mismatchedReplay.DeletedAt = completed.DeletedAt.Add(time.Microsecond)
	mismatchedReplay.UpdatedAt = mismatchedReplay.DeletedAt
	if err := restarted.SaveCleanupClaim(
		ctx,
		mismatchedReplay,
		takenOver.Asset.Version,
		takenOver.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("mismatched cleanup completion replay error = %v", err)
	}

	if err := repository.ReleaseCleanupClaim(
		ctx,
		audioTestOwnerA,
		availableClaim.Asset.ID,
		availableClaim.FencingToken+1,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("wrong cleanup fence release error = %v", err)
	}
	if err := repository.ReleaseCleanupClaim(
		ctx,
		audioTestOwnerA,
		availableClaim.Asset.ID,
		availableClaim.FencingToken,
	); err != nil {
		t.Fatalf("release failed-delete claim: %v", err)
	}
	releasedCleanup, err := repository.GetOwned(
		ctx,
		audioTestOwnerA,
		availableClaim.Asset.ID,
	)
	if err != nil {
		t.Fatalf("load released cleanup claim: %v", err)
	}
	if err := repository.ReleaseCleanupClaim(
		ctx,
		audioTestOwnerA,
		availableClaim.Asset.ID,
		availableClaim.FencingToken,
	); err != nil {
		t.Fatalf("retry after lost ReleaseCleanupClaim response: %v", err)
	}
	replayedCleanupRelease, err := repository.GetOwned(
		ctx,
		audioTestOwnerA,
		availableClaim.Asset.ID,
	)
	if err != nil {
		t.Fatalf("load replayed cleanup release: %v", err)
	}
	if replayedCleanupRelease.Version != releasedCleanup.Version ||
		!replayedCleanupRelease.UpdatedAt.Equal(releasedCleanup.UpdatedAt) {
		t.Fatalf(
			"cleanup release replay mutated asset from %#v to %#v",
			releasedCleanup,
			replayedCleanupRelease,
		)
	}
	if err := repository.ReleaseCleanupClaim(
		ctx,
		audioTestOwnerB,
		availableClaim.Asset.ID,
		availableClaim.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner ReleaseCleanupClaim error = %v", err)
	}
	claims, err = repository.ClaimDeleting(ctx, time.Minute, 1)
	if err != nil {
		t.Fatalf("reclaim released delete: %v", err)
	}
	if len(claims) != 1 ||
		claims[0].Asset.ID != availableClaim.Asset.ID ||
		claims[0].FencingToken != availableClaim.FencingToken+1 ||
		claims[0].Asset.Version != availableClaim.Asset.Version+2 {
		t.Fatalf("released claim was not immediately fenced and reclaimed: %#v", claims)
	}
	reclaimedAvailable := claims[0]
	if err := repository.ReleaseCleanupClaim(
		ctx,
		audioTestOwnerA,
		reclaimedAvailable.Asset.ID,
		availableClaim.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("old cleanup fence release after takeover error = %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET cleanup_lease_until = transaction_timestamp() - interval '1 second'
		 WHERE audio_asset_id = $1`,
		reclaimedAvailable.Asset.ID,
	); err != nil {
		t.Fatalf("expire reclaimed available lease: %v", err)
	}

	ownerClaims, err := repository.ClaimOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerA,
		time.Minute,
		10,
	)
	if err != nil {
		t.Fatalf("ClaimOwnerAssetsForAccountCleanup: %v", err)
	}
	if len(ownerClaims) != 2 {
		t.Fatalf("owner cleanup claims = %#v, want two Alice assets", ownerClaims)
	}
	ownerClaimsByID := make(map[string]conversation.AudioAssetCleanupClaim)
	for _, claim := range ownerClaims {
		if claim.Asset.OwnerID != audioTestOwnerA {
			t.Fatalf("owner cleanup crossed ownership boundary: %#v", claim)
		}
		ownerClaimsByID[claim.Asset.ID] = claim
	}
	if _, found := ownerClaimsByID[future.ID]; !found {
		t.Fatalf("owner cleanup did not claim future staged asset: %#v", ownerClaims)
	}
	reclaimedByOwner, found := ownerClaimsByID[reclaimedAvailable.Asset.ID]
	if !found {
		t.Fatalf("owner cleanup did not reclaim deleting asset: %#v", ownerClaims)
	}
	if !reclaimedByOwner.Asset.UpdatedAt.Equal(
		reclaimedAvailable.Asset.UpdatedAt,
	) {
		t.Fatalf(
			"owner reclaim reset deleting timestamp from %s to %s",
			reclaimedAvailable.Asset.UpdatedAt,
			reclaimedByOwner.Asset.UpdatedAt,
		)
	}

	bobPersisted, err := repository.GetOwned(ctx, audioTestOwnerB, bob.ID)
	if err != nil || bobPersisted.Status != conversation.AudioAssetStaged {
		t.Fatalf("Bob asset changed by Alice cleanup = %#v, %v", bobPersisted, err)
	}
}

func TestReleaseCleanupClaimMovesFailureBehindNextAsset(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	first := newTestAudioAsset(
		"asset-a-poison",
		audioTestOwnerA,
		"upload-a-poison",
		databaseNow.Add(-3*time.Hour),
		databaseNow.Add(time.Hour),
	)
	second := newTestAudioAsset(
		"asset-b-next",
		audioTestOwnerA,
		"upload-b-next",
		databaseNow.Add(-3*time.Hour),
		databaseNow.Add(time.Hour),
	)
	for index, asset := range []*conversation.AudioAsset{&first, &second} {
		if err := repository.Create(ctx, *asset); err != nil {
			t.Fatalf("Create cleanup fairness fixture: %v", err)
		}
		persisted, err := repository.GetOwned(
			ctx,
			asset.OwnerID,
			asset.ID,
		)
		if err != nil {
			t.Fatalf("reload cleanup fairness fixture: %v", err)
		}
		*asset = persisted
		asset.Status = conversation.AudioAssetDeleting
		asset.UpdatedAt = audioAssetDatabaseNow(t, pool).
			Add(time.Duration(index) * time.Millisecond)
		asset.Version++
		if err := repository.Save(ctx, *asset, 1); err != nil {
			t.Fatalf("mark cleanup fairness fixture deleting: %v", err)
		}
	}

	claims, err := repository.ClaimDeleting(ctx, time.Minute, 1)
	if err != nil {
		t.Fatalf("claim first cleanup fairness row: %v", err)
	}
	if len(claims) != 1 || claims[0].Asset.ID != first.ID {
		t.Fatalf("first cleanup fairness claim = %#v", claims)
	}
	if err := repository.ReleaseCleanupClaim(
		ctx,
		audioTestOwnerA,
		claims[0].Asset.ID,
		claims[0].FencingToken,
	); err != nil {
		t.Fatalf("release poison cleanup claim: %v", err)
	}
	staleAfterRelease := claims[0].Asset
	staleAfterRelease.Status = conversation.AudioAssetDeleted
	staleAfterRelease.DeletedAt = audioAssetDatabaseNow(t, pool)
	staleAfterRelease.UpdatedAt = staleAfterRelease.DeletedAt
	staleAfterRelease.Version++
	if err := repository.Save(
		ctx,
		staleAfterRelease,
		claims[0].Asset.Version,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("cleanup release did not fence stale Save: %v", err)
	}

	claims, err = repository.ClaimDeleting(ctx, time.Minute, 1)
	if err != nil {
		t.Fatalf("claim row after poison release: %v", err)
	}
	if len(claims) != 1 || claims[0].Asset.ID != second.ID {
		t.Fatalf("poison row starved next cleanup item: %#v", claims)
	}
}

func TestRequestDeleteTakesOverExpiredCleanupLease(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	asset := newTestAudioAsset(
		"asset-request-delete-cleanup-takeover",
		audioTestOwnerA,
		"upload-request-delete-cleanup-takeover",
		databaseNow.Add(-time.Hour),
		databaseNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create cleanup-takeover fixture: %v", err)
	}
	asset, err := repository.GetOwned(ctx, audioTestOwnerA, asset.ID)
	if err != nil {
		t.Fatalf("reload cleanup-takeover fixture: %v", err)
	}
	asset.Status = conversation.AudioAssetDeleting
	asset.UpdatedAt = audioAssetDatabaseNow(t, pool)
	asset.Version++
	if err := repository.Save(ctx, asset, 1); err != nil {
		t.Fatalf("mark cleanup-takeover fixture deleting: %v", err)
	}
	claims, err := repository.ClaimDeleting(ctx, time.Minute, 1)
	if err != nil {
		t.Fatalf("ClaimDeleting cleanup-takeover fixture: %v", err)
	}
	if len(claims) != 1 || claims[0].Asset.ID != asset.ID {
		t.Fatalf("cleanup-takeover claim = %#v", claims)
	}
	claim := claims[0]
	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET cleanup_lease_until = transaction_timestamp() - interval '1 second'
		 WHERE audio_asset_id = $1`,
		asset.ID,
	); err != nil {
		t.Fatalf("expire cleanup-takeover lease: %v", err)
	}

	requestDeleted := claim.Asset
	requestDeleted.Status = conversation.AudioAssetDeleted
	requestDeleted.DeletedAt = audioAssetDatabaseNow(t, pool)
	requestDeleted.UpdatedAt = requestDeleted.DeletedAt
	requestDeleted.Version++
	if err := repository.Save(
		ctx,
		requestDeleted,
		claim.Asset.Version,
	); err != nil {
		t.Fatalf("request Delete after cleanup lease expiry: %v", err)
	}
	var cleanupLease *time.Time
	var cleanupFence int64
	if err := pool.QueryRow(
		ctx,
		`SELECT cleanup_lease_until, cleanup_fencing_token
		 FROM conversation_audio_assets
		 WHERE audio_asset_id = $1`,
		asset.ID,
	).Scan(&cleanupLease, &cleanupFence); err != nil {
		t.Fatalf("inspect cleared cleanup lease: %v", err)
	}
	if cleanupLease != nil {
		t.Fatalf("request Delete retained expired cleanup lease %s", *cleanupLease)
	}
	if cleanupFence != int64(claim.FencingToken+1) {
		t.Fatalf(
			"request Delete cleanup fence = %d, want %d",
			cleanupFence,
			claim.FencingToken+1,
		)
	}
	if err := repository.SaveCleanupClaim(
		ctx,
		requestDeleted,
		claim.Asset.Version,
		claim.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("stale cleanup completion after request Delete error = %v", err)
	}
	if err := repository.ReleaseCleanupClaim(
		ctx,
		audioTestOwnerA,
		asset.ID,
		claim.FencingToken,
	); !errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("stale cleanup release after request Delete error = %v", err)
	}
}

func TestAudioAssetCleanupLeaseToleratesApplicationClockAheadOfDatabase(
	t *testing.T,
) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	applicationNow := databaseNow.Add(5 * time.Minute)
	asset := newTestAudioAsset(
		"asset-application-clock-ahead",
		audioTestOwnerA,
		"upload-application-clock-ahead",
		applicationNow,
		applicationNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create future-clock asset: %v", err)
	}
	asset, err := repository.GetOwned(ctx, audioTestOwnerA, asset.ID)
	if err != nil {
		t.Fatalf("reload future-clock asset: %v", err)
	}
	asset.Status = conversation.AudioAssetDeleting
	asset.UpdatedAt = applicationNow
	asset.Version++
	if err := repository.Save(ctx, asset, 1); err != nil {
		t.Fatalf("mark future-clock asset deleting: %v", err)
	}

	claims, err := repository.ClaimDeleting(ctx, time.Minute, 1)
	if err != nil {
		t.Fatalf("ClaimDeleting with future application clock: %v", err)
	}
	if len(claims) != 1 || claims[0].Asset.ID != asset.ID {
		t.Fatalf("future-clock claims = %#v", claims)
	}
	if !claims[0].LeaseExpiresAt.Before(claims[0].Asset.UpdatedAt) {
		t.Fatalf(
			"fixture did not prove independent clocks: lease %s, updated %s",
			claims[0].LeaseExpiresAt,
			claims[0].Asset.UpdatedAt,
		)
	}
}

func TestAudioAssetUploadAndCleanupTolerateApplicationClockAheadOfDatabase(
	t *testing.T,
) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	applicationNow := databaseNow.Add(5 * time.Minute)
	asset := newTestAudioAsset(
		"asset-upload-clock-ahead",
		audioTestOwnerA,
		"upload-clock-ahead",
		applicationNow,
		applicationNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create future-clock upload asset: %v", err)
	}
	uploadClaim, err := repository.ClaimUpload(
		ctx,
		audioTestOwnerA,
		asset.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimUpload with future application clock: %v", err)
	}
	if !uploadClaim.Asset.CreatedAt.Before(applicationNow) ||
		!uploadClaim.Asset.StagedUntil.Equal(
			uploadClaim.Asset.CreatedAt.Add(time.Hour),
		) {
		t.Fatalf("future-clock upload claim = %#v", uploadClaim)
	}
	uploaded := uploadClaim.Asset
	uploaded.Status = conversation.AudioAssetMetadataCommitted
	uploaded.ETag = "etag-future-clock"
	uploaded.UpdatedAt = applicationNow
	uploaded.Version++
	if err := repository.CommitUploadClaim(
		ctx,
		uploaded,
		uploadClaim.Asset.Version,
		uploadClaim.FencingToken,
	); err != nil {
		t.Fatalf("CommitUploadClaim with future application clock: %v", err)
	}
	uploaded.Status = conversation.AudioAssetDeleting
	uploaded.Version++
	if err := repository.Save(
		ctx,
		uploaded,
		uploaded.Version-1,
	); err != nil {
		t.Fatalf("mark future-clock upload deleting: %v", err)
	}
	cleanupClaims, err := repository.ClaimDeleting(ctx, time.Minute, 1)
	if err != nil {
		t.Fatalf("ClaimDeleting future-clock upload: %v", err)
	}
	if len(cleanupClaims) != 1 ||
		!cleanupClaims[0].LeaseExpiresAt.Before(
			cleanupClaims[0].Asset.UpdatedAt,
		) {
		t.Fatalf("future-clock cleanup claim = %#v", cleanupClaims)
	}
	deleted := cleanupClaims[0].Asset
	deleted.Status = conversation.AudioAssetDeleted
	deleted.DeletedAt = deleted.UpdatedAt
	deleted.Version++
	if err := repository.SaveCleanupClaim(
		ctx,
		deleted,
		cleanupClaims[0].Asset.Version,
		cleanupClaims[0].FencingToken,
	); err != nil {
		t.Fatalf("finish future-clock cleanup: %v", err)
	}
}

func TestPurgeOwnerDeletedAssetsUnblocksIdentityDeletion(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	aliceAssets := []conversation.AudioAsset{
		newTestAudioAsset(
			"asset-alice-purge-a",
			audioTestOwnerA,
			"upload-alice-purge-a",
			databaseNow.Add(-time.Hour),
			databaseNow.Add(time.Hour),
		),
		newTestAudioAsset(
			"asset-alice-purge-b",
			audioTestOwnerA,
			"upload-alice-purge-b",
			databaseNow.Add(-time.Hour),
			databaseNow.Add(time.Hour),
		),
	}
	bobAsset := newTestAudioAsset(
		"asset-bob-tombstone",
		audioTestOwnerB,
		"upload-bob-tombstone",
		databaseNow.Add(-time.Hour),
		databaseNow.Add(time.Hour),
	)
	for _, original := range append(aliceAssets, bobAsset) {
		asset := original
		if err := repository.Create(ctx, asset); err != nil {
			t.Fatalf("Create purge fixture %s: %v", asset.ID, err)
		}
		persisted, err := repository.GetOwned(ctx, asset.OwnerID, asset.ID)
		if err != nil {
			t.Fatalf("reload purge fixture %s: %v", asset.ID, err)
		}
		asset = persisted
		asset.Status = conversation.AudioAssetDeleting
		asset.UpdatedAt = audioAssetDatabaseNow(t, pool)
		asset.Version++
		if err := repository.Save(ctx, asset, 1); err != nil {
			t.Fatalf("mark purge fixture deleting %s: %v", asset.ID, err)
		}
		asset.Status = conversation.AudioAssetDeleted
		asset.DeletedAt = asset.UpdatedAt.Add(time.Minute)
		asset.UpdatedAt = asset.DeletedAt
		asset.Version++
		if err := repository.Save(ctx, asset, 2); err != nil {
			t.Fatalf("mark purge fixture deleted %s: %v", asset.ID, err)
		}
	}

	pending, err := repository.HasOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerA,
	)
	if err != nil || !pending {
		t.Fatalf("deleted Alice tombstones pending = %t, %v", pending, err)
	}
	assertAudioAssetConstraint(
		t,
		pool,
		"DELETE FROM identity_users WHERE id = $1",
		audioTestOwnerA,
		"23001",
		"conversation_audio_assets_owner_user_id_fkey",
	)

	lockTransaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deleted-tombstone lock: %v", err)
	}
	defer func() { _ = lockTransaction.Rollback(ctx) }()
	if _, err := lockTransaction.Exec(
		ctx,
		`SELECT audio_asset_id
		 FROM conversation_audio_assets
		 WHERE owner_user_id = $1 AND audio_asset_id = $2
		 FOR UPDATE`,
		audioTestOwnerA,
		aliceAssets[0].ID,
	); err != nil {
		t.Fatalf("lock first deleted tombstone: %v", err)
	}
	purgeContext, cancelPurge := context.WithTimeout(ctx, time.Second)
	defer cancelPurge()
	purged, err := repository.PurgeOwnerDeletedAssets(
		purgeContext,
		audioTestOwnerA,
		1,
	)
	if err != nil || purged != 1 {
		t.Fatalf("first bounded owner purge = %d, %v", purged, err)
	}
	if _, err := repository.GetOwned(
		ctx,
		audioTestOwnerA,
		aliceAssets[0].ID,
	); err != nil {
		t.Fatalf("SKIP LOCKED purged locked tombstone: %v", err)
	}
	if err := lockTransaction.Rollback(ctx); err != nil {
		t.Fatalf("release deleted-tombstone lock: %v", err)
	}
	pending, err = repository.HasOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerA,
	)
	if err != nil || !pending {
		t.Fatalf("Alice pending after first bounded purge = %t, %v", pending, err)
	}
	purged, err = repository.PurgeOwnerDeletedAssets(
		ctx,
		audioTestOwnerA,
		1,
	)
	if err != nil || purged != 1 {
		t.Fatalf("second bounded owner purge = %d, %v", purged, err)
	}
	pending, err = repository.HasOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerA,
	)
	if err != nil || pending {
		t.Fatalf("Alice pending after complete purge = %t, %v", pending, err)
	}
	purged, err = repository.PurgeOwnerDeletedAssets(
		ctx,
		audioTestOwnerA,
		1,
	)
	if err != nil || purged != 0 {
		t.Fatalf("idempotent owner purge = %d, %v", purged, err)
	}
	if _, err := pool.Exec(
		ctx,
		"DELETE FROM identity_users WHERE id = $1",
		audioTestOwnerA,
	); err != nil {
		t.Fatalf("delete Identity user after audio purge: %v", err)
	}

	pending, err = repository.HasOwnerAssetsForAccountCleanup(
		ctx,
		audioTestOwnerB,
	)
	if err != nil || !pending {
		t.Fatalf("Bob tombstone changed by Alice purge = %t, %v", pending, err)
	}
	if _, err := repository.GetOwned(
		ctx,
		audioTestOwnerB,
		bobAsset.ID,
	); err != nil {
		t.Fatalf("Bob tombstone missing after Alice purge: %v", err)
	}
}

func TestAudioAssetMigrationEnforcesConstraintsIndexesAndDown(t *testing.T) {
	repository, pool := newAudioAssetIntegrationRepository(t)
	ctx := context.Background()
	databaseNow := audioAssetDatabaseNow(t, pool)
	asset := newTestAudioAsset(
		"asset-schema",
		audioTestOwnerA,
		"upload-schema",
		databaseNow,
		databaseNow.Add(time.Hour),
	)
	if err := repository.Create(ctx, asset); err != nil {
		t.Fatalf("Create schema fixture: %v", err)
	}

	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET audio_asset_id = $1
		 WHERE audio_asset_id = 'asset-schema'`,
		strings.Repeat("a", 129),
		"23514",
		"conversation_audio_assets_id_length_check",
	)
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET upload_request_id = $1
		 WHERE audio_asset_id = 'asset-schema'`,
		strings.Repeat("界", 43),
		"23514",
		"conversation_audio_assets_upload_request_length_check",
	)
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET status = 'readable',
		     etag = 'etag-schema',
		     candidate_id = $1,
		     turn_id = 'turn-schema'
		 WHERE audio_asset_id = 'asset-schema'`,
		strings.Repeat("a", 129),
		"23514",
		"conversation_audio_assets_binding_lengths_check",
	)
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET object_key = 'public.wav'
		 WHERE audio_asset_id = $1`,
		asset.ID,
		"23514",
		"conversation_audio_assets_object_key_check",
	)
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET etag = 'etag-before-upload'
		 WHERE audio_asset_id = $1`,
		asset.ID,
		"23514",
		"conversation_audio_assets_staged_etag_check",
	)
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET status = 'metadata_committed'
		 WHERE audio_asset_id = $1`,
		asset.ID,
		"23514",
		"conversation_audio_assets_committed_etag_check",
	)
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET status = 'readable', etag = 'etag-schema'
		 WHERE audio_asset_id = $1`,
		asset.ID,
		"23514",
		"conversation_audio_assets_readable_binding_check",
	)
	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET upload_lease_until = transaction_timestamp() + interval '1 minute'
		 WHERE audio_asset_id = $1`,
		asset.ID,
	); err != nil {
		t.Fatalf("set staged upload lease: %v", err)
	}
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET status = 'metadata_committed', etag = 'etag-upload-lease'
		 WHERE audio_asset_id = $1`,
		asset.ID,
		"23514",
		"conversation_audio_assets_upload_lease_check",
	)
	if _, err := pool.Exec(
		ctx,
		`UPDATE conversation_audio_assets
		 SET upload_lease_until = NULL
		 WHERE audio_asset_id = $1`,
		asset.ID,
	); err != nil {
		t.Fatalf("clear staged upload lease: %v", err)
	}
	assertAudioAssetConstraint(
		t,
		pool,
		`UPDATE conversation_audio_assets
		 SET upload_fencing_token = -1
		 WHERE audio_asset_id = $1`,
		asset.ID,
		"23514",
		"conversation_audio_assets_upload_fence_check",
	)
	assertAudioAssetConstraint(
		t,
		pool,
		`DELETE FROM identity_users WHERE id = $1`,
		audioTestOwnerA,
		"23001",
		"conversation_audio_assets_owner_user_id_fkey",
	)

	rows, err := pool.Query(
		ctx,
		`SELECT indexname
		 FROM pg_indexes
		 WHERE schemaname = current_schema()
		   AND tablename = 'conversation_audio_assets'`,
	)
	if err != nil {
		t.Fatalf("query audio asset indexes: %v", err)
	}
	defer rows.Close()
	indexes := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan audio asset index: %v", err)
		}
		indexes[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audio asset indexes: %v", err)
	}
	for _, name := range []string{
		"conversation_audio_assets_owner_upload_request_key",
		"conversation_audio_assets_object_key_key",
		"conversation_audio_assets_owner_candidate_key",
		"conversation_audio_assets_owner_turn_key",
		"conversation_audio_assets_expired_cleanup_idx",
		"conversation_audio_assets_upload_recovery_idx",
		"conversation_audio_assets_deleting_cleanup_idx",
		"conversation_audio_assets_owner_cleanup_idx",
		"conversation_audio_assets_owner_deleted_purge_idx",
	} {
		if !indexes[name] {
			t.Errorf("audio asset index %q is missing", name)
		}
	}

	downSQL, err := migrations.Files.ReadFile(
		"000009_conversation_audio_assets.down.sql",
	)
	if err != nil {
		t.Fatalf("read audio asset down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(downSQL)); err != nil {
		t.Fatalf("apply audio asset down migration: %v", err)
	}
	var relation *string
	if err := pool.QueryRow(
		ctx,
		"SELECT to_regclass('conversation_audio_assets')",
	).Scan(&relation); err != nil {
		t.Fatalf("inspect table after down migration: %v", err)
	}
	if relation != nil {
		t.Fatalf("conversation_audio_assets remains after down migration as %q", *relation)
	}
}

func newAudioAssetIntegrationRepository(
	t *testing.T,
) (*AudioAssetRepository, *pgxpool.Pool) {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	admin, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("open audio integration admin pool: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("audio_asset_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+identifier,
	); err != nil {
		t.Fatalf("create audio integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop audio integration schema: %v", err)
		}
	})

	testConfig := config.Copy()
	if testConfig.ConnConfig.RuntimeParams == nil {
		testConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	testConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), testConfig)
	if err != nil {
		t.Fatalf("open audio integration pool: %v", err)
	}
	t.Cleanup(pool.Close)

	identityFixture := `CREATE TABLE identity_users (
		id uuid PRIMARY KEY,
		account_status text NOT NULL CHECK (
			account_status IN ('active', 'deleting', 'deleted')
		)
	)`
	if _, err := pool.Exec(
		context.Background(),
		identityFixture,
	); err != nil {
		t.Fatalf("create Identity dependency fixture: %v", err)
	}
	migrationSQL, err := migrations.Files.ReadFile(
		"000009_conversation_audio_assets.up.sql",
	)
	if err != nil {
		t.Fatalf("read audio asset migration: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		string(migrationSQL),
	); err != nil {
		t.Fatalf("apply audio asset migration: %v", err)
	}
	for _, userID := range []string{audioTestOwnerA, audioTestOwnerB} {
		if _, err := pool.Exec(
			context.Background(),
			`INSERT INTO identity_users (id, account_status)
			 VALUES ($1, 'active')`,
			userID,
		); err != nil {
			t.Fatalf("insert Identity user %s: %v", userID, err)
		}
	}

	repository, err := NewAudioAssetRepository(pool)
	if err != nil {
		t.Fatalf("NewAudioAssetRepository: %v", err)
	}
	return repository, pool
}

func newTestAudioAsset(
	id string,
	ownerID string,
	uploadRequestID string,
	createdAt time.Time,
	stagedUntil time.Time,
) conversation.AudioAsset {
	return conversation.AudioAsset{
		ID:              id,
		OwnerID:         ownerID,
		UploadRequestID: uploadRequestID,
		ObjectKey:       "audio/v1/assets/" + id + ".wav",
		ContentType:     "audio/wav",
		Size:            3,
		ChecksumSHA256:  strings.Repeat("a", 64),
		Duration:        time.Second,
		Status:          conversation.AudioAssetStaged,
		StagedUntil:     stagedUntil.UTC().Truncate(time.Microsecond),
		CreatedAt:       createdAt.UTC().Truncate(time.Microsecond),
		UpdatedAt:       createdAt.UTC().Truncate(time.Microsecond),
		Version:         1,
	}
}

func audioAssetDatabaseNow(
	t *testing.T,
	pool *pgxpool.Pool,
) time.Time {
	t.Helper()

	var now time.Time
	if err := pool.QueryRow(
		context.Background(),
		"SELECT transaction_timestamp()",
	).Scan(&now); err != nil {
		t.Fatalf("read database time: %v", err)
	}
	return now.UTC().Truncate(time.Microsecond)
}

func setAudioAssetStagedUntil(
	t *testing.T,
	pool *pgxpool.Pool,
	audioAssetID string,
	stagedUntil time.Time,
) {
	t.Helper()

	createdAt := stagedUntil.Add(-time.Hour)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE conversation_audio_assets
		 SET created_at = $1,
		     updated_at = $1,
		     staged_until = $2
		 WHERE audio_asset_id = $3`,
		createdAt,
		stagedUntil,
		audioAssetID,
	); err != nil {
		t.Fatalf("set staged_until for %s: %v", audioAssetID, err)
	}
}

func assertAudioAssetConstraint(
	t *testing.T,
	pool *pgxpool.Pool,
	query string,
	argument any,
	code string,
	constraint string,
) {
	t.Helper()

	_, err := pool.Exec(context.Background(), query, argument)
	if err == nil {
		t.Fatalf("statement unexpectedly succeeded; want constraint %s", constraint)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("statement error = %v, want PostgreSQL constraint violation", err)
	}
	if postgresError.Code != code ||
		postgresError.ConstraintName != constraint {
		t.Fatalf(
			"statement error code/constraint = %s/%s, want %s/%s",
			postgresError.Code,
			postgresError.ConstraintName,
			code,
			constraint,
		)
	}
}
