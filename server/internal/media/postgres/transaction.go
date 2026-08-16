package postgres

import (
	"context"
	"slices"

	"github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/jackc/pgx/v5"
)

// LockAttachableInTransaction validates and locks assets in stable ID order.
// Callers own the surrounding Message transaction; media keeps asset state SQL.
func LockAttachableInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	kind media.Kind,
	assetIDs []string,
) error {
	if ctx == nil || tx == nil || !media.ValidUUID(userID) ||
		(kind != media.KindImage && kind != media.KindAudio &&
			kind != media.KindDocument) ||
		len(assetIDs) == 0 {
		return media.ErrInvalidRequest
	}
	ordered := slices.Clone(assetIDs)
	slices.Sort(ordered)
	for index, assetID := range ordered {
		if !media.ValidUUID(assetID) ||
			(index > 0 && assetID == ordered[index-1]) {
			return media.ErrInvalidRequest
		}
		asset, err := LockOwnedInTransaction(
			ctx, tx, userID, assetID, kind,
		)
		if err != nil {
			return err
		}
		var attachable bool
		if err := tx.QueryRow(ctx, `
SELECT status = 'ready'
   AND etag <> ''
   AND expires_at > CURRENT_TIMESTAMP
FROM media_assets
WHERE id = $1`, asset.ID).Scan(&attachable); err != nil {
			return mapError(err)
		}
		if !attachable {
			return media.ErrConflict
		}
	}
	return nil
}

// LockOwnedInTransaction is the shared ownership guard for business
// transactions that bind or detach an Asset.
func LockOwnedInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	assetID string,
	kind media.Kind,
) (media.Asset, error) {
	if ctx == nil || tx == nil || !media.ValidUUID(userID) ||
		!media.ValidUUID(assetID) ||
		(kind != media.KindImage && kind != media.KindAudio &&
			kind != media.KindDocument) {
		return media.Asset{}, media.ErrInvalidRequest
	}
	asset, err := scanAsset(tx.QueryRow(ctx, `
SELECT `+assetColumns+`
FROM media_assets AS asset
WHERE asset.id = $1
  AND asset.user_id = $2
  AND asset.kind = $3
FOR UPDATE`, assetID, userID, kind))
	if err != nil {
		return media.Asset{}, mapError(err)
	}
	return asset, nil
}

// LockReadyByUploadRequestInTransaction resolves an idempotent upload inside
// the business transaction that is about to retain it. The row lock prevents
// cleanup from claiming the asset between validation and reference creation.
func LockReadyByUploadRequestInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	kind media.Kind,
	uploadRequestID string,
) (media.Asset, error) {
	if ctx == nil || tx == nil || !media.ValidUUID(userID) ||
		!media.ValidIdempotencyKey(uploadRequestID) ||
		(kind != media.KindImage && kind != media.KindAudio &&
			kind != media.KindDocument) {
		return media.Asset{}, media.ErrInvalidRequest
	}
	asset, err := scanAsset(tx.QueryRow(ctx, `
SELECT `+assetColumns+`
FROM media_assets AS asset
WHERE asset.user_id = $1
  AND asset.kind = $2
  AND asset.upload_request_id = $3
FOR UPDATE`, userID, kind, uploadRequestID))
	if err != nil {
		return media.Asset{}, mapError(err)
	}
	var ready bool
	if err := tx.QueryRow(ctx, `
SELECT status = 'ready'
   AND etag <> ''
   AND expires_at > CURRENT_TIMESTAMP
FROM media_assets
WHERE id = $1`, asset.ID).Scan(&ready); err != nil {
		return media.Asset{}, mapError(err)
	}
	if !ready {
		return media.Asset{}, media.ErrConflict
	}
	return asset, nil
}

// RetainInTransaction removes expiry after a business reference has been
// inserted in the same transaction.
func RetainInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	kind media.Kind,
	assetIDs []string,
) error {
	if ctx == nil || tx == nil || !media.ValidUUID(userID) ||
		(kind != media.KindImage && kind != media.KindAudio &&
			kind != media.KindDocument) ||
		len(assetIDs) == 0 {
		return media.ErrInvalidRequest
	}
	tag, err := tx.Exec(ctx, `
UPDATE media_assets
SET
    expires_at = NULL,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE user_id = $1
  AND kind = $2
  AND id = ANY($3::uuid[])
  AND status = 'ready'
  AND etag <> ''`, userID, kind, assetIDs)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != int64(len(assetIDs)) {
		return media.ErrConflict
	}
	return nil
}

// ScheduleDeletionInTransaction moves owned assets out of every readable or
// attachable state. The cleanup worker performs the object deletion after the
// business transaction commits.
func ScheduleDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	assetIDs []string,
) error {
	if ctx == nil || tx == nil || !media.ValidUUID(userID) {
		return media.ErrInvalidRequest
	}
	if len(assetIDs) == 0 {
		return nil
	}
	ordered := slices.Clone(assetIDs)
	slices.Sort(ordered)
	for index, assetID := range ordered {
		if !media.ValidUUID(assetID) ||
			(index > 0 && assetID == ordered[index-1]) {
			return media.ErrInvalidRequest
		}
		var status string
		if err := tx.QueryRow(ctx, `
SELECT status
FROM media_assets
WHERE id = $1 AND user_id = $2
FOR UPDATE`, assetID, userID).Scan(&status); err != nil {
			return mapError(err)
		}
		if media.Status(status) == media.StatusDeleting {
			continue
		}
		tag, err := tx.Exec(ctx, `
UPDATE media_assets
SET
    status = 'deleting',
    upload_lease_until = NULL,
    expires_at = NULL,
    cleanup_lease_until = NULL,
    cleanup_available_at = CURRENT_TIMESTAMP,
    cleanup_error = NULL,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE id = $1 AND user_id = $2`, assetID, userID)
		if err != nil {
			return mapError(err)
		}
		if tag.RowsAffected() != 1 {
			return media.ErrNotFound
		}
	}
	return nil
}
