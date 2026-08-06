package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationmodel "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
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

func (r *PostgresProfileRepository) ReplayProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	intent IdempotencyIntent,
) (Profile, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		intent.Method != "POST" ||
		intent.CanonicalPath != "/v1/preparation-profiles" ||
		!validIdempotencyKey(intent.Key) {
		return Profile{}, false, ErrProfileInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, false, profileDatabaseFailure(
			"begin profile replay",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActivePreparationActor(ctx, tx, actor.UserID); err != nil {
		return Profile{}, false, err
	}
	profile, found, err := replayProfile(ctx, tx, actor.UserID, intent)
	if err != nil {
		return Profile{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, false, profileDatabaseFailure(
			"commit profile replay",
		)
	}
	return profile, found, nil
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
		!validCreateProfileContext(command) ||
		!validCreateProfileResume(command) ||
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
	if command.Request.JobTargetID != "" {
		if err := lockConfirmedProfileJobTarget(
			ctx,
			tx,
			actor.UserID,
			command.Request.JobTargetID,
			command.Request.JobTargetConfirmationVersion,
		); err != nil {
			return Profile{}, false, err
		}
	}

	profile := Profile{
		ID:                           command.ProfileID,
		UserID:                       actor.UserID,
		ResumeID:                     command.Request.ResumeID,
		ResumeRevision:               command.Request.ResumeRevision,
		JobDescriptionRef:            command.Request.JobDescriptionRef,
		BackgroundSummary:            command.Request.BackgroundSummary,
		JobTargetID:                  command.Request.JobTargetID,
		JobTargetConfirmationVersion: command.Request.JobTargetConfirmationVersion,
		Context:                      command.Context,
		Version:                      1,
	}
	if profile.BackgroundSummary == "" && profile.Context != nil &&
		profile.Context.Scenario != nil {
		profile.BackgroundSummary = profile.Context.Scenario.Situation
	}
	resumeMaterial, err := encodeResumeMaterial(command.ResumeRevision)
	if err != nil {
		return Profile{}, false, err
	}
	encodedContext, err := encodePreparationContext(profile.Context)
	if err != nil {
		return Profile{}, false, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_profiles (
			owner_user_id,
			profile_id,
				resume_id,
				resume_revision,
				resume_material,
				job_description_ref,
				background_summary,
				job_target_id,
				job_target_confirmation_version,
				preparation_kind,
				preparation_context
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING version, updated_at
	`,
		actor.UserID,
		profile.ID,
		nullablePreparationText(profile.ResumeID),
		nullablePreparationRevision(profile.ResumeRevision),
		resumeMaterial,
		nullablePreparationText(profile.JobDescriptionRef),
		profile.BackgroundSummary,
		nullablePreparationText(profile.JobTargetID),
		nullablePreparationVersion(
			profile.JobTargetConfirmationVersion,
		),
		nullablePreparationKind(profile.Context),
		encodedContext,
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

func (r *PostgresProfileRepository) ReplaySnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	intent IdempotencyIntent,
) (Snapshot, bool, error) {
	const prefix = "/v1/preparation-profiles/"
	const suffix = "/snapshots"
	profileID := strings.TrimSuffix(
		strings.TrimPrefix(intent.CanonicalPath, prefix),
		suffix,
	)
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		intent.Method != "POST" ||
		!strings.HasPrefix(intent.CanonicalPath, prefix) ||
		!strings.HasSuffix(intent.CanonicalPath, suffix) ||
		!validResourceIdentifier(profileID) ||
		!validIdempotencyKey(intent.Key) {
		return Snapshot{}, false, ErrProfileInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Snapshot{}, false, profileDatabaseFailure(
			"begin snapshot replay",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActivePreparationActor(ctx, tx, actor.UserID); err != nil {
		return Snapshot{}, false, err
	}
	snapshot, found, err := replaySnapshot(ctx, tx, actor.UserID, intent)
	if err != nil {
		return Snapshot{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, false, profileDatabaseFailure(
			"commit snapshot replay",
		)
	}
	return snapshot, found, nil
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
	var resumeID string
	var resumeRevision int64
	var resumeMaterial, targetInput, targetCandidate, preparationContext []byte
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE(profile.resume_id::text, ''),
			COALESCE(profile.resume_revision, 0),
			profile.resume_material,
			COALESCE(profile.job_description_ref, ''),
			profile.background_summary,
			profile.version,
			COALESCE(profile.job_target_id, ''),
			COALESCE(profile.job_target_confirmation_version, 0),
			confirmation.input_snapshot,
			confirmation.candidate,
			profile.preparation_context
		FROM preparation_profiles AS profile
		LEFT JOIN preparation_job_target_confirmations AS confirmation
		  ON confirmation.owner_user_id = profile.owner_user_id
		 AND confirmation.target_id = profile.job_target_id
		 AND confirmation.confirmation_version =
		     profile.job_target_confirmation_version
		WHERE profile.owner_user_id = $1
		  AND profile.profile_id = $2
		FOR SHARE OF profile
	`, actor.UserID, command.ProfileID).Scan(
		&resumeID,
		&resumeRevision,
		&resumeMaterial,
		&snapshot.JobDescriptionSnapshot,
		&snapshot.BackgroundSnapshot,
		&sourceVersion,
		&snapshot.SourceJobTargetID,
		&snapshot.SourceJobTargetConfirmationVersion,
		&targetInput,
		&targetCandidate,
		&preparationContext,
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
	if len(preparationContext) > 0 {
		if json.Unmarshal(preparationContext, &snapshot.Context) != nil ||
			snapshot.Context == nil || !snapshot.Context.ValidShape() {
			return Snapshot{}, false, profileDatabaseFailure(
				"decode profile Preparation context",
			)
		}
	}
	if resumeID != "" {
		var material ResumeMaterial
		if len(resumeMaterial) == 0 ||
			json.Unmarshal(resumeMaterial, &material) != nil {
			return Snapshot{}, false, profileDatabaseFailure(
				"decode profile Resume material",
			)
		}
		resumeSnapshot := ResumeRevisionSnapshot{
			ResumeID: resumeID,
			Revision: resumeRevision,
			Material: material,
		}
		if !validResumeRevisionSnapshot(resumeSnapshot) {
			return Snapshot{}, false, profileDatabaseFailure(
				"validate profile Resume material",
			)
		}
		snapshot.ResumeSnapshot = &resumeSnapshot
	}
	if snapshot.SourceJobTargetID != "" {
		if len(targetInput) == 0 || len(targetCandidate) == 0 {
			return Snapshot{}, false, ErrProfileConflict
		}
		var input JobTargetInput
		var candidate JobTargetCandidate
		if err := json.Unmarshal(targetInput, &input); err != nil ||
			json.Unmarshal(targetCandidate, &candidate) != nil ||
			!validJobTargetInput(input) ||
			!validJobTargetCandidateShape(candidate, input.Source) {
			return Snapshot{}, false, profileDatabaseFailure(
				"decode confirmed job target snapshot",
			)
		}
		snapshot.JobTargetInputSnapshot = &input
		snapshot.JobTargetCandidateSnapshot = &candidate
	}

	resumeSnapshotID, resumeSnapshotRevision, encodedResumeMaterial, err :=
		encodeSnapshotResume(snapshot.ResumeSnapshot)
	if err != nil {
		return Snapshot{}, false, err
	}
	encodedTargetInput, encodedTargetCandidate, err :=
		encodeSnapshotJobTarget(snapshot)
	if err != nil {
		return Snapshot{}, false, err
	}
	encodedContext, err := encodePreparationContext(snapshot.Context)
	if err != nil {
		return Snapshot{}, false, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_snapshots (
			owner_user_id,
			snapshot_id,
			source_profile_id,
			source_version,
				resume_id,
				resume_revision,
				resume_material,
				job_description_snapshot,
				background_snapshot,
				source_job_target_id,
				source_job_target_confirmation_version,
				job_target_input_snapshot,
				job_target_candidate_snapshot,
				preparation_kind,
				preparation_context
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
				$12, $13, $14, $15
			)
		RETURNING created_at
	`,
		actor.UserID,
		snapshot.ID,
		snapshot.SourceProfileID,
		snapshot.SourceVersion,
		resumeSnapshotID,
		resumeSnapshotRevision,
		encodedResumeMaterial,
		nullablePreparationText(snapshot.JobDescriptionSnapshot),
		snapshot.BackgroundSnapshot,
		nullablePreparationText(snapshot.SourceJobTargetID),
		nullablePreparationVersion(
			snapshot.SourceJobTargetConfirmationVersion,
		),
		encodedTargetInput,
		encodedTargetCandidate,
		nullablePreparationKind(snapshot.Context),
		encodedContext,
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
			COALESCE(profile.resume_id::text, ''),
			COALESCE(profile.resume_revision, 0),
				COALESCE(profile.job_description_ref, ''),
				profile.background_summary,
				COALESCE(profile.job_target_id, ''),
				COALESCE(profile.job_target_confirmation_version, 0),
				profile.version,
			profile.updated_at,
			profile.preparation_context
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
			COALESCE(snapshot.resume_id::text, ''),
			COALESCE(snapshot.resume_revision, 0),
			snapshot.resume_material,
				COALESCE(snapshot.job_description_snapshot, ''),
				snapshot.background_snapshot,
				COALESCE(snapshot.source_job_target_id, ''),
				COALESCE(
				    snapshot.source_job_target_confirmation_version,
				    0
				),
				snapshot.job_target_input_snapshot,
				snapshot.job_target_candidate_snapshot,
				snapshot.created_at,
				snapshot.preparation_context
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
		DELETE FROM preparation_practice_plans
		WHERE owner_user_id = $1
	`, command.UserID); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) &&
			(postgresError.Code == "23001" || postgresError.Code == "23503") {
			return ErrProfileConflict
		}
		return profileDatabaseFailure("delete Preparation Plans")
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
	if _, err := tx.Exec(ctx, `
		DELETE FROM preparation_job_target_idempotency_records
		WHERE owner_user_id = $1
	`, command.UserID); err != nil {
		return profileDatabaseFailure(
			"delete job target idempotency records",
		)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM preparation_job_targets
		WHERE owner_user_id = $1
	`, command.UserID); err != nil {
		return profileDatabaseFailure("delete job targets")
	}
	if err := tx.Commit(ctx); err != nil {
		return profileDatabaseFailure("commit profile data deletion")
	}
	return nil
}

type preparationRowScanner interface {
	Scan(...any) error
}

type preparationQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanPreparationProfile(row preparationRowScanner) (Profile, error) {
	var profile Profile
	var preparationContext []byte
	err := row.Scan(
		&profile.ID,
		&profile.UserID,
		&profile.ResumeID,
		&profile.ResumeRevision,
		&profile.JobDescriptionRef,
		&profile.BackgroundSummary,
		&profile.JobTargetID,
		&profile.JobTargetConfirmationVersion,
		&profile.Version,
		&profile.UpdatedAt,
		&preparationContext,
	)
	if err == nil && len(preparationContext) > 0 {
		if json.Unmarshal(preparationContext, &profile.Context) != nil ||
			profile.Context == nil || !profile.Context.ValidShape() {
			return Profile{}, ErrProfileRepository
		}
	}
	profile.UpdatedAt = profile.UpdatedAt.UTC()
	return profile, err
}

func scanPreparationSnapshot(row preparationRowScanner) (Snapshot, error) {
	var snapshot Snapshot
	var resumeID string
	var resumeRevision int64
	var resumeMaterial, targetInput, targetCandidate, preparationContext []byte
	err := row.Scan(
		&snapshot.ID,
		&snapshot.SourceProfileID,
		&snapshot.SourceVersion,
		&resumeID,
		&resumeRevision,
		&resumeMaterial,
		&snapshot.JobDescriptionSnapshot,
		&snapshot.BackgroundSnapshot,
		&snapshot.SourceJobTargetID,
		&snapshot.SourceJobTargetConfirmationVersion,
		&targetInput,
		&targetCandidate,
		&snapshot.CreatedAt,
		&preparationContext,
	)
	if err == nil && len(preparationContext) > 0 {
		if json.Unmarshal(preparationContext, &snapshot.Context) != nil ||
			snapshot.Context == nil || !snapshot.Context.ValidShape() {
			return Snapshot{}, ErrProfileRepository
		}
	}
	if err == nil && resumeID != "" {
		var material ResumeMaterial
		if len(resumeMaterial) == 0 ||
			json.Unmarshal(resumeMaterial, &material) != nil {
			return Snapshot{}, ErrProfileRepository
		}
		snapshot.ResumeSnapshot = &ResumeRevisionSnapshot{
			ResumeID: resumeID,
			Revision: resumeRevision,
			Material: material,
		}
		if !validResumeRevisionSnapshot(*snapshot.ResumeSnapshot) {
			return Snapshot{}, ErrProfileRepository
		}
	}
	if err == nil && snapshot.SourceJobTargetID != "" {
		var input JobTargetInput
		var candidate JobTargetCandidate
		if len(targetInput) == 0 || len(targetCandidate) == 0 ||
			json.Unmarshal(targetInput, &input) != nil ||
			json.Unmarshal(targetCandidate, &candidate) != nil {
			return Snapshot{}, ErrProfileRepository
		}
		snapshot.JobTargetInputSnapshot = &input
		snapshot.JobTargetCandidateSnapshot = &candidate
	}
	snapshot.CreatedAt = snapshot.CreatedAt.UTC()
	return snapshot, err
}

func replayProfile(
	ctx context.Context,
	query preparationQueryRow,
	userID string,
	intent IdempotencyIntent,
) (Profile, bool, error) {
	body, resourceID, found, err := readPreparationReplay(
		ctx,
		query,
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
		!validResourceIdentifier(profile.ID) ||
		((profile.ResumeID == "") != (profile.ResumeRevision == 0)) ||
		((profile.JobTargetID == "") !=
			(profile.JobTargetConfirmationVersion == 0)) ||
		(profile.Context != nil && !profile.Context.ValidShape()) {
		return Profile{}, false, profileDatabaseFailure(
			"decode profile replay",
		)
	}
	profile.UpdatedAt = profile.UpdatedAt.UTC()
	return profile, true, nil
}

func replaySnapshot(
	ctx context.Context,
	query preparationQueryRow,
	userID string,
	intent IdempotencyIntent,
) (Snapshot, bool, error) {
	body, resourceID, found, err := readPreparationReplay(
		ctx,
		query,
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
		snapshot.SourceVersion < 1 ||
		(snapshot.ResumeSnapshot != nil &&
			!validResumeRevisionSnapshot(*snapshot.ResumeSnapshot)) ||
		((snapshot.SourceJobTargetID == "") !=
			(snapshot.SourceJobTargetConfirmationVersion == 0)) ||
		(snapshot.Context != nil && !snapshot.Context.ValidShape()) {
		return Snapshot{}, false, profileDatabaseFailure(
			"decode snapshot replay",
		)
	}
	snapshot.CreatedAt = snapshot.CreatedAt.UTC()
	return snapshot, true, nil
}

func readPreparationReplay(
	ctx context.Context,
	query preparationQueryRow,
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
	err := query.QueryRow(ctx, `
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

func lockConfirmedProfileJobTarget(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	targetID string,
	confirmationVersion int,
) error {
	var confirmed bool
	err := tx.QueryRow(ctx, `
		SELECT true
		FROM preparation_job_targets AS target
		JOIN preparation_job_target_confirmations AS confirmation
		  ON confirmation.owner_user_id = target.owner_user_id
		 AND confirmation.target_id = target.target_id
		 AND confirmation.input_version = target.input_version
		WHERE target.owner_user_id = $1
		  AND target.target_id = $2
		  AND confirmation.confirmation_version = $3
		  AND confirmation.input_snapshot IS NOT NULL
		  AND target.stage = 'confirmed'
		FOR SHARE OF target, confirmation
	`, userID, targetID, confirmationVersion).Scan(&confirmed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProfileNotFound
	}
	if err != nil {
		return profileDatabaseFailure("lock confirmed job target")
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

func nullablePreparationVersion(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullablePreparationRevision(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func validCreateProfileResume(command CreateProfileCommand) bool {
	if command.Request.ResumeID == "" {
		return command.Request.ResumeRevision == 0 &&
			command.ResumeRevision == nil
	}
	return command.ResumeRevision != nil &&
		command.ResumeRevision.ResumeID == command.Request.ResumeID &&
		command.ResumeRevision.Revision == command.Request.ResumeRevision &&
		validResumeRevisionSnapshot(*command.ResumeRevision)
}

func encodeResumeMaterial(snapshot *ResumeRevisionSnapshot) (any, error) {
	if snapshot == nil {
		return nil, nil
	}
	if !validResumeRevisionSnapshot(*snapshot) {
		return nil, ErrProfileInvalid
	}
	encoded, err := json.Marshal(snapshot.Material)
	if err != nil {
		return nil, ErrProfileRepository
	}
	return encoded, nil
}

func encodeSnapshotResume(
	snapshot *ResumeRevisionSnapshot,
) (any, any, any, error) {
	if snapshot == nil {
		return nil, nil, nil, nil
	}
	if !validResumeRevisionSnapshot(*snapshot) {
		return nil, nil, nil, ErrProfileConflict
	}
	encoded, err := json.Marshal(snapshot.Material)
	if err != nil {
		return nil, nil, nil, ErrProfileRepository
	}
	return snapshot.ResumeID, snapshot.Revision, encoded, nil
}

func encodeSnapshotJobTarget(snapshot Snapshot) (any, any, error) {
	if snapshot.SourceJobTargetID == "" &&
		snapshot.SourceJobTargetConfirmationVersion == 0 &&
		snapshot.JobTargetInputSnapshot == nil &&
		snapshot.JobTargetCandidateSnapshot == nil {
		return nil, nil, nil
	}
	if !targetedPreparationSnapshot(snapshot) {
		return nil, nil, ErrProfileConflict
	}
	input, err := json.Marshal(snapshot.JobTargetInputSnapshot)
	if err != nil {
		return nil, nil, ErrProfileRepository
	}
	candidate, err := json.Marshal(snapshot.JobTargetCandidateSnapshot)
	if err != nil {
		return nil, nil, ErrProfileRepository
	}
	return input, candidate, nil
}

func encodePreparationContext(
	context *preparationmodel.ResolvedContext,
) ([]byte, error) {
	if context == nil {
		return nil, nil
	}
	if !context.ValidShape() {
		return nil, ErrProfileInvalid
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return nil, ErrProfileRepository
	}
	return encoded, nil
}

func validCreateProfileContext(command CreateProfileCommand) bool {
	if command.Request.Kind == "" {
		return command.Context == nil
	}
	return command.Context != nil && command.Context.ValidShape() &&
		command.Context.Kind == command.Request.Kind
}

func nullablePreparationKind(context *preparationmodel.ResolvedContext) any {
	if context == nil || !context.ValidShape() {
		return nil
	}
	return context.Kind
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
