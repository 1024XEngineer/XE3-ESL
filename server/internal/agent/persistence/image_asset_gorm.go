package persistence

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var imageChecksumPattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

// GormImageAssetRepository stays intentionally separate from PostgresRepository.
// The existing Agent persistence remains pgx-based while image assets use GORM.
type GormImageAssetRepository struct {
	database *gorm.DB
}

func NewGormImageAssetRepository(
	database *gorm.DB,
) (*GormImageAssetRepository, error) {
	if database == nil {
		return nil, ErrRepository
	}
	return &GormImageAssetRepository{database: database}, nil
}

// NewGormImageAssetRepositoryFromPool adapts the process-owned pgx pool for
// GORM. It does not create or own a second connection pool.
func NewGormImageAssetRepositoryFromPool(
	pool *pgxpool.Pool,
) (*GormImageAssetRepository, error) {
	database, err := newGormDatabaseFromPool(pool)
	if err != nil {
		return nil, err
	}
	return NewGormImageAssetRepository(database)
}

func newGormDatabaseFromPool(pool *pgxpool.Pool) (*gorm.DB, error) {
	if pool == nil {
		return nil, ErrRepository
	}
	sqlDatabase := stdlib.OpenDBFromPool(pool)
	database, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{Conn: sqlDatabase}),
		&gorm.Config{
			DisableAutomaticPing: true,
			Logger:               logger.Default.LogMode(logger.Silent),
		},
	)
	if err != nil {
		return nil, ErrRepository
	}
	return database, nil
}

type imageAssetRecord struct {
	ID                  string     `gorm:"column:image_asset_id;type:uuid;primaryKey"`
	OwnerID             string     `gorm:"column:owner_user_id;type:uuid"`
	ThreadID            string     `gorm:"column:thread_id;type:uuid"`
	UploadRequestID     string     `gorm:"column:upload_request_id"`
	ObjectKey           string     `gorm:"column:object_key"`
	ContentType         string     `gorm:"column:content_type"`
	Size                int64      `gorm:"column:size_bytes"`
	Width               int        `gorm:"column:width"`
	Height              int        `gorm:"column:height"`
	ChecksumSHA256      string     `gorm:"column:checksum_sha256"`
	ETag                string     `gorm:"column:etag"`
	UploadLeaseUntil    *time.Time `gorm:"column:upload_lease_until"`
	UploadFencingToken  int64      `gorm:"column:upload_fencing_token"`
	Status              string     `gorm:"column:status"`
	ExpiresAt           time.Time  `gorm:"column:expires_at"`
	CleanupLeaseUntil   *time.Time `gorm:"column:cleanup_lease_until"`
	CleanupFencingToken int64      `gorm:"column:cleanup_fencing_token"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
	AttachedAt          *time.Time `gorm:"column:attached_at"`
	DeletedAt           *time.Time `gorm:"column:deleted_at"`
}

func (imageAssetRecord) TableName() string {
	return "agent_image_assets"
}

type messageImageRecord struct {
	OwnerID   string    `gorm:"column:owner_user_id;type:uuid"`
	ThreadID  string    `gorm:"column:thread_id;type:uuid"`
	MessageID string    `gorm:"column:message_id;type:uuid;primaryKey"`
	AssetID   string    `gorm:"column:image_asset_id;type:uuid"`
	Position  int       `gorm:"column:position;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (messageImageRecord) TableName() string {
	return "agent_message_images"
}

func (r *GormImageAssetRepository) StageImageAsset(
	ctx context.Context,
	asset ImageAsset,
) (ImageAssetStage, error) {
	if ctx == nil || !validNewImageAsset(asset) {
		return ImageAssetStage{}, ErrInvalidRequest
	}

	record := imageAssetToRecord(asset)
	result := r.database.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "owner_user_id"},
				{Name: "thread_id"},
				{Name: "upload_request_id"},
			},
			DoNothing: true,
		}).
		Create(&record)
	if result.Error != nil {
		return ImageAssetStage{}, mapGormError(result.Error)
	}
	if result.RowsAffected == 1 {
		created, err := record.toDomain()
		return ImageAssetStage{Asset: created, Created: true}, err
	}

	existing, err := r.findImageAssetByUpload(
		ctx,
		asset.OwnerID,
		asset.ThreadID,
		asset.UploadRequestID,
	)
	if err != nil {
		return ImageAssetStage{}, err
	}
	return ImageAssetStage{Asset: existing, Created: false}, nil
}

func (r *GormImageAssetRepository) ClaimImageUpload(
	ctx context.Context,
	ownerID string,
	assetID string,
	leaseDuration time.Duration,
) (ImageUploadClaim, bool, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(assetID) ||
		leaseDuration < time.Second || leaseDuration > 10*time.Minute {
		return ImageUploadClaim{}, false, ErrInvalidRequest
	}

	var record imageAssetRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Where("status = ? AND etag = ''", string(ImageAssetStaged)).
		Where(
			"(upload_lease_until IS NULL OR upload_lease_until <= transaction_timestamp())",
		).
		Where("expires_at > transaction_timestamp()").
		Where(
			"EXISTS (?)",
			r.database.
				Table("identity_users").
				Select("1").
				Where("id = ? AND account_status = ?", ownerID, "active"),
		).
		Updates(map[string]any{
			"upload_lease_until": time.Now().UTC().Add(leaseDuration),
			"upload_fencing_token": gorm.Expr(
				"upload_fencing_token + 1",
			),
			"updated_at": nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return ImageUploadClaim{}, false, mapGormError(result.Error)
	}
	if result.RowsAffected == 1 {
		asset, err := record.toDomain()
		if err != nil {
			return ImageUploadClaim{}, false, err
		}
		return ImageUploadClaim{
			Asset:          asset,
			FencingToken:   asset.UploadFencingToken,
			LeaseExpiresAt: asset.UploadLeaseUntil,
		}, true, nil
	}

	current, err := r.FindImageAsset(ctx, ownerID, assetID)
	if err != nil {
		return ImageUploadClaim{}, false, err
	}
	return ImageUploadClaim{
		Asset:          current,
		FencingToken:   current.UploadFencingToken,
		LeaseExpiresAt: current.UploadLeaseUntil,
	}, false, nil
}

func (r *GormImageAssetRepository) CommitImageUpload(
	ctx context.Context,
	ownerID string,
	assetID string,
	fencingToken uint64,
	etag string,
) (ImageAsset, error) {
	etag = strings.TrimSpace(etag)
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(assetID) ||
		fencingToken == 0 || fencingToken > uint64(1<<63-1) ||
		etag == "" || len(etag) > 512 {
		return ImageAsset{}, ErrInvalidRequest
	}

	var record imageAssetRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Where("status = ? AND etag = ''", string(ImageAssetStaged)).
		Where(
			"upload_fencing_token = ? AND upload_lease_until > transaction_timestamp()",
			int64(fencingToken),
		).
		Updates(map[string]any{
			"etag":               etag,
			"upload_lease_until": nil,
			"updated_at":         nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return ImageAsset{}, mapGormError(result.Error)
	}
	if result.RowsAffected == 1 {
		return record.toDomain()
	}

	current, err := r.FindImageAsset(ctx, ownerID, assetID)
	if err != nil {
		return ImageAsset{}, err
	}
	if current.ETag == etag &&
		current.UploadFencingToken == fencingToken {
		return current, nil
	}
	return ImageAsset{}, ErrConflict
}

func (r *GormImageAssetRepository) FindImageAsset(
	ctx context.Context,
	ownerID string,
	assetID string,
) (ImageAsset, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(assetID) {
		return ImageAsset{}, ErrNotFound
	}

	var record imageAssetRecord
	err := r.database.WithContext(ctx).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Take(&record).Error
	if err != nil {
		return ImageAsset{}, mapGormError(err)
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) findImageAssetByUpload(
	ctx context.Context,
	ownerID string,
	threadID string,
	uploadRequestID string,
) (ImageAsset, error) {
	var record imageAssetRecord
	err := r.database.WithContext(ctx).
		Where(
			"owner_user_id = ? AND thread_id = ? AND upload_request_id = ?",
			ownerID,
			threadID,
			uploadRequestID,
		).
		Take(&record).Error
	if err != nil {
		return ImageAsset{}, mapGormError(err)
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) ListMessageImageAssets(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
) ([]ImageAsset, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(threadID) ||
		!ValidUUID(messageID) {
		return nil, ErrNotFound
	}
	var records []imageAssetRecord
	err := r.database.WithContext(ctx).
		Table("agent_image_assets AS asset").
		Select("asset.*").
		Joins(
			`JOIN agent_message_images AS link
				ON link.image_asset_id = asset.image_asset_id
				AND link.owner_user_id = asset.owner_user_id
				AND link.thread_id = asset.thread_id`,
		).
		Where(
			`link.owner_user_id = ? AND link.thread_id = ?
				AND link.message_id = ?`,
			ownerID,
			threadID,
			messageID,
		).
		Order("link.position").
		Find(&records).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	assets := make([]ImageAsset, 0, len(records))
	for _, record := range records {
		asset, err := record.toDomain()
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (r *GormImageAssetRepository) AttachImageAssets(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
	assetIDs []string,
) ([]ImageAsset, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(threadID) ||
		!ValidUUID(messageID) || !validImageAssetIDs(assetIDs) {
		return nil, ErrInvalidRequest
	}

	var attached []ImageAsset
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var message struct {
			Role string `gorm:"column:role"`
		}
		err := tx.
			Table("agent_messages").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("role").
			Where(
				"id = ? AND owner_user_id = ? AND thread_id = ?",
				messageID,
				ownerID,
				threadID,
			).
			Take(&message).Error
		if err != nil {
			return mapGormError(err)
		}
		if message.Role != string(MessageRoleUser) {
			return ErrConflict
		}

		var records []imageAssetRecord
		err = tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_user_id = ? AND thread_id = ?", ownerID, threadID).
			Where("image_asset_id IN ?", assetIDs).
			Order("image_asset_id").
			Find(&records).Error
		if err != nil {
			return mapGormError(err)
		}
		if len(records) != len(assetIDs) {
			return ErrNotFound
		}

		byID := make(map[string]imageAssetRecord, len(records))
		for _, record := range records {
			byID[record.ID] = record
		}
		now := time.Now().UTC()
		attached = make([]ImageAsset, 0, len(assetIDs))
		for position, assetID := range assetIDs {
			record := byID[assetID]
			if record.Status != string(ImageAssetStaged) ||
				record.ETag == "" ||
				!record.ExpiresAt.After(now) {
				return ErrConflict
			}

			link := messageImageRecord{
				OwnerID:   ownerID,
				ThreadID:  threadID,
				MessageID: messageID,
				AssetID:   assetID,
				Position:  position,
				CreatedAt: now,
			}
			if err := tx.Create(&link).Error; err != nil {
				return mapGormError(err)
			}

			result := tx.
				Model(&record).
				Clauses(clause.Returning{}).
				Where(
					"image_asset_id = ? AND owner_user_id = ? AND thread_id = ?",
					assetID,
					ownerID,
					threadID,
				).
				Where("status = ? AND etag <> ''", string(ImageAssetStaged)).
				Updates(map[string]any{
					"status":      string(ImageAssetAttached),
					"attached_at": now,
					"updated_at":  nextDatabaseTimestamp(),
				})
			if result.Error != nil {
				return mapGormError(result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrConflict
			}

			asset, err := record.toDomain()
			if err != nil {
				return err
			}
			attached = append(attached, asset)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return attached, nil
}

func (r *GormImageAssetRepository) BeginImageAssetDeletion(
	ctx context.Context,
	ownerID string,
	assetID string,
) (ImageAsset, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(assetID) {
		return ImageAsset{}, ErrNotFound
	}

	var record imageAssetRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Where(
			`(
				status IN ? OR (
					status = ?
					AND (
						upload_lease_until IS NULL
						OR upload_lease_until <= transaction_timestamp()
					)
				)
			)`,
			[]string{
				string(ImageAssetAttached),
				string(ImageAssetDeleting),
				string(ImageAssetDeleted),
			},
			string(ImageAssetStaged),
		).
		Updates(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN status = ? THEN status ELSE ? END",
				string(ImageAssetDeleted),
				string(ImageAssetDeleting),
			),
			"upload_lease_until":  nil,
			"cleanup_lease_until": nil,
			"updated_at":          nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return ImageAsset{}, mapGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ImageAsset{}, ErrNotFound
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) FinishImageAssetDeletion(
	ctx context.Context,
	ownerID string,
	assetID string,
) (ImageAsset, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(assetID) {
		return ImageAsset{}, ErrNotFound
	}

	var record imageAssetRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Where(
			"status IN ?",
			[]string{string(ImageAssetDeleting), string(ImageAssetDeleted)},
		).
		Updates(map[string]any{
			"status":              string(ImageAssetDeleted),
			"cleanup_lease_until": nil,
			"deleted_at": gorm.Expr(
				"COALESCE(deleted_at, transaction_timestamp())",
			),
			"updated_at": nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return ImageAsset{}, mapGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ImageAsset{}, ErrNotFound
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) ClaimImageCleanup(
	ctx context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]ImageCleanupClaim, error) {
	if ctx == nil || leaseDuration < time.Second ||
		leaseDuration > 10*time.Minute || limit < 1 || limit > 100 {
		return nil, ErrInvalidRequest
	}

	claims := make([]ImageCleanupClaim, 0, limit)
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ownerDeletion := tx.
			Where("owner.account_status IN ?", []string{"deleting", "deleted"}).
			Where(
				"asset.status IN ?",
				[]string{
					string(ImageAssetStaged),
					string(ImageAssetAttached),
					string(ImageAssetDeleting),
				},
			).
			Where(
				"(asset.upload_lease_until IS NULL OR asset.upload_lease_until <= transaction_timestamp())",
			).
			Where(
				"(asset.cleanup_lease_until IS NULL OR asset.cleanup_lease_until <= transaction_timestamp())",
			)
		expiredStage := tx.
			Where("asset.status = ?", string(ImageAssetStaged)).
			Where("asset.expires_at <= transaction_timestamp()").
			Where(
				"(asset.upload_lease_until IS NULL OR asset.upload_lease_until <= transaction_timestamp())",
			)
		retryDeletion := tx.
			Where("asset.status = ?", string(ImageAssetDeleting)).
			Where(
				"(asset.cleanup_lease_until IS NULL OR asset.cleanup_lease_until <= transaction_timestamp())",
			)
		activeCleanup := tx.
			Where("owner.account_status = ?", "active").
			Where(expiredStage.Or(retryDeletion))

		var candidates []imageAssetRecord
		err := tx.
			Table("agent_image_assets AS asset").
			Select("asset.*").
			Joins("JOIN identity_users AS owner ON owner.id = asset.owner_user_id").
			Where(ownerDeletion.Or(activeCleanup)).
			Order("asset.expires_at, asset.image_asset_id").
			Limit(limit).
			Clauses(clause.Locking{
				Strength: "UPDATE",
				Table:    clause.Table{Name: "asset"},
				Options:  "SKIP LOCKED",
			}).
			Find(&candidates).Error
		if err != nil {
			return mapGormError(err)
		}

		leaseUntil := time.Now().UTC().Add(leaseDuration)
		for _, candidate := range candidates {
			var claimed imageAssetRecord
			result := tx.
				Model(&claimed).
				Clauses(clause.Returning{}).
				Where("image_asset_id = ?", candidate.ID).
				Updates(map[string]any{
					"status":              string(ImageAssetDeleting),
					"upload_lease_until":  nil,
					"cleanup_lease_until": leaseUntil,
					"cleanup_fencing_token": gorm.Expr(
						"cleanup_fencing_token + 1",
					),
					"updated_at": nextDatabaseTimestamp(),
				})
			if result.Error != nil {
				return mapGormError(result.Error)
			}
			if result.RowsAffected != 1 ||
				claimed.CleanupFencingToken <= 0 {
				return ErrRepository
			}
			claims = append(claims, ImageCleanupClaim{
				AssetID:      claimed.ID,
				OwnerID:      claimed.OwnerID,
				ObjectKey:    claimed.ObjectKey,
				FencingToken: uint64(claimed.CleanupFencingToken),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (r *GormImageAssetRepository) FinishImageCleanup(
	ctx context.Context,
	claim ImageCleanupClaim,
) error {
	if ctx == nil || !validImageCleanupClaim(claim) {
		return ErrInvalidRequest
	}

	result := r.database.WithContext(ctx).
		Model(&imageAssetRecord{}).
		Where(
			`image_asset_id = ?
				AND owner_user_id = ?
				AND object_key = ?
				AND status = ?
				AND cleanup_fencing_token = ?`,
			claim.AssetID,
			claim.OwnerID,
			claim.ObjectKey,
			string(ImageAssetDeleting),
			int64(claim.FencingToken),
		).
		Updates(map[string]any{
			"status":              string(ImageAssetDeleted),
			"cleanup_lease_until": nil,
			"deleted_at": gorm.Expr(
				"COALESCE(deleted_at, transaction_timestamp())",
			),
			"updated_at": nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return mapGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *GormImageAssetRepository) ReleaseImageCleanup(
	ctx context.Context,
	claim ImageCleanupClaim,
) error {
	if ctx == nil || !validImageCleanupClaim(claim) {
		return ErrInvalidRequest
	}

	result := r.database.WithContext(ctx).
		Model(&imageAssetRecord{}).
		Where(
			`image_asset_id = ?
				AND owner_user_id = ?
				AND object_key = ?
				AND status = ?
				AND cleanup_fencing_token = ?`,
			claim.AssetID,
			claim.OwnerID,
			claim.ObjectKey,
			string(ImageAssetDeleting),
			int64(claim.FencingToken),
		).
		Updates(map[string]any{
			"cleanup_lease_until": nil,
			"updated_at":          nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return mapGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func imageAssetToRecord(asset ImageAsset) imageAssetRecord {
	record := imageAssetRecord{
		ID:                 asset.ID,
		OwnerID:            asset.OwnerID,
		ThreadID:           asset.ThreadID,
		UploadRequestID:    asset.UploadRequestID,
		ObjectKey:          asset.ObjectKey,
		ContentType:        asset.ContentType,
		Size:               asset.Size,
		Width:              asset.Width,
		Height:             asset.Height,
		ChecksumSHA256:     asset.ChecksumSHA256,
		ETag:               asset.ETag,
		UploadFencingToken: int64(asset.UploadFencingToken),
		Status:             string(asset.Status),
		ExpiresAt:          asset.ExpiresAt.UTC(),
		CreatedAt:          asset.CreatedAt.UTC(),
		UpdatedAt:          asset.UpdatedAt.UTC(),
	}
	record.UploadLeaseUntil = timePointer(asset.UploadLeaseUntil)
	record.AttachedAt = timePointer(asset.AttachedAt)
	record.DeletedAt = timePointer(asset.DeletedAt)
	return record
}

func (record imageAssetRecord) toDomain() (ImageAsset, error) {
	if record.UploadFencingToken < 0 || record.CleanupFencingToken < 0 {
		return ImageAsset{}, ErrRepository
	}
	asset := ImageAsset{
		ID:                 record.ID,
		OwnerID:            record.OwnerID,
		ThreadID:           record.ThreadID,
		UploadRequestID:    record.UploadRequestID,
		ObjectKey:          record.ObjectKey,
		ContentType:        record.ContentType,
		Size:               record.Size,
		Width:              record.Width,
		Height:             record.Height,
		ChecksumSHA256:     record.ChecksumSHA256,
		ETag:               record.ETag,
		UploadFencingToken: uint64(record.UploadFencingToken),
		Status:             ImageAssetStatus(record.Status),
		ExpiresAt:          record.ExpiresAt.UTC(),
		CreatedAt:          record.CreatedAt.UTC(),
		UpdatedAt:          record.UpdatedAt.UTC(),
	}
	if record.UploadLeaseUntil != nil {
		asset.UploadLeaseUntil = record.UploadLeaseUntil.UTC()
	}
	if record.AttachedAt != nil {
		asset.AttachedAt = record.AttachedAt.UTC()
	}
	if record.DeletedAt != nil {
		asset.DeletedAt = record.DeletedAt.UTC()
	}
	return asset, nil
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func nextDatabaseTimestamp() clause.Expr {
	return gorm.Expr(
		"GREATEST(transaction_timestamp(), updated_at + interval '1 microsecond')",
	)
}

func mapGormError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return mapPostgresError(err)
}

func validNewImageAsset(asset ImageAsset) bool {
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
		strings.HasPrefix(asset.ObjectKey, AgentImageObjectPrefix) &&
		!strings.Contains(asset.ObjectKey, "..") &&
		suffixValid &&
		asset.Size > 0 &&
		asset.Size <= MaxImageBytes &&
		asset.Width > 0 &&
		asset.Height > 0 &&
		int64(asset.Width)*int64(asset.Height) <= MaxImagePixels &&
		imageChecksumPattern.MatchString(asset.ChecksumSHA256) &&
		asset.ETag == "" &&
		asset.Status == ImageAssetStaged &&
		asset.ExpiresAt.After(asset.CreatedAt) &&
		asset.ExpiresAt.Sub(asset.CreatedAt) <= 7*24*time.Hour
}

func validImageAssetIDs(assetIDs []string) bool {
	if len(assetIDs) < 1 || len(assetIDs) > MaxImagesPerMessage {
		return false
	}
	seen := make(map[string]struct{}, len(assetIDs))
	for _, id := range assetIDs {
		if !ValidUUID(id) {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func validImageCleanupClaim(claim ImageCleanupClaim) bool {
	return ValidUUID(claim.AssetID) &&
		ValidUUID(claim.OwnerID) &&
		strings.HasPrefix(claim.ObjectKey, AgentImageObjectPrefix) &&
		!strings.Contains(claim.ObjectKey, "..") &&
		claim.FencingToken > 0 &&
		claim.FencingToken <= uint64(1<<63-1)
}

var _ ImageAssetRepository = (*GormImageAssetRepository)(nil)
