// Package media owns durable private-object metadata and object lifecycle.
// Business modules own references to Assets; media owns only bytes and their
// upload/deletion state.
package media

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

type Kind string

const (
	KindImage    Kind = "image"
	KindAudio    Kind = "audio"
	KindDocument Kind = "document"
)

type Status string

const (
	StatusStaged   Status = "staged"
	StatusReady    Status = "ready"
	StatusDeleting Status = "deleting"
)

var (
	ErrInvalidRequest      = errors.New("media: invalid request")
	ErrNotFound            = errors.New("media: not found")
	ErrConflict            = errors.New("media: conflict")
	ErrIdempotencyConflict = errors.New("media: idempotency conflict")
	ErrRepository          = errors.New("media repository: operation failed")
)

var (
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
	)
	checksumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Asset struct {
	ID                  string
	UserID              string
	Kind                Kind
	UploadRequestID     string
	ObjectKey           string
	ContentType         string
	Size                int64
	ChecksumSHA256      string
	ETag                string
	Width               int
	Height              int
	Duration            time.Duration
	SampleRate          int
	Status              Status
	UploadFencingToken  uint64
	UploadLeaseUntil    time.Time
	ExpiresAt           time.Time
	CleanupAttempts     int
	CleanupFencingToken uint64
	CleanupLeaseUntil   time.Time
	CleanupAvailableAt  time.Time
	CleanupError        string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Upload struct {
	UserID         string
	Kind           Kind
	IdempotencyKey string
	ContentType    string
	Body           io.ReadSeeker
	Size           int64
	ChecksumSHA256 string
	Width          int
	Height         int
	Duration       time.Duration
	SampleRate     int
	ExpiresAt      time.Time
}

type Stage struct {
	Asset   Asset
	Created bool
}

type UploadClaim struct {
	Asset          Asset
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type CleanupClaim struct {
	AssetID        string
	Kind           Kind
	ObjectKey      string
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type CleanupResult struct {
	Deleted int
	Failed  int
}

type Stores struct {
	Images    objectstore.Store
	Audio     objectstore.Store
	Documents DocumentStore
}

// DocumentStore adds the server-side read needed by trusted document parsers.
// It is explicit because image and audio consumers never need raw-object reads.
type DocumentStore interface {
	objectstore.Store
	Open(context.Context, string) (io.ReadCloser, error)
}

type Config struct {
	UploadLease  time.Duration
	CleanupLease time.Duration
	PlaybackTTL  time.Duration
	CleanupBatch int
}

type Repository interface {
	Stage(context.Context, Asset) (Stage, error)
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
	FindOwned(context.Context, string, string) (Asset, error)
	BeginDeletion(
		context.Context,
		string,
		string,
		time.Duration,
	) (Asset, error)
	ClaimCleanup(context.Context, time.Duration, int) ([]CleanupClaim, error)
	FinishCleanup(context.Context, CleanupClaim) error
	ReleaseCleanup(context.Context, CleanupClaim, string) error
}

type IDGenerator interface {
	NewID() (string, error)
}

func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func ValidIdempotencyKey(value string) bool {
	return len(value) >= 8 && len(value) <= 128 &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func ValidChecksum(value string) bool {
	return checksumPattern.MatchString(value)
}
