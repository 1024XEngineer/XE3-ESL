package avatar

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) (*PostgresRepository, error) {
	if database == nil {
		return nil, ErrRepository
	}
	return &PostgresRepository{database: database}, nil
}

type lockedProfile struct {
	userID, displayName, avatarAssetID string
	version                            int64
	createdAt, updatedAt               time.Time
}

func (repository *PostgresRepository) Attach(
	ctx context.Context,
	userID string,
	uploadRequestID string,
	expectedVersion int64,
) (_ identity.UserProfile, resultErr error) {
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return identity.UserProfile{}, ErrRepository
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	profile, err := lockProfile(ctx, tx, userID)
	if err != nil {
		return identity.UserProfile{}, err
	}
	if profile.avatarAssetID != "" {
		var current sharedmedia.Asset
		if err := tx.QueryRow(ctx, `
SELECT id::text, upload_request_id, width, height, updated_at
FROM media_assets
WHERE id = $1 AND user_id = $2 AND kind = 'image' AND status = 'ready'
FOR UPDATE`, profile.avatarAssetID, userID).Scan(
			&current.ID,
			&current.UploadRequestID,
			&current.Width,
			&current.Height,
			&current.UpdatedAt,
		); err != nil {
			return identity.UserProfile{}, mapTransactionMediaError(err)
		}
		if current.UploadRequestID == uploadRequestID {
			if err := tx.Commit(ctx); err != nil {
				return identity.UserProfile{}, ErrRepository
			}
			return projectProfile(profile, &current), nil
		}
	}
	asset, err := mediapostgres.LockReadyByUploadRequestInTransaction(
		ctx, tx, userID, sharedmedia.KindImage, uploadRequestID,
	)
	if err != nil {
		return identity.UserProfile{}, mapTransactionMediaError(err)
	}
	if profile.version != expectedVersion {
		return identity.UserProfile{}, ErrConflict
	}
	oldAssetID := profile.avatarAssetID
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE users
SET
    avatar_asset_id = $2,
    profile_version = profile_version + 1,
    updated_at = GREATEST(CURRENT_TIMESTAMP, updated_at + INTERVAL '1 microsecond')
WHERE id = $1 AND status = 'active'
RETURNING updated_at`, userID, asset.ID).Scan(&updatedAt); err != nil {
		return identity.UserProfile{}, ErrRepository
	}
	if err := mediapostgres.RetainInTransaction(
		ctx, tx, userID, sharedmedia.KindImage, []string{asset.ID},
	); err != nil {
		return identity.UserProfile{}, mapTransactionMediaError(err)
	}
	if oldAssetID != "" {
		if err := mediapostgres.ScheduleDeletionInTransaction(
			ctx, tx, userID, []string{oldAssetID},
		); err != nil {
			return identity.UserProfile{}, mapTransactionMediaError(err)
		}
	}
	profile.avatarAssetID = asset.ID
	profile.version++
	profile.updatedAt = updatedAt
	if err := tx.Commit(ctx); err != nil {
		return identity.UserProfile{}, ErrRepository
	}
	return projectProfile(profile, &asset), nil
}

func (repository *PostgresRepository) UseDefault(
	ctx context.Context,
	userID string,
	expectedVersion int64,
) (_ identity.UserProfile, resultErr error) {
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return identity.UserProfile{}, ErrRepository
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	profile, err := lockProfile(ctx, tx, userID)
	if err != nil {
		return identity.UserProfile{}, err
	}
	if profile.avatarAssetID == "" {
		if err := tx.Commit(ctx); err != nil {
			return identity.UserProfile{}, ErrRepository
		}
		return projectProfile(profile, nil), nil
	}
	if profile.version != expectedVersion {
		return identity.UserProfile{}, ErrConflict
	}
	oldAssetID := profile.avatarAssetID
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE users
SET
    avatar_asset_id = NULL,
    profile_version = profile_version + 1,
    updated_at = GREATEST(CURRENT_TIMESTAMP, updated_at + INTERVAL '1 microsecond')
WHERE id = $1 AND status = 'active'
RETURNING updated_at`, userID).Scan(&updatedAt); err != nil {
		return identity.UserProfile{}, ErrRepository
	}
	if err := mediapostgres.ScheduleDeletionInTransaction(
		ctx, tx, userID, []string{oldAssetID},
	); err != nil {
		return identity.UserProfile{}, mapTransactionMediaError(err)
	}
	profile.avatarAssetID = ""
	profile.version++
	profile.updatedAt = updatedAt
	if err := tx.Commit(ctx); err != nil {
		return identity.UserProfile{}, ErrRepository
	}
	return projectProfile(profile, nil), nil
}

func (repository *PostgresRepository) CurrentAssetID(
	ctx context.Context,
	userID string,
) (string, error) {
	var assetID string
	err := repository.database.QueryRow(ctx, `
SELECT asset.id::text
FROM users AS owner
JOIN media_assets AS asset
  ON asset.id = owner.avatar_asset_id AND asset.user_id = owner.id
WHERE owner.id = $1 AND owner.status = 'active'
  AND owner.display_name IS NOT NULL
  AND asset.kind = 'image' AND asset.status = 'ready' AND asset.etag <> ''`,
		userID,
	).Scan(&assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrRepository
	}
	return assetID, nil
}

func lockProfile(ctx context.Context, tx pgx.Tx, userID string) (lockedProfile, error) {
	var profile lockedProfile
	err := tx.QueryRow(ctx, `
SELECT
    id::text,
    display_name,
    profile_version,
    COALESCE(avatar_asset_id::text, ''),
    created_at,
    updated_at
FROM users
WHERE id = $1 AND status = 'active' AND display_name IS NOT NULL
FOR UPDATE`, userID).Scan(
		&profile.userID,
		&profile.displayName,
		&profile.version,
		&profile.avatarAssetID,
		&profile.createdAt,
		&profile.updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedProfile{}, ErrNotFound
	}
	if err != nil {
		return lockedProfile{}, ErrRepository
	}
	return profile, nil
}

func projectProfile(profile lockedProfile, asset *sharedmedia.Asset) identity.UserProfile {
	result := identity.UserProfile{
		UserID: profile.userID, DisplayName: profile.displayName,
		ProfileVersion: profile.version, CreatedAt: profile.createdAt,
		UpdatedAt: profile.updatedAt,
	}
	if asset != nil {
		result.Avatar = &identity.ProfileAvatar{
			Width: asset.Width, Height: asset.Height, UpdatedAt: profile.updatedAt,
		}
	}
	return result
}

func mapTransactionMediaError(err error) error {
	switch {
	case errors.Is(err, sharedmedia.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, sharedmedia.ErrConflict):
		return ErrConflict
	case errors.Is(err, sharedmedia.ErrInvalidRequest):
		return ErrInvalidRequest
	default:
		return ErrRepository
	}
}
