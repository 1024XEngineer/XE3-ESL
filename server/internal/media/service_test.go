package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
	"time"

	objectstorefake "github.com/1024XEngineer/XE3-ESL/server/test/support/objectstorefake"
)

func TestDocumentUploadOpenAndSignedGet(t *testing.T) {
	t.Parallel()

	store, err := objectstorefake.New("resume/v1", time.Minute)
	if err != nil {
		t.Fatalf("new document store: %v", err)
	}
	repository := &documentRepository{}
	service, err := NewService(
		repository,
		Stores{Documents: store},
		fixedIDGenerator{id: "10000000-0000-4000-8000-000000000001"},
		Config{
			UploadLease:  time.Minute,
			CleanupLease: time.Minute,
			PlaybackTTL:  time.Minute,
			CleanupBatch: 8,
		},
	)
	if err != nil {
		t.Fatalf("new media service: %v", err)
	}
	body := []byte("%PDF-1.7\nresume\n%%EOF")
	sum := sha256.Sum256(body)
	asset, err := service.Upload(context.Background(), Upload{
		UserID:         "20000000-0000-4000-8000-000000000001",
		Kind:           KindDocument,
		IdempotencyKey: "resume-upload-0001",
		ContentType:    "application/pdf",
		Body:           bytes.NewReader(body),
		Size:           int64(len(body)),
		ChecksumSHA256: hex.EncodeToString(sum[:]),
		ExpiresAt:      time.Now().UTC().Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upload document: %v", err)
	}
	if asset.Kind != KindDocument || asset.Status != StatusReady ||
		asset.ObjectKey != "resume/v1/media/10000000-0000-4000-8000-000000000001.pdf" {
		t.Fatalf("uploaded asset = %#v", asset)
	}

	reader, err := service.Open(
		context.Background(), asset.UserID, asset.ID,
	)
	if err != nil {
		t.Fatalf("open document: %v", err)
	}
	defer reader.Close()
	readBody, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read document: %v", err)
	}
	if !bytes.Equal(readBody, body) {
		t.Fatalf("opened body = %q", readBody)
	}
	if _, err := service.SignedGet(
		context.Background(), asset.UserID, asset.ID,
	); err != nil {
		t.Fatalf("sign document read: %v", err)
	}
}

type fixedIDGenerator struct {
	id string
}

func (generator fixedIDGenerator) NewID() (string, error) {
	return generator.id, nil
}

type documentRepository struct {
	asset Asset
}

func (repository *documentRepository) Stage(
	_ context.Context,
	asset Asset,
) (Stage, error) {
	repository.asset = asset
	return Stage{Asset: asset, Created: true}, nil
}

func (repository *documentRepository) ClaimUpload(
	_ context.Context,
	userID string,
	assetID string,
	lease time.Duration,
) (UploadClaim, bool, error) {
	if repository.asset.UserID != userID || repository.asset.ID != assetID {
		return UploadClaim{}, false, ErrNotFound
	}
	repository.asset.UploadFencingToken = 1
	repository.asset.UploadLeaseUntil = time.Now().UTC().Add(lease)
	return UploadClaim{
		Asset:          repository.asset,
		FencingToken:   1,
		LeaseExpiresAt: repository.asset.UploadLeaseUntil,
	}, true, nil
}

func (repository *documentRepository) CommitUpload(
	_ context.Context,
	userID string,
	assetID string,
	fencingToken uint64,
	etag string,
) (Asset, error) {
	if repository.asset.UserID != userID || repository.asset.ID != assetID ||
		fencingToken != 1 || etag == "" {
		return Asset{}, ErrConflict
	}
	repository.asset.Status = StatusReady
	repository.asset.ETag = etag
	repository.asset.UploadLeaseUntil = time.Time{}
	return repository.asset, nil
}

func (repository *documentRepository) FindOwned(
	_ context.Context,
	userID string,
	assetID string,
) (Asset, error) {
	if repository.asset.UserID != userID || repository.asset.ID != assetID {
		return Asset{}, ErrNotFound
	}
	return repository.asset, nil
}

func (*documentRepository) BeginDeletion(
	context.Context,
	string,
	string,
	time.Duration,
) (Asset, error) {
	return Asset{}, ErrRepository
}

func (*documentRepository) ClaimCleanup(
	context.Context,
	time.Duration,
	int,
) ([]CleanupClaim, error) {
	return nil, ErrRepository
}

func (*documentRepository) FinishCleanup(context.Context, CleanupClaim) error {
	return ErrRepository
}

func (*documentRepository) ReleaseCleanup(
	context.Context,
	CleanupClaim,
	string,
) error {
	return ErrRepository
}

var _ Repository = (*documentRepository)(nil)
