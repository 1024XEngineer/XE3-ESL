// Package avatar owns the authenticated user's profile-avatar lifecycle.
package avatar

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const MaxBytes = 10 * 1024 * 1024

var (
	ErrInvalidRequest      = errors.New("profile avatar: invalid request")
	ErrNotFound            = errors.New("profile avatar: not found")
	ErrConflict            = errors.New("profile avatar: profile version conflict")
	ErrUploadInProgress    = errors.New("profile avatar: upload in progress")
	ErrIdempotencyConflict = errors.New("profile avatar: idempotency conflict")
	ErrRepository          = errors.New("profile avatar: repository failed")
)

type UploadRequest struct {
	IdempotencyKey         string
	ContentType            string
	Body                   io.Reader
	ExpectedProfileVersion int64
}

type Repository interface {
	Attach(context.Context, string, string, int64) (identity.UserProfile, error)
	UseDefault(context.Context, string, int64) (identity.UserProfile, error)
	CurrentAssetID(context.Context, string) (string, error)
}

type Application interface {
	Upload(context.Context, requestcontext.Actor, UploadRequest) (identity.UserProfile, error)
	UseDefault(context.Context, requestcontext.Actor, int64) (identity.UserProfile, error)
	Content(context.Context, requestcontext.Actor) (objectstore.SignedGetResult, error)
}

type Config struct {
	StagedTTL time.Duration
}
