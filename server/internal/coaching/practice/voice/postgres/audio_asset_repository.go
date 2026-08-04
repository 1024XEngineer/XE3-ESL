// Package postgres implements Practice Voice's PostgreSQL persistence adapters.
package postgres

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
)

var ErrAudioAssetDatabase = errors.New("audio asset database operation failed")

const (
	// maxAudioAssetStagedTTL is an adapter safety bound, not a product
	// retention promise. The Practice Voice service currently uses 24 hours.
	maxAudioAssetStagedTTL       = 30 * 24 * time.Hour
	maxAudioAssetIdentifierBytes = 128
)

const audioAssetColumns = `
	audio_asset_id,
	owner_user_id,
	upload_request_id,
	object_key,
	candidate_id,
	turn_id,
	content_type,
	size_bytes,
	checksum_sha256,
	duration_ns,
	etag,
	status,
	staged_until,
	upload_lease_until,
	upload_fencing_token,
	cleanup_lease_until,
	cleanup_fencing_token,
	created_at,
	updated_at,
	deleted_at,
	version`

const audioAssetReturningColumns = `
	asset.audio_asset_id,
	asset.owner_user_id,
	asset.upload_request_id,
	asset.object_key,
	asset.candidate_id,
	asset.turn_id,
	asset.content_type,
	asset.size_bytes,
	asset.checksum_sha256,
	asset.duration_ns,
	asset.etag,
	asset.status,
	asset.staged_until,
	asset.upload_lease_until,
	asset.upload_fencing_token,
	asset.cleanup_lease_until,
	asset.cleanup_fencing_token,
	asset.created_at,
	asset.updated_at,
	asset.deleted_at,
	asset.version`

type AudioAssetRepository struct {
	pool *pgxpool.Pool
}

var (
	_ practicevoice.AudioAssetLifecycleRepository = (*AudioAssetRepository)(nil)
	_ practicevoice.AudioAssetTurnVerifier        = (*AudioAssetRepository)(nil)
)

func NewAudioAssetRepository(
	pool *pgxpool.Pool,
) (*AudioAssetRepository, error) {
	if pool == nil {
		return nil, practicevoice.ErrAudioAssetInvalidDependency
	}
	return &AudioAssetRepository{pool: pool}, nil
}

func (r *AudioAssetRepository) VerifyOwnedTurn(
	ctx context.Context,
	ownerID string,
	turnID string,
	candidateID string,
) error {
	if !validOwnerID(ownerID) ||
		!validAudioAssetIdentifier(turnID) ||
		!validAudioAssetIdentifier(candidateID) {
		return practicevoice.ErrAudioAssetInvalid
	}
	var exists bool
	err := r.pool.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM practice_turns
			WHERE owner_user_id = $1
			  AND turn_id = $2
			  AND candidate_id = $3
		)`,
		ownerID,
		turnID,
		candidateID,
	).Scan(&exists)
	if err != nil {
		return safeAudioAssetDatabaseError(err)
	}
	if !exists {
		return practicevoice.ErrAudioAssetTurnNotFound
	}
	return nil
}

func (r *AudioAssetRepository) Create(
	ctx context.Context,
	asset practicevoice.AudioAsset,
) error {
	stagedTTLMicroseconds, valid := validAudioAssetStagedTTL(
		asset.StagedUntil.Sub(asset.CreatedAt),
	)
	if !validNewAudioAsset(asset) || !valid {
		return practicevoice.ErrAudioAssetInvalid
	}

	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO practice_audio_assets (
			audio_asset_id,
			owner_user_id,
			upload_request_id,
			object_key,
			candidate_id,
			turn_id,
			content_type,
			size_bytes,
			checksum_sha256,
			duration_ns,
			etag,
			status,
			staged_until,
			created_at,
			updated_at,
			deleted_at,
			version
		) VALUES (
			$1, $2, $3, $4, NULL, NULL, $5, $6, $7, $8, '', $9,
			transaction_timestamp() + ($10::bigint * interval '1 microsecond'),
			transaction_timestamp(), transaction_timestamp(), NULL, $11
		)`,
		asset.ID,
		asset.OwnerID,
		asset.UploadRequestID,
		asset.ObjectKey,
		asset.ContentType,
		asset.Size,
		asset.ChecksumSHA256,
		int64(asset.Duration),
		asset.Status,
		stagedTTLMicroseconds,
		int64(asset.Version),
	)
	if err != nil {
		return mapAudioAssetWriteError(err)
	}
	return nil
}

// GetOwned is the only asset-ID read primitive exposed by the production
// adapter. Request paths must supply the trusted Actor's owner ID.
func (r *AudioAssetRepository) GetOwned(
	ctx context.Context,
	ownerID string,
	audioAssetID string,
) (practicevoice.AudioAsset, error) {
	if !validOwnerID(ownerID) ||
		!validAudioAssetIdentifier(audioAssetID) {
		return practicevoice.AudioAsset{}, practicevoice.ErrAudioAssetInvalid
	}

	record, err := scanAudioAsset(r.pool.QueryRow(
		ctx,
		"SELECT "+audioAssetColumns+
			` FROM practice_audio_assets
			  WHERE owner_user_id = $1 AND audio_asset_id = $2`,
		ownerID,
		audioAssetID,
	))
	if err != nil {
		return practicevoice.AudioAsset{}, mapAudioAssetReadError(err)
	}
	return record.asset, nil
}

func (r *AudioAssetRepository) GetByUploadRequest(
	ctx context.Context,
	ownerID string,
	requestID string,
) (practicevoice.AudioAsset, error) {
	if !validOwnerID(ownerID) ||
		!validAudioAssetIdentifier(requestID) {
		return practicevoice.AudioAsset{}, practicevoice.ErrAudioAssetInvalid
	}

	record, err := scanAudioAsset(r.pool.QueryRow(
		ctx,
		"SELECT "+audioAssetColumns+
			` FROM practice_audio_assets
			  WHERE owner_user_id = $1 AND upload_request_id = $2`,
		ownerID,
		requestID,
	))
	if err != nil {
		return practicevoice.AudioAsset{}, mapAudioAssetReadError(err)
	}
	return record.asset, nil
}

func (r *AudioAssetRepository) GetByCandidate(
	ctx context.Context,
	ownerID string,
	candidateID string,
) (practicevoice.AudioAsset, error) {
	return r.getByOwnerBinding(ctx, ownerID, "candidate_id", candidateID)
}

func (r *AudioAssetRepository) GetByTurn(
	ctx context.Context,
	ownerID string,
	turnID string,
) (practicevoice.AudioAsset, error) {
	return r.getByOwnerBinding(ctx, ownerID, "turn_id", turnID)
}

func (r *AudioAssetRepository) getByOwnerBinding(
	ctx context.Context,
	ownerID string,
	column string,
	value string,
) (practicevoice.AudioAsset, error) {
	if !validOwnerID(ownerID) ||
		!validAudioAssetIdentifier(value) {
		return practicevoice.AudioAsset{}, practicevoice.ErrAudioAssetInvalid
	}

	var query string
	switch column {
	case "candidate_id":
		query = "SELECT " + audioAssetColumns +
			` FROM practice_audio_assets
			  WHERE owner_user_id = $1 AND candidate_id = $2`
	case "turn_id":
		query = "SELECT " + audioAssetColumns +
			` FROM practice_audio_assets
			  WHERE owner_user_id = $1 AND turn_id = $2`
	default:
		return practicevoice.AudioAsset{}, practicevoice.ErrAudioAssetInvalid
	}

	record, err := scanAudioAsset(r.pool.QueryRow(ctx, query, ownerID, value))
	if err != nil {
		return practicevoice.AudioAsset{}, mapAudioAssetReadError(err)
	}
	return record.asset, nil
}

func (r *AudioAssetRepository) Save(
	ctx context.Context,
	asset practicevoice.AudioAsset,
	expectedVersion uint64,
) error {
	if !validPersistedAudioAsset(asset) ||
		expectedVersion == 0 ||
		expectedVersion >= uint64(^uint64(0)>>1) ||
		asset.Version != expectedVersion+1 {
		return practicevoice.ErrAudioAssetInvalid
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE practice_audio_assets
		 SET candidate_id = NULLIF($1, ''),
		     turn_id = NULLIF($2, ''),
		     etag = $3,
		     status = $4,
		     updated_at = $5,
		     deleted_at = $6,
		     version = $7,
		     upload_lease_until = CASE
		         WHEN $4 = 'staged' THEN upload_lease_until
		         ELSE NULL
		     END,
		     upload_fencing_token = CASE
		         WHEN upload_lease_until IS NOT NULL
		              AND upload_lease_until <= transaction_timestamp()
		         THEN upload_fencing_token + 1
		         ELSE upload_fencing_token
		     END,
		     cleanup_lease_until = CASE
		         WHEN $4 = 'deleting' THEN cleanup_lease_until
		         ELSE NULL
		     END,
		     cleanup_fencing_token = CASE
		         WHEN cleanup_lease_until IS NOT NULL
		              AND cleanup_lease_until <= transaction_timestamp()
		         THEN cleanup_fencing_token + 1
		         ELSE cleanup_fencing_token
		     END
		 WHERE audio_asset_id = $8
		   AND owner_user_id = $9
		   AND version = $10
		   AND (
		       cleanup_lease_until IS NULL
		       OR cleanup_lease_until <= transaction_timestamp()
		   )
		   AND (
		       upload_lease_until IS NULL
		       OR upload_lease_until <= transaction_timestamp()
		   )`,
		asset.CandidateID,
		asset.TurnID,
		asset.ETag,
		asset.Status,
		asset.UpdatedAt,
		nullableTime(asset.DeletedAt),
		int64(asset.Version),
		asset.ID,
		asset.OwnerID,
		int64(expectedVersion),
	)
	if err != nil {
		return mapAudioAssetWriteError(err)
	}
	if commandTag.RowsAffected() == 0 {
		return r.classifyWriteMiss(ctx, asset.OwnerID, asset.ID)
	}
	return nil
}

func (r *AudioAssetRepository) ClaimUpload(
	ctx context.Context,
	ownerID string,
	uploadRequestID string,
	leaseDuration time.Duration,
) (practicevoice.AudioAssetUploadClaim, error) {
	leaseMicroseconds, valid := validLeaseDuration(leaseDuration)
	if !validOwnerID(ownerID) ||
		!validAudioAssetIdentifier(uploadRequestID) ||
		!valid {
		return practicevoice.AudioAssetUploadClaim{},
			practicevoice.ErrAudioAssetInvalid
	}

	record, err := scanAudioAsset(r.pool.QueryRow(
		ctx,
		`WITH selected AS (
			SELECT audio_asset_id
			FROM practice_audio_assets
			WHERE owner_user_id = $1
			  AND upload_request_id = $2
			  AND status = 'staged'
			  AND (
			      upload_lease_until IS NULL
			      OR upload_lease_until <= transaction_timestamp()
			  )
			FOR UPDATE SKIP LOCKED
		)
		UPDATE practice_audio_assets AS asset
		SET upload_lease_until =
		        transaction_timestamp() + ($3::bigint * interval '1 microsecond'),
		    upload_fencing_token = asset.upload_fencing_token + 1,
		    updated_at = GREATEST(asset.updated_at, transaction_timestamp()),
		    version = asset.version + 1
		FROM selected
		WHERE asset.audio_asset_id = selected.audio_asset_id
		RETURNING `+audioAssetReturningColumns,
		ownerID,
		uploadRequestID,
		leaseMicroseconds,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return practicevoice.AudioAssetUploadClaim{},
			r.classifyUploadClaimMiss(ctx, ownerID, uploadRequestID)
	}
	if err != nil {
		return practicevoice.AudioAssetUploadClaim{},
			safeAudioAssetDatabaseError(err)
	}
	if !record.uploadLease.Valid || record.uploadFence <= 0 {
		return practicevoice.AudioAssetUploadClaim{}, ErrAudioAssetDatabase
	}
	return practicevoice.AudioAssetUploadClaim{
		Asset:          record.asset,
		FencingToken:   uint64(record.uploadFence),
		LeaseExpiresAt: record.uploadLease.Time.UTC(),
	}, nil
}

func (r *AudioAssetRepository) CommitUploadClaim(
	ctx context.Context,
	asset practicevoice.AudioAsset,
	expectedVersion uint64,
	fencingToken uint64,
) error {
	if !validPersistedAudioAsset(asset) ||
		asset.Status != practicevoice.AudioAssetMetadataCommitted ||
		expectedVersion == 0 ||
		expectedVersion >= uint64(^uint64(0)>>1) ||
		asset.Version != expectedVersion+1 ||
		fencingToken == 0 ||
		fencingToken > uint64(^uint64(0)>>1) {
		return practicevoice.ErrAudioAssetInvalid
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE practice_audio_assets
		 SET etag = $1,
		     status = 'metadata_committed',
		     updated_at = $2,
		     upload_lease_until = NULL,
		     version = $3
		 WHERE audio_asset_id = $4
		   AND owner_user_id = $5
		   AND status = 'staged'
		   AND version = $6
		   AND upload_fencing_token = $7
		   AND upload_lease_until > transaction_timestamp()`,
		asset.ETag,
		asset.UpdatedAt,
		int64(asset.Version),
		asset.ID,
		asset.OwnerID,
		int64(expectedVersion),
		int64(fencingToken),
	)
	if err != nil {
		return mapAudioAssetWriteError(err)
	}
	if commandTag.RowsAffected() == 0 {
		return r.reconcileUploadCommit(
			ctx,
			asset,
			fencingToken,
		)
	}
	return nil
}

// ReleaseUploadClaim is only safe when the caller knows Put never reached the
// object-store network boundary. Ambiguous failures and timeouts must retain
// the lease until expiry so cleanup cannot race a late object write.
func (r *AudioAssetRepository) ReleaseUploadClaim(
	ctx context.Context,
	ownerID string,
	audioAssetID string,
	fencingToken uint64,
) error {
	if !validOwnerID(ownerID) ||
		!validAudioAssetIdentifier(audioAssetID) ||
		fencingToken == 0 ||
		fencingToken > uint64(^uint64(0)>>1) {
		return practicevoice.ErrAudioAssetInvalid
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE practice_audio_assets
		 SET upload_lease_until = NULL,
		     updated_at = GREATEST(updated_at, transaction_timestamp()),
		     version = version + 1
		 WHERE owner_user_id = $1
		   AND audio_asset_id = $2
		   AND status = 'staged'
		   AND upload_fencing_token = $3
		   AND upload_lease_until IS NOT NULL`,
		ownerID,
		audioAssetID,
		int64(fencingToken),
	)
	if err != nil {
		return safeAudioAssetDatabaseError(err)
	}
	if commandTag.RowsAffected() == 0 {
		return r.reconcileUploadRelease(
			ctx,
			ownerID,
			audioAssetID,
			fencingToken,
		)
	}
	return nil
}

func (r *AudioAssetRepository) ClaimExpiredUnconfirmed(
	ctx context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]practicevoice.AudioAssetCleanupClaim, error) {
	leaseMicroseconds, valid := validLease(leaseDuration, limit)
	if !valid {
		return nil, practicevoice.ErrAudioAssetInvalid
	}
	return r.claimAudioAssets(
		ctx,
		`WITH selected AS (
			SELECT audio_asset_id
			FROM practice_audio_assets
			WHERE status IN ('staged', 'metadata_committed')
			  AND candidate_id IS NULL
			  AND turn_id IS NULL
			  AND staged_until <= transaction_timestamp()
			  AND (
			      upload_lease_until IS NULL
			      OR upload_lease_until <= transaction_timestamp()
			  )
			  AND (
			      cleanup_lease_until IS NULL
			      OR cleanup_lease_until <= transaction_timestamp()
			  )
			ORDER BY staged_until, audio_asset_id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE practice_audio_assets AS asset
		SET status = 'deleting',
		    updated_at = GREATEST(asset.updated_at, transaction_timestamp()),
		    upload_lease_until = NULL,
		    cleanup_lease_until =
		        transaction_timestamp() + ($2::bigint * interval '1 microsecond'),
		    cleanup_fencing_token = asset.cleanup_fencing_token + 1,
		    version = asset.version + 1
		FROM selected
		WHERE asset.audio_asset_id = selected.audio_asset_id
		RETURNING `+audioAssetReturningColumns,
		limit,
		leaseMicroseconds,
	)
}

func (r *AudioAssetRepository) ClaimDeleting(
	ctx context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]practicevoice.AudioAssetCleanupClaim, error) {
	leaseMicroseconds, valid := validLease(leaseDuration, limit)
	if !valid {
		return nil, practicevoice.ErrAudioAssetInvalid
	}
	return r.claimAudioAssets(
		ctx,
		`WITH selected AS (
			SELECT audio_asset_id
			FROM practice_audio_assets
			WHERE status = 'deleting'
			  AND (
			      upload_lease_until IS NULL
			      OR upload_lease_until <= transaction_timestamp()
			  )
			  AND (
			      cleanup_lease_until IS NULL
			      OR cleanup_lease_until <= transaction_timestamp()
			  )
			ORDER BY updated_at, audio_asset_id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE practice_audio_assets AS asset
		SET cleanup_lease_until =
		        transaction_timestamp() + ($2::bigint * interval '1 microsecond'),
		    cleanup_fencing_token = asset.cleanup_fencing_token + 1,
		    version = asset.version + 1
		FROM selected
		WHERE asset.audio_asset_id = selected.audio_asset_id
		RETURNING `+audioAssetReturningColumns,
		limit,
		leaseMicroseconds,
	)
}

func (r *AudioAssetRepository) ClaimOwnerAssetsForAccountCleanup(
	ctx context.Context,
	ownerID string,
	leaseDuration time.Duration,
	limit int,
) ([]practicevoice.AudioAssetCleanupClaim, error) {
	leaseMicroseconds, valid := validLease(leaseDuration, limit)
	if !validOwnerID(ownerID) || !valid {
		return nil, practicevoice.ErrAudioAssetInvalid
	}
	return r.claimAudioAssets(
		ctx,
		`WITH selected AS (
			SELECT audio_asset_id
			FROM practice_audio_assets
			WHERE owner_user_id = $1
			  AND status <> 'deleted'
			  AND (
			      upload_lease_until IS NULL
			      OR upload_lease_until <= transaction_timestamp()
			  )
			  AND (
			      cleanup_lease_until IS NULL
			      OR cleanup_lease_until <= transaction_timestamp()
			  )
			ORDER BY updated_at, audio_asset_id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE practice_audio_assets AS asset
		SET status = 'deleting',
		    updated_at = CASE
		        WHEN asset.status = 'deleting' THEN asset.updated_at
		        ELSE GREATEST(asset.updated_at, transaction_timestamp())
		    END,
		    upload_lease_until = NULL,
		    cleanup_lease_until =
		        transaction_timestamp() + ($3::bigint * interval '1 microsecond'),
		    cleanup_fencing_token = asset.cleanup_fencing_token + 1,
		    version = asset.version + 1
		FROM selected
		WHERE asset.audio_asset_id = selected.audio_asset_id
		RETURNING `+audioAssetReturningColumns,
		ownerID,
		limit,
		leaseMicroseconds,
	)
}

func (r *AudioAssetRepository) SaveCleanupClaim(
	ctx context.Context,
	asset practicevoice.AudioAsset,
	expectedVersion uint64,
	fencingToken uint64,
) error {
	if !validPersistedAudioAsset(asset) ||
		asset.Status != practicevoice.AudioAssetDeleted ||
		expectedVersion == 0 ||
		expectedVersion >= uint64(^uint64(0)>>1) ||
		asset.Version != expectedVersion+1 ||
		fencingToken == 0 ||
		fencingToken > uint64(^uint64(0)>>1) {
		return practicevoice.ErrAudioAssetInvalid
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE practice_audio_assets
		 SET status = 'deleted',
		     updated_at = $1,
		     deleted_at = $2,
		     cleanup_lease_until = NULL,
		     version = $3
		 WHERE audio_asset_id = $4
		   AND owner_user_id = $5
		   AND status = 'deleting'
		   AND version = $6
		   AND cleanup_fencing_token = $7
		   AND cleanup_lease_until > transaction_timestamp()`,
		asset.UpdatedAt,
		asset.DeletedAt,
		int64(asset.Version),
		asset.ID,
		asset.OwnerID,
		int64(expectedVersion),
		int64(fencingToken),
	)
	if err != nil {
		return mapAudioAssetWriteError(err)
	}
	if commandTag.RowsAffected() == 0 {
		return r.reconcileCleanupCommit(
			ctx,
			asset,
			fencingToken,
		)
	}
	return nil
}

func (r *AudioAssetRepository) ReleaseCleanupClaim(
	ctx context.Context,
	ownerID string,
	audioAssetID string,
	fencingToken uint64,
) error {
	if !validOwnerID(ownerID) ||
		!validAudioAssetIdentifier(audioAssetID) ||
		fencingToken == 0 ||
		fencingToken > uint64(^uint64(0)>>1) {
		return practicevoice.ErrAudioAssetInvalid
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE practice_audio_assets
		 SET cleanup_lease_until = NULL,
		     updated_at = GREATEST(updated_at, transaction_timestamp()),
		     version = version + 1
		 WHERE owner_user_id = $1
		   AND audio_asset_id = $2
		   AND status = 'deleting'
		   AND cleanup_fencing_token = $3
		   AND cleanup_lease_until IS NOT NULL`,
		ownerID,
		audioAssetID,
		int64(fencingToken),
	)
	if err != nil {
		return safeAudioAssetDatabaseError(err)
	}
	if commandTag.RowsAffected() == 0 {
		return r.reconcileCleanupRelease(
			ctx,
			ownerID,
			audioAssetID,
			fencingToken,
		)
	}
	return nil
}

func (r *AudioAssetRepository) HasOwnerAssetsForAccountCleanup(
	ctx context.Context,
	ownerID string,
) (bool, error) {
	if !validOwnerID(ownerID) {
		return false, practicevoice.ErrAudioAssetInvalid
	}

	var pending bool
	if err := r.pool.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM practice_audio_assets
			WHERE owner_user_id = $1
		)`,
		ownerID,
	).Scan(&pending); err != nil {
		return false, safeAudioAssetDatabaseError(err)
	}
	return pending, nil
}

func (r *AudioAssetRepository) PurgeOwnerDeletedAssets(
	ctx context.Context,
	ownerID string,
	limit int,
) (int, error) {
	if !validOwnerID(ownerID) || !validLimit(limit) {
		return 0, practicevoice.ErrAudioAssetInvalid
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`WITH selected AS (
			SELECT audio_asset_id
			FROM practice_audio_assets
			WHERE owner_user_id = $1
			  AND status = 'deleted'
			ORDER BY deleted_at, audio_asset_id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		DELETE FROM practice_audio_assets AS asset
		USING selected
		WHERE asset.owner_user_id = $1
		  AND asset.audio_asset_id = selected.audio_asset_id`,
		ownerID,
		limit,
	)
	if err != nil {
		return 0, safeAudioAssetDatabaseError(err)
	}
	return int(commandTag.RowsAffected()), nil
}

func (r *AudioAssetRepository) claimAudioAssets(
	ctx context.Context,
	query string,
	arguments ...any,
) ([]practicevoice.AudioAssetCleanupClaim, error) {
	rows, err := r.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, safeAudioAssetDatabaseError(err)
	}
	defer rows.Close()

	claims := make([]practicevoice.AudioAssetCleanupClaim, 0)
	for rows.Next() {
		record, scanErr := scanAudioAsset(rows)
		if scanErr != nil {
			return nil, safeAudioAssetDatabaseError(scanErr)
		}
		if !record.cleanupLease.Valid ||
			record.cleanupFence <= 0 {
			return nil, ErrAudioAssetDatabase
		}
		claims = append(claims, practicevoice.AudioAssetCleanupClaim{
			Asset:          record.asset,
			FencingToken:   uint64(record.cleanupFence),
			LeaseExpiresAt: record.cleanupLease.Time.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, safeAudioAssetDatabaseError(err)
	}
	return claims, nil
}

func (r *AudioAssetRepository) classifyWriteMiss(
	ctx context.Context,
	ownerID string,
	audioAssetID string,
) error {
	var version int64
	err := r.pool.QueryRow(
		ctx,
		`SELECT version
		 FROM practice_audio_assets
		 WHERE owner_user_id = $1 AND audio_asset_id = $2`,
		ownerID,
		audioAssetID,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return practicevoice.ErrAudioAssetNotFound
	}
	if err != nil {
		return safeAudioAssetDatabaseError(err)
	}
	return practicevoice.ErrAudioAssetConcurrentUpdate
}

func (r *AudioAssetRepository) classifyUploadClaimMiss(
	ctx context.Context,
	ownerID string,
	uploadRequestID string,
) error {
	var audioAssetID string
	err := r.pool.QueryRow(
		ctx,
		`SELECT audio_asset_id
		 FROM practice_audio_assets
		 WHERE owner_user_id = $1 AND upload_request_id = $2`,
		ownerID,
		uploadRequestID,
	).Scan(&audioAssetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return practicevoice.ErrAudioAssetNotFound
	}
	if err != nil {
		return safeAudioAssetDatabaseError(err)
	}
	return practicevoice.ErrAudioAssetConcurrentUpdate
}

func (r *AudioAssetRepository) reconcileUploadCommit(
	ctx context.Context,
	expected practicevoice.AudioAsset,
	fencingToken uint64,
) error {
	record, err := scanAudioAsset(r.pool.QueryRow(
		ctx,
		"SELECT "+audioAssetColumns+
			` FROM practice_audio_assets
			  WHERE owner_user_id = $1 AND audio_asset_id = $2`,
		expected.OwnerID,
		expected.ID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return practicevoice.ErrAudioAssetNotFound
	}
	if err != nil {
		return safeAudioAssetDatabaseError(err)
	}
	actual := record.asset
	if record.uploadFence == int64(fencingToken) &&
		!record.uploadLease.Valid &&
		actual.ID == expected.ID &&
		actual.OwnerID == expected.OwnerID &&
		actual.UploadRequestID == expected.UploadRequestID &&
		actual.ObjectKey == expected.ObjectKey &&
		actual.CandidateID == "" &&
		actual.TurnID == "" &&
		actual.ContentType == expected.ContentType &&
		actual.Size == expected.Size &&
		actual.ChecksumSHA256 == expected.ChecksumSHA256 &&
		actual.Duration == expected.Duration &&
		actual.ETag == expected.ETag &&
		actual.Status == practicevoice.AudioAssetMetadataCommitted &&
		actual.Version == expected.Version {
		return nil
	}
	return practicevoice.ErrAudioAssetConcurrentUpdate
}

func (r *AudioAssetRepository) reconcileUploadRelease(
	ctx context.Context,
	ownerID string,
	audioAssetID string,
	fencingToken uint64,
) error {
	record, err := r.getOwnedRecord(ctx, ownerID, audioAssetID)
	if err != nil {
		return err
	}
	if record.asset.Status == practicevoice.AudioAssetStaged &&
		record.uploadFence == int64(fencingToken) &&
		!record.uploadLease.Valid {
		return nil
	}
	return practicevoice.ErrAudioAssetConcurrentUpdate
}

func (r *AudioAssetRepository) reconcileCleanupCommit(
	ctx context.Context,
	expected practicevoice.AudioAsset,
	fencingToken uint64,
) error {
	record, err := r.getOwnedRecord(ctx, expected.OwnerID, expected.ID)
	if err != nil {
		return err
	}
	if record.cleanupFence == int64(fencingToken) &&
		!record.cleanupLease.Valid &&
		audioAssetsMatchPersisted(record.asset, expected) {
		return nil
	}
	return practicevoice.ErrAudioAssetConcurrentUpdate
}

func (r *AudioAssetRepository) reconcileCleanupRelease(
	ctx context.Context,
	ownerID string,
	audioAssetID string,
	fencingToken uint64,
) error {
	record, err := r.getOwnedRecord(ctx, ownerID, audioAssetID)
	if err != nil {
		return err
	}
	if record.asset.Status == practicevoice.AudioAssetDeleting &&
		record.cleanupFence == int64(fencingToken) &&
		!record.cleanupLease.Valid {
		return nil
	}
	return practicevoice.ErrAudioAssetConcurrentUpdate
}

func (r *AudioAssetRepository) getOwnedRecord(
	ctx context.Context,
	ownerID string,
	audioAssetID string,
) (audioAssetRecord, error) {
	record, err := scanAudioAsset(r.pool.QueryRow(
		ctx,
		"SELECT "+audioAssetColumns+
			` FROM practice_audio_assets
			  WHERE owner_user_id = $1 AND audio_asset_id = $2`,
		ownerID,
		audioAssetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return audioAssetRecord{}, practicevoice.ErrAudioAssetNotFound
	}
	if err != nil {
		return audioAssetRecord{}, safeAudioAssetDatabaseError(err)
	}
	return record, nil
}

type audioAssetScanner interface {
	Scan(...any) error
}

type audioAssetRecord struct {
	asset        practicevoice.AudioAsset
	uploadLease  pgtype.Timestamptz
	uploadFence  int64
	cleanupLease pgtype.Timestamptz
	cleanupFence int64
}

func scanAudioAsset(scanner audioAssetScanner) (audioAssetRecord, error) {
	var record audioAssetRecord
	var candidateID pgtype.Text
	var turnID pgtype.Text
	var deletedAt pgtype.Timestamptz
	var durationNanoseconds int64
	var version int64

	err := scanner.Scan(
		&record.asset.ID,
		&record.asset.OwnerID,
		&record.asset.UploadRequestID,
		&record.asset.ObjectKey,
		&candidateID,
		&turnID,
		&record.asset.ContentType,
		&record.asset.Size,
		&record.asset.ChecksumSHA256,
		&durationNanoseconds,
		&record.asset.ETag,
		&record.asset.Status,
		&record.asset.StagedUntil,
		&record.uploadLease,
		&record.uploadFence,
		&record.cleanupLease,
		&record.cleanupFence,
		&record.asset.CreatedAt,
		&record.asset.UpdatedAt,
		&deletedAt,
		&version,
	)
	if err != nil {
		return audioAssetRecord{}, err
	}
	if durationNanoseconds <= 0 ||
		version <= 0 ||
		record.uploadFence < 0 ||
		record.cleanupFence < 0 {
		return audioAssetRecord{}, ErrAudioAssetDatabase
	}
	record.asset.Duration = time.Duration(durationNanoseconds)
	record.asset.Version = uint64(version)
	if candidateID.Valid {
		record.asset.CandidateID = candidateID.String
	}
	if turnID.Valid {
		record.asset.TurnID = turnID.String
	}
	if deletedAt.Valid {
		record.asset.DeletedAt = deletedAt.Time.UTC()
	}
	record.asset.StagedUntil = record.asset.StagedUntil.UTC()
	record.asset.CreatedAt = record.asset.CreatedAt.UTC()
	record.asset.UpdatedAt = record.asset.UpdatedAt.UTC()
	return record, nil
}

func audioAssetsMatchPersisted(
	actual practicevoice.AudioAsset,
	expected practicevoice.AudioAsset,
) bool {
	return actual.ID == expected.ID &&
		actual.OwnerID == expected.OwnerID &&
		actual.UploadRequestID == expected.UploadRequestID &&
		actual.ObjectKey == expected.ObjectKey &&
		actual.CandidateID == expected.CandidateID &&
		actual.TurnID == expected.TurnID &&
		actual.ContentType == expected.ContentType &&
		actual.Size == expected.Size &&
		actual.ChecksumSHA256 == expected.ChecksumSHA256 &&
		actual.Duration == expected.Duration &&
		actual.ETag == expected.ETag &&
		actual.Status == expected.Status &&
		postgresTimestampsEqual(actual.StagedUntil, expected.StagedUntil) &&
		postgresTimestampsEqual(actual.CreatedAt, expected.CreatedAt) &&
		postgresTimestampsEqual(actual.UpdatedAt, expected.UpdatedAt) &&
		postgresTimestampsEqual(actual.DeletedAt, expected.DeletedAt) &&
		actual.Version == expected.Version
}

func postgresTimestampsEqual(actual time.Time, expected time.Time) bool {
	if actual.IsZero() || expected.IsZero() {
		return actual.IsZero() && expected.IsZero()
	}
	return actual.Equal(expected.UTC().Truncate(time.Microsecond))
}

func validNewAudioAsset(asset practicevoice.AudioAsset) bool {
	return validPersistedAudioAsset(asset) &&
		asset.Status == practicevoice.AudioAssetStaged &&
		asset.CandidateID == "" &&
		asset.TurnID == "" &&
		asset.ETag == "" &&
		asset.DeletedAt.IsZero() &&
		asset.Version == 1
}

func validPersistedAudioAsset(asset practicevoice.AudioAsset) bool {
	if !validAudioAssetIdentifier(asset.ID) ||
		!validOwnerID(asset.OwnerID) ||
		!validAudioAssetIdentifier(asset.UploadRequestID) ||
		!validAudioObjectKey(asset.ObjectKey) ||
		(asset.ContentType != "audio/wav" &&
			asset.ContentType != "audio/x-wav") ||
		asset.Size <= 0 ||
		!validChecksum(asset.ChecksumSHA256) ||
		asset.Duration <= 0 ||
		asset.CreatedAt.IsZero() ||
		asset.UpdatedAt.Before(asset.CreatedAt) ||
		!asset.StagedUntil.After(asset.CreatedAt) ||
		asset.Version == 0 ||
		asset.Version > uint64(^uint64(0)>>1) {
		return false
	}
	bindingsPaired := (asset.CandidateID == "") == (asset.TurnID == "")
	if !bindingsPaired {
		return false
	}
	if asset.CandidateID != "" &&
		(!validAudioAssetIdentifier(asset.CandidateID) ||
			!validAudioAssetIdentifier(asset.TurnID)) {
		return false
	}
	switch asset.Status {
	case practicevoice.AudioAssetStaged:
		return asset.CandidateID == "" &&
			asset.TurnID == "" &&
			asset.DeletedAt.IsZero()
	case practicevoice.AudioAssetMetadataCommitted:
		return strings.TrimSpace(asset.ETag) != "" &&
			asset.CandidateID == "" &&
			asset.TurnID == "" &&
			asset.DeletedAt.IsZero()
	case practicevoice.AudioAssetReadable:
		return strings.TrimSpace(asset.ETag) != "" &&
			asset.CandidateID != "" &&
			asset.TurnID != "" &&
			asset.DeletedAt.IsZero()
	case practicevoice.AudioAssetDeleting:
		return asset.DeletedAt.IsZero()
	case practicevoice.AudioAssetDeleted:
		return !asset.DeletedAt.IsZero() &&
			!asset.DeletedAt.Before(asset.CreatedAt)
	default:
		return false
	}
}

func validOwnerID(ownerID string) bool {
	if ownerID == "" || ownerID != strings.TrimSpace(ownerID) {
		return false
	}
	var id pgtype.UUID
	if err := id.Scan(ownerID); err != nil {
		return false
	}
	return id.Valid
}

func validAudioAssetIdentifier(value string) bool {
	return value == strings.TrimSpace(value) &&
		len(value) >= 1 &&
		len(value) <= maxAudioAssetIdentifierBytes &&
		utf8.ValidString(value)
}

func validAudioObjectKey(objectKey string) bool {
	return objectKey == strings.TrimSpace(objectKey) &&
		strings.HasPrefix(objectKey, "audio/v1/assets/") &&
		strings.HasSuffix(objectKey, ".wav") &&
		!strings.Contains(objectKey, "..") &&
		!strings.ContainsAny(objectKey, "\\\r\n\x00")
}

func validChecksum(checksum string) bool {
	if len(checksum) != 64 {
		return false
	}
	for _, character := range checksum {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validLimit(limit int) bool {
	return limit > 0 && limit <= 1000
}

func validLease(
	leaseDuration time.Duration,
	limit int,
) (int64, bool) {
	if !validLimit(limit) {
		return 0, false
	}
	return validLeaseDuration(leaseDuration)
}

func validLeaseDuration(
	leaseDuration time.Duration,
) (int64, bool) {
	if leaseDuration <= 0 {
		return 0, false
	}
	microseconds := leaseDuration.Microseconds()
	if microseconds <= 0 {
		return 0, false
	}
	return microseconds, true
}

func validAudioAssetStagedTTL(stagedTTL time.Duration) (int64, bool) {
	if stagedTTL <= 0 || stagedTTL > maxAudioAssetStagedTTL {
		return 0, false
	}
	microseconds := stagedTTL.Microseconds()
	if microseconds <= 0 {
		return 0, false
	}
	return microseconds, true
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func mapAudioAssetReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return practicevoice.ErrAudioAssetNotFound
	}
	return safeAudioAssetDatabaseError(err)
}

func mapAudioAssetWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.ConstraintName {
		case "practice_audio_assets_owner_candidate_key",
			"practice_audio_assets_owner_turn_key":
			return practicevoice.ErrAudioAssetAlreadyBound
		case "practice_audio_assets_pkey",
			"practice_audio_assets_owner_upload_request_key",
			"practice_audio_assets_object_key_key":
			return practicevoice.ErrAudioAssetConcurrentUpdate
		}
		switch postgresError.Code {
		case "23503", "23514", "22P02":
			return practicevoice.ErrAudioAssetInvalid
		}
	}
	return safeAudioAssetDatabaseError(err)
}

func safeAudioAssetDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrAudioAssetDatabase
}
