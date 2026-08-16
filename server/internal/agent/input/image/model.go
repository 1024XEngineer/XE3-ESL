// Package image validates and projects images used by Agent messages. Durable
// object metadata and cleanup belong to the shared media component.
package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	MaxPerMessage = 4
	MaxBytes      = 10 * 1024 * 1024
	MaxPixels     = 16_000_000
)

var (
	ErrInvalidRequest      = errors.New("agent image input: invalid request")
	ErrNotFound            = errors.New("agent image input: not found")
	ErrConflict            = errors.New("agent image input: conflict")
	ErrIdempotencyConflict = errors.New("agent image input: idempotency conflict")
	ErrRepository          = errors.New("agent image input repository: operation failed")
	ErrTooLarge            = fmt.Errorf("%w: image exceeds limits", ErrInvalidRequest)
	ErrUnsupported         = fmt.Errorf("%w: image format is unsupported", ErrInvalidRequest)
	ErrInvalid             = fmt.Errorf("%w: image payload is invalid", ErrInvalidRequest)
)

type Image struct {
	ID          string
	ContentType string
	Size        int64
	Width       int
	Height      int
	Status      string
	CreatedAt   time.Time
	AttachedAt  time.Time
}

type Config struct {
	StagedTTL time.Duration
}

type UploadRequest struct {
	ThreadID       string
	IdempotencyKey string
	ContentType    string
	Body           io.Reader
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
	) (Image, error)
	Get(context.Context, requestcontext.Actor, string) (Image, error)
	Content(
		context.Context,
		requestcontext.Actor,
		string,
	) (objectstore.SignedGetResult, error)
	Delete(context.Context, requestcontext.Actor, string) error
	MessageAssets(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) ([]Image, error)
}
