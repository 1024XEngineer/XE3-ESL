// Package image owns durable Agent image assets before transport and message
// integration expose them to clients.
package image

import (
	"context"
	"io"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Asset = core.ImageAsset
type AssetStatus = core.ImageAssetStatus
type CleanupClaim = core.ImageCleanupClaim
type Repository = core.ImageAssetRepository
type IDGenerator = core.IDGenerator
type Content = objectstore.SignedGetResult

const (
	StatusStaged   = core.ImageAssetStaged
	StatusAttached = core.ImageAssetAttached
	StatusDeleting = core.ImageAssetDeleting
	StatusDeleted  = core.ImageAssetDeleted
)

var (
	ErrInvalidRequest      = core.ErrInvalidRequest
	ErrNotFound            = core.ErrNotFound
	ErrConflict            = core.ErrConflict
	ErrIdempotencyConflict = core.ErrIdempotencyConflict
	ErrRepository          = core.ErrRepository
	ErrImageTooLarge       = core.ErrImageTooLarge
	ErrUnsupportedImage    = core.ErrUnsupportedImage
	ErrInvalidImage        = core.ErrInvalidImage
)

type Config struct {
	StagedTTL   time.Duration
	UploadLease time.Duration
}

type UploadRequest struct {
	ThreadID       string
	IdempotencyKey string
	ContentType    string
	Body           io.Reader
}

type CleanupResult struct {
	Deleted int
	Failed  int
}

type ContextImage struct {
	AssetID string
	URL     string
}

type ContextReader interface {
	MessageImages(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) ([]ContextImage, error)
}

type Application interface {
	Upload(
		context.Context,
		requestcontext.Actor,
		UploadRequest,
	) (Asset, error)
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (Asset, error)
	Content(
		context.Context,
		requestcontext.Actor,
		string,
	) (Content, error)
	Delete(
		context.Context,
		requestcontext.Actor,
		string,
	) error
	MessageAssets(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) ([]Asset, error)
	Attach(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		[]string,
	) ([]Asset, error)
	Reclaim(context.Context, int) (CleanupResult, error)
}
