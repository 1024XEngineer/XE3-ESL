// Package storage 把平台私有对象存储适配为 Resume 文件存储端口。
package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

// readableObjectStore 是 Resume 解析需要的最小私有对象能力。
type readableObjectStore interface {
	objectstore.Store
	Open(context.Context, string) (io.ReadCloser, error)
}

// ObjectStore 使用平台对象存储保存和读取私有 PDF。
type ObjectStore struct {
	store readableObjectStore
}

// NewObjectStore 创建 Resume 文件存储适配器。
func NewObjectStore(store readableObjectStore) (*ObjectStore, error) {
	if store == nil {
		return nil, errors.New("resume: readable object store is required")
	}
	return &ObjectStore{store: store}, nil
}

// Put 校验字节数和摘要后保存私有 PDF 对象。
func (storage *ObjectStore) Put(
	ctx context.Context,
	key string,
	reader io.Reader,
	size int64,
	checksum string,
) error {
	if storage == nil || storage.store == nil || ctx == nil || reader == nil || size < 1 {
		return objectstore.ErrInvalidObject
	}
	body, err := io.ReadAll(io.LimitReader(reader, size+1))
	if err != nil || int64(len(body)) != size {
		return objectstore.ErrInvalidObject
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != checksum {
		return objectstore.ErrInvalidObject
	}
	_, err = storage.store.Put(ctx, objectstore.PutRequest{
		Key:            key,
		Body:           bytes.NewReader(body),
		Size:           size,
		ContentType:    "application/pdf",
		ChecksumSHA256: checksum,
	})
	return err
}

// Open 打开一份仅供服务端解析的私有 PDF 对象流。
func (storage *ObjectStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if storage == nil || storage.store == nil || ctx == nil {
		return nil, objectstore.ErrOperationFailed
	}
	return storage.store.Open(ctx, key)
}

// SignedReadURL 创建短时有效的私有 PDF 读取地址。
func (storage *ObjectStore) SignedReadURL(
	ctx context.Context,
	key string,
	lifetime time.Duration,
) (string, time.Time, error) {
	if storage == nil || storage.store == nil || ctx == nil || lifetime <= 0 {
		return "", time.Time{}, objectstore.ErrInvalidTTL
	}
	result, err := storage.store.SignedGet(ctx, key)
	if err != nil {
		return "", time.Time{}, err
	}
	if result.URL == "" || result.ExpiresAt.IsZero() {
		return "", time.Time{}, objectstore.ErrOperationFailed
	}
	return result.URL, result.ExpiresAt, nil
}

// Delete 删除一份私有 PDF 对象；对象不存在由具体 Store 按幂等删除处理。
func (storage *ObjectStore) Delete(ctx context.Context, key string) error {
	if storage == nil || storage.store == nil || ctx == nil {
		return objectstore.ErrOperationFailed
	}
	return storage.store.Delete(ctx, key)
}
