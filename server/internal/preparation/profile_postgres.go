package preparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// PostgresProfileRepository stores actor-owned Preparation profiles,
// immutable snapshots, and their idempotency results in one database.
type PostgresProfileRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresProfileRepository(
	pool *pgxpool.Pool,
) *PostgresProfileRepository {
	return &PostgresProfileRepository{pool: pool}
}

func (r *PostgresProfileRepository) CreateProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreateProfileCommand,
) (Profile, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		!validResourceIdentifier(command.ProfileID) ||
		!validCreateProfileRequest(command.Request) ||
		!validPreparationIntent(
			command.Intent,
			"/v1/preparation-profiles",
			command.Request,
		) {
		return Profile{}, false, ErrProfileInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, false, profileDatabaseFailure(
			"begin profile creation",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)

	if err := lockActivePreparationActor(ctx, tx, actor.UserID); err != nil {
		return Profile{}, false, err
	}
	if err := lockPreparationIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return Profile{}, false, err
	}

	replayed, found, err := replayProfile(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	)
	if err != nil {
		return Profile{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return Profile{}, false, profileDatabaseFailure(
				"commit profile replay",
			)
		}
		return replayed, true, nil
	}

	profile := Profile{
		ID:                command.ProfileID,
		UserID:            actor.UserID,
		ResumeRef:         command.Request.ResumeRef,
		JobDescriptionRef: command.Request.JobDescriptionRef,
		BackgroundSummary: command.Request.BackgroundSummary,
		Version:           1,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_profiles (
			owner_user_id,
			profile_id,
			resume_ref,
			job_description_ref,
			background_summary
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING version, updated_at
	`,
		actor.UserID,
		profile.ID,
		nullablePreparationText(profile.ResumeRef),
		nullablePreparationText(profile.JobDescriptionRef),
		profile.BackgroundSummary,
	).Scan(&profile.Version, &profile.UpdatedAt)
	if err != nil {
		return Profile{}, false, classifyPreparationWriteError(err)
	}
	profile.UpdatedAt = profile.UpdatedAt.UTC()

	if err := persistPreparationIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"profile",
		profile.ID,
		profile,
	); err != nil {
		return Profile{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, false, profileDatabaseFailure(
			"commit profile creation",
		)
	}
	return profile, false, nil
}

func (r *PostgresProfileRepository) CreateSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreateSnapshotCommand,
) (Snapshot, bool, error) {
	expectedPath := "/v1/preparation-profiles/" +
		command.ProfileID + "/snapshots"
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		!validResourceIdentifier(command.SnapshotID) ||
		!validResourceIdentifier(command.ProfileID) ||
		command.Request.SourceVersion < 1 ||
		!validPreparationIntent(
			command.Intent,
			expectedPath,
			command.Request,
		) {
		return Snapshot{}, false, ErrProfileInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Snapshot{}, false, profileDatabaseFailure(
			"begin snapshot creation",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)

	if err := lockActivePreparationActor(ctx, tx, actor.UserID); err != nil {
		return Snapshot{}, false, err
	}
	if err := lockPreparationIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return Snapshot{}, false, err
	}

	replayed, found, err := replaySnapshot(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	)
	if err != nil {
		return Snapshot{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return Snapshot{}, false, profileDatabaseFailure(
				"commit snapshot replay",
			)
		}
		return replayed, true, nil
	}

	snapshot := Snapshot{
		ID:              command.SnapshotID,
		SourceProfileID: command.ProfileID,
		SourceVersion:   command.Request.SourceVersion,
	}
	var sourceVersion int
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE(resume_ref, ''),
			COALESCE(job_description_ref, ''),
			background_summary,
			version
		FROM preparation_profiles
		WHERE owner_user_id = $1
		  AND profile_id = $2
		FOR SHARE
	`, actor.UserID, command.ProfileID).Scan(
		&snapshot.ResumeSnapshot,
		&snapshot.JobDescriptionSnapshot,
		&snapshot.BackgroundSnapshot,
		&sourceVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, false, ErrProfileNotFound
	}
	if err != nil {
		return Snapshot{}, false, profileDatabaseFailure(
			"lock snapshot source",
		)
	}
	if sourceVersion != command.Request.SourceVersion {
		return Snapshot{}, false, ErrProfileConflict
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_snapshots (
			owner_user_id,
			snapshot_id,
			source_profile_id,
			source_version,
			resume_snapshot,
			job_description_snapshot,
			background_snapshot
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`,
		actor.UserID,
		snapshot.ID,
		snapshot.SourceProfileID,
		snapshot.SourceVersion,
		nullablePreparationText(snapshot.ResumeSnapshot),
		nullablePreparationText(snapshot.JobDescriptionSnapshot),
		snapshot.BackgroundSnapshot,
	).Scan(&snapshot.CreatedAt)
	if err != nil {
		return Snapshot{}, false, classifyPreparationWriteError(err)
	}
	snapshot.CreatedAt = snapshot.CreatedAt.UTC()

	if err := persistPreparationIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"snapshot",
		snapshot.ID,
		snapshot,
	); err != nil {
		return Snapshot{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, false, profileDatabaseFailure(
			"commit snapshot creation",
		)
	}
	return snapshot, false, nil
}

func (r *PostgresProfileRepository) ReadProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	profileID string,
) (Profile, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		!validResourceIdentifier(profileID) {
		return Profile{}, ErrProfileNotFound
	}

	profile, err := scanPreparationProfile(r.pool.QueryRow(ctx, `
		SELECT
			profile.profile_id,
			profile.owner_user_id::text,
			COALESCE(profile.resume_ref, ''),
			COALESCE(profile.job_description_ref, ''),
			profile.background_summary,
			profile.version,
			profile.updated_at
		FROM preparation_profiles AS profile
		JOIN identity_users AS owner
		  ON owner.id = profile.owner_user_id
		LEFT JOIN preparation_deletion_fences AS fence
		  ON fence.owner_user_id = profile.owner_user_id
		WHERE profile.owner_user_id = $1
		  AND profile.profile_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, profileID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrProfileNotFound
	}
	if err != nil {
		return Profile{}, profileDatabaseFailure("read profile")
	}
	return profile, nil
}

func (r *PostgresProfileRepository) ReadSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	snapshotID string,
) (Snapshot, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		!validResourceIdentifier(snapshotID) {
		return Snapshot{}, ErrProfileNotFound
	}

	snapshot, err := scanPreparationSnapshot(r.pool.QueryRow(ctx, `
		SELECT
			snapshot.snapshot_id,
			snapshot.source_profile_id,
			snapshot.source_version,
			COALESCE(snapshot.resume_snapshot, ''),
			COALESCE(snapshot.job_description_snapshot, ''),
			snapshot.background_snapshot,
			snapshot.created_at
		FROM preparation_snapshots AS snapshot
		JOIN identity_users AS owner
		  ON owner.id = snapshot.owner_user_id
		LEFT JOIN preparation_deletion_fences AS fence
		  ON fence.owner_user_id = snapshot.owner_user_id
		WHERE snapshot.owner_user_id = $1
		  AND snapshot.snapshot_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, snapshotID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrProfileNotFound
	}
	if err != nil {
		return Snapshot{}, profileDatabaseFailure("read snapshot")
	}
	return snapshot, nil
}

func (r *PostgresProfileRepository) DeleteProfileData(
	ctx context.Context,
	command DeleteProfileDataCommand,
) error {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationUUID(command.UserID) ||
		command.Generation == 0 ||
		command.Generation > math.MaxInt64 {
		return ErrProfileInvalid
	}
	generation := int64(command.Generation)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return profileDatabaseFailure("begin profile data deletion")
	}
	defer rollbackPreparationTransaction(ctx, tx)

	var accountStatus string
	err = tx.QueryRow(ctx, `
		SELECT account_status
		FROM identity_users
		WHERE id = $1
		FOR SHARE
	`, command.UserID).Scan(&accountStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		var fenceExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM preparation_deletion_fences
				WHERE owner_user_id = $1
			)
		`, command.UserID).Scan(&fenceExists); err != nil {
			return profileDatabaseFailure("verify profile deletion fence")
		}
		if !fenceExists {
			return ErrProfileNotFound
		}
	} else if err != nil {
		return profileDatabaseFailure("lock profile deletion owner")
	} else if accountStatus != "deleting" && accountStatus != "deleted" {
		return ErrProfileNotFound
	}

	var persistedGeneration int64
	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_deletion_fences (
			owner_user_id,
			deletion_generation
		)
		VALUES ($1, $2)
		ON CONFLICT (owner_user_id) DO UPDATE
		SET deletion_generation = GREATEST(
		        preparation_deletion_fences.deletion_generation,
		        EXCLUDED.deletion_generation
		    ),
		    updated_at = CASE
		        WHEN EXCLUDED.deletion_generation >
		             preparation_deletion_fences.deletion_generation
		        THEN transaction_timestamp()
		        ELSE preparation_deletion_fences.updated_at
		    END
		RETURNING deletion_generation
	`, command.UserID, generation).Scan(&persistedGeneration)
	if err != nil {
		return profileDatabaseFailure("persist profile deletion fence")
	}
	if generation < persistedGeneration {
		return ErrProfileDeletionGeneration
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM preparation_idempotency_records
		WHERE owner_user_id = $1
	`, command.UserID); err != nil {
		return profileDatabaseFailure("delete profile idempotency records")
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM preparation_snapshots
		WHERE owner_user_id = $1
	`, command.UserID); err != nil {
		return profileDatabaseFailure("delete profile snapshots")
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM preparation_profiles
		WHERE owner_user_id = $1
	`, command.UserID); err != nil {
		return profileDatabaseFailure("delete profiles")
	}
	if err := tx.Commit(ctx); err != nil {
		return profileDatabaseFailure("commit profile data deletion")
	}
	return nil
}

type preparationRowScanner interface {
	Scan(...any) error
}

func scanPreparationProfile(row preparationRowScanner) (Profile, error) {
	var profile Profile
	err := row.Scan(
		&profile.ID,
		&profile.UserID,
		&profile.ResumeRef,
		&profile.JobDescriptionRef,
		&profile.BackgroundSummary,
		&profile.Version,
		&profile.UpdatedAt,
	)
	profile.UpdatedAt = profile.UpdatedAt.UTC()
	return profile, err
}

func scanPreparationSnapshot(row preparationRowScanner) (Snapshot, error) {
	var snapshot Snapshot
	err := row.Scan(
		&snapshot.ID,
		&snapshot.SourceProfileID,
		&snapshot.SourceVersion,
		&snapshot.ResumeSnapshot,
		&snapshot.JobDescriptionSnapshot,
		&snapshot.BackgroundSnapshot,
		&snapshot.CreatedAt,
	)
	snapshot.CreatedAt = snapshot.CreatedAt.UTC()
	return snapshot, err
}

func replayProfile(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent IdempotencyIntent,
) (Profile, bool, error) {
	body, resourceID, found, err := readPreparationReplay(
		ctx,
		tx,
		userID,
		intent,
		"profile",
	)
	if err != nil || !found {
		return Profile{}, found, err
	}
	var profile Profile
	if err := json.Unmarshal(body, &profile); err != nil ||
		profile.ID != resourceID ||
		profile.UserID != userID ||
		!validResourceIdentifier(profile.ID) {
		return Profile{}, false, profileDatabaseFailure(
			"decode profile replay",
		)
	}
	profile.UpdatedAt = profile.UpdatedAt.UTC()
	return profile, true, nil
}

func replaySnapshot(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent IdempotencyIntent,
) (Snapshot, bool, error) {
	body, resourceID, found, err := readPreparationReplay(
		ctx,
		tx,
		userID,
		intent,
		"snapshot",
	)
	if err != nil || !found {
		return Snapshot{}, found, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(body, &snapshot); err != nil ||
		snapshot.ID != resourceID ||
		!validResourceIdentifier(snapshot.ID) ||
		!validResourceIdentifier(snapshot.SourceProfileID) ||
		snapshot.SourceVersion < 1 {
		return Snapshot{}, false, profileDatabaseFailure(
			"decode snapshot replay",
		)
	}
	snapshot.CreatedAt = snapshot.CreatedAt.UTC()
	return snapshot, true, nil
}

func readPreparationReplay(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent IdempotencyIntent,
	expectedKind string,
) ([]byte, string, bool, error) {
	var (
		fingerprint  []byte
		resourceKind string
		resourceID   string
		status       int
		body         []byte
	)
	err := tx.QueryRow(ctx, `
		SELECT
			payload_fingerprint,
			resource_kind,
			resource_id,
			response_status,
			response_body
		FROM preparation_idempotency_records
		WHERE owner_user_id = $1
		  AND method = $2
		  AND canonical_path = $3
		  AND idempotency_key = $4
	`, userID, intent.Method, intent.CanonicalPath, intent.Key).Scan(
		&fingerprint,
		&resourceKind,
		&resourceID,
		&status,
		&body,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, profileDatabaseFailure(
			"read idempotency replay",
		)
	}
	if !bytes.Equal(fingerprint, intent.PayloadFingerprint[:]) ||
		resourceKind != expectedKind {
		return nil, "", false, ErrProfileIdempotencyConflict
	}
	if status != 201 || !validResourceIdentifier(resourceID) {
		return nil, "", false, profileDatabaseFailure(
			"validate idempotency replay",
		)
	}
	return body, resourceID, true, nil
}

func persistPreparationIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent IdempotencyIntent,
	resourceKind string,
	resourceID string,
	response any,
) error {
	body, err := json.Marshal(response)
	if err != nil {
		return profileDatabaseFailure("encode idempotency response")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO preparation_idempotency_records (
			owner_user_id,
			method,
			canonical_path,
			idempotency_key,
			payload_fingerprint,
			resource_kind,
			resource_id,
			response_status,
			response_body
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 201, $8)
	`,
		userID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
		intent.PayloadFingerprint[:],
		resourceKind,
		resourceID,
		body,
	)
	if err != nil {
		return classifyPreparationWriteError(err)
	}
	return nil
}

func lockActivePreparationActor(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
) error {
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT true
		FROM identity_users AS owner
		WHERE owner.id = $1
		  AND owner.account_status = 'active'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM preparation_deletion_fences AS fence
		      WHERE fence.owner_user_id = owner.id
		  )
		FOR SHARE OF owner
	`, userID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProfileNotFound
	}
	if err != nil {
		return profileDatabaseFailure("lock active preparation actor")
	}
	return nil
}

func lockPreparationIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent IdempotencyIntent,
) error {
	lockDocument, err := json.Marshal([]string{
		userID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
	})
	if err != nil {
		return profileDatabaseFailure("encode idempotency lock")
	}
	if _, err := tx.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		string(lockDocument),
	); err != nil {
		return profileDatabaseFailure("lock profile idempotency")
	}
	return nil
}

func validPreparationActor(actor requestcontext.Actor) bool {
	return actor.Valid() &&
		validPreparationUUID(actor.UserID) &&
		validPreparationUUID(actor.SessionID)
}

func validPreparationUUID(value string) bool {
	var identifier pgtype.UUID
	return identifier.Scan(value) == nil && identifier.Valid
}

func validPreparationIntent(
	intent IdempotencyIntent,
	expectedPath string,
	payload any,
) bool {
	if intent.Method != "POST" ||
		intent.CanonicalPath != expectedPath ||
		!validCanonicalPath(intent.CanonicalPath) ||
		!validIdempotencyKey(intent.Key) {
		return false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	expectedFingerprint := sha256.Sum256(encoded)
	return bytes.Equal(
		intent.PayloadFingerprint[:],
		expectedFingerprint[:],
	)
}

func nullablePreparationText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func rollbackPreparationTransaction(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func classifyPreparationWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrProfileNotFound
		case "23505":
			return ErrProfileConflict
		}
	}
	return profileDatabaseFailure("write preparation data")
}

func profileDatabaseFailure(operation string) error {
	return fmt.Errorf("%w: %s", ErrProfileRepository, operation)
}

var _ ProfileRepository = (*PostgresProfileRepository)(nil)
