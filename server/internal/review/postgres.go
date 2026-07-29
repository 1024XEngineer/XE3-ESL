package review

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) EnsurePending(
	ctx context.Context,
	command EnsureReviewCommand,
) (FormalReview, error) {
	if r == nil || r.pool == nil {
		return FormalReview{}, ErrInvalidReview
	}
	if err := command.validate(); err != nil {
		return FormalReview{}, err
	}
	var contextJSON []byte
	if command.ImplementationVersion == "qianwen-scenario-review-v2" {
		var err error
		contextJSON, err = command.EvaluationContext.CanonicalJSON(
			DefaultPolicyRegistry(),
		)
		if err != nil {
			return FormalReview{}, ErrInvalidReview
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FormalReview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockReviewUser(ctx, tx, command.Actor.UserID); err != nil {
		return FormalReview{}, err
	}
	if err := lockActiveIdentityUser(
		ctx,
		tx,
		command.Actor.UserID,
		command.Actor.DeletionGeneration,
	); err != nil {
		return FormalReview{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO reviews (
			owner_user_id,
			practice_session_id,
			implementation_version,
			source_turn_id,
			source_turn_version,
			source_manifest_fingerprint,
			evaluation_context,
			deletion_generation,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending')
		ON CONFLICT (
			owner_user_id,
			practice_session_id
		) DO NOTHING
	`, command.Actor.UserID, command.PracticeSessionID,
		command.ImplementationVersion, command.SourceTurnID,
		command.SourceTurnVersion, command.SourceManifestFingerprint,
		contextJSON, command.Actor.DeletionGeneration)
	if err != nil {
		return FormalReview{}, err
	}

	row := tx.QueryRow(ctx, reviewSelect+`
		WHERE owner_user_id = $1
		  AND practice_session_id = $2
	`, command.Actor.UserID, command.PracticeSessionID)
	review, err := scanReview(row)
	if err != nil {
		return FormalReview{}, err
	}
	if review.DeletionGeneration != command.Actor.DeletionGeneration {
		return FormalReview{}, ErrAccountDeleted
	}
	if review.ImplementationVersion != command.ImplementationVersion {
		return FormalReview{}, ErrReviewImplementationConflict
	}
	if review.SourceTurnID != command.SourceTurnID ||
		review.SourceTurnVersion != command.SourceTurnVersion ||
		review.SourceManifestFingerprint != command.SourceManifestFingerprint ||
		(command.ImplementationVersion == "qianwen-scenario-review-v2" &&
			!sameEvaluationContext(
				review.EvaluationContext,
				command.EvaluationContext,
			)) {
		return FormalReview{}, ErrReviewSourceConflict
	}
	if review.Status == FormalReviewCompleted {
		evidence, err := listEvidence(
			ctx,
			tx,
			review.ID,
			command.Actor.UserID,
		)
		if err != nil {
			return FormalReview{}, err
		}
		review.Evidence = evidence
	}
	if err := tx.Commit(ctx); err != nil {
		return FormalReview{}, err
	}
	return review, nil
}

func (r *PostgresRepository) ClaimGeneration(
	ctx context.Context,
	actor Actor,
	reviewID string,
	lease time.Duration,
) (FormalReview, GenerationJobContext, bool, error) {
	if r == nil || r.pool == nil || actor.validate() != nil ||
		strings.TrimSpace(reviewID) == "" || lease <= 0 {
		return FormalReview{}, GenerationJobContext{}, false, ErrInvalidReview
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FormalReview{}, GenerationJobContext{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockReviewUser(ctx, tx, actor.UserID); err != nil {
		return FormalReview{}, GenerationJobContext{}, false, err
	}
	if err := lockActiveIdentityUser(
		ctx,
		tx,
		actor.UserID,
		actor.DeletionGeneration,
	); err != nil {
		return FormalReview{}, GenerationJobContext{}, false, err
	}

	review, err := scanReview(tx.QueryRow(ctx, reviewSelect+`
		WHERE id = $1 AND owner_user_id = $2
		FOR UPDATE
	`, reviewID, actor.UserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return FormalReview{}, GenerationJobContext{}, false, ErrReviewNotFound
	}
	if err != nil {
		return FormalReview{}, GenerationJobContext{}, false, err
	}
	if review.DeletionGeneration != actor.DeletionGeneration {
		return FormalReview{}, GenerationJobContext{}, false, ErrAccountDeleted
	}
	if review.Status == FormalReviewCompleted {
		evidence, err := listEvidence(ctx, tx, review.ID, actor.UserID)
		if err != nil {
			return FormalReview{}, GenerationJobContext{}, false, err
		}
		review.Evidence = evidence
		if err := tx.Commit(ctx); err != nil {
			return FormalReview{}, GenerationJobContext{}, false, err
		}
		return review, GenerationJobContext{}, false, nil
	}
	if review.Status == FormalReviewFailed &&
		terminalGenerationCategory(review.StableErrorCategory) {
		if err := tx.Commit(ctx); err != nil {
			return FormalReview{}, GenerationJobContext{}, false, err
		}
		return review, GenerationJobContext{}, false, nil
	}

	var runningID string
	var leaseUntil time.Time
	var leaseValid bool
	err = tx.QueryRow(ctx, `
		SELECT id::text, lease_until, lease_until > now()
		FROM review_generation_attempts
		WHERE review_id = $1
		  AND owner_user_id = $2
		  AND status = 'running'
		ORDER BY attempt_number DESC
		LIMIT 1
		FOR UPDATE
	`, review.ID, actor.UserID).Scan(
		&runningID,
		&leaseUntil,
		&leaseValid,
	)
	switch {
	case err == nil && leaseValid:
		if err := tx.Commit(ctx); err != nil {
			return FormalReview{}, GenerationJobContext{}, false, err
		}
		return review, GenerationJobContext{}, false, nil
	case err == nil:
		if _, err := tx.Exec(ctx, `
			UPDATE review_generation_attempts
			SET status = 'failed',
			    stable_error_category = 'lease_expired',
			    finished_at = now()
			WHERE id = $1
			  AND owner_user_id = $2
		`, runningID, actor.UserID); err != nil {
			return FormalReview{}, GenerationJobContext{}, false, err
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return FormalReview{}, GenerationJobContext{}, false, err
	}

	var claim GenerationJobContext
	err = tx.QueryRow(ctx, `
		INSERT INTO review_generation_attempts (
			review_id,
			owner_user_id,
			attempt_number,
			deletion_generation,
			status,
			lease_until
		)
		SELECT
			$1,
			$2,
			coalesce(max(attempt_number), 0) + 1,
			$3,
			'running',
			now() + make_interval(secs => $4)
		FROM review_generation_attempts
		WHERE review_id = $1 AND owner_user_id = $2
		RETURNING
			id::text,
			review_id::text,
			owner_user_id,
			worker_token::text,
			deletion_generation,
			attempt_number,
			lease_until
	`, review.ID, actor.UserID, actor.DeletionGeneration, lease.Seconds()).Scan(
		&claim.AttemptID,
		&claim.ReviewID,
		&claim.OwnerUserID,
		&claim.WorkerToken,
		&claim.DeletionGeneration,
		&claim.AttemptNumber,
		&claim.LeaseUntil,
	)
	if err != nil {
		return FormalReview{}, GenerationJobContext{}, false, err
	}

	err = tx.QueryRow(ctx, `
		UPDATE reviews
		SET status = 'generating',
		    stable_error_category = NULL,
		    updated_at = now()
		WHERE id = $1
		  AND owner_user_id = $2
		  AND deletion_generation = $3
		RETURNING status, updated_at
	`, review.ID, actor.UserID, actor.DeletionGeneration).Scan(
		&review.Status,
		&review.UpdatedAt,
	)
	if err != nil {
		return FormalReview{}, GenerationJobContext{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FormalReview{}, GenerationJobContext{}, false, err
	}
	return review, claim, true, nil
}

func (r *PostgresRepository) CompleteGeneration(
	ctx context.Context,
	claim GenerationJobContext,
	result ReviewResult,
	evidence []ReviewEvidence,
) (FormalReview, error) {
	if r == nil || r.pool == nil || errInvalidClaim(claim) {
		return FormalReview{}, ErrInvalidReview
	}
	if err := validateCompletionPayload(result, evidence); err != nil {
		return FormalReview{}, err
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return FormalReview{}, ErrInvalidReview
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FormalReview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockReviewUser(ctx, tx, claim.OwnerUserID); err != nil {
		return FormalReview{}, err
	}
	if err := lockActiveIdentityUser(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.DeletionGeneration,
	); err != nil {
		return FormalReview{}, err
	}

	var attemptStatus string
	var persistedGeneration int64
	var leaseUntil time.Time
	var leaseValid bool
	var latestAttemptID string
	err = tx.QueryRow(ctx, `
		SELECT
			attempt.status,
			attempt.deletion_generation,
			attempt.lease_until,
			attempt.lease_until > now(),
			(
				SELECT latest.id::text
				FROM review_generation_attempts latest
				WHERE latest.review_id = attempt.review_id
				  AND latest.owner_user_id = attempt.owner_user_id
				ORDER BY latest.attempt_number DESC
				LIMIT 1
			)
		FROM review_generation_attempts attempt
		JOIN reviews review
		  ON review.id = attempt.review_id
		 AND review.owner_user_id = attempt.owner_user_id
		WHERE attempt.id = $1
		  AND attempt.review_id = $2
		  AND attempt.owner_user_id = $3
		  AND attempt.worker_token = $4
		FOR UPDATE OF attempt, review
	`, claim.AttemptID, claim.ReviewID, claim.OwnerUserID,
		claim.WorkerToken).Scan(
		&attemptStatus,
		&persistedGeneration,
		&leaseUntil,
		&leaseValid,
		&latestAttemptID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return FormalReview{}, ErrGenerationClaimLost
	}
	if err != nil {
		return FormalReview{}, err
	}
	if attemptStatus != "running" ||
		persistedGeneration != claim.DeletionGeneration ||
		latestAttemptID != claim.AttemptID ||
		!leaseValid {
		return FormalReview{}, ErrGenerationClaimLost
	}

	for _, item := range evidence {
		item = normalizeReviewEvidence(item)
		var snapshot any
		if len(item.Snapshot) > 0 {
			if !json.Valid(item.Snapshot) {
				return FormalReview{}, ErrInvalidReview
			}
			snapshot = item.Snapshot
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO review_evidence (
				review_id,
				owner_user_id,
				target_kind,
				target_key,
				source_type,
				source_id,
				source_version,
				field,
				anchor_kind,
				quote,
				start_utf8_byte,
				end_utf8_byte,
				source_checksum,
				evidence_snapshot
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9,
				$10, $11, $12, nullif($13, ''), $14
			)
			ON CONFLICT (
				review_id,
				target_kind,
				target_key,
				source_type,
				source_id,
				source_version,
				field,
				anchor_kind,
				quote
			) DO NOTHING
		`, claim.ReviewID, claim.OwnerUserID, item.TargetKind,
			item.TargetKey, item.SourceType, item.SourceID,
			item.SourceVersion, item.Field, item.AnchorKind, item.Quote,
			item.StartUTF8Byte, item.EndUTF8Byte, item.Checksum, snapshot)
		if err != nil {
			return FormalReview{}, err
		}
	}

	commandTag, err := tx.Exec(ctx, `
		UPDATE reviews
		SET status = 'completed',
		    result = $2,
		    stable_error_category = NULL,
		    updated_at = now(),
		    completed_at = now()
		WHERE id = $1
		  AND owner_user_id = $3
		  AND deletion_generation = $4
		  AND status = 'generating'
	`, claim.ReviewID, resultJSON, claim.OwnerUserID,
		claim.DeletionGeneration)
	if err != nil {
		return FormalReview{}, err
	}
	if commandTag.RowsAffected() != 1 {
		return FormalReview{}, ErrGenerationClaimLost
	}
	commandTag, err = tx.Exec(ctx, `
		UPDATE review_generation_attempts
		SET status = 'succeeded',
		    finished_at = now()
		WHERE id = $1
		  AND review_id = $2
		  AND owner_user_id = $3
		  AND worker_token = $4
		  AND status = 'running'
	`, claim.AttemptID, claim.ReviewID, claim.OwnerUserID, claim.WorkerToken)
	if err != nil {
		return FormalReview{}, err
	}
	if commandTag.RowsAffected() != 1 {
		return FormalReview{}, ErrGenerationClaimLost
	}
	if err := tx.Commit(ctx); err != nil {
		return FormalReview{}, err
	}
	return r.Get(ctx, Actor{
		UserID:             claim.OwnerUserID,
		DeletionGeneration: claim.DeletionGeneration,
	}, claim.ReviewID)
}

func (r *PostgresRepository) FailGeneration(
	ctx context.Context,
	claim GenerationJobContext,
	stableErrorCategory string,
) error {
	category := strings.TrimSpace(stableErrorCategory)
	if r == nil || r.pool == nil || errInvalidClaim(claim) ||
		!validStableErrorCategory(category) {
		return ErrInvalidReview
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockReviewUser(ctx, tx, claim.OwnerUserID); err != nil {
		return err
	}
	if err := lockActiveIdentityUser(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.DeletionGeneration,
	); err != nil {
		return err
	}

	commandTag, err := tx.Exec(ctx, `
		UPDATE review_generation_attempts
		SET status = 'failed',
		    stable_error_category = $2,
		    finished_at = now()
		WHERE id = $1
		  AND review_id = $3
		  AND owner_user_id = $4
		  AND worker_token = $5
		  AND deletion_generation = $6
		  AND status = 'running'
		  AND lease_until > now()
		  AND id = (
			SELECT id
			FROM review_generation_attempts
			WHERE review_id = $3
			  AND owner_user_id = $4
			ORDER BY attempt_number DESC
			LIMIT 1
		  )
	`, claim.AttemptID, category, claim.ReviewID, claim.OwnerUserID,
		claim.WorkerToken, claim.DeletionGeneration)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return ErrGenerationClaimLost
	}
	commandTag, err = tx.Exec(ctx, `
		UPDATE reviews
		SET status = 'failed',
		    stable_error_category = $2,
		    updated_at = now()
		WHERE id = $1
		  AND owner_user_id = $3
		  AND status = 'generating'
	`, claim.ReviewID, category, claim.OwnerUserID)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return ErrGenerationClaimLost
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) Get(
	ctx context.Context,
	actor Actor,
	reviewID string,
) (FormalReview, error) {
	if r == nil || r.pool == nil || actor.validate() != nil ||
		strings.TrimSpace(reviewID) == "" {
		return FormalReview{}, ErrInvalidReview
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FormalReview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockReviewUser(ctx, tx, actor.UserID); err != nil {
		return FormalReview{}, err
	}
	if err := lockActiveIdentityUser(
		ctx,
		tx,
		actor.UserID,
		actor.DeletionGeneration,
	); err != nil {
		return FormalReview{}, err
	}
	review, err := scanReview(tx.QueryRow(ctx, reviewSelect+`
		WHERE id = $1
		  AND owner_user_id = $2
		  AND deletion_generation = $3
	`, reviewID, actor.UserID, actor.DeletionGeneration))
	if errors.Is(err, pgx.ErrNoRows) {
		return FormalReview{}, ErrReviewNotFound
	}
	if err != nil {
		return FormalReview{}, err
	}
	evidence, err := listEvidence(ctx, tx, review.ID, actor.UserID)
	if err != nil {
		return FormalReview{}, err
	}
	review.Evidence = evidence
	if err := tx.Commit(ctx); err != nil {
		return FormalReview{}, err
	}
	return review, nil
}

func (r *PostgresRepository) List(
	ctx context.Context,
	actor Actor,
) ([]FormalReview, error) {
	if r == nil || r.pool == nil || actor.validate() != nil {
		return nil, ErrInvalidReview
	}
	deleted, err := accountUnavailable(ctx, r.pool, actor.UserID)
	if err != nil {
		return nil, err
	}
	if deleted {
		return nil, ErrAccountDeleted
	}
	rows, err := r.pool.Query(ctx, reviewSelect+`
		WHERE owner_user_id = $1 AND deletion_generation = $2
		ORDER BY created_at DESC, id DESC
	`, actor.UserID, actor.DeletionGeneration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []FormalReview
	for rows.Next() {
		review, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reviews, nil
}

func (r *PostgresRepository) ListCompletedHistory(
	ctx context.Context,
	actor Actor,
	query HistoryQuery,
) (HistoryPage, error) {
	if r == nil || r.pool == nil || actor.validate() != nil ||
		query.Limit < 1 || query.Limit > MaxHistoryPageSize ||
		(query.Before != nil && !validHistoryCursor(*query.Before)) {
		return HistoryPage{}, ErrInvalidReview
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return HistoryPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockReviewUser(ctx, tx, actor.UserID); err != nil {
		return HistoryPage{}, err
	}
	if err := lockActiveIdentityUser(
		ctx,
		tx,
		actor.UserID,
		actor.DeletionGeneration,
	); err != nil {
		return HistoryPage{}, err
	}

	var beforeCreatedAt any
	var beforeReviewID any
	if query.Before != nil {
		beforeCreatedAt = query.Before.CreatedAt
		beforeReviewID = query.Before.ReviewID
	}
	rows, err := tx.Query(ctx, reviewSelect+`
		WHERE owner_user_id = $1
		  AND deletion_generation = $2
		  AND status = 'completed'
		  AND (
		      $3::timestamptz IS NULL
		      OR created_at < $3
		      OR (created_at = $3 AND id < $4::uuid)
		  )
		ORDER BY created_at DESC, id DESC
		LIMIT $5
	`, actor.UserID, actor.DeletionGeneration,
		beforeCreatedAt, beforeReviewID, query.Limit+1)
	if err != nil {
		return HistoryPage{}, err
	}
	defer rows.Close()

	items := make([]FormalReview, 0, query.Limit+1)
	for rows.Next() {
		item, scanErr := scanReview(rows)
		if scanErr != nil {
			return HistoryPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return HistoryPage{}, err
	}
	rows.Close()

	page := HistoryPage{Items: items}
	if len(page.Items) > query.Limit {
		page.Items = page.Items[:query.Limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &HistoryCursor{
			CreatedAt: last.CreatedAt,
			ReviewID:  last.ID,
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return HistoryPage{}, err
	}
	return page, nil
}

func (r *PostgresRepository) ListAttempts(
	ctx context.Context,
	actor Actor,
	reviewID string,
) ([]GenerationAttempt, error) {
	if _, err := r.Get(ctx, actor, reviewID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			review_id::text,
			attempt_number,
			status,
			coalesce(stable_error_category, ''),
			started_at,
			finished_at
		FROM review_generation_attempts
		WHERE review_id = $1 AND owner_user_id = $2
		ORDER BY attempt_number
	`, reviewID, actor.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []GenerationAttempt
	for rows.Next() {
		var attempt GenerationAttempt
		if err := rows.Scan(
			&attempt.ID,
			&attempt.ReviewID,
			&attempt.AttemptNumber,
			&attempt.Status,
			&attempt.StableErrorCategory,
			&attempt.StartedAt,
			&attempt.FinishedAt,
		); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func (r *PostgresRepository) DeleteUserData(
	ctx context.Context,
	command DeleteUserReviewsCommand,
) error {
	if r == nil || r.pool == nil ||
		!validUUID(command.UserID) ||
		command.DeletionGeneration <= 0 {
		return ErrInvalidReview
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockReviewUser(ctx, tx, command.UserID); err != nil {
		return err
	}
	var accountStatus string
	err = tx.QueryRow(ctx, `
		SELECT account_status
		FROM identity_users
		WHERE id = $1
		FOR UPDATE
	`, command.UserID).Scan(&accountStatus)
	identityMissing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !identityMissing {
		return err
	}
	if !identityMissing &&
		accountStatus != "deleting" &&
		accountStatus != "deleted" {
		return ErrInvalidReview
	}

	var persistedGeneration int64
	err = tx.QueryRow(ctx, `
		SELECT deletion_generation
		FROM review_deletion_fences
		WHERE owner_user_id = $1
		FOR UPDATE
	`, command.UserID).Scan(&persistedGeneration)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && command.DeletionGeneration < persistedGeneration {
		return ErrDeletionGenerationStale
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO review_deletion_fences (
			owner_user_id,
			deletion_generation,
			deleted_at
		)
		VALUES ($1, $2, now())
		ON CONFLICT (owner_user_id) DO UPDATE
		SET deletion_generation = EXCLUDED.deletion_generation,
		    deleted_at = least(
		        review_deletion_fences.deleted_at,
		        EXCLUDED.deleted_at
		    )
	`, command.UserID, command.DeletionGeneration); err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx,
		`DELETE FROM reviews WHERE owner_user_id = $1`,
		command.UserID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const reviewSelect = `
	SELECT
		id::text,
		owner_user_id,
		practice_session_id,
		implementation_version,
		source_turn_id,
		source_turn_version,
		source_manifest_fingerprint,
		evaluation_context,
		deletion_generation,
		status,
		result,
		coalesce(stable_error_category, ''),
		created_at,
		updated_at,
		completed_at
	FROM reviews
`

type rowScanner interface {
	Scan(dest ...any) error
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func scanReview(row rowScanner) (FormalReview, error) {
	var review FormalReview
	var resultJSON []byte
	var contextJSON []byte
	if err := row.Scan(
		&review.ID,
		&review.OwnerUserID,
		&review.PracticeSessionID,
		&review.ImplementationVersion,
		&review.SourceTurnID,
		&review.SourceTurnVersion,
		&review.SourceManifestFingerprint,
		&contextJSON,
		&review.DeletionGeneration,
		&review.Status,
		&resultJSON,
		&review.StableErrorCategory,
		&review.CreatedAt,
		&review.UpdatedAt,
		&review.CompletedAt,
	); err != nil {
		return FormalReview{}, err
	}
	if len(contextJSON) > 0 {
		if err := json.Unmarshal(contextJSON, &review.EvaluationContext); err != nil {
			return FormalReview{}, ErrInvalidReview
		}
		if err := review.EvaluationContext.Validate(
			DefaultPolicyRegistry(),
		); err != nil {
			return FormalReview{}, ErrInvalidReview
		}
	}
	if len(resultJSON) > 0 {
		var result ReviewResult
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return FormalReview{}, ErrInvalidReview
		}
		if err := validatePersistedReviewResult(result); err != nil {
			return FormalReview{}, ErrInvalidReview
		}
		review.Result = &result
	}
	return review, nil
}

func listEvidence(
	ctx context.Context,
	database queryer,
	reviewID string,
	ownerUserID string,
) ([]ReviewEvidence, error) {
	rows, err := database.Query(ctx, `
		SELECT
			id::text,
			review_id::text,
			owner_user_id::text,
			target_kind,
			target_key,
			source_type,
			source_id,
			source_version,
			field,
			anchor_kind,
			coalesce(quote, ''),
			start_utf8_byte,
			end_utf8_byte,
			coalesce(source_checksum, ''),
			evidence_snapshot,
			created_at
		FROM review_evidence
		WHERE review_id = $1 AND owner_user_id = $2
		ORDER BY target_kind, target_key, source_type, source_id,
			source_version, start_utf8_byte
	`, reviewID, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var evidence []ReviewEvidence
	for rows.Next() {
		var item ReviewEvidence
		if err := rows.Scan(
			&item.ID,
			&item.ReviewID,
			&item.OwnerUserID,
			&item.TargetKind,
			&item.TargetKey,
			&item.SourceType,
			&item.SourceID,
			&item.SourceVersion,
			&item.Field,
			&item.AnchorKind,
			&item.Quote,
			&item.StartUTF8Byte,
			&item.EndUTF8Byte,
			&item.Checksum,
			&item.Snapshot,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if item.TargetKind == EvidenceTargetConclusion {
			item.ConclusionKey = item.TargetKey
		}
		evidence = append(evidence, item)
	}
	return evidence, rows.Err()
}

func lockReviewUser(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		userID,
	)
	return err
}

type rowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func accountUnavailable(
	ctx context.Context,
	database rowQueryer,
	userID string,
) (bool, error) {
	var deleted bool
	err := database.QueryRow(ctx, `
		SELECT
			NOT EXISTS (
				SELECT 1
				FROM identity_users
				WHERE id = $1 AND account_status = 'active'
			)
			OR EXISTS (
				SELECT 1
				FROM review_deletion_fences
				WHERE owner_user_id = $1
			)
	`, userID).Scan(&deleted)
	return deleted, err
}

func lockActiveIdentityUser(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	deletionGeneration int64,
) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT users.account_status
		FROM identity_users users
		WHERE users.id = $1
		FOR SHARE OF users
	`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountDeleted
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return ErrAccountDeleted
	}

	var fenceGeneration int64
	err = tx.QueryRow(ctx, `
		SELECT deletion_generation
		FROM review_deletion_fences
		WHERE owner_user_id = $1
	`, userID).Scan(&fenceGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if fenceGeneration >= deletionGeneration {
		return ErrAccountDeleted
	}
	return ErrAccountDeleted
}

func errInvalidClaim(claim GenerationJobContext) bool {
	return strings.TrimSpace(claim.AttemptID) == "" ||
		strings.TrimSpace(claim.ReviewID) == "" ||
		!validUUID(claim.OwnerUserID) ||
		strings.TrimSpace(claim.WorkerToken) == "" ||
		claim.DeletionGeneration < 0
}

var _ ReviewRepository = (*PostgresRepository)(nil)
var _ HistoryRepository = (*PostgresRepository)(nil)
