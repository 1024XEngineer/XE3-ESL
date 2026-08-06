// Package image owns images supplied to Agent messages, from upload through
// attachment and eventual object cleanup.
package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	ObjectPrefix  = "image/v1/agent/"
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

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

var checksumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type AssetStatus string

const (
	StatusStaged   AssetStatus = "staged"
	StatusAttached AssetStatus = "attached"
	StatusDeleting AssetStatus = "deleting"
	StatusDeleted  AssetStatus = "deleted"
)

type Asset struct {
	ID                 string
	OwnerID            string
	ThreadID           string
	UploadRequestID    string
	ObjectKey          string
	ContentType        string
	Size               int64
	Width              int
	Height             int
	ChecksumSHA256     string
	ETag               string
	UploadLeaseUntil   time.Time
	UploadFencingToken uint64
	Status             AssetStatus
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	AttachedAt         time.Time
	DeletedAt          time.Time
}

type AssetStage struct {
	Asset   Asset
	Created bool
}

type UploadClaim struct {
	Asset          Asset
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type CleanupClaim struct {
	AssetID      string
	OwnerID      string
	ObjectKey    string
	FencingToken uint64
}

func (asset Asset) ValidNew() bool {
	suffixValid := false
	switch asset.ContentType {
	case "image/jpeg":
		suffixValid = strings.HasSuffix(asset.ObjectKey, ".jpg")
	case "image/png":
		suffixValid = strings.HasSuffix(asset.ObjectKey, ".png")
	case "image/webp":
		suffixValid = strings.HasSuffix(asset.ObjectKey, ".webp")
	}
	return ValidUUID(asset.ID) &&
		ValidUUID(asset.OwnerID) &&
		ValidUUID(asset.ThreadID) &&
		len(asset.UploadRequestID) >= 8 &&
		len(asset.UploadRequestID) <= 128 &&
		!strings.ContainsAny(asset.UploadRequestID, "\x00\r\n") &&
		strings.HasPrefix(asset.ObjectKey, ObjectPrefix) &&
		!strings.Contains(asset.ObjectKey, "..") &&
		suffixValid &&
		asset.Size > 0 &&
		asset.Size <= MaxBytes &&
		asset.Width > 0 &&
		asset.Height > 0 &&
		int64(asset.Width)*int64(asset.Height) <= MaxPixels &&
		checksumPattern.MatchString(asset.ChecksumSHA256) &&
		asset.ETag == "" &&
		asset.Status == StatusStaged &&
		asset.ExpiresAt.After(asset.CreatedAt) &&
		asset.ExpiresAt.Sub(asset.CreatedAt) <= 7*24*time.Hour
}

func (claim CleanupClaim) Valid() bool {
	return ValidUUID(claim.AssetID) &&
		ValidUUID(claim.OwnerID) &&
		strings.HasPrefix(claim.ObjectKey, ObjectPrefix) &&
		!strings.Contains(claim.ObjectKey, "..") &&
		claim.FencingToken > 0 &&
		claim.FencingToken <= uint64(1<<63-1)
}

type Repository interface {
	StageAsset(context.Context, Asset) (AssetStage, error)
	ClaimUpload(
		context.Context,
		string,
		string,
		time.Duration,
	) (UploadClaim, bool, error)
	CommitUpload(
		context.Context,
		string,
		string,
		uint64,
		string,
	) (Asset, error)
	FindAsset(context.Context, string, string) (Asset, error)
	ListMessageAssets(
		context.Context,
		string,
		string,
		string,
	) ([]Asset, error)
	AttachAssets(
		context.Context,
		string,
		string,
		string,
		[]string,
	) ([]Asset, error)
	BeginDeletion(context.Context, string, string) (Asset, error)
	FinishDeletion(context.Context, string, string) (Asset, error)
	ClaimCleanup(
		context.Context,
		time.Duration,
		int,
	) ([]CleanupClaim, error)
	FinishCleanup(context.Context, CleanupClaim) error
	ReleaseCleanup(context.Context, CleanupClaim) error
}

type IDGenerator interface {
	NewID() (string, error)
}

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
	Get(context.Context, requestcontext.Actor, string) (Asset, error)
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

func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func ValidAssetIDs(assetIDs []string) bool {
	if len(assetIDs) < 1 || len(assetIDs) > MaxPerMessage {
		return false
	}
	seen := make(map[string]struct{}, len(assetIDs))
	for _, id := range assetIDs {
		if !ValidUUID(id) {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}
