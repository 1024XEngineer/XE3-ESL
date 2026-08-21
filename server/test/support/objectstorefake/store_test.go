package objectstorefake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

func TestStorePutReplayReadAndDelete(t *testing.T) {
	store, err := New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := []byte("private audio")
	sum := sha256.Sum256(body)
	request := objectstore.PutRequest{
		Key:            "audio/v1/agent/candidate.wav",
		Body:           bytes.NewReader(body),
		Size:           int64(len(body)),
		ContentType:    "audio/wav",
		ChecksumSHA256: hex.EncodeToString(sum[:]),
	}
	first, err := store.Put(context.Background(), request)
	if err != nil {
		t.Fatalf("first Put() error = %v", err)
	}
	replayed, err := store.Put(context.Background(), request)
	if err != nil || replayed.ETag != first.ETag {
		t.Fatalf("replayed Put() = %#v, %v", replayed, err)
	}
	copied, found := store.Bytes(request.Key)
	if !found || !bytes.Equal(copied, body) {
		t.Fatalf("Bytes() = %q, %t", copied, found)
	}
	copied[0] = 'X'
	again, _ := store.Bytes(request.Key)
	if bytes.Equal(copied, again) {
		t.Fatal("Bytes() exposed mutable store state")
	}
	if err := store.Delete(context.Background(), request.Key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if store.Has(request.Key) {
		t.Fatal("Delete() retained object")
	}
}

func TestStoreRejectsSameKeyWithDifferentObject(t *testing.T) {
	store, _ := New("audio/v1", time.Minute)
	first := []byte("first")
	second := []byte("other")
	if _, err := store.Put(
		context.Background(),
		fakePut("audio/v1/agent/reused.wav", first),
	); err != nil {
		t.Fatalf("first Put() error = %v", err)
	}
	if _, err := store.Put(
		context.Background(),
		fakePut("audio/v1/agent/reused.wav", second),
	); !errors.Is(err, objectstore.ErrAlreadyExists) {
		t.Fatalf("changed Put() error = %v", err)
	}
}

func fakePut(key string, body []byte) objectstore.PutRequest {
	sum := sha256.Sum256(body)
	return objectstore.PutRequest{
		Key:            key,
		Body:           bytes.NewReader(body),
		Size:           int64(len(body)),
		ContentType:    "audio/wav",
		ChecksumSHA256: hex.EncodeToString(sum[:]),
	}
}
