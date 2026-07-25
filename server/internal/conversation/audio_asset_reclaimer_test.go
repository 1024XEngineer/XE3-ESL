package conversation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

type failingExpiredClaimRepository struct {
	*memoryAudioAssetRepository
	err error
}

func (r *failingExpiredClaimRepository) ClaimExpiredUnconfirmed(
	context.Context,
	time.Duration,
	int,
) ([]AudioAssetCleanupClaim, error) {
	return nil, r.err
}

func TestAudioAssetReclaimerDoesNotRequireTurnVerifier(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "cleanup-only-constructor")
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)

	reclaimer, err := NewAudioAssetReclaimer(
		fixture.repository,
		fixture.store,
		fixture.clock,
	)
	if err != nil {
		t.Fatalf("NewAudioAssetReclaimer() error = %v", err)
	}
	result, err := reclaimer.ReclaimExpired(fixture.ctx, 2)
	if err != nil || result.Deleted != 1 || result.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v, %v", result, err)
	}
	assertAudioAssetStatus(
		t,
		fixture,
		asset.ID,
		AudioAssetDeleted,
	)
}

func TestAudioAssetReclaimerReturnsProcessedPartialResult(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "partial-cleanup")
	fixture.store.deleteErr = objectstore.ErrOperationFailed
	if _, err := fixture.service.Delete(
		fixture.ctx,
		fixture.alice,
		asset.ID,
	); !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatalf("Delete() error = %v", err)
	}
	fixture.store.deleteErr = nil

	claimErr := errors.New("claim expired failed")
	repository := &failingExpiredClaimRepository{
		memoryAudioAssetRepository: fixture.repository,
		err:                        claimErr,
	}
	reclaimer, err := NewAudioAssetReclaimer(
		repository,
		fixture.store,
		fixture.clock,
	)
	if err != nil {
		t.Fatalf("NewAudioAssetReclaimer() error = %v", err)
	}

	result, err := reclaimer.ReclaimExpired(fixture.ctx, 2)
	if !errors.Is(err, claimErr) || result.Deleted != 1 || result.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v, %v", result, err)
	}
	assertAudioAssetStatus(
		t,
		fixture,
		asset.ID,
		AudioAssetDeleted,
	)
}

func TestAudioAssetReclaimerReservesCapacityForDeletingRetries(t *testing.T) {
	fixture := newAudioAssetFixture()
	deleting := make([]AudioAsset, 0, 2)
	for index := range 2 {
		asset := fixture.upload(
			t,
			fixture.alice,
			fmt.Sprintf("deleting-%d", index),
		)
		fixture.store.deleteErr = objectstore.ErrOperationFailed
		if _, err := fixture.service.Delete(
			fixture.ctx,
			fixture.alice,
			asset.ID,
		); !errors.Is(err, objectstore.ErrOperationFailed) {
			t.Fatalf("Delete(%q) error = %v", asset.ID, err)
		}
		deleting = append(deleting, asset)
	}
	fixture.store.deleteErr = nil

	expired := make([]AudioAsset, 0, 4)
	for index := range 4 {
		expired = append(
			expired,
			fixture.upload(
				t,
				fixture.alice,
				fmt.Sprintf("expired-%d", index),
			),
		)
	}
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)

	result, err := fixture.service.ReclaimExpired(fixture.ctx, 100)
	if err != nil || result.Deleted != defaultCleanupLimit || result.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v, %v", result, err)
	}
	for _, asset := range deleting {
		assertAudioAssetStatus(t, fixture, asset.ID, AudioAssetDeleted)
	}
	expiredDeleted := 0
	for _, asset := range expired {
		stored, lookupErr := fixture.repository.Get(fixture.ctx, asset.ID)
		if lookupErr != nil {
			t.Fatal(lookupErr)
		}
		if stored.Status == AudioAssetDeleted {
			expiredDeleted++
		}
	}
	if expiredDeleted != 2 {
		t.Fatalf("expired assets deleted = %d, want 2", expiredDeleted)
	}
}

func TestAudioAssetAccountCleanupCapsEachLeaseBatch(t *testing.T) {
	fixture := newAudioAssetFixture()
	assets := make([]AudioAsset, 0, defaultCleanupLimit+1)
	for index := range defaultCleanupLimit + 1 {
		assets = append(
			assets,
			fixture.upload(
				t,
				fixture.alice,
				fmt.Sprintf("account-cleanup-%d", index),
			),
		)
	}

	first, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		100,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		first.Deleted != defaultCleanupLimit ||
		first.Purged != defaultCleanupLimit ||
		!first.Pending {
		t.Fatalf("first CleanupAccountData() = %#v, %v", first, err)
	}
	remaining := 0
	for _, asset := range assets {
		if _, lookupErr := fixture.repository.Get(
			fixture.ctx,
			asset.ID,
		); lookupErr == nil {
			remaining++
		} else if !errors.Is(lookupErr, ErrAudioAssetNotFound) {
			t.Fatal(lookupErr)
		}
	}
	if remaining != 1 {
		t.Fatalf("remaining account assets = %d, want 1", remaining)
	}

	second, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		100,
	)
	if err != nil ||
		second.Deleted != 1 ||
		second.Purged != 1 ||
		second.Pending {
		t.Fatalf("second CleanupAccountData() = %#v, %v", second, err)
	}
}

func TestAudioAssetSystemClockIsUTC(t *testing.T) {
	now := NewAudioAssetSystemClock().Now()
	if now.Location() != time.UTC {
		t.Fatalf("system clock location = %v, want UTC", now.Location())
	}
	if delta := time.Since(now); delta < 0 || delta > time.Second {
		t.Fatalf("system clock delta = %v", delta)
	}
}
