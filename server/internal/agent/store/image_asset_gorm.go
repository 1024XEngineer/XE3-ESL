package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// GormImageAssetRepository keeps image asset transactions on GORM while the
// other Agent records use the process-owned pgx pool.
type GormImageAssetRepository struct {
	database *gorm.DB
}

func NewGormImageAssetRepository(
	database *gorm.DB,
) (*GormImageAssetRepository, error) {
	if database == nil {
		return nil, agentimage.ErrRepository
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
		return nil, agentimage.ErrRepository
	}
	return NewGormImageAssetRepository(database)
}

func newGormDatabaseFromPool(pool *pgxpool.Pool) (*gorm.DB, error) {
	if pool == nil {
		return nil, errors.New("agent store: database pool is required")
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
		return nil, err
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

func (r *GormImageAssetRepository) StageAsset(
	ctx context.Context,
	asset agentimage.Asset,
) (agentimage.AssetStage, error) {
	if ctx == nil || !asset.ValidNew() {
		return agentimage.AssetStage{}, agentimage.ErrInvalidRequest
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
		return agentimage.AssetStage{}, mapImageGormError(result.Error)
	}
	if result.RowsAffected == 1 {
		created, err := record.toDomain()
		return agentimage.AssetStage{Asset: created, Created: true}, err
	}

	existing, err := r.findImageAssetByUpload(
		ctx,
		asset.OwnerID,
		asset.ThreadID,
		asset.UploadRequestID,
	)
	if err != nil {
		return agentimage.AssetStage{}, err
	}
	return agentimage.AssetStage{Asset: existing, Created: false}, nil
}

func (r *GormImageAssetRepository) ClaimUpload(
	ctx context.Context,
	ownerID string,
	assetID string,
	leaseDuration time.Duration,
) (agentimage.UploadClaim, bool, error) {
	if ctx == nil || !agentimage.ValidUUID(ownerID) || !agentimage.ValidUUID(assetID) ||
		leaseDuration < time.Second || leaseDuration > 10*time.Minute {
		return agentimage.UploadClaim{}, false, agentimage.ErrInvalidRequest
	}

	var record imageAssetRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Where("status = ? AND etag = ''", string(agentimage.StatusStaged)).
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
		return agentimage.UploadClaim{}, false, mapImageGormError(result.Error)
	}
	if result.RowsAffected == 1 {
		asset, err := record.toDomain()
		if err != nil {
			return agentimage.UploadClaim{}, false, err
		}
		return agentimage.UploadClaim{
			Asset:          asset,
			FencingToken:   asset.UploadFencingToken,
			LeaseExpiresAt: asset.UploadLeaseUntil,
		}, true, nil
	}

	current, err := r.FindAsset(ctx, ownerID, assetID)
	if err != nil {
		return agentimage.UploadClaim{}, false, err
	}
	return agentimage.UploadClaim{
		Asset:          current,
		FencingToken:   current.UploadFencingToken,
		LeaseExpiresAt: current.UploadLeaseUntil,
	}, false, nil
}

func (r *GormImageAssetRepository) CommitUpload(
	ctx context.Context,
	ownerID string,
	assetID string,
	fencingToken uint64,
	etag string,
) (agentimage.Asset, error) {
	etag = strings.TrimSpace(etag)
	if ctx == nil || !agentimage.ValidUUID(ownerID) || !agentimage.ValidUUID(assetID) ||
		fencingToken == 0 || fencingToken > uint64(1<<63-1) ||
		etag == "" || len(etag) > 512 {
		return agentimage.Asset{}, agentimage.ErrInvalidRequest
	}

	var record imageAssetRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Where("status = ? AND etag = ''", string(agentimage.StatusStaged)).
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
		return agentimage.Asset{}, mapImageGormError(result.Error)
	}
	if result.RowsAffected == 1 {
		return record.toDomain()
	}

	current, err := r.FindAsset(ctx, ownerID, assetID)
	if err != nil {
		return agentimage.Asset{}, err
	}
	if current.ETag == etag &&
		current.UploadFencingToken == fencingToken {
		return current, nil
	}
	return agentimage.Asset{}, agentimage.ErrConflict
}

func (r *GormImageAssetRepository) FindAsset(
	ctx context.Context,
	ownerID string,
	assetID string,
) (agentimage.Asset, error) {
	if ctx == nil || !agentimage.ValidUUID(ownerID) || !agentimage.ValidUUID(assetID) {
		return agentimage.Asset{}, agentimage.ErrNotFound
	}

	var record imageAssetRecord
	err := r.database.WithContext(ctx).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Take(&record).Error
	if err != nil {
		return agentimage.Asset{}, mapImageGormError(err)
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) findImageAssetByUpload(
	ctx context.Context,
	ownerID string,
	threadID string,
	uploadRequestID string,
) (agentimage.Asset, error) {
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
		return agentimage.Asset{}, mapImageGormError(err)
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) ListMessageAssets(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
) ([]agentimage.Asset, error) {
	if ctx == nil || !agentimage.ValidUUID(ownerID) || !agentimage.ValidUUID(threadID) ||
		!agentimage.ValidUUID(messageID) {
		return nil, agentimage.ErrNotFound
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
		return nil, mapImageGormError(err)
	}
	if len(records) == 0 {
		return nil, agentimage.ErrNotFound
	}
	assets := make([]agentimage.Asset, 0, len(records))
	for _, record := range records {
		asset, err := record.toDomain()
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (r *GormImageAssetRepository) AttachAssets(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
	assetIDs []string,
) ([]agentimage.Asset, error) {
	if ctx == nil || !agentimage.ValidUUID(ownerID) || !agentimage.ValidUUID(threadID) ||
		!agentimage.ValidUUID(messageID) || !agentimage.ValidAssetIDs(assetIDs) {
		return nil, agentimage.ErrInvalidRequest
	}

	var attached []agentimage.Asset
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
			return mapImageGormError(err)
		}
		if message.Role != string(conversation.MessageRoleUser) {
			return agentimage.ErrConflict
		}

		var records []imageAssetRecord
		err = tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_user_id = ? AND thread_id = ?", ownerID, threadID).
			Where("image_asset_id IN ?", assetIDs).
			Order("image_asset_id").
			Find(&records).Error
		if err != nil {
			return mapImageGormError(err)
		}
		if len(records) != len(assetIDs) {
			return agentimage.ErrNotFound
		}

		byID := make(map[string]imageAssetRecord, len(records))
		for _, record := range records {
			byID[record.ID] = record
		}
		now := time.Now().UTC()
		attached = make([]agentimage.Asset, 0, len(assetIDs))
		for position, assetID := range assetIDs {
			record := byID[assetID]
			if record.Status != string(agentimage.StatusStaged) ||
				record.ETag == "" ||
				!record.ExpiresAt.After(now) {
				return agentimage.ErrConflict
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
				return mapImageGormError(err)
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
				Where("status = ? AND etag <> ''", string(agentimage.StatusStaged)).
				Updates(map[string]any{
					"status":      string(agentimage.StatusAttached),
					"attached_at": now,
					"updated_at":  nextDatabaseTimestamp(),
				})
			if result.Error != nil {
				return mapImageGormError(result.Error)
			}
			if result.RowsAffected != 1 {
				return agentimage.ErrConflict
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

func (r *GormImageAssetRepository) BeginDeletion(
	ctx context.Context,
	ownerID string,
	assetID string,
) (agentimage.Asset, error) {
	if ctx == nil || !agentimage.ValidUUID(ownerID) || !agentimage.ValidUUID(assetID) {
		return agentimage.Asset{}, agentimage.ErrNotFound
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
				string(agentimage.StatusAttached),
				string(agentimage.StatusDeleting),
				string(agentimage.StatusDeleted),
			},
			string(agentimage.StatusStaged),
		).
		Updates(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN status = ? THEN status ELSE ? END",
				string(agentimage.StatusDeleted),
				string(agentimage.StatusDeleting),
			),
			"upload_lease_until":  nil,
			"cleanup_lease_until": nil,
			"updated_at":          nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return agentimage.Asset{}, mapImageGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return agentimage.Asset{}, agentimage.ErrNotFound
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) FinishDeletion(
	ctx context.Context,
	ownerID string,
	assetID string,
) (agentimage.Asset, error) {
	if ctx == nil || !agentimage.ValidUUID(ownerID) || !agentimage.ValidUUID(assetID) {
		return agentimage.Asset{}, agentimage.ErrNotFound
	}

	var record imageAssetRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where("image_asset_id = ? AND owner_user_id = ?", assetID, ownerID).
		Where(
			"status IN ?",
			[]string{string(agentimage.StatusDeleting), string(agentimage.StatusDeleted)},
		).
		Updates(map[string]any{
			"status":              string(agentimage.StatusDeleted),
			"cleanup_lease_until": nil,
			"deleted_at": gorm.Expr(
				"COALESCE(deleted_at, transaction_timestamp())",
			),
			"updated_at": nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return agentimage.Asset{}, mapImageGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return agentimage.Asset{}, agentimage.ErrNotFound
	}
	return record.toDomain()
}

func (r *GormImageAssetRepository) ClaimCleanup(
	ctx context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]agentimage.CleanupClaim, error) {
	if ctx == nil || leaseDuration < time.Second ||
		leaseDuration > 10*time.Minute || limit < 1 || limit > 100 {
		return nil, agentimage.ErrInvalidRequest
	}

	claims := make([]agentimage.CleanupClaim, 0, limit)
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ownerDeletion := tx.
			Where("owner.account_status IN ?", []string{"deleting", "deleted"}).
			Where(
				"asset.status IN ?",
				[]string{
					string(agentimage.StatusStaged),
					string(agentimage.StatusAttached),
					string(agentimage.StatusDeleting),
				},
			).
			Where(
				"(asset.upload_lease_until IS NULL OR asset.upload_lease_until <= transaction_timestamp())",
			).
			Where(
				"(asset.cleanup_lease_until IS NULL OR asset.cleanup_lease_until <= transaction_timestamp())",
			)
		expiredStage := tx.
			Where("asset.status = ?", string(agentimage.StatusStaged)).
			Where("asset.expires_at <= transaction_timestamp()").
			Where(
				"(asset.upload_lease_until IS NULL OR asset.upload_lease_until <= transaction_timestamp())",
			)
		retryDeletion := tx.
			Where("asset.status = ?", string(agentimage.StatusDeleting)).
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
			return mapImageGormError(err)
		}

		leaseUntil := time.Now().UTC().Add(leaseDuration)
		for _, candidate := range candidates {
			var claimed imageAssetRecord
			result := tx.
				Model(&claimed).
				Clauses(clause.Returning{}).
				Where("image_asset_id = ?", candidate.ID).
				Updates(map[string]any{
					"status":              string(agentimage.StatusDeleting),
					"upload_lease_until":  nil,
					"cleanup_lease_until": leaseUntil,
					"cleanup_fencing_token": gorm.Expr(
						"cleanup_fencing_token + 1",
					),
					"updated_at": nextDatabaseTimestamp(),
				})
			if result.Error != nil {
				return mapImageGormError(result.Error)
			}
			if result.RowsAffected != 1 ||
				claimed.CleanupFencingToken <= 0 {
				return agentimage.ErrRepository
			}
			claims = append(claims, agentimage.CleanupClaim{
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

func (r *GormImageAssetRepository) FinishCleanup(
	ctx context.Context,
	claim agentimage.CleanupClaim,
) error {
	if ctx == nil || !claim.Valid() {
		return agentimage.ErrInvalidRequest
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
			string(agentimage.StatusDeleting),
			int64(claim.FencingToken),
		).
		Updates(map[string]any{
			"status":              string(agentimage.StatusDeleted),
			"cleanup_lease_until": nil,
			"deleted_at": gorm.Expr(
				"COALESCE(deleted_at, transaction_timestamp())",
			),
			"updated_at": nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return mapImageGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return agentimage.ErrConflict
	}
	return nil
}

func (r *GormImageAssetRepository) ReleaseCleanup(
	ctx context.Context,
	claim agentimage.CleanupClaim,
) error {
	if ctx == nil || !claim.Valid() {
		return agentimage.ErrInvalidRequest
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
			string(agentimage.StatusDeleting),
			int64(claim.FencingToken),
		).
		Updates(map[string]any{
			"cleanup_lease_until": nil,
			"updated_at":          nextDatabaseTimestamp(),
		})
	if result.Error != nil {
		return mapImageGormError(result.Error)
	}
	if result.RowsAffected != 1 {
		return agentimage.ErrConflict
	}
	return nil
}

func imageAssetToRecord(asset agentimage.Asset) imageAssetRecord {
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

func (record imageAssetRecord) toDomain() (agentimage.Asset, error) {
	if record.UploadFencingToken < 0 || record.CleanupFencingToken < 0 {
		return agentimage.Asset{}, agentimage.ErrRepository
	}
	asset := agentimage.Asset{
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
		Status:             agentimage.AssetStatus(record.Status),
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

func mapImageGormError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentimage.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return agentimage.ErrNotFound
		case "23505":
			return agentimage.ErrConflict
		case "23514":
			return agentimage.ErrInvalidRequest
		}
	}
	return agentimage.ErrRepository
}

var _ agentimage.Repository = (*GormImageAssetRepository)(nil)
