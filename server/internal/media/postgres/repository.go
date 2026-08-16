package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type database interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type rowScanner interface {
	Scan(...any) error
}

type Repository struct {
	database database
}

func New(database database) (*Repository, error) {
	if database == nil {
		return nil, media.ErrRepository
	}
	return &Repository{database: database}, nil
}

const assetColumns = `
    asset.id::text,
    asset.user_id::text,
    asset.kind,
    asset.upload_request_id,
    asset.object_key,
    asset.content_type,
    asset.size_bytes,
    asset.checksum_sha256,
    asset.etag,
    asset.width,
    asset.height,
    asset.duration_ns,
    asset.sample_rate,
    asset.status,
    asset.upload_fencing_token,
    asset.upload_lease_until,
    asset.expires_at,
    asset.cleanup_attempt_count,
    asset.cleanup_fencing_token,
    asset.cleanup_lease_until,
    asset.cleanup_available_at,
    COALESCE(asset.cleanup_error, ''),
    asset.created_at,
    asset.updated_at`

func (repository *Repository) Stage(
	ctx context.Context,
	asset media.Asset,
) (media.Stage, error) {
	if ctx == nil || !validNewAsset(asset) {
		return media.Stage{}, media.ErrInvalidRequest
	}
	inserted, err := scanAsset(repository.database.QueryRow(ctx, `
INSERT INTO media_assets AS asset (
    id,
    user_id,
    kind,
    upload_request_id,
    object_key,
    content_type,
    size_bytes,
    checksum_sha256,
    width,
    height,
    duration_ns,
    sample_rate,
    status,
    expires_at,
    created_at,
    updated_at
)
SELECT
    $1, owner.id, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, 'staged', $13, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM users AS owner
WHERE owner.id = $2 AND owner.status = 'active'
ON CONFLICT (user_id, kind, upload_request_id) DO NOTHING
RETURNING `+assetColumns,
		asset.ID,
		asset.UserID,
		asset.Kind,
		asset.UploadRequestID,
		asset.ObjectKey,
		asset.ContentType,
		asset.Size,
		asset.ChecksumSHA256,
		nullableInt(asset.Width),
		nullableInt(asset.Height),
		nullableDuration(asset.Duration),
		nullableInt(asset.SampleRate),
		asset.ExpiresAt,
	))
	if err == nil {
		return media.Stage{Asset: inserted, Created: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return media.Stage{}, mapError(err)
	}
	existing, err := scanAsset(repository.database.QueryRow(ctx, `
SELECT `+assetColumns+`
FROM media_assets AS asset
JOIN users AS owner ON owner.id = asset.user_id
WHERE asset.user_id = $1
  AND asset.kind = $2
  AND asset.upload_request_id = $3
  AND owner.status = 'active'`,
		asset.UserID,
		asset.Kind,
		asset.UploadRequestID,
	))
	if err != nil {
		return media.Stage{}, mapError(err)
	}
	return media.Stage{Asset: existing}, nil
}

func (repository *Repository) ClaimUpload(
	ctx context.Context,
	userID string,
	assetID string,
	leaseDuration time.Duration,
) (media.UploadClaim, bool, error) {
	if ctx == nil || !media.ValidUUID(userID) || !media.ValidUUID(assetID) ||
		leaseDuration <= 0 {
		return media.UploadClaim{}, false, media.ErrInvalidRequest
	}
	asset, err := scanAsset(repository.database.QueryRow(ctx, `
UPDATE media_assets AS asset
SET
    upload_fencing_token = upload_fencing_token + 1,
    upload_lease_until = CURRENT_TIMESTAMP + $3::interval,
    updated_at = CURRENT_TIMESTAMP
WHERE asset.id = $1
  AND asset.user_id = $2
  AND asset.status = 'staged'
  AND (asset.upload_lease_until IS NULL OR asset.upload_lease_until <= CURRENT_TIMESTAMP)
  AND EXISTS (
      SELECT 1 FROM users AS owner
      WHERE owner.id = asset.user_id AND owner.status = 'active'
  )
RETURNING `+assetColumns,
		assetID,
		userID,
		leaseDuration,
	))
	if err == nil {
		return media.UploadClaim{
			Asset: asset, FencingToken: asset.UploadFencingToken,
			LeaseExpiresAt: asset.UploadLeaseUntil,
		}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return media.UploadClaim{}, false, mapError(err)
	}
	asset, err = repository.FindOwned(ctx, userID, assetID)
	if err != nil {
		return media.UploadClaim{}, false, err
	}
	return media.UploadClaim{Asset: asset}, false, nil
}

func (repository *Repository) CommitUpload(
	ctx context.Context,
	userID string,
	assetID string,
	fencingToken uint64,
	etag string,
) (media.Asset, error) {
	if ctx == nil || !media.ValidUUID(userID) || !media.ValidUUID(assetID) ||
		fencingToken == 0 || fencingToken > uint64(1<<63-1) ||
		len(etag) < 1 || len(etag) > 512 {
		return media.Asset{}, media.ErrInvalidRequest
	}
	asset, err := scanAsset(repository.database.QueryRow(ctx, `
UPDATE media_assets AS asset
SET
    status = 'ready',
    etag = $4,
    upload_lease_until = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE asset.id = $1
  AND asset.user_id = $2
  AND asset.status = 'staged'
  AND asset.upload_fencing_token = $3
  AND asset.upload_lease_until > CURRENT_TIMESTAMP
RETURNING `+assetColumns,
		assetID,
		userID,
		int64(fencingToken),
		etag,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return media.Asset{}, media.ErrConflict
	}
	if err != nil {
		return media.Asset{}, mapError(err)
	}
	return asset, nil
}

func (repository *Repository) FindOwned(
	ctx context.Context,
	userID string,
	assetID string,
) (media.Asset, error) {
	if ctx == nil || !media.ValidUUID(userID) || !media.ValidUUID(assetID) {
		return media.Asset{}, media.ErrNotFound
	}
	asset, err := scanAsset(repository.database.QueryRow(ctx, `
SELECT `+assetColumns+`
FROM media_assets AS asset
WHERE asset.id = $1 AND asset.user_id = $2`,
		assetID,
		userID,
	))
	if err != nil {
		return media.Asset{}, mapError(err)
	}
	return asset, nil
}

func (repository *Repository) BeginDeletion(
	ctx context.Context,
	userID string,
	assetID string,
	leaseDuration time.Duration,
) (media.Asset, error) {
	if ctx == nil || !media.ValidUUID(userID) || !media.ValidUUID(assetID) ||
		leaseDuration <= 0 {
		return media.Asset{}, media.ErrNotFound
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return media.Asset{}, media.ErrRepository
	}
	defer rollback(tx)
	var activeUser string
	if err := tx.QueryRow(ctx, `
SELECT id::text
FROM users
WHERE id = $1 AND status = 'active'
FOR UPDATE`, userID).Scan(&activeUser); err != nil {
		return media.Asset{}, mapError(err)
	}
	asset, err := scanAsset(tx.QueryRow(ctx, `
SELECT `+assetColumns+`
FROM media_assets AS asset
WHERE asset.id = $1 AND asset.user_id = $2
FOR UPDATE`, assetID, userID))
	if err != nil {
		return media.Asset{}, mapError(err)
	}
	var referenced bool
	if err := tx.QueryRow(ctx, `
SELECT
    EXISTS (
        SELECT 1 FROM agent_message_attachments
        WHERE asset_id = $1
	    ) OR EXISTS (
	        SELECT 1 FROM agent_voice_drafts
	        WHERE id = $1
	    ) OR EXISTS (
	        SELECT 1 FROM practice_turns
	        WHERE audio_asset_id = $1
	    )`, assetID).Scan(&referenced); err != nil {
		return media.Asset{}, media.ErrRepository
	}
	if referenced {
		return media.Asset{}, media.ErrConflict
	}
	asset, err = scanAsset(tx.QueryRow(ctx, `
UPDATE media_assets AS asset
SET
    status = 'deleting',
    upload_lease_until = NULL,
    expires_at = NULL,
    cleanup_attempt_count = cleanup_attempt_count + 1,
    cleanup_fencing_token = cleanup_fencing_token + 1,
    cleanup_lease_until = CURRENT_TIMESTAMP + $3::interval,
    cleanup_available_at = CURRENT_TIMESTAMP,
    cleanup_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE asset.id = $1 AND asset.user_id = $2
  AND (
      asset.upload_lease_until IS NULL
      OR asset.upload_lease_until <= CURRENT_TIMESTAMP
  )
  AND (
      asset.status <> 'deleting'
      OR asset.cleanup_lease_until IS NULL
      OR asset.cleanup_lease_until <= CURRENT_TIMESTAMP
  )
RETURNING `+assetColumns, assetID, userID, leaseDuration))
	if errors.Is(err, pgx.ErrNoRows) {
		return media.Asset{}, media.ErrConflict
	}
	if err != nil {
		return media.Asset{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return media.Asset{}, media.ErrRepository
	}
	return asset, nil
}

func (repository *Repository) ClaimCleanup(
	ctx context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]media.CleanupClaim, error) {
	if ctx == nil || leaseDuration <= 0 || limit < 1 || limit > 100 {
		return nil, media.ErrInvalidRequest
	}
	rows, err := repository.database.Query(ctx, `
WITH eligible AS (
    SELECT asset.id
    FROM media_assets AS asset
    JOIN users AS owner ON owner.id = asset.user_id
    WHERE asset.cleanup_available_at <= CURRENT_TIMESTAMP
	      AND (asset.upload_lease_until IS NULL OR asset.upload_lease_until <= CURRENT_TIMESTAMP)
	      AND NOT EXISTS (
	          SELECT 1 FROM agent_message_attachments
	          WHERE asset_id = asset.id
	      )
	      AND NOT EXISTS (
	          SELECT 1 FROM agent_voice_drafts
	          WHERE id = asset.id
	      )
	      AND NOT EXISTS (
	          SELECT 1 FROM practice_turns
	          WHERE audio_asset_id = asset.id
	      )
	      AND (
          owner.status = 'deleting'
          OR (
              asset.status = 'deleting'
              AND (
                  asset.cleanup_lease_until IS NULL
                  OR asset.cleanup_lease_until <= CURRENT_TIMESTAMP
              )
          )
          OR (
              asset.status IN ('staged', 'ready')
              AND asset.expires_at <= CURRENT_TIMESTAMP
          )
      )
    ORDER BY
        CASE WHEN owner.status = 'deleting' THEN 0 ELSE 1 END,
        asset.cleanup_available_at,
        asset.expires_at,
        asset.id
    FOR UPDATE OF asset SKIP LOCKED
    LIMIT $1
)
UPDATE media_assets AS asset
SET
    status = 'deleting',
    upload_lease_until = NULL,
    expires_at = NULL,
    cleanup_attempt_count = cleanup_attempt_count + 1,
    cleanup_fencing_token = cleanup_fencing_token + 1,
    cleanup_lease_until = CURRENT_TIMESTAMP + $2::interval,
    cleanup_error = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM eligible
WHERE asset.id = eligible.id
RETURNING
    asset.id::text,
    asset.kind,
    asset.object_key,
    asset.cleanup_fencing_token,
    asset.cleanup_lease_until`,
		limit,
		leaseDuration,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	claims := make([]media.CleanupClaim, 0, limit)
	for rows.Next() {
		var claim media.CleanupClaim
		if err := rows.Scan(
			&claim.AssetID,
			&claim.Kind,
			&claim.ObjectKey,
			&claim.FencingToken,
			&claim.LeaseExpiresAt,
		); err != nil {
			return nil, media.ErrRepository
		}
		claims = append(claims, claim)
	}
	if rows.Err() != nil {
		return nil, media.ErrRepository
	}
	return claims, nil
}

func (repository *Repository) FinishCleanup(
	ctx context.Context,
	claim media.CleanupClaim,
) error {
	if !validClaim(claim) {
		return media.ErrInvalidRequest
	}
	tag, err := repository.database.Exec(ctx, `
DELETE FROM media_assets
WHERE id = $1
  AND status = 'deleting'
  AND cleanup_fencing_token = $2`,
		claim.AssetID,
		int64(claim.FencingToken),
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return media.ErrConflict
	}
	return nil
}

func (repository *Repository) ReleaseCleanup(
	ctx context.Context,
	claim media.CleanupClaim,
	errorCode string,
) error {
	if !validClaim(claim) || errorCode != "object_delete_failed" {
		return media.ErrInvalidRequest
	}
	tag, err := repository.database.Exec(ctx, `
UPDATE media_assets
SET
    cleanup_lease_until = NULL,
    cleanup_available_at = CURRENT_TIMESTAMP + (
        LEAST(300, power(2, LEAST(cleanup_attempt_count, 8))::integer)
        * interval '1 second'
    ),
    cleanup_error = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND status = 'deleting'
  AND cleanup_fencing_token = $2`,
		claim.AssetID,
		int64(claim.FencingToken),
		errorCode,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return media.ErrConflict
	}
	return nil
}

func validNewAsset(asset media.Asset) bool {
	return media.ValidUUID(asset.ID) && media.ValidUUID(asset.UserID) &&
		media.ValidIdempotencyKey(asset.UploadRequestID) &&
		media.ValidChecksum(asset.ChecksumSHA256) && asset.Status == media.StatusStaged &&
		asset.Size > 0 && asset.ExpiresAt.After(asset.CreatedAt)
}

func validClaim(claim media.CleanupClaim) bool {
	return media.ValidUUID(claim.AssetID) &&
		(claim.Kind == media.KindImage || claim.Kind == media.KindAudio ||
			claim.Kind == media.KindDocument) &&
		claim.ObjectKey != "" && claim.FencingToken > 0 &&
		claim.FencingToken <= uint64(1<<63-1)
}

func scanAsset(row rowScanner) (media.Asset, error) {
	var asset media.Asset
	var kind string
	var status string
	var width pgtype.Int4
	var height pgtype.Int4
	var duration pgtype.Int8
	var sampleRate pgtype.Int4
	var uploadLease pgtype.Timestamptz
	var expiresAt pgtype.Timestamptz
	var cleanupLease pgtype.Timestamptz
	if err := row.Scan(
		&asset.ID,
		&asset.UserID,
		&kind,
		&asset.UploadRequestID,
		&asset.ObjectKey,
		&asset.ContentType,
		&asset.Size,
		&asset.ChecksumSHA256,
		&asset.ETag,
		&width,
		&height,
		&duration,
		&sampleRate,
		&status,
		&asset.UploadFencingToken,
		&uploadLease,
		&expiresAt,
		&asset.CleanupAttempts,
		&asset.CleanupFencingToken,
		&cleanupLease,
		&asset.CleanupAvailableAt,
		&asset.CleanupError,
		&asset.CreatedAt,
		&asset.UpdatedAt,
	); err != nil {
		return media.Asset{}, err
	}
	asset.Kind = media.Kind(kind)
	asset.Status = media.Status(status)
	if width.Valid {
		asset.Width = int(width.Int32)
	}
	if height.Valid {
		asset.Height = int(height.Int32)
	}
	if duration.Valid {
		asset.Duration = time.Duration(duration.Int64)
	}
	if sampleRate.Valid {
		asset.SampleRate = int(sampleRate.Int32)
	}
	if uploadLease.Valid {
		asset.UploadLeaseUntil = uploadLease.Time
	}
	if expiresAt.Valid {
		asset.ExpiresAt = expiresAt.Time
	}
	if cleanupLease.Valid {
		asset.CleanupLeaseUntil = cleanupLease.Time
	}
	return asset, nil
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableDuration(value time.Duration) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return media.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return media.ErrNotFound
		case "23514", "22P02", "22003":
			return media.ErrInvalidRequest
		}
	}
	return media.ErrRepository
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

var _ media.Repository = (*Repository)(nil)
