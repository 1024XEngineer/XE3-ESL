package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	testOwnerID  = "10000000-0000-4000-8000-000000000001"
	testThreadID = "20000000-0000-4000-8000-000000000001"
	testAssetID  = "30000000-0000-4000-8000-000000000001"
)

func TestUploadStoresValidatedImageAndCommitsFencedUpload(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)
	payload := testPNG(t, 32, 24)

	asset, err := service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-upload-1",
			ContentType:    "image/png",
			Body:           bytes.NewReader(payload),
		},
	)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if asset.ID != testAssetID ||
		asset.ObjectKey != ObjectPrefix+testAssetID+".png" ||
		asset.ContentType != "image/png" ||
		asset.Width != 32 ||
		asset.Height != 24 ||
		asset.ETag == "" ||
		asset.UploadFencingToken != 1 {
		t.Fatalf("uploaded asset = %#v", asset)
	}
	body, found := store.Bytes(asset.ObjectKey)
	if !found {
		t.Fatal("validated image was not stored")
	}
	configuration, format, err := stdimage.DecodeConfig(bytes.NewReader(body))
	if err != nil || format != "png" ||
		configuration.Width != 32 || configuration.Height != 24 {
		t.Fatalf("stored image config = %#v, %q, %v", configuration, format, err)
	}
}

func TestUploadTelemetryIsUsefulAndDoesNotLeakObjectData(t *testing.T) {
	var logs bytes.Buffer
	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service, err := NewService(
		repository,
		store,
		imageTestIDs{},
		Config{StagedTTL: time.Hour, UploadLease: time.Minute},
		WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if _, err := service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-telemetry-1",
			ContentType:    "image/png",
			Body:           bytes.NewReader(testPNG(t, 7, 5)),
		},
	); err != nil {
		t.Fatalf("upload: %v", err)
	}

	output := logs.String()
	for _, want := range []string{
		`"msg":"agent.image.upload.succeeded"`,
		`"image_asset_id":"` + testAssetID + `"`,
		`"content_type":"image/png"`,
		`"width":7`,
		`"height":5`,
		`"duration_ms":`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("logs = %s, want %s", output, want)
		}
	}
	for _, secret := range []string{
		ObjectPrefix + testAssetID + ".png",
		"signature=",
		"checksum_sha256",
	} {
		if strings.Contains(output, secret) {
			t.Fatalf("logs leaked %q: %s", secret, output)
		}
	}
}

func TestUploadRejectsMIMEConfusionBeforePersistence(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)

	_, err = service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-upload-2",
			ContentType:    "image/jpeg",
			Body:           bytes.NewReader(testPNG(t, 8, 8)),
		},
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("upload error = %v, want invalid request", err)
	}
	if repository.stageCalls != 0 {
		t.Fatal("invalid image reached persistence")
	}
}

func TestUploadRejectsAnimatedPNGBeforePersistence(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)

	_, err = service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-apng-1",
			ContentType:    "image/png",
			Body: bytes.NewReader(
				injectPNGAnimationControl(t, testPNG(t, 8, 8)),
			),
		},
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("upload error = %v, want invalid request", err)
	}
	if repository.stageCalls != 0 {
		t.Fatal("animated image reached persistence")
	}
}

func TestUploadRejectsMalformedAndOversizedImagesBeforePersistence(t *testing.T) {
	t.Parallel()

	valid := testPNG(t, 8, 8)
	truncated := append([]byte(nil), valid[:len(valid)/2]...)
	oversized := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(oversized[16:20], maxImageDimension+1)
	binary.BigEndian.PutUint32(
		oversized[29:33],
		crc32.ChecksumIEEE(oversized[12:29]),
	)

	for name, payload := range map[string][]byte{
		"truncated": truncated,
		"oversized": oversized,
	} {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repository := &imageTestRepository{}
			store, err := objectfake.New("image/v1", 2*time.Minute)
			if err != nil {
				t.Fatalf("new object store: %v", err)
			}
			service := newImageTestService(t, repository, store)
			_, err = service.Upload(
				context.Background(),
				testActor(),
				UploadRequest{
					ThreadID:       testThreadID,
					IdempotencyKey: "image-invalid-" + name,
					ContentType:    "image/png",
					Body:           bytes.NewReader(payload),
				},
			)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("upload error = %v, want invalid request", err)
			}
			if repository.stageCalls != 0 {
				t.Fatal("invalid image reached persistence")
			}
		})
	}
}

func TestUploadAcceptsDecodedWebP(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)
	payload, err := base64.StdEncoding.DecodeString(
		"UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA",
	)
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}

	asset, err := service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-webp-1",
			ContentType:    "image/webp",
			Body:           bytes.NewReader(payload),
		},
	)
	if err != nil {
		t.Fatalf("upload WebP: %v", err)
	}
	if asset.ContentType != "image/png" ||
		asset.ObjectKey != ObjectPrefix+testAssetID+".png" {
		t.Fatalf("WebP asset = %#v", asset)
	}
	body, found := store.Bytes(asset.ObjectKey)
	if !found {
		t.Fatal("normalized WebP was not stored")
	}
	if _, format, err := stdimage.Decode(bytes.NewReader(body)); err != nil ||
		format != "png" {
		t.Fatalf("normalized WebP format = %q, error = %v", format, err)
	}
}

func TestUploadStripsJPEGMetadataBeforeStorage(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)
	payload := testJPEGWithAPP1Metadata(t, 16, 12)

	asset, err := service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-jpeg-metadata-1",
			ContentType:    "image/jpeg",
			Body:           bytes.NewReader(payload),
		},
	)
	if err != nil {
		t.Fatalf("upload JPEG: %v", err)
	}
	body, found := store.Bytes(asset.ObjectKey)
	if !found {
		t.Fatal("normalized JPEG was not stored")
	}
	if bytes.Contains(body, []byte("PRIVATE_GPS_MARKER")) {
		t.Fatal("JPEG metadata marker reached object storage")
	}
	if _, format, err := stdimage.Decode(bytes.NewReader(body)); err != nil ||
		format != "jpeg" {
		t.Fatalf("normalized JPEG format = %q, error = %v", format, err)
	}
}

func TestUploadAppliesJPEGEXIFOrientationBeforeStrippingMetadata(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)
	payload := testJPEGWithOrientation(t, 16, 12, 6)

	asset, err := service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-jpeg-orientation-1",
			ContentType:    "image/jpeg",
			Body:           bytes.NewReader(payload),
		},
	)
	if err != nil {
		t.Fatalf("upload oriented JPEG: %v", err)
	}
	if asset.Width != 12 || asset.Height != 16 {
		t.Fatalf("oriented dimensions = %dx%d, want 12x16", asset.Width, asset.Height)
	}
	body, found := store.Bytes(asset.ObjectKey)
	if !found {
		t.Fatal("oriented JPEG was not stored")
	}
	if bytes.Contains(body, []byte("Exif\x00\x00")) {
		t.Fatal("EXIF header reached object storage")
	}
	configuration, format, err := stdimage.DecodeConfig(bytes.NewReader(body))
	if err != nil || format != "jpeg" ||
		configuration.Width != 12 || configuration.Height != 16 {
		t.Fatalf("stored image config = %#v, %q, %v", configuration, format, err)
	}
}

func TestUploadDetectsIdempotencyConflict(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)
	first := UploadRequest{
		ThreadID:       testThreadID,
		IdempotencyKey: "image-upload-replay",
		ContentType:    "image/png",
		Body:           bytes.NewReader(testPNG(t, 16, 16)),
	}
	if _, err := service.Upload(
		context.Background(),
		testActor(),
		first,
	); err != nil {
		t.Fatalf("first upload: %v", err)
	}

	_, err = service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-upload-replay",
			ContentType:    "image/png",
			Body:           bytes.NewReader(testPNG(t, 17, 16)),
		},
	)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("replay error = %v, want idempotency conflict", err)
	}
}

func TestContentSignsOnlyReadableOwnedImage(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)
	asset, err := service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-content-1",
			ContentType:    "image/png",
			Body:           bytes.NewReader(testPNG(t, 12, 8)),
		},
	)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	content, err := service.Content(
		context.Background(),
		testActor(),
		asset.ID,
	)
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	if content.URL == "" || !content.ExpiresAt.After(time.Now()) {
		t.Fatalf("content = %#v", content)
	}

	repository.mu.Lock()
	repository.asset.Status = StatusDeleted
	repository.mu.Unlock()
	if _, err := service.Content(
		context.Background(),
		testActor(),
		asset.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted content error = %v", err)
	}
}

func TestReclaimDeletesClaimedObjectsAndFinishesLifecycle(t *testing.T) {
	t.Parallel()

	repository := &imageTestRepository{}
	store, err := objectfake.New("image/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	service := newImageTestService(t, repository, store)
	payload := testPNG(t, 12, 12)
	asset, err := service.Upload(
		context.Background(),
		testActor(),
		UploadRequest{
			ThreadID:       testThreadID,
			IdempotencyKey: "image-cleanup-1",
			ContentType:    "image/png",
			Body:           bytes.NewReader(payload),
		},
	)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	repository.cleanupClaims = []CleanupClaim{{
		AssetID:      asset.ID,
		OwnerID:      asset.OwnerID,
		ObjectKey:    asset.ObjectKey,
		FencingToken: 1,
	}}

	result, err := service.Reclaim(context.Background(), 8)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if result.Deleted != 1 || result.Failed != 0 ||
		repository.finishedCleanups != 1 ||
		store.Has(asset.ObjectKey) {
		t.Fatalf("cleanup result = %#v, repository = %#v", result, repository)
	}
}

type imageTestIDs struct{}

func (imageTestIDs) NewID() (string, error) {
	return testAssetID, nil
}

type imageTestRepository struct {
	Repository

	mu               sync.Mutex
	asset            Asset
	stageCalls       int
	cleanupClaims    []CleanupClaim
	finishedCleanups int
}

func (r *imageTestRepository) StageAsset(
	_ context.Context,
	candidate Asset,
) (AssetStage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stageCalls++
	if r.asset.ID == "" {
		r.asset = candidate
		return AssetStage{Asset: r.asset, Created: true}, nil
	}
	return AssetStage{Asset: r.asset, Created: false}, nil
}

func (r *imageTestRepository) ClaimUpload(
	_ context.Context,
	ownerID string,
	assetID string,
	lease time.Duration,
) (UploadClaim, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.asset.OwnerID != ownerID || r.asset.ID != assetID {
		return UploadClaim{}, false, ErrNotFound
	}
	if r.asset.ETag != "" {
		return UploadClaim{Asset: r.asset}, false, nil
	}
	r.asset.UploadFencingToken++
	r.asset.UploadLeaseUntil = time.Now().UTC().Add(lease)
	return UploadClaim{
		Asset:          r.asset,
		FencingToken:   r.asset.UploadFencingToken,
		LeaseExpiresAt: r.asset.UploadLeaseUntil,
	}, true, nil
}

func (r *imageTestRepository) CommitUpload(
	_ context.Context,
	ownerID string,
	assetID string,
	token uint64,
	etag string,
) (Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.asset.OwnerID != ownerID ||
		r.asset.ID != assetID ||
		r.asset.UploadFencingToken != token {
		return Asset{}, ErrConflict
	}
	r.asset.ETag = etag
	r.asset.UploadLeaseUntil = time.Time{}
	return r.asset, nil
}

func (r *imageTestRepository) FindAsset(
	_ context.Context,
	ownerID string,
	assetID string,
) (Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.asset.OwnerID != ownerID || r.asset.ID != assetID {
		return Asset{}, ErrNotFound
	}
	return r.asset, nil
}

func (r *imageTestRepository) ClaimCleanup(
	_ context.Context,
	_ time.Duration,
	_ int,
) ([]CleanupClaim, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]CleanupClaim(nil), r.cleanupClaims...), nil
}

func (r *imageTestRepository) FinishCleanup(
	_ context.Context,
	_ CleanupClaim,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishedCleanups++
	return nil
}

func (r *imageTestRepository) ReleaseCleanup(
	context.Context,
	CleanupClaim,
) error {
	return nil
}

func newImageTestService(
	t *testing.T,
	repository Repository,
	store *objectfake.Store,
) *Service {
	t.Helper()
	service, err := NewService(
		repository,
		store,
		imageTestIDs{},
		Config{
			StagedTTL:   time.Hour,
			UploadLease: time.Minute,
		},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func testActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    testOwnerID,
		SessionID: "image-test-session",
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	value := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, color.RGBA{
				R: uint8(x),
				G: uint8(y),
				B: 0x7f,
				A: 0xff,
			})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, value); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return buffer.Bytes()
}

func testJPEGWithAPP1Metadata(t *testing.T, width, height int) []byte {
	t.Helper()
	value := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, color.RGBA{
				R: uint8(x * 8),
				G: uint8(y * 8),
				B: 0x7f,
				A: 0xff,
			})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, value, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return injectJPEGAPP1(t, encoded.Bytes(), []byte("Exif\x00\x00PRIVATE_GPS_MARKER"))
}

func testJPEGWithOrientation(
	t *testing.T,
	width int,
	height int,
	orientation byte,
) []byte {
	t.Helper()
	value := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, color.RGBA{
				R: uint8(x * 8),
				G: uint8(y * 8),
				B: 0x7f,
				A: 0xff,
			})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, value, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	metadata := []byte{
		'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0, 42,
		0, 0, 0, 8,
		0, 1,
		0x01, 0x12,
		0, 3,
		0, 0, 0, 1,
		0, orientation, 0, 0,
		0, 0, 0, 0,
	}
	return injectJPEGAPP1(t, encoded.Bytes(), metadata)
}

func injectJPEGAPP1(t *testing.T, original, metadata []byte) []byte {
	t.Helper()
	if len(original) < 2 || original[0] != 0xff || original[1] != 0xd8 {
		t.Fatal("invalid JPEG fixture")
	}
	segmentLength := len(metadata) + 2
	withMetadata := make([]byte, 0, len(original)+len(metadata)+4)
	withMetadata = append(withMetadata, original[:2]...)
	withMetadata = append(
		withMetadata,
		0xff,
		0xe1,
		byte(segmentLength>>8),
		byte(segmentLength),
	)
	withMetadata = append(withMetadata, metadata...)
	withMetadata = append(withMetadata, original[2:]...)
	return withMetadata
}

func injectPNGAnimationControl(t *testing.T, original []byte) []byte {
	t.Helper()
	if len(original) < 33 ||
		!bytes.Equal(original[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		t.Fatal("invalid PNG fixture")
	}
	// DecodeConfig only needs IHDR. The normalization layer detects acTL
	// before the full decoder evaluates the synthetic chunk CRC.
	animationControl := []byte{
		0, 0, 0, 8,
		'a', 'c', 'T', 'L',
		0, 0, 0, 2,
		0, 0, 0, 0,
		0, 0, 0, 0,
	}
	result := make([]byte, 0, len(original)+len(animationControl))
	result = append(result, original[:33]...)
	result = append(result, animationControl...)
	result = append(result, original[33:]...)
	return result
}
