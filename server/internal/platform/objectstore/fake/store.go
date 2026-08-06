// Package fake provides an explicit deterministic ObjectStore for offline
// tests. Production composition must select a real private-storage adapter.
package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

type object struct {
	body        []byte
	contentType string
	checksum    string
	etag        string
}

type Store struct {
	mu        sync.RWMutex
	prefix    string
	signedTTL time.Duration
	now       func() time.Time
	objects   map[string]object
}

func New(prefix string, signedTTL time.Duration) (*Store, error) {
	prefix = strings.TrimSuffix(strings.TrimSpace(prefix), "/")
	if prefix == "" ||
		signedTTL <= 0 ||
		signedTTL > 2*time.Minute {
		return nil, objectstore.ErrInvalidTTL
	}
	return &Store{
		prefix:    prefix,
		signedTTL: signedTTL,
		now:       func() time.Time { return time.Now().UTC() },
		objects:   make(map[string]object),
	}, nil
}

func (store *Store) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return objectstore.PutResult{}, context.Canceled
	}
	if err := objectstore.ValidateKey(store.prefix, request.Key); err != nil {
		return objectstore.PutResult{}, err
	}
	if request.Body == nil ||
		request.Size <= 0 ||
		request.ContentType == "" ||
		len(request.ChecksumSHA256) != 64 {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	offset, err := request.Body.Seek(0, io.SeekCurrent)
	if err != nil {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	defer func() {
		_, _ = request.Body.Seek(offset, io.SeekStart)
	}()
	body, err := io.ReadAll(io.LimitReader(request.Body, request.Size+1))
	if err != nil || int64(len(body)) != request.Size {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != request.ChecksumSHA256 {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	etag := hex.EncodeToString(sum[:16])

	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, found := store.objects[request.Key]; found {
		if existing.contentType != request.ContentType ||
			existing.checksum != request.ChecksumSHA256 ||
			string(existing.body) != string(body) {
			return objectstore.PutResult{}, objectstore.ErrAlreadyExists
		}
		return objectstore.PutResult{ETag: existing.etag}, nil
	}
	copied := append([]byte(nil), body...)
	store.objects[request.Key] = object{
		body:        copied,
		contentType: request.ContentType,
		checksum:    request.ChecksumSHA256,
		etag:        etag,
	}
	return objectstore.PutResult{ETag: etag}, nil
}

func (store *Store) SignedGet(
	ctx context.Context,
	key string,
) (objectstore.SignedGetResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return objectstore.SignedGetResult{}, context.Canceled
	}
	if err := objectstore.ValidateKey(store.prefix, key); err != nil {
		return objectstore.SignedGetResult{}, err
	}
	store.mu.RLock()
	_, found := store.objects[key]
	store.mu.RUnlock()
	if !found {
		return objectstore.SignedGetResult{}, objectstore.ErrOperationFailed
	}
	return objectstore.SignedGetResult{
		URL: "https://fake-object-store.invalid/" +
			url.PathEscape(key) + "?signature=fake",
		ExpiresAt: store.now().Add(store.signedTTL),
	}, nil
}

// Open 返回对象内容的独立只读流，供受控服务端处理使用。
func (store *Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, context.Canceled
	}
	if err := objectstore.ValidateKey(store.prefix, key); err != nil {
		return nil, err
	}
	store.mu.RLock()
	item, found := store.objects[key]
	store.mu.RUnlock()
	if !found {
		return nil, objectstore.ErrOperationFailed
	}
	return io.NopCloser(strings.NewReader(string(item.body))), nil
}

func (store *Store) Delete(ctx context.Context, key string) error {
	if ctx == nil || ctx.Err() != nil {
		return context.Canceled
	}
	if err := objectstore.ValidateKey(store.prefix, key); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.objects, key)
	return nil
}

// Bytes returns a copy for test adapters without exposing mutable store state.
func (store *Store) Bytes(key string) ([]byte, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	item, found := store.objects[key]
	if !found {
		return nil, false
	}
	return append([]byte(nil), item.body...), true
}

func (store *Store) Has(key string) bool {
	store.mu.RLock()
	defer store.mu.RUnlock()
	_, found := store.objects[key]
	return found
}

var _ objectstore.Store = (*Store)(nil)
