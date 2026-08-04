package voice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

func TestAudioAssetUploadPersistsStagedBeforePutAndIsIdempotent(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.store.onPut = func(
		_ context.Context,
		request objectstore.PutRequest,
	) error {
		asset, err := fixture.repository.Get(context.Background(), "asset-1")
		if err != nil {
			t.Fatalf("metadata was not created before Put: %v", err)
		}
		if asset.Status != AudioAssetStaged {
			t.Fatalf("status before Put = %q, want staged", asset.Status)
		}
		if request.Key != asset.ObjectKey {
			t.Fatalf("Put key = %q, want %q", request.Key, asset.ObjectKey)
		}
		return nil
	}
	request := fixture.uploadRequest("upload-1")

	asset, err := fixture.service.Upload(fixture.ctx, fixture.alice, request)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if asset.Status != AudioAssetMetadataCommitted {
		t.Fatalf("Upload() status = %q, want metadata_committed", asset.Status)
	}
	if asset.ObjectKey != "audio/v1/assets/asset-1.wav" {
		t.Fatalf("Upload() key = %q", asset.ObjectKey)
	}

	request.Body = bytes.NewReader([]byte("wav"))
	retried, err := fixture.service.Upload(fixture.ctx, fixture.alice, request)
	if err != nil {
		t.Fatalf("retry Upload() error = %v", err)
	}
	if calls := fixture.store.putCallCount(); retried.ID != asset.ID || calls != 1 {
		t.Fatalf("retry created or uploaded duplicate: asset=%q calls=%d", retried.ID, calls)
	}

	conflicting := fixture.uploadRequest("upload-1")
	conflicting.Size++
	if _, err := fixture.service.Upload(fixture.ctx, fixture.alice, conflicting); !errors.Is(err, ErrAudioAssetIdempotencyConflict) {
		t.Fatalf("conflicting retry Upload() error = %v", err)
	}
}

func TestAudioAssetUploadFailureLeavesExpiredStagedForCleanup(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.store.putErr = objectstore.ErrOperationFailed

	_, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("upload-fails"),
	)
	if !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatalf("Upload() error = %v", err)
	}
	staged, err := fixture.repository.Get(fixture.ctx, "asset-1")
	if err != nil || staged.Status != AudioAssetStaged {
		t.Fatalf("stored asset = %#v, error = %v", staged, err)
	}
}

func TestAudioAssetUploadAlreadyExistsLeavesStaged(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.store.putErr = objectstore.ErrAlreadyExists

	_, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("upload-already-exists"),
	)
	if !errors.Is(err, objectstore.ErrAlreadyExists) {
		t.Fatalf("Upload() error = %v", err)
	}
	staged, err := fixture.repository.Get(fixture.ctx, "asset-1")
	if err != nil {
		t.Fatal(err)
	}
	if staged.Status != AudioAssetStaged || staged.ETag != "" {
		t.Fatalf("stored asset = %#v, want staged without ETag", staged)
	}
}

func TestAudioAssetCommitMetadataRejectsBlankETag(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.uploadStaged(t, fixture.alice, "blank-etag")
	version := asset.Version

	for _, etag := range []string{" \t", strings.Repeat("e", 513)} {
		if err := asset.commitMetadata(etag, fixture.clock.Now()); !errors.Is(err, ErrAudioAssetInvalid) {
			t.Fatalf("commitMetadata(%d bytes) error = %v", len(etag), err)
		}
	}
	if asset.Status != AudioAssetStaged ||
		asset.ETag != "" ||
		asset.Version != version {
		t.Fatalf("commitMetadata() mutated asset = %#v", asset)
	}
}

func TestAudioAssetMutationTimesNeverMoveBackward(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	asset, err := newStagedAudioAsset(
		"asset-clock",
		"alice",
		"clock-upload",
		"audio/v1/assets/asset-clock.wav",
		"audio/wav",
		3,
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		time.Second,
		now,
		now.Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	slowClock := now.Add(-time.Hour)
	if err := asset.commitMetadata("etag", slowClock); err != nil {
		t.Fatal(err)
	}
	if err := asset.bindTurn("candidate", "turn", slowClock); err != nil {
		t.Fatal(err)
	}
	if err := asset.beginDeleting(slowClock); err != nil {
		t.Fatal(err)
	}
	if err := asset.finishDeleting(slowClock); err != nil {
		t.Fatal(err)
	}
	if !asset.UpdatedAt.Equal(now) || !asset.DeletedAt.Equal(now) {
		t.Fatalf("slow clock moved timestamps backward: %#v", asset)
	}
	if err := asset.resumeDeletingForLateObject(slowClock); err != nil {
		t.Fatal(err)
	}
	if !asset.UpdatedAt.Equal(now) || !asset.DeletedAt.IsZero() {
		t.Fatalf("slow clock reopened timestamps incorrectly: %#v", asset)
	}
}

func TestAudioAssetUploadRetryRejectsDeletingAndDeletedTombstones(t *testing.T) {
	fixture := newAudioAssetFixture()

	deleting := fixture.upload(t, fixture.alice, "deleting-upload")
	fixture.store.deleteErr = objectstore.ErrOperationFailed
	if _, err := fixture.service.Delete(
		fixture.ctx,
		fixture.alice,
		deleting.ID,
	); !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatalf("Delete() error = %v", err)
	}
	putCalls := fixture.store.putCallCount()
	if _, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("deleting-upload"),
	); !errors.Is(err, ErrAudioAssetUploadTerminated) {
		t.Fatalf("retry deleting Upload() error = %v", err)
	}
	if fixture.store.putCallCount() != putCalls {
		t.Fatal("retry of deleting upload reached object storage")
	}

	fixture.store.deleteErr = nil
	if _, err := fixture.service.Delete(
		fixture.ctx,
		fixture.alice,
		deleting.ID,
	); err != nil {
		t.Fatalf("retry Delete() error = %v", err)
	}
	if _, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("deleting-upload"),
	); !errors.Is(err, ErrAudioAssetUploadTerminated) {
		t.Fatalf("retry deleted Upload() error = %v", err)
	}
	if fixture.store.putCallCount() != putCalls {
		t.Fatal("retry of deleted upload reached object storage")
	}
}

func TestAudioAssetUploadLeaseExcludesRetryCleanupAndDelete(t *testing.T) {
	if audioUploadOperationTimeout >= defaultUploadLease {
		t.Fatalf(
			"upload timeout %s must be shorter than lease %s",
			audioUploadOperationTimeout,
			defaultUploadLease,
		)
	}
	fixture := newAudioAssetFixture()
	putStarted := make(chan struct{})
	allowPut := make(chan struct{})
	var once sync.Once
	fixture.store.onPut = func(
		ctx context.Context,
		_ objectstore.PutRequest,
	) error {
		once.Do(func() {
			close(putStarted)
		})
		select {
		case <-allowPut:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	type uploadOutcome struct {
		asset AudioAsset
		err   error
	}
	first := make(chan uploadOutcome, 1)
	go func() {
		asset, err := fixture.service.Upload(
			fixture.ctx,
			fixture.alice,
			fixture.uploadRequest("exclusive-upload"),
		)
		first <- uploadOutcome{asset: asset, err: err}
	}()
	<-putStarted

	if _, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("exclusive-upload"),
	); !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("concurrent Upload() error = %v", err)
	}
	staged, err := fixture.repository.GetByUploadRequest(
		fixture.ctx,
		fixture.alice.UserID,
		"exclusive-upload",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Delete(
		fixture.ctx,
		fixture.alice,
		staged.ID,
	); !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("Delete() during upload error = %v", err)
	}
	if fixture.store.deleteCallCount() != 0 {
		t.Fatal("Delete() reached object storage during an active upload lease")
	}
	pending, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		10,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!pending.Pending ||
		pending.Deleted != 0 ||
		pending.Purged != 0 {
		t.Fatalf("CleanupAccountData() during upload = %#v, %v", pending, err)
	}

	close(allowPut)
	completed := <-first
	fixture.store.onPut = nil
	if completed.err != nil ||
		completed.asset.Status != AudioAssetMetadataCommitted {
		t.Fatalf("first Upload() = %#v, %v", completed.asset, completed.err)
	}
	retried, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("exclusive-upload"),
	)
	if err != nil || retried.ID != completed.asset.ID {
		t.Fatalf("settled retry Upload() = %#v, %v", retried, err)
	}
	if calls := fixture.store.putCallCount(); calls != 1 {
		t.Fatalf("Put() calls = %d, want one exclusive writer", calls)
	}
}

func TestAudioAssetUploadClaimDeadlineNeverReachesObjectStore(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.repository.beforeUploadClaim = func(ctx context.Context) error {
		<-ctx.Done()
		// Model an adapter that returns a claim after its caller deadline. The
		// service must reject it before crossing the object-store boundary.
		return nil
	}
	uploadCtx, cancelUpload := context.WithTimeout(
		context.Background(),
		25*time.Millisecond,
	)
	defer cancelUpload()

	_, err := fixture.service.Upload(
		uploadCtx,
		fixture.alice,
		fixture.uploadRequest("claim-deadline"),
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Upload() error = %v", err)
	}
	if fixture.store.putCallCount() != 0 {
		t.Fatal("expired claim operation reached object storage")
	}

	fixture.repository.beforeUploadClaim = nil
	pending, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!pending.Pending ||
		pending.Deleted != 0 ||
		pending.Purged != 0 {
		t.Fatalf("pre-expiry CleanupAccountData() = %#v, %v", pending, err)
	}
	fixture.clock.now = fixture.clock.now.Add(defaultUploadLease + time.Second)
	completed, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if err != nil ||
		completed.Pending ||
		completed.Deleted != 1 ||
		completed.Purged != 1 {
		t.Fatalf("post-expiry CleanupAccountData() = %#v, %v", completed, err)
	}
	if fixture.store.hasObject("audio/v1/assets/asset-1.wav") {
		t.Fatal("claim deadline left an object after account purge")
	}
}

func TestAudioAssetExpiredUploadClaimNeverReachesObjectStore(t *testing.T) {
	fixture := newAudioAssetFixture()
	uploadCtx, cancelUpload := context.WithCancel(context.Background())
	defer cancelUpload()
	fixture.repository.expireUploadClaim = true
	fixture.repository.afterUploadClaim = cancelUpload

	_, err := fixture.service.Upload(
		uploadCtx,
		fixture.alice,
		fixture.uploadRequest("expired-claim"),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Upload() error = %v", err)
	}
	if fixture.store.putCallCount() != 0 {
		t.Fatal("expired database claim reached object storage")
	}

	fixture.repository.expireUploadClaim = false
	fixture.repository.afterUploadClaim = nil
	completed, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if err != nil ||
		completed.Pending ||
		completed.Deleted != 1 ||
		completed.Purged != 1 {
		t.Fatalf("CleanupAccountData() = %#v, %v", completed, err)
	}
	if fixture.store.hasObject("audio/v1/assets/asset-1.wav") {
		t.Fatal("expired claim left an object after account purge")
	}
}

func TestAudioAssetTimedOutPutRemainsPendingUntilLeaseCleanup(t *testing.T) {
	fixture := newAudioAssetFixture()
	putStarted := make(chan struct{})
	fixture.store.onPut = func(
		ctx context.Context,
		_ objectstore.PutRequest,
	) error {
		close(putStarted)
		<-ctx.Done()
		return ctx.Err()
	}
	uploadCtx, cancelUpload := context.WithTimeout(
		context.Background(),
		25*time.Millisecond,
	)
	defer cancelUpload()
	uploadDone := make(chan error, 1)
	go func() {
		_, err := fixture.service.Upload(
			uploadCtx,
			fixture.alice,
			fixture.uploadRequest("timed-out-put"),
		)
		uploadDone <- err
	}()
	<-putStarted

	active, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!active.Pending ||
		active.Deleted != 0 ||
		active.Purged != 0 {
		t.Fatalf("active CleanupAccountData() = %#v, %v", active, err)
	}
	if err := <-uploadDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out Upload() error = %v", err)
	}
	asset, err := fixture.repository.GetByUploadRequest(
		fixture.ctx,
		fixture.alice.UserID,
		"timed-out-put",
	)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.store.hasObject(asset.ObjectKey) {
		t.Fatal("context-aware Put materialized an object after timing out")
	}

	stillLeased, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!stillLeased.Pending ||
		stillLeased.Deleted != 0 {
		t.Fatalf("pre-expiry CleanupAccountData() = %#v, %v", stillLeased, err)
	}

	fixture.clock.now = fixture.clock.now.Add(defaultUploadLease + time.Second)
	completed, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if err != nil ||
		completed.Pending ||
		completed.Deleted != 1 ||
		completed.Purged != 1 {
		t.Fatalf("post-expiry CleanupAccountData() = %#v, %v", completed, err)
	}
	if fixture.store.hasObject(asset.ObjectKey) {
		t.Fatal("post-expiry cleanup left an audio object")
	}
}

func TestAudioAssetAmbiguousPutFailureKeepsLeaseUntilCleanupTakeover(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.store.putErr = objectstore.ErrOperationFailed
	fixture.store.putObjectOnError = true
	_, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("ambiguous-put"),
	)
	if !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatalf("Upload() error = %v", err)
	}
	stale, err := fixture.repository.GetByUploadRequest(
		fixture.ctx,
		fixture.alice.UserID,
		"ambiguous-put",
	)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.repository.uploadReleaseCallCount() != 0 {
		t.Fatal("ambiguous Put failure released its write lease")
	}
	if !fixture.store.hasObject(stale.ObjectKey) {
		t.Fatal("fixture did not model an object created before an ambiguous response")
	}

	fixture.store.putErr = nil
	fixture.store.putObjectOnError = false
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)
	reclaimed, err := fixture.service.ReclaimExpired(fixture.ctx, 1)
	if err != nil || reclaimed.Deleted != 1 {
		t.Fatalf("ReclaimExpired() = %#v, %v", reclaimed, err)
	}
	current, err := fixture.repository.GetOwned(
		fixture.ctx,
		stale.OwnerID,
		stale.ID,
	)
	if err != nil || current.Status != AudioAssetDeleted {
		t.Fatalf("reclaimed asset = %#v, %v", current, err)
	}
	if fixture.store.hasObject(stale.ObjectKey) {
		t.Fatal("lease-expiry cleanup left the ambiguously created object")
	}

	staleCommitted := stale
	if err := staleCommitted.commitMetadata("late-etag", fixture.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.CommitUploadClaim(
		fixture.ctx,
		staleCommitted,
		stale.Version,
		1,
	); !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("stale CommitUploadClaim() error = %v", err)
	}
	if err := fixture.repository.ReleaseUploadClaim(
		fixture.ctx,
		stale.OwnerID,
		stale.ID,
		1,
	); !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("stale ReleaseUploadClaim() error = %v", err)
	}
}

func TestAudioAssetLocalPutFailureReleasesUploadLease(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.store.putErr = objectstore.ErrInvalidObject
	if _, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("local-put-failure"),
	); !errors.Is(err, objectstore.ErrInvalidObject) {
		t.Fatalf("Upload() error = %v", err)
	}
	if fixture.repository.uploadReleaseCallCount() != 1 {
		t.Fatal("local Put validation failure did not release upload lease")
	}

	fixture.store.putErr = nil
	asset, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		fixture.uploadRequest("local-put-failure"),
	)
	if err != nil || asset.Status != AudioAssetMetadataCommitted {
		t.Fatalf("retry Upload() = %#v, %v", asset, err)
	}
}

func TestAudioAssetLatePutCompensationIsDurablyRetryable(t *testing.T) {
	fixture := newAudioAssetFixture()
	putStarted := make(chan struct{})
	allowPut := make(chan struct{})
	fixture.store.onPut = func(
		ctx context.Context,
		_ objectstore.PutRequest,
	) error {
		close(putStarted)
		select {
		case <-allowPut:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	type uploadOutcome struct {
		asset AudioAsset
		err   error
	}
	outcome := make(chan uploadOutcome, 1)
	go func() {
		asset, err := fixture.service.Upload(
			fixture.ctx,
			fixture.alice,
			fixture.uploadRequest("late-put"),
		)
		outcome <- uploadOutcome{asset: asset, err: err}
	}()

	<-putStarted
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)
	reclaimed, err := fixture.service.ReclaimExpired(fixture.ctx, 10)
	if err != nil {
		t.Fatalf("ReclaimExpired() error = %v", err)
	}
	if reclaimed.Deleted != 1 || reclaimed.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v", reclaimed)
	}
	assertAudioAssetStatus(t, fixture, "asset-1", AudioAssetDeleted)
	if fixture.store.hasObject("audio/v1/assets/asset-1.wav") {
		t.Fatal("cleanup observed an object before delayed Put completed")
	}

	// Force the immediate compensation to fail. It must first reopen the
	// tombstone as deleting so the background cleanup can retry durably.
	fixture.store.deleteErr = objectstore.ErrOperationFailed
	close(allowPut)
	uploaded := <-outcome
	fixture.store.onPut = nil
	if !errors.Is(uploaded.err, ErrAudioAssetConcurrentUpdate) ||
		!errors.Is(uploaded.err, objectstore.ErrOperationFailed) {
		t.Fatalf("late Upload() = %#v, %v", uploaded.asset, uploaded.err)
	}
	retryable, err := fixture.repository.Get(fixture.ctx, "asset-1")
	if err != nil {
		t.Fatal(err)
	}
	if retryable.Status != AudioAssetDeleting ||
		retryable.Version != 5 ||
		!retryable.DeletedAt.IsZero() ||
		!retryable.UpdatedAt.Equal(fixture.clock.now) {
		t.Fatalf("retryable cleanup asset = %#v", retryable)
	}
	if _, err := fixture.service.Playback(
		fixture.ctx,
		fixture.alice,
		retryable.ID,
	); !errors.Is(err, ErrAudioAssetInvalidTransition) {
		t.Fatalf("Playback() during compensation error = %v", err)
	}
	if !fixture.store.hasObject("audio/v1/assets/asset-1.wav") {
		t.Fatal("fixture did not reproduce a late object after cleanup")
	}

	fixture.store.deleteErr = nil
	retried, err := fixture.service.ReclaimExpired(fixture.ctx, 10)
	if err != nil {
		t.Fatalf("retry ReclaimExpired() error = %v", err)
	}
	if retried.Deleted != 1 || retried.Failed != 0 {
		t.Fatalf("retry ReclaimExpired() = %#v", retried)
	}
	assertAudioAssetStatus(t, fixture, "asset-1", AudioAssetDeleted)
	if fixture.store.hasObject("audio/v1/assets/asset-1.wav") {
		t.Fatal("retry cleanup left the late object orphaned")
	}
	if calls := fixture.store.deleteCallCount(); calls != 3 {
		t.Fatalf("Delete() calls = %d, want cleanup + compensation + retry", calls)
	}
}

func TestAudioAssetUploadRejectsInvalidSHA256BeforePut(t *testing.T) {
	fixture := newAudioAssetFixture()
	request := fixture.uploadRequest("invalid-checksum")
	request.ChecksumSHA256 = "checksum"

	if _, err := fixture.service.Upload(fixture.ctx, fixture.alice, request); !errors.Is(err, ErrAudioAssetInvalid) {
		t.Fatalf("Upload() error = %v", err)
	}
	if fixture.store.putCallCount() != 0 {
		t.Fatal("invalid checksum reached object store")
	}
}

func TestAudioAssetIdentifiersRejectInvalidUTF8AndOver128Bytes(t *testing.T) {
	fixture := newAudioAssetFixture()
	tooLong := strings.Repeat("a", maxAudioAssetIdentifierBytes+1)
	invalidUTF8 := string([]byte{0xff})

	for name, values := range map[string][3]string{
		"asset ID":       {tooLong, "alice", "request"},
		"owner ID":       {"asset", invalidUTF8, "request"},
		"upload request": {"asset", "alice", tooLong},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newStagedAudioAsset(
				values[0],
				values[1],
				values[2],
				"audio/v1/assets/asset.wav",
				"audio/wav",
				3,
				"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				time.Second,
				fixture.clock.Now(),
				fixture.clock.Now().Add(time.Hour),
			)
			if !errors.Is(err, ErrAudioAssetInvalid) {
				t.Fatalf("newStagedAudioAsset() error = %v", err)
			}
		})
	}

	if _, err := fixture.service.Upload(
		fixture.ctx,
		AudioAssetActor{UserID: tooLong},
		fixture.uploadRequest("invalid-owner"),
	); !errors.Is(err, ErrAudioAssetInvalid) {
		t.Fatalf("long-owner Upload() error = %v", err)
	}
	request := fixture.uploadRequest(tooLong)
	if _, err := fixture.service.Upload(
		fixture.ctx,
		fixture.alice,
		request,
	); !errors.Is(err, ErrAudioAssetInvalid) {
		t.Fatalf("long-request Upload() error = %v", err)
	}
	if fixture.store.putCallCount() != 0 {
		t.Fatal("invalid identifier reached object storage")
	}

	asset := fixture.upload(t, fixture.alice, "valid-identifiers")
	if _, err := fixture.service.Confirm(
		fixture.ctx,
		fixture.alice,
		asset.ID,
		tooLong,
		"turn-1",
	); !errors.Is(err, ErrAudioAssetInvalid) {
		t.Fatalf("long-candidate Confirm() error = %v", err)
	}
	if _, err := fixture.service.Confirm(
		fixture.ctx,
		fixture.alice,
		asset.ID,
		"candidate-1",
		invalidUTF8,
	); !errors.Is(err, ErrAudioAssetInvalid) {
		t.Fatalf("invalid-turn Confirm() error = %v", err)
	}
	if _, err := fixture.service.Playback(
		fixture.ctx,
		fixture.alice,
		tooLong,
	); !errors.Is(err, ErrAudioAssetInvalid) {
		t.Fatalf("long-ID Playback() error = %v", err)
	}
}

func TestAudioAssetOpaqueActorAndAssetIDsRejectPadding(t *testing.T) {
	fixture := newAudioAssetFixture()
	paddedAlice := AudioAssetActor{UserID: " alice "}
	if _, err := fixture.service.Upload(
		fixture.ctx,
		paddedAlice,
		fixture.uploadRequest("padded-actor"),
	); !errors.Is(err, ErrAudioAssetInvalid) {
		t.Fatalf("padded-actor Upload() error = %v", err)
	}
	if fixture.store.putCallCount() != 0 {
		t.Fatal("padded Actor reached object storage")
	}

	asset := fixture.upload(t, fixture.alice, "opaque-identifiers")
	for name, operation := range map[string]func() error{
		"Confirm actor": func() error {
			_, err := fixture.service.Confirm(
				fixture.ctx,
				paddedAlice,
				asset.ID,
				"candidate-1",
				"turn-1",
			)
			return err
		},
		"Confirm asset": func() error {
			_, err := fixture.service.Confirm(
				fixture.ctx,
				fixture.alice,
				" "+asset.ID,
				"candidate-1",
				"turn-1",
			)
			return err
		},
		"Playback asset": func() error {
			_, err := fixture.service.Playback(
				fixture.ctx,
				fixture.alice,
				asset.ID+" ",
			)
			return err
		},
		"Delete actor": func() error {
			_, err := fixture.service.Delete(
				fixture.ctx,
				paddedAlice,
				asset.ID,
			)
			return err
		},
		"Account cleanup actor": func() error {
			_, err := fixture.service.CleanupAccountData(
				fixture.ctx,
				paddedAlice,
				1,
			)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, ErrAudioAssetInvalid) {
				t.Fatalf("operation error = %v", err)
			}
		})
	}
	persisted, err := fixture.repository.GetOwned(
		fixture.ctx,
		fixture.alice.UserID,
		asset.ID,
	)
	if err != nil || persisted.Status != AudioAssetMetadataCommitted {
		t.Fatalf("padded input mutated asset = %#v, %v", persisted, err)
	}
}

func TestAudioAssetConfirmIsIdempotentAndTurnIsUnique(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "upload-1")

	if _, err := fixture.service.Confirm(fixture.ctx, fixture.bob, asset.ID, "candidate-1", "turn-1"); !errors.Is(err, ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner Confirm() error = %v", err)
	}
	confirmed, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "candidate-1", "turn-1")
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if confirmed.Status != AudioAssetReadable ||
		confirmed.CandidateID != "candidate-1" ||
		confirmed.TurnID != "turn-1" {
		t.Fatalf("Confirm() = %#v", confirmed)
	}
	version := confirmed.Version

	again, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "candidate-1", "turn-1")
	if err != nil {
		t.Fatalf("idempotent Confirm() error = %v", err)
	}
	if again.Version != version {
		t.Fatalf("idempotent Confirm() version = %d, want %d", again.Version, version)
	}

	second := fixture.upload(t, fixture.alice, "upload-2")
	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, second.ID, "candidate-1", "turn-1"); !errors.Is(err, ErrAudioAssetAlreadyBound) {
		t.Fatalf("duplicate turn Confirm() error = %v", err)
	}
}

func TestAudioAssetConfirmRequiresOwnedExistingTurn(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "upload-1")

	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "missing-candidate", "missing-turn"); !errors.Is(err, ErrAudioAssetTurnNotFound) {
		t.Fatalf("missing Turn Confirm() error = %v", err)
	}
	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "bob-candidate", "bob-turn"); !errors.Is(err, ErrAudioAssetTurnNotFound) {
		t.Fatalf("cross-owner Turn Confirm() error = %v", err)
	}
	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "candidate-2", "turn-1"); !errors.Is(err, ErrAudioAssetAlreadyBound) {
		t.Fatalf("wrong Candidate Confirm() error = %v", err)
	}
	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "candidate-1", "turn-2"); !errors.Is(err, ErrAudioAssetAlreadyBound) {
		t.Fatalf("same-owner wrong Turn Confirm() error = %v", err)
	}
}

func TestAudioAssetBindingsAreScopedByOwner(t *testing.T) {
	fixture := newAudioAssetFixture()
	aliceAsset := fixture.upload(t, fixture.alice, "alice-shared-ids")
	bobAsset := fixture.upload(t, fixture.bob, "bob-shared-ids")

	aliceAsset, err := fixture.service.Confirm(
		fixture.ctx,
		fixture.alice,
		aliceAsset.ID,
		"shared-candidate",
		"shared-turn",
	)
	if err != nil {
		t.Fatalf("Alice Confirm() error = %v", err)
	}
	bobAsset, err = fixture.service.Confirm(
		fixture.ctx,
		fixture.bob,
		bobAsset.ID,
		"shared-candidate",
		"shared-turn",
	)
	if err != nil {
		t.Fatalf("Bob Confirm() error = %v", err)
	}
	if aliceAsset.Status != AudioAssetReadable || bobAsset.Status != AudioAssetReadable {
		t.Fatalf("scoped bindings = %#v, %#v", aliceAsset, bobAsset)
	}

	aliceByTurn, err := fixture.repository.GetByTurn(
		fixture.ctx,
		fixture.alice.UserID,
		"shared-turn",
	)
	if err != nil || aliceByTurn.ID != aliceAsset.ID {
		t.Fatalf("Alice GetByTurn() = %#v, %v", aliceByTurn, err)
	}
	bobByCandidate, err := fixture.repository.GetByCandidate(
		fixture.ctx,
		fixture.bob.UserID,
		"shared-candidate",
	)
	if err != nil || bobByCandidate.ID != bobAsset.ID {
		t.Fatalf("Bob GetByCandidate() = %#v, %v", bobByCandidate, err)
	}
}

func TestAudioAssetPlaybackRequiresOwnerReadableAndShortTTL(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "upload-1")

	if _, err := fixture.service.Playback(fixture.ctx, fixture.alice, asset.ID); !errors.Is(err, ErrAudioAssetInvalidTransition) {
		t.Fatalf("unconfirmed Playback() error = %v", err)
	}
	asset, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "candidate-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Playback(fixture.ctx, fixture.bob, asset.ID); !errors.Is(err, ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner Playback() error = %v", err)
	}

	fixture.store.signedResult = objectstore.SignedGetResult{
		URL:       "https://private.invalid/signed",
		ExpiresAt: fixture.clock.now.Add(MaxPlaybackURLTTL),
	}
	result, err := fixture.service.Playback(fixture.ctx, fixture.alice, asset.ID)
	if err != nil || result.URL == "" {
		t.Fatalf("Playback() = %#v, %v", result, err)
	}
	fixture.store.signedResult.ExpiresAt = fixture.clock.now.Add(MaxPlaybackURLTTL + time.Second)
	if _, err := fixture.service.Playback(fixture.ctx, fixture.alice, asset.ID); !errors.Is(err, ErrAudioAssetPlaybackTTL) {
		t.Fatalf("long TTL Playback() error = %v", err)
	}

	for name, signedResult := range map[string]objectstore.SignedGetResult{
		"empty URL": {
			ExpiresAt: fixture.clock.now.Add(time.Minute),
		},
		"HTTP URL": {
			URL:       "http://private.invalid/signed",
			ExpiresAt: fixture.clock.now.Add(time.Minute),
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture.store.signedResult = signedResult
			if _, err := fixture.service.Playback(fixture.ctx, fixture.alice, asset.ID); !errors.Is(err, ErrAudioAssetPlaybackURL) {
				t.Fatalf("Playback() error = %v", err)
			}
		})
	}
	fixture.store.signedResult = objectstore.SignedGetResult{
		URL:       "https://private.invalid/signed",
		ExpiresAt: fixture.clock.now,
	}
	if _, err := fixture.service.Playback(fixture.ctx, fixture.alice, asset.ID); !errors.Is(err, ErrAudioAssetPlaybackTTL) {
		t.Fatalf("non-future expiry Playback() error = %v", err)
	}
}

func TestAudioAssetDeleteFailureStaysDeletingAndRetries(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "upload-1")
	asset, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "candidate-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Delete(fixture.ctx, fixture.bob, asset.ID); !errors.Is(err, ErrAudioAssetNotFound) {
		t.Fatalf("cross-owner Delete() error = %v", err)
	}

	fixture.store.deleteErr = objectstore.ErrOperationFailed
	deleting, err := fixture.service.Delete(fixture.ctx, fixture.alice, asset.ID)
	if !errors.Is(err, objectstore.ErrOperationFailed) || deleting.Status != AudioAssetDeleting {
		t.Fatalf("failed Delete() = %#v, %v", deleting, err)
	}
	persisted, _ := fixture.repository.Get(fixture.ctx, asset.ID)
	if persisted.Status != AudioAssetDeleting {
		t.Fatalf("persisted status = %q, want deleting", persisted.Status)
	}

	fixture.store.deleteErr = nil
	deleted, err := fixture.service.Delete(fixture.ctx, fixture.alice, asset.ID)
	if err != nil {
		t.Fatalf("retry Delete() error = %v", err)
	}
	if deleted.Status != AudioAssetDeleted || deleted.DeletedAt.IsZero() {
		t.Fatalf("retry Delete() = %#v", deleted)
	}
}

func TestAudioAssetReclaimExpiredUnconfirmedAndDeleting(t *testing.T) {
	fixture := newAudioAssetFixture()
	expiredStaged := fixture.uploadStaged(t, fixture.alice, "expired-staged")
	expiredUploaded := fixture.upload(t, fixture.alice, "expired-uploaded")
	if expiredUploaded.Status != AudioAssetMetadataCommitted || expiredUploaded.TurnID != "" {
		t.Fatalf("unconfirmed uploaded fixture = %#v", expiredUploaded)
	}
	pending := fixture.upload(t, fixture.alice, "pending")
	pending, err := fixture.service.Delete(fixture.ctx, fixture.alice, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != AudioAssetDeleted {
		t.Fatal("fixture delete did not finish")
	}

	retry := fixture.upload(t, fixture.alice, "retry")
	fixture.store.deleteErr = objectstore.ErrOperationFailed
	if _, err := fixture.service.Delete(fixture.ctx, fixture.alice, retry.ID); err == nil {
		t.Fatal("Delete() unexpectedly succeeded")
	}
	fixture.store.deleteErr = nil
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)

	result, err := fixture.service.ReclaimExpired(fixture.ctx, 10)
	if err != nil {
		t.Fatalf("ReclaimExpired() error = %v", err)
	}
	if result.Deleted != 3 || result.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v", result)
	}
	for _, id := range []string{expiredStaged.ID, expiredUploaded.ID, retry.ID} {
		asset, _ := fixture.repository.Get(fixture.ctx, id)
		if asset.Status != AudioAssetDeleted {
			t.Fatalf("asset %q status = %q", id, asset.Status)
		}
	}
}

func TestAudioAssetReclaimSkipsWhenConfirmWinsClaimRace(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "confirm-wins")
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)

	claimReady := make(chan struct{})
	allowClaim := make(chan struct{})
	var once sync.Once
	fixture.repository.beforeCleanupClaim = func() {
		once.Do(func() {
			close(claimReady)
			<-allowClaim
		})
	}
	type reclaimOutcome struct {
		result AudioAssetCleanupResult
		err    error
	}
	outcome := make(chan reclaimOutcome, 1)
	go func() {
		result, err := fixture.service.ReclaimExpired(fixture.ctx, 10)
		outcome <- reclaimOutcome{result: result, err: err}
	}()

	<-claimReady
	confirmed, err := fixture.service.Confirm(
		fixture.ctx,
		fixture.alice,
		asset.ID,
		"candidate-1",
		"turn-1",
	)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	close(allowClaim)
	reclaimed := <-outcome
	fixture.repository.beforeCleanupClaim = nil

	if reclaimed.err != nil {
		t.Fatalf("ReclaimExpired() error = %v", reclaimed.err)
	}
	if reclaimed.result.Deleted != 0 || reclaimed.result.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v", reclaimed.result)
	}
	if confirmed.Status != AudioAssetReadable {
		t.Fatalf("Confirm() asset = %#v", confirmed)
	}
	assertAudioAssetStatus(t, fixture, asset.ID, AudioAssetReadable)
	if fixture.store.deleteCallCount() != 0 {
		t.Fatal("cleanup deleted an asset after Confirm won the claim race")
	}
}

func TestAudioAssetConfirmFailsWhenReclaimClaimsFirst(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "cleanup-wins")
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)

	claimed := make(chan struct{})
	allowDelete := make(chan struct{})
	var once sync.Once
	fixture.store.beforeDelete = func(key string) {
		if key == asset.ObjectKey {
			once.Do(func() {
				close(claimed)
				<-allowDelete
			})
		}
	}
	type reclaimOutcome struct {
		result AudioAssetCleanupResult
		err    error
	}
	outcome := make(chan reclaimOutcome, 1)
	go func() {
		result, err := fixture.service.ReclaimExpired(fixture.ctx, 10)
		outcome <- reclaimOutcome{result: result, err: err}
	}()

	<-claimed
	if _, err := fixture.service.Confirm(
		fixture.ctx,
		fixture.alice,
		asset.ID,
		"candidate-1",
		"turn-1",
	); !errors.Is(err, ErrAudioAssetInvalidTransition) {
		t.Fatalf("Confirm() error = %v", err)
	}
	close(allowDelete)
	reclaimed := <-outcome
	fixture.store.beforeDelete = nil

	if reclaimed.err != nil ||
		reclaimed.result.Deleted != 1 ||
		reclaimed.result.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v, %v", reclaimed.result, reclaimed.err)
	}
	assertAudioAssetStatus(t, fixture, asset.ID, AudioAssetDeleted)
}

func TestAudioAssetAccountDataCleanupIsOwnerScopedAndRetriesFailures(t *testing.T) {
	fixture := newAudioAssetFixture()
	aliceUnconfirmed := fixture.upload(t, fixture.alice, "alice-unconfirmed")
	aliceReadable := fixture.upload(t, fixture.alice, "alice-readable")
	var err error
	aliceReadable, err = fixture.service.Confirm(
		fixture.ctx,
		fixture.alice,
		aliceReadable.ID,
		"candidate-1",
		"turn-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	bobAsset := fixture.upload(t, fixture.bob, "bob-unconfirmed")

	fixture.store.deleteErrors = map[string]error{
		aliceReadable.ObjectKey: objectstore.ErrOperationFailed,
	}
	result, err := fixture.service.CleanupAccountData(fixture.ctx, fixture.alice, 10)
	if !errors.Is(err, ErrAudioAssetCleanupPending) {
		t.Fatalf("CleanupAccountData() error = %v", err)
	}
	if result.Deleted != 1 ||
		result.Failed != 1 ||
		result.Purged != 1 ||
		!result.Pending {
		t.Fatalf("CleanupAccountData() = %#v", result)
	}
	if _, err := fixture.repository.Get(
		fixture.ctx,
		aliceUnconfirmed.ID,
	); !errors.Is(err, ErrAudioAssetNotFound) {
		t.Fatalf("purged Alice asset lookup error = %v", err)
	}
	assertAudioAssetStatus(t, fixture, aliceReadable.ID, AudioAssetDeleting)
	assertAudioAssetStatus(t, fixture, bobAsset.ID, AudioAssetMetadataCommitted)

	fixture.store.deleteErrors = nil
	retried, err := fixture.service.ReclaimExpired(fixture.ctx, 10)
	if err != nil {
		t.Fatalf("ReclaimExpired() error = %v", err)
	}
	if retried.Deleted != 1 || retried.Failed != 0 {
		t.Fatalf("ReclaimExpired() = %#v", retried)
	}
	assertAudioAssetStatus(t, fixture, aliceReadable.ID, AudioAssetDeleted)
	assertAudioAssetStatus(t, fixture, bobAsset.ID, AudioAssetMetadataCommitted)

	completed, err := fixture.service.CleanupAccountData(fixture.ctx, fixture.alice, 10)
	if err != nil {
		t.Fatalf("completion CleanupAccountData() error = %v", err)
	}
	if completed.Deleted != 0 ||
		completed.Failed != 0 ||
		completed.Purged != 1 ||
		completed.Pending {
		t.Fatalf("completion CleanupAccountData() = %#v", completed)
	}
	idempotent, err := fixture.service.CleanupAccountData(fixture.ctx, fixture.alice, 10)
	if err != nil {
		t.Fatalf("idempotent CleanupAccountData() error = %v", err)
	}
	if idempotent != (AudioAssetCleanupResult{}) {
		t.Fatalf("idempotent CleanupAccountData() = %#v", idempotent)
	}
}

func TestAudioAssetAccountCleanupStaysPendingBeyondBatchLimit(t *testing.T) {
	fixture := newAudioAssetFixture()
	first := fixture.upload(t, fixture.alice, "batch-first")
	second := fixture.upload(t, fixture.alice, "batch-second")

	result, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!result.Pending ||
		result.Deleted != 1 ||
		result.Purged != 1 {
		t.Fatalf("first CleanupAccountData() = %#v, %v", result, err)
	}
	_, firstErr := fixture.repository.Get(fixture.ctx, first.ID)
	_, secondErr := fixture.repository.Get(fixture.ctx, second.ID)
	if !errors.Is(firstErr, ErrAudioAssetNotFound) &&
		!errors.Is(secondErr, ErrAudioAssetNotFound) {
		t.Fatal("bounded cleanup did not purge either terminal row")
	}

	result, err = fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if err != nil ||
		result.Pending ||
		result.Deleted != 1 ||
		result.Purged != 1 {
		t.Fatalf("second CleanupAccountData() = %#v, %v", result, err)
	}
}

func TestAudioAssetAccountCleanupSeesAnotherWorkersClaim(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.upload(t, fixture.alice, "claimed-elsewhere")
	claims, err := fixture.repository.ClaimOwnerAssetsForAccountCleanup(
		fixture.ctx,
		fixture.alice.UserID,
		defaultCleanupLease,
		1,
	)
	if err != nil || len(claims) != 1 {
		t.Fatalf("ClaimOwnerAssetsForAccountCleanup() = %#v, %v", claims, err)
	}

	result, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!result.Pending ||
		result.Deleted != 0 ||
		result.Purged != 0 {
		t.Fatalf("CleanupAccountData() with held claim = %#v, %v", result, err)
	}
}

func TestAudioAssetAccountCleanupReleaseFailureRemainsPending(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.upload(t, fixture.alice, "release-failure")
	fixture.store.deleteErr = objectstore.ErrOperationFailed
	releaseErr := errors.New("release cleanup claim failed")
	fixture.repository.cleanupReleaseErr = releaseErr

	result, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!result.Pending ||
		result.Failed != 1 {
		t.Fatalf("CleanupAccountData() = %#v, %v", result, err)
	}
	if fixture.repository.releaseCallCount() != 0 {
		t.Fatal("failed ReleaseCleanupClaim was recorded as released")
	}

	fixture.store.deleteErr = nil
	fixture.repository.cleanupReleaseErr = nil
	stillLeased, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!stillLeased.Pending ||
		stillLeased.Deleted != 0 {
		t.Fatalf("immediate retry with live lease = %#v, %v", stillLeased, err)
	}

	fixture.clock.now = fixture.clock.now.Add(defaultCleanupLease + time.Second)
	completed, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if err != nil ||
		completed.Pending ||
		completed.Deleted != 1 ||
		completed.Purged != 1 {
		t.Fatalf("expired lease retry = %#v, %v", completed, err)
	}
}

func TestAudioAssetAccountCleanupPropagatesPendingQueryFailure(t *testing.T) {
	fixture := newAudioAssetFixture()
	queryErr := errors.New("pending query failed")
	fixture.repository.hasOwnerErr = queryErr

	result, err := fixture.service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, queryErr) {
		t.Fatalf("CleanupAccountData() = %#v, %v", result, err)
	}
}

func TestAudioAssetAccountCleanupDoesNotReleaseCrossOwnerClaim(t *testing.T) {
	fixture := newAudioAssetFixture()
	bobAsset := fixture.upload(t, fixture.bob, "malicious-owner-claim")
	claims, err := fixture.repository.ClaimOwnerAssetsForAccountCleanup(
		fixture.ctx,
		fixture.bob.UserID,
		defaultCleanupLease,
		1,
	)
	if err != nil || len(claims) != 1 {
		t.Fatalf("Bob ClaimOwnerAssetsForAccountCleanup() = %#v, %v", claims, err)
	}
	repository := &crossOwnerAudioAssetRepository{
		memoryAudioAssetRepository: fixture.repository,
		claim:                      claims[0],
	}
	service, err := NewAudioAssetService(
		repository,
		fixture.store,
		fixture.ids,
		fixture.clock,
		fixture.turns,
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.CleanupAccountData(
		fixture.ctx,
		fixture.alice,
		1,
	)
	if !errors.Is(err, ErrAudioAssetCleanupPending) ||
		!result.Pending ||
		result.Failed != 1 {
		t.Fatalf("CleanupAccountData() = %#v, %v", result, err)
	}
	if fixture.repository.releaseCallCount() != 0 ||
		fixture.store.deleteCallCount() != 0 {
		t.Fatal("cross-owner claim was released or deleted")
	}
	persisted, err := fixture.repository.GetOwned(
		fixture.ctx,
		fixture.bob.UserID,
		bobAsset.ID,
	)
	if err != nil || persisted.Status != AudioAssetDeleting {
		t.Fatalf("Bob asset = %#v, %v", persisted, err)
	}
}

func TestAudioAssetClaimCleanupWorkersDeleteEachObjectOnce(t *testing.T) {
	fixture := newClaimAudioAssetFixture()
	const (
		assets  = 8
		workers = 16
	)
	for index := range assets {
		fixture.upload(
			t,
			fixture.alice,
			"claim-worker-"+strconv.Itoa(index),
		)
	}
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)

	start := make(chan struct{})
	results := make(chan AudioAssetCleanupResult, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := fixture.service.ReclaimExpired(fixture.ctx, 1)
			results <- result
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)

	deleted := 0
	failed := 0
	for result := range results {
		deleted += result.Deleted
		failed += result.Failed
	}
	for err := range errs {
		if err != nil {
			t.Errorf("ReclaimExpired() error = %v", err)
		}
	}
	if deleted != assets || failed != 0 {
		t.Fatalf("worker cleanup deleted=%d failed=%d", deleted, failed)
	}
	if calls := fixture.store.deleteCallCount(); calls != assets {
		t.Fatalf("Delete() calls = %d, want %d", calls, assets)
	}
}

func TestAudioAssetClaimCleanupRejectsExpiredWorkerFence(t *testing.T) {
	fixture := newClaimAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "expired-worker")
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)

	staleClaims, err := fixture.cleanupRepository.ClaimExpiredUnconfirmed(
		fixture.ctx,
		defaultCleanupLease,
		1,
	)
	if err != nil || len(staleClaims) != 1 {
		t.Fatalf("first ClaimExpiredUnconfirmed() = %#v, %v", staleClaims, err)
	}
	fixture.clock.now = fixture.clock.now.Add(defaultCleanupLease + time.Second)
	freshClaims, err := fixture.cleanupRepository.ClaimDeleting(
		fixture.ctx,
		defaultCleanupLease,
		1,
	)
	if err != nil || len(freshClaims) != 1 {
		t.Fatalf("ClaimDeleting() = %#v, %v", freshClaims, err)
	}
	if freshClaims[0].FencingToken <= staleClaims[0].FencingToken {
		t.Fatalf(
			"fresh fence = %d, stale fence = %d",
			freshClaims[0].FencingToken,
			staleClaims[0].FencingToken,
		)
	}

	if _, err := fixture.service.reclaimer.processCleanupClaim(
		fixture.ctx,
		staleClaims[0],
	); !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("stale processCleanupClaim() error = %v", err)
	}
	deleted, err := fixture.service.reclaimer.processCleanupClaim(
		fixture.ctx,
		freshClaims[0],
	)
	if err != nil || !deleted {
		t.Fatalf("fresh processCleanupClaim() = %v, %v", deleted, err)
	}
	assertAudioAssetStatus(
		t,
		fixture.audioAssetFixture,
		asset.ID,
		AudioAssetDeleted,
	)
	if calls := fixture.store.deleteCallCount(); calls != 2 {
		t.Fatalf("Delete() calls = %d, want stale and fresh attempts", calls)
	}
}

func TestAudioAssetClaimReleaseAndSaveAreIdempotent(t *testing.T) {
	fixture := newClaimAudioAssetFixture()

	uploadClaim, err := fixture.repository.ClaimUpload(
		fixture.ctx,
		fixture.alice.UserID,
		"missing",
		defaultUploadLease,
	)
	if !errors.Is(err, ErrAudioAssetNotFound) ||
		uploadClaim != (AudioAssetUploadClaim{}) {
		t.Fatalf("missing ClaimUpload() = %#v, %v", uploadClaim, err)
	}
	staged, err := fixture.service.createStaged(
		fixture.ctx,
		fixture.alice.UserID,
		fixture.uploadRequest("release-upload"),
	)
	if err != nil {
		t.Fatal(err)
	}
	uploadClaim, err = fixture.repository.ClaimUpload(
		fixture.ctx,
		fixture.alice.UserID,
		staged.UploadRequestID,
		defaultUploadLease,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.ReleaseUploadClaim(
		fixture.ctx,
		staged.OwnerID,
		staged.ID,
		uploadClaim.FencingToken,
	); err != nil {
		t.Fatal(err)
	}
	releasedUpload, _ := fixture.repository.GetOwned(
		fixture.ctx,
		staged.OwnerID,
		staged.ID,
	)
	if releasedUpload.Version != uploadClaim.Asset.Version+1 {
		t.Fatalf("released upload version = %d", releasedUpload.Version)
	}
	if err := fixture.repository.ReleaseUploadClaim(
		fixture.ctx,
		staged.OwnerID,
		staged.ID,
		uploadClaim.FencingToken,
	); err != nil {
		t.Fatalf("replayed ReleaseUploadClaim() error = %v", err)
	}
	replayedUpload, _ := fixture.repository.GetOwned(
		fixture.ctx,
		staged.OwnerID,
		staged.ID,
	)
	if replayedUpload.Version != releasedUpload.Version ||
		!replayedUpload.UpdatedAt.Equal(releasedUpload.UpdatedAt) {
		t.Fatal("replayed upload release mutated metadata")
	}

	releaseClaims, err := fixture.repository.ClaimOwnerAssetsForAccountCleanup(
		fixture.ctx,
		fixture.alice.UserID,
		defaultCleanupLease,
		1,
	)
	if err != nil || len(releaseClaims) != 1 {
		t.Fatalf("cleanup claim for release = %#v, %v", releaseClaims, err)
	}
	releaseClaim := releaseClaims[0]
	if err := fixture.repository.ReleaseCleanupClaim(
		fixture.ctx,
		releaseClaim.Asset.OwnerID,
		releaseClaim.Asset.ID,
		releaseClaim.FencingToken,
	); err != nil {
		t.Fatal(err)
	}
	releasedCleanup, _ := fixture.repository.GetOwned(
		fixture.ctx,
		releaseClaim.Asset.OwnerID,
		releaseClaim.Asset.ID,
	)
	if releasedCleanup.Version != releaseClaim.Asset.Version+1 {
		t.Fatalf("released cleanup version = %d", releasedCleanup.Version)
	}
	if err := fixture.repository.ReleaseCleanupClaim(
		fixture.ctx,
		releaseClaim.Asset.OwnerID,
		releaseClaim.Asset.ID,
		releaseClaim.FencingToken,
	); err != nil {
		t.Fatalf("replayed ReleaseCleanupClaim() error = %v", err)
	}
	replayedCleanup, _ := fixture.repository.GetOwned(
		fixture.ctx,
		releaseClaim.Asset.OwnerID,
		releaseClaim.Asset.ID,
	)
	if replayedCleanup.Version != releasedCleanup.Version ||
		!replayedCleanup.UpdatedAt.Equal(releasedCleanup.UpdatedAt) {
		t.Fatal("replayed cleanup release mutated metadata")
	}
	newClaims, err := fixture.repository.ClaimDeleting(
		fixture.ctx,
		defaultCleanupLease,
		1,
	)
	if err != nil || len(newClaims) != 1 {
		t.Fatalf("reclaimed cleanup = %#v, %v", newClaims, err)
	}
	if err := fixture.repository.ReleaseCleanupClaim(
		fixture.ctx,
		releaseClaim.Asset.OwnerID,
		releaseClaim.Asset.ID,
		releaseClaim.FencingToken,
	); !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("old-fence ReleaseCleanupClaim() error = %v", err)
	}

	asset := fixture.upload(t, fixture.bob, "save-cleanup")
	claims, err := fixture.repository.ClaimOwnerAssetsForAccountCleanup(
		fixture.ctx,
		fixture.bob.UserID,
		defaultCleanupLease,
		1,
	)
	if err != nil || len(claims) != 1 {
		t.Fatalf("ClaimOwnerAssetsForAccountCleanup() = %#v, %v", claims, err)
	}
	claim := claims[0]
	if claim.Asset.ID != asset.ID {
		t.Fatalf("claimed asset = %q, want %q", claim.Asset.ID, asset.ID)
	}
	deleted := claim.Asset
	expectedVersion := deleted.Version
	if err := deleted.finishDeleting(fixture.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.SaveCleanupClaim(
		fixture.ctx,
		deleted,
		expectedVersion,
		claim.FencingToken,
	); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.SaveCleanupClaim(
		fixture.ctx,
		deleted,
		expectedVersion,
		claim.FencingToken,
	); err != nil {
		t.Fatalf("replayed SaveCleanupClaim() error = %v", err)
	}
	conflicting := deleted
	conflicting.UpdatedAt = conflicting.UpdatedAt.Add(time.Second)
	if err := fixture.repository.SaveCleanupClaim(
		fixture.ctx,
		conflicting,
		expectedVersion,
		claim.FencingToken,
	); !errors.Is(err, ErrAudioAssetConcurrentUpdate) {
		t.Fatalf("conflicting SaveCleanupClaim() error = %v", err)
	}
}

func TestAudioAssetClaimCleanupReleasesFailedDeleteForImmediateRetry(t *testing.T) {
	fixture := newClaimAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "release-retry")
	fixture.clock.now = fixture.clock.now.Add(2 * time.Hour)
	fixture.store.deleteErr = objectstore.ErrOperationFailed

	failed, err := fixture.service.ReclaimExpired(fixture.ctx, 1)
	if err != nil {
		t.Fatalf("failed ReclaimExpired() error = %v", err)
	}
	if failed.Deleted != 0 || failed.Failed != 1 {
		t.Fatalf("failed ReclaimExpired() = %#v", failed)
	}
	if fixture.cleanupRepository.releaseCallCount() != 1 {
		t.Fatal("failed object delete did not release its cleanup claim")
	}

	fixture.store.deleteErr = nil
	retried, err := fixture.service.ReclaimExpired(fixture.ctx, 1)
	if err != nil {
		t.Fatalf("retry ReclaimExpired() error = %v", err)
	}
	if retried.Deleted != 1 || retried.Failed != 0 {
		t.Fatalf("retry ReclaimExpired() = %#v", retried)
	}
	assertAudioAssetStatus(
		t,
		fixture.audioAssetFixture,
		asset.ID,
		AudioAssetDeleted,
	)
}

func TestAudioAssetClaimReleaseMovesPoisonObjectBehindQueue(t *testing.T) {
	fixture := newClaimAudioAssetFixture()
	poison := fixture.upload(t, fixture.alice, "poison-first")
	next := fixture.upload(t, fixture.alice, "healthy-second")
	fixture.store.deleteErr = objectstore.ErrOperationFailed
	if _, err := fixture.service.Delete(
		fixture.ctx,
		fixture.alice,
		poison.ID,
	); !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatal(err)
	}
	if _, err := fixture.service.Delete(
		fixture.ctx,
		fixture.alice,
		next.ID,
	); !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatal(err)
	}
	fixture.store.deleteErr = nil
	fixture.store.deleteErrors = map[string]error{
		poison.ObjectKey: objectstore.ErrOperationFailed,
	}
	fixture.clock.now = fixture.clock.now.Add(time.Minute)

	first, err := fixture.service.ReclaimExpired(fixture.ctx, 1)
	if err != nil || first.Failed != 1 || first.Deleted != 0 {
		t.Fatalf("first ReclaimExpired() = %#v, %v", first, err)
	}
	second, err := fixture.service.ReclaimExpired(fixture.ctx, 1)
	if err != nil || second.Failed != 0 || second.Deleted != 1 {
		t.Fatalf("second ReclaimExpired() = %#v, %v", second, err)
	}
	assertAudioAssetStatus(
		t,
		fixture.audioAssetFixture,
		poison.ID,
		AudioAssetDeleting,
	)
	assertAudioAssetStatus(
		t,
		fixture.audioAssetFixture,
		next.ID,
		AudioAssetDeleted,
	)
}

func TestAudioAssetConcurrentRetriesConverge(t *testing.T) {
	fixture := newAudioAssetFixture()
	const workers = 12

	runConcurrent := func(operation func() error) {
		t.Helper()
		start := make(chan struct{})
		failures := make(chan error, workers)
		var wait sync.WaitGroup
		for range workers {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				if err := operation(); err != nil {
					failures <- err
				}
			}()
		}
		close(start)
		wait.Wait()
		close(failures)
		for err := range failures {
			t.Errorf("concurrent operation error = %v", err)
		}
	}

	runConcurrent(func() error {
		_, err := fixture.service.Upload(
			fixture.ctx,
			fixture.alice,
			fixture.uploadRequest("concurrent-upload"),
		)
		return err
	})
	asset, err := fixture.repository.GetByUploadRequest(
		fixture.ctx,
		fixture.alice.UserID,
		"concurrent-upload",
	)
	if err != nil || asset.Status != AudioAssetMetadataCommitted {
		t.Fatalf("concurrent Upload() asset = %#v, error = %v", asset, err)
	}

	runConcurrent(func() error {
		_, err := fixture.service.Confirm(
			fixture.ctx,
			fixture.alice,
			asset.ID,
			"candidate-1",
			"turn-1",
		)
		return err
	})
	asset, _ = fixture.repository.Get(fixture.ctx, asset.ID)
	if asset.Status != AudioAssetReadable ||
		asset.CandidateID != "candidate-1" ||
		asset.TurnID != "turn-1" {
		t.Fatalf("concurrent Confirm() asset = %#v", asset)
	}

	runConcurrent(func() error {
		_, err := fixture.service.Delete(fixture.ctx, fixture.alice, asset.ID)
		return err
	})
	asset, _ = fixture.repository.Get(fixture.ctx, asset.ID)
	if asset.Status != AudioAssetDeleted {
		t.Fatalf("concurrent Delete() asset = %#v", asset)
	}
}

func TestNewAudioAssetServiceRejectsNilDependencies(t *testing.T) {
	fixture := newAudioAssetFixture()
	var typedNilRepository *memoryAudioAssetRepository

	for name, build := range map[string]func() (*AudioAssetService, error){
		"repository": func() (*AudioAssetService, error) {
			return NewAudioAssetService(nil, fixture.store, fixture.ids, fixture.clock, fixture.turns, time.Hour)
		},
		"typed nil repository": func() (*AudioAssetService, error) {
			return NewAudioAssetService(typedNilRepository, fixture.store, fixture.ids, fixture.clock, fixture.turns, time.Hour)
		},
		"store": func() (*AudioAssetService, error) {
			return NewAudioAssetService(fixture.repository, nil, fixture.ids, fixture.clock, fixture.turns, time.Hour)
		},
		"IDs": func() (*AudioAssetService, error) {
			return NewAudioAssetService(fixture.repository, fixture.store, nil, fixture.clock, fixture.turns, time.Hour)
		},
		"clock": func() (*AudioAssetService, error) {
			return NewAudioAssetService(fixture.repository, fixture.store, fixture.ids, nil, fixture.turns, time.Hour)
		},
		"Turns": func() (*AudioAssetService, error) {
			return NewAudioAssetService(fixture.repository, fixture.store, fixture.ids, fixture.clock, nil, time.Hour)
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, err := build()
			if service != nil || !errors.Is(err, ErrAudioAssetInvalidDependency) {
				t.Fatalf("NewAudioAssetService() = %#v, %v", service, err)
			}
		})
	}
}

type audioAssetFixture struct {
	ctx        context.Context
	repository *memoryAudioAssetRepository
	store      *fakeAudioObjectStore
	ids        *sequenceAudioAssetIDs
	clock      *fakeAudioAssetClock
	turns      *fakeAudioAssetTurnVerifier
	service    *AudioAssetService
	alice      AudioAssetActor
	bob        AudioAssetActor
}

func newAudioAssetFixture() *audioAssetFixture {
	store := &fakeAudioObjectStore{}
	ids := &sequenceAudioAssetIDs{}
	clock := &fakeAudioAssetClock{now: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	repository := &memoryAudioAssetRepository{
		assets:        make(map[string]AudioAsset),
		now:           clock.Now,
		cleanupClaims: make(map[string]memoryAudioAssetClaimState),
		uploadClaims:  make(map[string]memoryAudioAssetClaimState),
	}
	turns := &fakeAudioAssetTurnVerifier{
		turns: map[fakeTurnKey]fakeConfirmedTurn{
			{ownerID: "alice", turnID: "turn-1"}: {
				ownerID:     "alice",
				candidateID: "candidate-1",
			},
			{ownerID: "alice", turnID: "turn-2"}: {
				ownerID:     "alice",
				candidateID: "candidate-2",
			},
			{ownerID: "bob", turnID: "bob-turn"}: {
				ownerID:     "bob",
				candidateID: "bob-candidate",
			},
			{ownerID: "alice", turnID: "shared-turn"}: {
				ownerID:     "alice",
				candidateID: "shared-candidate",
			},
			{ownerID: "bob", turnID: "shared-turn"}: {
				ownerID:     "bob",
				candidateID: "shared-candidate",
			},
		},
	}
	service, err := NewAudioAssetService(repository, store, ids, clock, turns, time.Hour)
	if err != nil {
		panic(err)
	}
	return &audioAssetFixture{
		ctx:        context.Background(),
		repository: repository,
		store:      store,
		ids:        ids,
		clock:      clock,
		turns:      turns,
		service:    service,
		alice:      AudioAssetActor{UserID: "alice"},
		bob:        AudioAssetActor{UserID: "bob"},
	}
}

type claimAudioAssetFixture struct {
	*audioAssetFixture
	cleanupRepository *memoryAudioAssetRepository
}

func newClaimAudioAssetFixture() *claimAudioAssetFixture {
	fixture := newAudioAssetFixture()
	return &claimAudioAssetFixture{
		audioAssetFixture: fixture,
		cleanupRepository: fixture.repository,
	}
}

func (f *audioAssetFixture) uploadRequest(requestID string) UploadRecordingRequest {
	return UploadRecordingRequest{
		RequestID:      requestID,
		Body:           bytes.NewReader([]byte("wav")),
		Size:           3,
		ContentType:    "audio/wav",
		ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Duration:       time.Second,
	}
}

func (f *audioAssetFixture) upload(t *testing.T, actor AudioAssetActor, requestID string) AudioAsset {
	t.Helper()
	asset, err := f.service.Upload(f.ctx, actor, f.uploadRequest(requestID))
	if err != nil {
		t.Fatalf("Upload(%q) error = %v", requestID, err)
	}
	return asset
}

func (f *audioAssetFixture) uploadStaged(t *testing.T, actor AudioAssetActor, requestID string) AudioAsset {
	t.Helper()
	f.store.putErr = objectstore.ErrOperationFailed
	_, err := f.service.Upload(f.ctx, actor, f.uploadRequest(requestID))
	f.store.putErr = nil
	if !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatalf("Upload(%q) error = %v", requestID, err)
	}
	asset, err := f.repository.GetByUploadRequest(f.ctx, actor.UserID, requestID)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func assertAudioAssetStatus(
	t *testing.T,
	fixture *audioAssetFixture,
	assetID string,
	want AudioAssetStatus,
) {
	t.Helper()
	asset, err := fixture.repository.Get(fixture.ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Status != want {
		t.Fatalf("asset %q status = %q, want %q", assetID, asset.Status, want)
	}
}

type memoryAudioAssetRepository struct {
	mu                  sync.RWMutex
	assets              map[string]AudioAsset
	now                 func() time.Time
	cleanupClaims       map[string]memoryAudioAssetClaimState
	uploadClaims        map[string]memoryAudioAssetClaimState
	beforeSave          func(AudioAsset)
	beforeCleanupClaim  func()
	beforeUploadClaim   func(context.Context) error
	afterUploadClaim    func()
	expireUploadClaim   bool
	cleanupReleaseErr   error
	hasOwnerErr         error
	cleanupReleaseCalls int
	uploadReleaseCalls  int
}

func (r *memoryAudioAssetRepository) Create(_ context.Context, asset AudioAsset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.assets {
		if existing.OwnerID == asset.OwnerID && existing.UploadRequestID == asset.UploadRequestID {
			return ErrAudioAssetConcurrentUpdate
		}
	}
	if _, found := r.assets[asset.ID]; found {
		return ErrAudioAssetConcurrentUpdate
	}
	r.assets[asset.ID] = asset
	return nil
}

func (r *memoryAudioAssetRepository) Get(_ context.Context, id string) (AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	asset, found := r.assets[id]
	if !found {
		return AudioAsset{}, ErrAudioAssetNotFound
	}
	return asset, nil
}

func (r *memoryAudioAssetRepository) GetOwned(
	_ context.Context,
	ownerID string,
	id string,
) (AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	asset, found := r.assets[id]
	if !found || asset.OwnerID != ownerID {
		return AudioAsset{}, ErrAudioAssetNotFound
	}
	return asset, nil
}

func (r *memoryAudioAssetRepository) GetByUploadRequest(
	_ context.Context,
	ownerID string,
	requestID string,
) (AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, asset := range r.assets {
		if asset.OwnerID == ownerID && asset.UploadRequestID == requestID {
			return asset, nil
		}
	}
	return AudioAsset{}, ErrAudioAssetNotFound
}

func (r *memoryAudioAssetRepository) GetByTurn(
	_ context.Context,
	ownerID string,
	turnID string,
) (AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, asset := range r.assets {
		if asset.OwnerID == ownerID && asset.TurnID == turnID {
			return asset, nil
		}
	}
	return AudioAsset{}, ErrAudioAssetNotFound
}

func (r *memoryAudioAssetRepository) GetByCandidate(
	_ context.Context,
	ownerID string,
	candidateID string,
) (AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, asset := range r.assets {
		if asset.OwnerID == ownerID && asset.CandidateID == candidateID {
			return asset, nil
		}
	}
	return AudioAsset{}, ErrAudioAssetNotFound
}

func (r *memoryAudioAssetRepository) Save(
	_ context.Context,
	asset AudioAsset,
	expectedVersion uint64,
) error {
	if r.beforeSave != nil {
		r.beforeSave(asset)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.assets[asset.ID]
	if !found {
		return ErrAudioAssetNotFound
	}
	if current.Version != expectedVersion {
		return ErrAudioAssetConcurrentUpdate
	}
	now := r.now()
	if r.uploadClaims[asset.ID].leaseExpiresAt.After(now) ||
		r.cleanupClaims[asset.ID].leaseExpiresAt.After(now) {
		return ErrAudioAssetConcurrentUpdate
	}
	if asset.CandidateID != "" || asset.TurnID != "" {
		for id, existing := range r.assets {
			if id != asset.ID &&
				existing.OwnerID == asset.OwnerID &&
				(existing.CandidateID == asset.CandidateID ||
					existing.TurnID == asset.TurnID) {
				return ErrAudioAssetAlreadyBound
			}
		}
	}
	r.assets[asset.ID] = asset
	return nil
}

type memoryAudioAssetClaimState struct {
	leaseExpiresAt time.Time
	fencingToken   uint64
}

type crossOwnerAudioAssetRepository struct {
	*memoryAudioAssetRepository
	claim AudioAssetCleanupClaim
}

func (r *crossOwnerAudioAssetRepository) ClaimOwnerAssetsForAccountCleanup(
	_ context.Context,
	_ string,
	_ time.Duration,
	_ int,
) ([]AudioAssetCleanupClaim, error) {
	return []AudioAssetCleanupClaim{r.claim}, nil
}

func (r *memoryAudioAssetRepository) ClaimExpiredUnconfirmed(
	_ context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]AudioAssetCleanupClaim, error) {
	return r.claim(
		leaseDuration,
		limit,
		func(asset AudioAsset, now time.Time) bool {
			return isExpiredUnconfirmed(asset, now)
		},
		true,
	)
}

func (r *memoryAudioAssetRepository) ClaimDeleting(
	_ context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]AudioAssetCleanupClaim, error) {
	return r.claim(
		leaseDuration,
		limit,
		func(asset AudioAsset, _ time.Time) bool {
			return asset.Status == AudioAssetDeleting
		},
		false,
	)
}

func (r *memoryAudioAssetRepository) ClaimOwnerAssetsForAccountCleanup(
	_ context.Context,
	ownerID string,
	leaseDuration time.Duration,
	limit int,
) ([]AudioAssetCleanupClaim, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, ErrAudioAssetInvalid
	}
	return r.claim(
		leaseDuration,
		limit,
		func(asset AudioAsset, _ time.Time) bool {
			return asset.OwnerID == ownerID && asset.Status != AudioAssetDeleted
		},
		true,
	)
}

func (r *memoryAudioAssetRepository) claim(
	leaseDuration time.Duration,
	limit int,
	eligible func(AudioAsset, time.Time) bool,
	beginDeleting bool,
) ([]AudioAssetCleanupClaim, error) {
	if leaseDuration <= 0 || limit <= 0 {
		return nil, ErrAudioAssetInvalid
	}
	if r.beforeCleanupClaim != nil {
		r.beforeCleanupClaim()
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := make([]string, 0, len(r.assets))
	for id := range r.assets {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left := r.assets[ids[i]]
		right := r.assets[ids[j]]
		if left.UpdatedAt.Equal(right.UpdatedAt) {
			return ids[i] < ids[j]
		}
		return left.UpdatedAt.Before(right.UpdatedAt)
	})

	claims := make([]AudioAssetCleanupClaim, 0, limit)
	for _, id := range ids {
		if len(claims) == limit {
			break
		}
		asset := r.assets[id]
		state := r.cleanupClaims[id]
		if state.leaseExpiresAt.After(now) ||
			r.uploadClaims[id].leaseExpiresAt.After(now) ||
			!eligible(asset, now) {
			continue
		}
		if asset.Status != AudioAssetDeleting {
			if !beginDeleting {
				continue
			}
			if err := asset.beginDeleting(now); err != nil {
				return nil, err
			}
		} else {
			// A fresh lease fences ordinary request-path saves even though the
			// business lifecycle status itself does not change.
			asset.Version++
		}
		state.fencingToken++
		state.leaseExpiresAt = now.Add(leaseDuration)
		uploadState := r.uploadClaims[id]
		uploadState.leaseExpiresAt = time.Time{}
		r.assets[id] = asset
		r.cleanupClaims[id] = state
		r.uploadClaims[id] = uploadState
		claims = append(claims, AudioAssetCleanupClaim{
			Asset:          asset,
			FencingToken:   state.fencingToken,
			LeaseExpiresAt: state.leaseExpiresAt,
		})
	}
	return claims, nil
}

func (r *memoryAudioAssetRepository) SaveCleanupClaim(
	_ context.Context,
	asset AudioAsset,
	expectedVersion uint64,
	fencingToken uint64,
) error {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	current, found := r.assets[asset.ID]
	if !found {
		return ErrAudioAssetNotFound
	}
	state := r.cleanupClaims[asset.ID]
	if current == asset &&
		asset.Status == AudioAssetDeleted &&
		asset.Version == expectedVersion+1 &&
		state.fencingToken == fencingToken &&
		state.leaseExpiresAt.IsZero() {
		return nil
	}
	if current.OwnerID != asset.OwnerID ||
		current.Status != AudioAssetDeleting ||
		current.Version != expectedVersion ||
		asset.Status != AudioAssetDeleted ||
		asset.Version != expectedVersion+1 ||
		state.fencingToken != fencingToken ||
		!state.leaseExpiresAt.After(now) {
		return ErrAudioAssetConcurrentUpdate
	}
	r.assets[asset.ID] = asset
	state.leaseExpiresAt = time.Time{}
	r.cleanupClaims[asset.ID] = state
	return nil
}

func (r *memoryAudioAssetRepository) ReleaseCleanupClaim(
	_ context.Context,
	ownerID string,
	audioAssetID string,
	fencingToken uint64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	asset, found := r.assets[audioAssetID]
	if !found {
		return ErrAudioAssetNotFound
	}
	state := r.cleanupClaims[audioAssetID]
	if asset.OwnerID != ownerID ||
		asset.Status != AudioAssetDeleting ||
		state.fencingToken != fencingToken {
		return ErrAudioAssetConcurrentUpdate
	}
	if state.leaseExpiresAt.IsZero() {
		return nil
	}
	if r.cleanupReleaseErr != nil {
		return r.cleanupReleaseErr
	}
	asset.UpdatedAt = asset.effectiveMutationTime(r.now())
	asset.Version++
	r.assets[audioAssetID] = asset
	state.leaseExpiresAt = time.Time{}
	r.cleanupClaims[audioAssetID] = state
	r.cleanupReleaseCalls++
	return nil
}

func (r *memoryAudioAssetRepository) releaseCallCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cleanupReleaseCalls
}

func (r *memoryAudioAssetRepository) HasOwnerAssetsForAccountCleanup(
	_ context.Context,
	ownerID string,
) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.hasOwnerErr != nil {
		return false, r.hasOwnerErr
	}
	for _, asset := range r.assets {
		if asset.OwnerID == ownerID {
			return true, nil
		}
	}
	return false, nil
}

func (r *memoryAudioAssetRepository) PurgeOwnerDeletedAssets(
	_ context.Context,
	ownerID string,
	limit int,
) (int, error) {
	if strings.TrimSpace(ownerID) == "" || limit <= 0 {
		return 0, ErrAudioAssetInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := make([]string, 0)
	for id, asset := range r.assets {
		if asset.OwnerID == ownerID && asset.Status == AudioAssetDeleted {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	for _, id := range ids {
		delete(r.assets, id)
		delete(r.cleanupClaims, id)
		delete(r.uploadClaims, id)
	}
	return len(ids), nil
}

func (r *memoryAudioAssetRepository) ClaimUpload(
	ctx context.Context,
	ownerID string,
	uploadRequestID string,
	leaseDuration time.Duration,
) (AudioAssetUploadClaim, error) {
	if r.beforeUploadClaim != nil {
		if err := r.beforeUploadClaim(ctx); err != nil {
			return AudioAssetUploadClaim{}, err
		}
	}
	if strings.TrimSpace(ownerID) == "" ||
		strings.TrimSpace(uploadRequestID) == "" ||
		leaseDuration <= 0 {
		return AudioAssetUploadClaim{}, ErrAudioAssetInvalid
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	var asset AudioAsset
	found := false
	for _, candidate := range r.assets {
		if candidate.OwnerID == ownerID &&
			candidate.UploadRequestID == uploadRequestID {
			asset = candidate
			found = true
			break
		}
	}
	if !found {
		return AudioAssetUploadClaim{}, ErrAudioAssetNotFound
	}
	state := r.uploadClaims[asset.ID]
	if asset.Status != AudioAssetStaged ||
		state.leaseExpiresAt.After(now) ||
		r.cleanupClaims[asset.ID].leaseExpiresAt.After(now) {
		return AudioAssetUploadClaim{}, ErrAudioAssetConcurrentUpdate
	}
	asset.Version++
	state.fencingToken++
	state.leaseExpiresAt = now.Add(leaseDuration)
	if r.expireUploadClaim {
		state.leaseExpiresAt = now.Add(-time.Second)
	}
	r.assets[asset.ID] = asset
	r.uploadClaims[asset.ID] = state
	claim := AudioAssetUploadClaim{
		Asset:          asset,
		FencingToken:   state.fencingToken,
		LeaseExpiresAt: state.leaseExpiresAt,
	}
	if r.afterUploadClaim != nil {
		r.afterUploadClaim()
	}
	return claim, nil
}

func (r *memoryAudioAssetRepository) CommitUploadClaim(
	_ context.Context,
	asset AudioAsset,
	expectedVersion uint64,
	fencingToken uint64,
) error {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	current, found := r.assets[asset.ID]
	if !found {
		return ErrAudioAssetNotFound
	}
	state := r.uploadClaims[asset.ID]
	if current == asset &&
		asset.Status == AudioAssetMetadataCommitted &&
		asset.Version == expectedVersion+1 &&
		state.fencingToken == fencingToken &&
		state.leaseExpiresAt.IsZero() {
		return nil
	}
	if current.OwnerID != asset.OwnerID ||
		current.Status != AudioAssetStaged ||
		current.Version != expectedVersion ||
		asset.Status != AudioAssetMetadataCommitted ||
		asset.Version != expectedVersion+1 ||
		state.fencingToken != fencingToken ||
		!state.leaseExpiresAt.After(now) {
		return ErrAudioAssetConcurrentUpdate
	}
	r.assets[asset.ID] = asset
	state.leaseExpiresAt = time.Time{}
	r.uploadClaims[asset.ID] = state
	return nil
}

func (r *memoryAudioAssetRepository) ReleaseUploadClaim(
	_ context.Context,
	ownerID string,
	audioAssetID string,
	fencingToken uint64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	asset, found := r.assets[audioAssetID]
	if !found {
		return ErrAudioAssetNotFound
	}
	state := r.uploadClaims[audioAssetID]
	if asset.OwnerID != ownerID ||
		asset.Status != AudioAssetStaged ||
		state.fencingToken != fencingToken {
		return ErrAudioAssetConcurrentUpdate
	}
	if state.leaseExpiresAt.IsZero() {
		return nil
	}
	state.leaseExpiresAt = time.Time{}
	r.uploadClaims[audioAssetID] = state
	asset.UpdatedAt = asset.effectiveMutationTime(r.now())
	asset.Version++
	r.assets[audioAssetID] = asset
	r.uploadReleaseCalls++
	return nil
}

func (r *memoryAudioAssetRepository) uploadReleaseCallCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.uploadReleaseCalls
}

type fakeAudioObjectStore struct {
	mu               sync.Mutex
	putCalls         int
	deleteCalls      int
	putErr           error
	putObjectOnError bool
	deleteErr        error
	deleteErrors     map[string]error
	signedResult     objectstore.SignedGetResult
	onPut            func(context.Context, objectstore.PutRequest) error
	beforeDelete     func(string)
	objects          map[string]struct{}
}

func (s *fakeAudioObjectStore) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	s.mu.Lock()
	s.putCalls++
	putErr := s.putErr
	putObjectOnError := s.putObjectOnError
	onPut := s.onPut
	s.mu.Unlock()
	if onPut != nil {
		if err := onPut(ctx, request); err != nil {
			return objectstore.PutResult{}, err
		}
	}
	select {
	case <-ctx.Done():
		return objectstore.PutResult{}, ctx.Err()
	default:
	}
	if request.Body != nil {
		_, _ = io.Copy(io.Discard, request.Body)
	}
	if putErr == nil || putObjectOnError {
		s.mu.Lock()
		if s.objects == nil {
			s.objects = make(map[string]struct{})
		}
		s.objects[request.Key] = struct{}{}
		s.mu.Unlock()
	}
	return objectstore.PutResult{ETag: "etag"}, putErr
}

func (s *fakeAudioObjectStore) SignedGet(
	_ context.Context,
	_ string,
) (objectstore.SignedGetResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.signedResult, nil
}

func (s *fakeAudioObjectStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	beforeDelete := s.beforeDelete
	s.mu.Unlock()
	if beforeDelete != nil {
		beforeDelete(key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	if err := s.deleteErrors[key]; err != nil {
		return err
	}
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, key)
	return nil
}

func (s *fakeAudioObjectStore) putCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putCalls
}

func (s *fakeAudioObjectStore) deleteCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteCalls
}

func (s *fakeAudioObjectStore) hasObject(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, found := s.objects[key]
	return found
}

type sequenceAudioAssetIDs struct {
	mu   sync.Mutex
	next int
}

func (g *sequenceAudioAssetIDs) NewID() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return "asset-" + strconv.Itoa(g.next), nil
}

type fakeAudioAssetClock struct {
	now time.Time
}

func (c *fakeAudioAssetClock) Now() time.Time {
	return c.now
}

type fakeAudioAssetTurnVerifier struct {
	turns map[fakeTurnKey]fakeConfirmedTurn
}

type fakeTurnKey struct {
	ownerID string
	turnID  string
}

type fakeConfirmedTurn struct {
	ownerID     string
	candidateID string
}

func (v *fakeAudioAssetTurnVerifier) VerifyOwnedTurn(
	_ context.Context,
	actorID string,
	turnID string,
	expectedCandidateID string,
) error {
	turn, found := v.turns[fakeTurnKey{ownerID: actorID, turnID: turnID}]
	if !found {
		return ErrAudioAssetTurnNotFound
	}
	if turn.ownerID != actorID {
		return ErrAudioAssetForbidden
	}
	if turn.candidateID != expectedCandidateID {
		return ErrAudioAssetAlreadyBound
	}
	return nil
}
