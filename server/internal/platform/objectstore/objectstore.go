// Package objectstore defines the protected binary-storage boundary shared by
// business modules. PostgreSQL remains the source of truth for ownership and
// lifecycle state; implementations store only opaque object bytes.
package objectstore

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"time"
)

var (
	ErrDisabled        = errors.New("object storage is disabled")
	ErrCredentials     = errors.New("object storage credentials are unavailable")
	ErrInvalidKey      = errors.New("object key is outside the configured prefix")
	ErrInvalidObject   = errors.New("object body, size, or content type is invalid")
	ErrInvalidTTL      = errors.New("signed URL lifetime is invalid")
	ErrAlreadyExists   = errors.New("object already exists")
	ErrOperationFailed = errors.New("object storage operation failed")
)

type PutRequest struct {
	Key            string
	Body           io.ReadSeeker
	Size           int64
	ContentType    string
	ChecksumSHA256 string
}

type PutResult struct {
	ETag string
}

type SignedGetResult struct {
	URL       string
	ExpiresAt time.Time
}

type Store interface {
	Put(context.Context, PutRequest) (PutResult, error)
	SignedGet(context.Context, string) (SignedGetResult, error)
	Delete(context.Context, string) error
}

// ValidateKey rejects absolute, traversing, or cross-prefix object keys before
// they reach a provider SDK.
func ValidateKey(prefix, key string) error {
	normalizedPrefix := strings.TrimSuffix(strings.TrimSpace(prefix), "/")
	if normalizedPrefix == "" ||
		key == "" ||
		strings.HasPrefix(key, "/") ||
		strings.Contains(key, "\\") ||
		path.Clean(key) != key ||
		!strings.HasPrefix(key, normalizedPrefix+"/") {
		return ErrInvalidKey
	}
	return nil
}
