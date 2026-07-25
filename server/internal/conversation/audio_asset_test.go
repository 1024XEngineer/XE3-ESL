package conversation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

func TestAudioAssetUploadPersistsStagedBeforePutAndIsIdempotent(t *testing.T) {
	fixture := newAudioAssetFixture()
	fixture.store.onPut = func(request objectstore.PutRequest) {
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

func TestAudioAssetConfirmIsIdempotentAndTurnIsUnique(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "upload-1")

	if _, err := fixture.service.Confirm(fixture.ctx, fixture.bob, asset.ID, "turn-1"); !errors.Is(err, ErrAudioAssetForbidden) {
		t.Fatalf("cross-owner Confirm() error = %v", err)
	}
	confirmed, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "turn-1")
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if confirmed.Status != AudioAssetReadable || confirmed.TurnID != "turn-1" {
		t.Fatalf("Confirm() = %#v", confirmed)
	}
	version := confirmed.Version

	again, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "turn-1")
	if err != nil {
		t.Fatalf("idempotent Confirm() error = %v", err)
	}
	if again.Version != version {
		t.Fatalf("idempotent Confirm() version = %d, want %d", again.Version, version)
	}

	second := fixture.upload(t, fixture.alice, "upload-2")
	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, second.ID, "turn-1"); !errors.Is(err, ErrAudioAssetAlreadyBound) {
		t.Fatalf("duplicate turn Confirm() error = %v", err)
	}
}

func TestAudioAssetConfirmRequiresOwnedExistingTurn(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "upload-1")

	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "missing-turn"); !errors.Is(err, ErrAudioAssetTurnNotFound) {
		t.Fatalf("missing Turn Confirm() error = %v", err)
	}
	if _, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "bob-turn"); !errors.Is(err, ErrAudioAssetForbidden) {
		t.Fatalf("cross-owner Turn Confirm() error = %v", err)
	}
}

func TestAudioAssetPlaybackRequiresOwnerReadableAndShortTTL(t *testing.T) {
	fixture := newAudioAssetFixture()
	asset := fixture.upload(t, fixture.alice, "upload-1")

	if _, err := fixture.service.Playback(fixture.ctx, fixture.alice, asset.ID); !errors.Is(err, ErrAudioAssetInvalidTransition) {
		t.Fatalf("unconfirmed Playback() error = %v", err)
	}
	asset, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Playback(fixture.ctx, fixture.bob, asset.ID); !errors.Is(err, ErrAudioAssetForbidden) {
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
	asset, err := fixture.service.Confirm(fixture.ctx, fixture.alice, asset.ID, "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Delete(fixture.ctx, fixture.bob, asset.ID); !errors.Is(err, ErrAudioAssetForbidden) {
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

func TestAudioAssetAccountDataCleanupIsOwnerScopedAndRetriesFailures(t *testing.T) {
	fixture := newAudioAssetFixture()
	aliceUnconfirmed := fixture.upload(t, fixture.alice, "alice-unconfirmed")
	aliceReadable := fixture.upload(t, fixture.alice, "alice-readable")
	var err error
	aliceReadable, err = fixture.service.Confirm(
		fixture.ctx,
		fixture.alice,
		aliceReadable.ID,
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
	if err != nil {
		t.Fatalf("CleanupAccountData() error = %v", err)
	}
	if result.Deleted != 1 || result.Failed != 1 {
		t.Fatalf("CleanupAccountData() = %#v", result)
	}
	assertAudioAssetStatus(t, fixture, aliceUnconfirmed.ID, AudioAssetDeleted)
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

	idempotent, err := fixture.service.CleanupAccountData(fixture.ctx, fixture.alice, 10)
	if err != nil {
		t.Fatalf("idempotent CleanupAccountData() error = %v", err)
	}
	if idempotent.Deleted != 0 || idempotent.Failed != 0 {
		t.Fatalf("idempotent CleanupAccountData() = %#v", idempotent)
	}
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
			"turn-1",
		)
		return err
	})
	asset, _ = fixture.repository.Get(fixture.ctx, asset.ID)
	if asset.Status != AudioAssetReadable || asset.TurnID != "turn-1" {
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
	repository := &memoryAudioAssetRepository{assets: make(map[string]AudioAsset)}
	store := &fakeAudioObjectStore{}
	ids := &sequenceAudioAssetIDs{}
	clock := &fakeAudioAssetClock{now: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	turns := &fakeAudioAssetTurnVerifier{
		owners: map[string]string{
			"turn-1":   "alice",
			"bob-turn": "bob",
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
	mu     sync.RWMutex
	assets map[string]AudioAsset
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

func (r *memoryAudioAssetRepository) GetByTurn(_ context.Context, turnID string) (AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, asset := range r.assets {
		if asset.TurnID == turnID {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.assets[asset.ID]
	if !found {
		return ErrAudioAssetNotFound
	}
	if current.Version != expectedVersion {
		return ErrAudioAssetConcurrentUpdate
	}
	if asset.TurnID != "" {
		for id, existing := range r.assets {
			if id != asset.ID && existing.TurnID == asset.TurnID {
				return ErrAudioAssetAlreadyBound
			}
		}
	}
	r.assets[asset.ID] = asset
	return nil
}

func (r *memoryAudioAssetRepository) ListExpiredUnconfirmed(
	_ context.Context,
	now time.Time,
	limit int,
) ([]AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var assets []AudioAsset
	for _, asset := range r.assets {
		unconfirmed := asset.TurnID == "" &&
			(asset.Status == AudioAssetStaged ||
				asset.Status == AudioAssetMetadataCommitted)
		if unconfirmed && !asset.StagedUntil.After(now) {
			assets = append(assets, asset)
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	if len(assets) > limit {
		assets = assets[:limit]
	}
	return assets, nil
}

func (r *memoryAudioAssetRepository) ListDeleting(
	_ context.Context,
	limit int,
) ([]AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var assets []AudioAsset
	for _, asset := range r.assets {
		if asset.Status == AudioAssetDeleting {
			assets = append(assets, asset)
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	if len(assets) > limit {
		assets = assets[:limit]
	}
	return assets, nil
}

func (r *memoryAudioAssetRepository) ListOwnerAssetsForAccountCleanup(
	_ context.Context,
	ownerID string,
	limit int,
) ([]AudioAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var assets []AudioAsset
	for _, asset := range r.assets {
		if asset.OwnerID == ownerID && asset.Status != AudioAssetDeleted {
			assets = append(assets, asset)
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	if len(assets) > limit {
		assets = assets[:limit]
	}
	return assets, nil
}

type fakeAudioObjectStore struct {
	mu           sync.Mutex
	putCalls     int
	deleteCalls  int
	putErr       error
	deleteErr    error
	deleteErrors map[string]error
	signedResult objectstore.SignedGetResult
	onPut        func(objectstore.PutRequest)
}

func (s *fakeAudioObjectStore) Put(
	_ context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	s.mu.Lock()
	s.putCalls++
	putErr := s.putErr
	onPut := s.onPut
	s.mu.Unlock()
	if onPut != nil {
		onPut(request)
	}
	if request.Body != nil {
		_, _ = io.Copy(io.Discard, request.Body)
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
	defer s.mu.Unlock()
	s.deleteCalls++
	if err := s.deleteErrors[key]; err != nil {
		return err
	}
	return s.deleteErr
}

func (s *fakeAudioObjectStore) putCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putCalls
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
	owners map[string]string
}

func (v *fakeAudioAssetTurnVerifier) VerifyOwnedTurn(
	_ context.Context,
	actorID string,
	turnID string,
) error {
	ownerID, found := v.owners[turnID]
	if !found {
		return ErrAudioAssetTurnNotFound
	}
	if ownerID != actorID {
		return ErrAudioAssetForbidden
	}
	return nil
}
