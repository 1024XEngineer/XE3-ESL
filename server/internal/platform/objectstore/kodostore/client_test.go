package kodostore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	qiniuclient "github.com/qiniu/go-sdk/v7/client"
	"github.com/qiniu/go-sdk/v7/storagev2/apis"
	"github.com/qiniu/go-sdk/v7/storagev2/credentials"
	"github.com/qiniu/go-sdk/v7/storagev2/uploader"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

var fixedNow = time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)

type uploadStub struct {
	err     error
	inspect func(context.Context, io.Reader, *uploader.ObjectOptions) error
}

func (stub *uploadStub) UploadReader(
	ctx context.Context,
	reader io.Reader,
	options *uploader.ObjectOptions,
	response interface{},
) error {
	if stub.inspect != nil {
		if err := stub.inspect(ctx, reader, options); err != nil {
			return err
		}
	}
	if stub.err != nil {
		return stub.err
	}
	result := response.(*putResponse)
	result.Hash = "kodo-hash"
	result.Key = *options.ObjectName
	return nil
}

type managementStub struct {
	private      int64
	infoErr      error
	stat         *apis.StatObjectResponse
	statErr      error
	deletedEntry string
	deleteErr    error
}

func (stub *managementStub) GetBucketInfo(
	context.Context,
	*apis.GetBucketInfoRequest,
	*apis.Options,
) (*apis.GetBucketInfoResponse, error) {
	if stub.infoErr != nil {
		return nil, stub.infoErr
	}
	return &apis.GetBucketInfoResponse{Private: stub.private}, nil
}

func (stub *managementStub) StatObject(
	context.Context,
	*apis.StatObjectRequest,
	*apis.Options,
) (*apis.StatObjectResponse, error) {
	return stub.stat, stub.statErr
}

func (stub *managementStub) DeleteObject(
	_ context.Context,
	request *apis.DeleteObjectRequest,
	_ *apis.Options,
) (*apis.DeleteObjectResponse, error) {
	stub.deletedEntry = request.Entry
	return &apis.DeleteObjectResponse{}, stub.deleteErr
}

func TestClientPutSignAndDelete(t *testing.T) {
	payload := []byte("safe audio fixture")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	key := "audio/v1/assets/asset_test.wav"
	manager := &managementStub{private: 1}
	upload := &uploadStub{}
	upload.inspect = func(
		ctx context.Context,
		reader io.Reader,
		options *uploader.ObjectOptions,
	) error {
		body, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(body, payload) {
			t.Fatalf("uploaded body = %q, %v", body, err)
		}
		if *options.ObjectName != key ||
			options.FileName != "asset_test.wav" ||
			options.ContentType != "audio/wav" ||
			options.Metadata["sha256"] != checksum {
			t.Fatalf("upload options = %#v", options)
		}
		policy, err := options.UpToken.GetPutPolicy(ctx)
		if err != nil {
			t.Fatal(err)
		}
		insertOnly, ok := policy.GetInsertOnly()
		scope, scopeOK := policy.GetScope()
		minimum, minimumOK := policy.GetFsizeMin()
		limit, limitOK := policy.GetFsizeLimit()
		if !ok || insertOnly != 1 || !scopeOK || scope != "private-bucket:"+key ||
			!minimumOK || minimum != int64(len(payload)) ||
			!limitOK || limit != int64(len(payload)) {
			t.Fatalf("upload policy = %#v", policy)
		}
		return nil
	}
	client := newTestClient(upload, manager)
	body := bytes.NewReader(payload)

	result, err := client.Put(context.Background(), objectstore.PutRequest{
		Key: key, Body: body, Size: int64(len(payload)),
		ContentType: "audio/wav", ChecksumSHA256: checksum,
	})
	if err != nil || result.ETag != "kodo-hash" {
		t.Fatalf("Put() = %#v, %v", result, err)
	}
	if offset, _ := body.Seek(0, io.SeekCurrent); offset != 0 {
		t.Fatalf("body offset = %d, want 0", offset)
	}

	signed, err := client.SignedGet(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "private.example.com" ||
		parsed.Path != "/"+key || parsed.Query().Get("token") == "" ||
		parsed.Query().Get("e") != strconv.FormatInt(signed.ExpiresAt.Unix(), 10) ||
		!signed.ExpiresAt.Equal(fixedNow.Add(2*time.Minute)) {
		t.Fatalf("SignedGet() = %#v", signed)
	}

	if err := client.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if manager.deletedEntry != "private-bucket:"+key {
		t.Fatalf("deleted entry = %q", manager.deletedEntry)
	}
}

func TestClientReconcilesMatchingExistingObject(t *testing.T) {
	payload := []byte("same object")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	manager := &managementStub{
		private: 1,
		stat: &apis.StatObjectResponse{
			Size: int64(len(payload)), Hash: "existing-hash",
			MimeType: "audio/wav",
			Metadata: map[string]string{"x-qn-meta-sha256": checksum},
		},
	}
	client := newTestClient(
		&uploadStub{err: &qiniuclient.ErrorInfo{Code: 614, Err: "file exists"}},
		manager,
	)
	result, err := client.Put(context.Background(), objectstore.PutRequest{
		Key: "audio/v1/existing.wav", Body: bytes.NewReader(payload),
		Size: int64(len(payload)), ContentType: "audio/wav",
		ChecksumSHA256: checksum,
	})
	if err != nil || result.ETag != "existing-hash" {
		t.Fatalf("Put() = %#v, %v", result, err)
	}
}

func TestClientRejectsMismatchedExistingObject(t *testing.T) {
	payload := []byte("new object")
	digest := sha256.Sum256(payload)
	client := newTestClient(
		&uploadStub{err: &qiniuclient.ErrorInfo{Code: 614, Err: "secret object key"}},
		&managementStub{private: 1, stat: &apis.StatObjectResponse{
			Size: 1, Hash: "other", MimeType: "audio/wav",
		}},
	)
	_, err := client.Put(context.Background(), objectstore.PutRequest{
		Key: "audio/v1/existing.wav", Body: bytes.NewReader(payload),
		Size: int64(len(payload)), ContentType: "audio/wav",
		ChecksumSHA256: hex.EncodeToString(digest[:]),
	})
	if !errors.Is(err, objectstore.ErrAlreadyExists) ||
		strings.Contains(err.Error(), "secret object key") {
		t.Fatalf("Put() error = %v", err)
	}
}

func TestClientPreflightRequiresPrivateBucket(t *testing.T) {
	client := newTestClient(&uploadStub{}, &managementStub{private: 0})
	if err := client.Preflight(context.Background()); !errors.Is(err, ErrBucketNotPrivate) {
		t.Fatalf("Preflight() error = %v", err)
	}
}

func TestClientDeleteTreatsMissingObjectAsSuccess(t *testing.T) {
	manager := &managementStub{
		private:   1,
		deleteErr: &qiniuclient.ErrorInfo{Code: 612, Err: "no such file"},
	}
	client := newTestClient(&uploadStub{}, manager)
	if err := client.Delete(context.Background(), "audio/v1/missing.wav"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestClientOpenReturnsOnlyPDF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("token") == "" {
			t.Error("signed token missing")
		}
		writer.Header().Set("Content-Type", "application/pdf")
		_, _ = writer.Write([]byte("%PDF fixture"))
	}))
	defer server.Close()
	client := newTestClient(&uploadStub{}, &managementStub{private: 1})
	client.domain = server.URL
	client.httpClient = server.Client()
	client.prefix = "resume/v1"

	reader, err := client.Open(context.Background(), "resume/v1/document.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "%PDF fixture" {
		t.Fatalf("Open() content = %q, %v", content, err)
	}
}

func TestNewRejectsMissingCredentialsBeforeNetwork(t *testing.T) {
	t.Setenv("QINIU_ACCESS_KEY", "")
	t.Setenv("QINIU_SECRET_KEY", "")
	client, err := New(context.Background(), config.ObjectStorageConfig{
		Enabled: true, Provider: config.ObjectStorageProviderQiniuKodo,
		Bucket: "private-bucket", Domain: "https://private.example.com",
		AudioPrefix: "audio/v1", ImagePrefix: "image/v1", ResumePrefix: "resume/v1",
		SignedURLTTL: 2 * time.Minute, ServerSideEncryption: true,
	})
	if client != nil || !errors.Is(err, objectstore.ErrCredentials) {
		t.Fatalf("New() = %#v, %v", client, err)
	}
}

func newTestClient(upload uploadAPI, manager managementAPI) *Client {
	return &Client{
		uploader: upload, management: manager,
		credentials: credentials.NewCredentials("test-access", "test-secret"),
		httpClient:  http.DefaultClient,
		bucket:      "private-bucket", domain: "https://private.example.com",
		prefix: "audio/v1", signedURLTTL: 2 * time.Minute,
		now: func() time.Time { return fixedNow },
	}
}
