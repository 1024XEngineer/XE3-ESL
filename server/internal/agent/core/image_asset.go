package core

import (
	"context"
	"time"
)

const (
	AgentImageObjectPrefix = "image/v1/agent/"
	MaxImagesPerMessage    = 4
	MaxImageBytes          = 10 * 1024 * 1024
	MaxImagePixels         = 16_000_000
)

type ImageAssetStatus string

const (
	ImageAssetStaged   ImageAssetStatus = "staged"
	ImageAssetAttached ImageAssetStatus = "attached"
	ImageAssetDeleting ImageAssetStatus = "deleting"
	ImageAssetDeleted  ImageAssetStatus = "deleted"
)

type ImageAsset struct {
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
	Status             ImageAssetStatus
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	AttachedAt         time.Time
	DeletedAt          time.Time
}

type ImageAssetStage struct {
	Asset   ImageAsset
	Created bool
}

type ImageUploadClaim struct {
	Asset          ImageAsset
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type ImageCleanupClaim struct {
	AssetID      string
	OwnerID      string
	ObjectKey    string
	FencingToken uint64
}

type ImageAssetRepository interface {
	StageImageAsset(
		context.Context,
		ImageAsset,
	) (ImageAssetStage, error)
	ClaimImageUpload(
		context.Context,
		string,
		string,
		time.Duration,
	) (ImageUploadClaim, bool, error)
	CommitImageUpload(
		context.Context,
		string,
		string,
		uint64,
		string,
	) (ImageAsset, error)
	FindImageAsset(
		context.Context,
		string,
		string,
	) (ImageAsset, error)
	ListMessageImageAssets(
		context.Context,
		string,
		string,
		string,
	) ([]ImageAsset, error)
	AttachImageAssets(
		context.Context,
		string,
		string,
		string,
		[]string,
	) ([]ImageAsset, error)
	BeginImageAssetDeletion(
		context.Context,
		string,
		string,
	) (ImageAsset, error)
	FinishImageAssetDeletion(
		context.Context,
		string,
		string,
	) (ImageAsset, error)
	ClaimImageCleanup(
		context.Context,
		time.Duration,
		int,
	) ([]ImageCleanupClaim, error)
	FinishImageCleanup(
		context.Context,
		ImageCleanupClaim,
	) error
	ReleaseImageCleanup(
		context.Context,
		ImageCleanupClaim,
	) error
}
