package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *PostgresRepository) FindRetryRequestByKey(
	ctx context.Context,
	ownerUserID string,
	idempotencyKey string,
) (RepracticeRequest, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validRetryRequestIdempotencyKey(idempotencyKey) {
		return RepracticeRequest{}, false, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RepracticeRequest{}, false,
			fmt.Errorf("begin RepracticeRequest replay read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return RepracticeRequest{}, false, mapRetryRequestAccountError(err)
	}
	request, _, err := getRetryRequestByKey(
		ctx,
		tx,
		ownerUserID,
		idempotencyKey,
		"",
	)
	if errors.Is(err, ErrRetryRequestNotFound) {
		return RepracticeRequest{}, false, nil
	}
	if err != nil {
		return RepracticeRequest{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepracticeRequest{}, false,
			fmt.Errorf("commit RepracticeRequest replay read: %w", err)
	}
	return request, true, nil
}

func (r *PostgresRepository) ReserveRetryRequest(
	ctx context.Context,
	ownerUserID string,
	source RepracticeSource,
	idempotencyKey string,
) (RepracticeRequest, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !source.valid() ||
		!validRetryRequestIdempotencyKey(idempotencyKey) {
		return RepracticeRequest{}, false, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RepracticeRequest{}, false,
			fmt.Errorf("begin RepracticeRequest reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return RepracticeRequest{}, false, mapRetryRequestAccountError(err)
	}
	if err := lockRetryRequestKey(
		ctx,
		tx,
		ownerUserID,
		idempotencyKey,
	); err != nil {
		return RepracticeRequest{}, false, err
	}

	fingerprint := retryRequestFingerprint(source.FeedbackItemID)
	existing, storedFingerprint, err := getRetryRequestByKey(
		ctx,
		tx,
		ownerUserID,
		idempotencyKey,
		" FOR UPDATE",
	)
	if err == nil {
		if existing.FeedbackItemID != source.FeedbackItemID ||
			!bytes.Equal(storedFingerprint, fingerprint[:]) {
			return RepracticeRequest{}, false, ErrRetryRequestConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return RepracticeRequest{}, false,
				fmt.Errorf("commit replayed RepracticeRequest: %w", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrRetryRequestNotFound) {
		return RepracticeRequest{}, false, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO review_repractice_requests (
			owner_user_id,
			source_feedback_item_id,
			source_feedback_id,
			idempotency_key,
			request_fingerprint,
			deletion_generation,
			practice_session_id,
			original_turn_id,
			question_id,
			retry_status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'PENDING')
	`, ownerUserID, source.FeedbackItemID, source.SourceFeedbackID,
		idempotencyKey, fingerprint[:], source.SourceGeneration,
		source.PracticeSessionID, source.OriginalTurnID,
		source.QuestionID); err != nil {
		return RepracticeRequest{}, false, classifyRetryRequestWrite(err)
	}
	request, _, err := getRetryRequestByKey(
		ctx,
		tx,
		ownerUserID,
		idempotencyKey,
		"",
	)
	if err != nil {
		return RepracticeRequest{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepracticeRequest{}, false,
			fmt.Errorf("commit RepracticeRequest reservation: %w", err)
	}
	return request, true, nil
}

func (r *PostgresRepository) GetRetryRequest(
	ctx context.Context,
	ownerUserID string,
	retryRequestID string,
) (RepracticeRequest, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(retryRequestID) {
		return RepracticeRequest{}, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RepracticeRequest{}, fmt.Errorf("begin RepracticeRequest read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return RepracticeRequest{}, mapRetryRequestAccountError(err)
	}
	request, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		"",
	)
	if err != nil {
		return RepracticeRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepracticeRequest{}, fmt.Errorf("commit RepracticeRequest read: %w", err)
	}
	return request, nil
}

func (r *PostgresRepository) CompleteRetryRequest(
	ctx context.Context,
	ownerUserID string,
	retryRequestID string,
	newTurnID string,
) (RepracticeRequest, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(retryRequestID) ||
		!validRetryRequestResourceID(newTurnID) {
		return RepracticeRequest{}, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RepracticeRequest{},
			fmt.Errorf("begin RepracticeRequest completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return RepracticeRequest{}, mapRetryRequestAccountError(err)
	}
	request, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		" FOR UPDATE",
	)
	if err != nil {
		return RepracticeRequest{}, err
	}
	switch request.RetryStatus {
	case RetryRequestTurnCreated:
		if request.NewTurnID != newTurnID {
			return RepracticeRequest{}, ErrRetryRequestConflict
		}
	case RetryRequestFailed:
		return RepracticeRequest{}, ErrRetryRequestConflict
	case RetryRequestPending:
		tag, updateErr := tx.Exec(ctx, `
			UPDATE review_repractice_requests
			SET retry_status = 'TURN_CREATED',
			    new_turn_id = $3,
			    updated_at = transaction_timestamp(),
			    completed_at = transaction_timestamp()
			WHERE owner_user_id = $1
			  AND retry_request_id = $2
			  AND retry_status = 'PENDING'
		`, ownerUserID, retryRequestID, newTurnID)
		if updateErr != nil {
			return RepracticeRequest{},
				classifyRetryRequestWrite(updateErr)
		}
		if tag.RowsAffected() != 1 {
			return RepracticeRequest{}, ErrRetryRequestConflict
		}
	default:
		return RepracticeRequest{}, ErrRetryRequestConflict
	}
	completed, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		"",
	)
	if err != nil {
		return RepracticeRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepracticeRequest{},
			fmt.Errorf("commit RepracticeRequest completion: %w", err)
	}
	return completed, nil
}

func (r *PostgresRepository) FailRetryRequest(
	ctx context.Context,
	ownerUserID string,
	retryRequestID string,
	failure RetryRequestStableFailure,
) (RepracticeRequest, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(retryRequestID) ||
		!failure.valid() {
		return RepracticeRequest{}, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RepracticeRequest{}, fmt.Errorf("begin RepracticeRequest failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return RepracticeRequest{}, mapRetryRequestAccountError(err)
	}
	request, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		" FOR UPDATE",
	)
	if err != nil {
		return RepracticeRequest{}, err
	}
	switch request.RetryStatus {
	case RetryRequestFailed:
		if request.StableFailure == nil ||
			*request.StableFailure != failure {
			return RepracticeRequest{}, ErrRetryRequestConflict
		}
	case RetryRequestTurnCreated:
		return RepracticeRequest{}, ErrRetryRequestConflict
	case RetryRequestPending:
		tag, updateErr := tx.Exec(ctx, `
			UPDATE review_repractice_requests
			SET retry_status = 'FAILED',
			    stable_failure_reason = $3,
			    stable_failure_retryable = $4,
			    updated_at = transaction_timestamp(),
			    completed_at = transaction_timestamp()
			WHERE owner_user_id = $1
			  AND retry_request_id = $2
			  AND retry_status = 'PENDING'
		`, ownerUserID, retryRequestID, failure.ReasonCode,
			failure.Retryable)
		if updateErr != nil {
			return RepracticeRequest{},
				classifyRetryRequestWrite(updateErr)
		}
		if tag.RowsAffected() != 1 {
			return RepracticeRequest{}, ErrRetryRequestConflict
		}
	default:
		return RepracticeRequest{}, ErrRetryRequestConflict
	}
	failed, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		"",
	)
	if err != nil {
		return RepracticeRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepracticeRequest{}, fmt.Errorf("commit RepracticeRequest failure: %w", err)
	}
	return failed, nil
}

const retryRequestSelect = `
	SELECT
		retry_request_id::text,
		source_feedback_item_id::text,
		practice_session_id,
		original_turn_id,
		question_id,
		coalesce(new_turn_id, ''),
		retry_status,
		coalesce(stable_failure_reason, ''),
		stable_failure_retryable,
		created_at,
		updated_at,
		completed_at,
		request_fingerprint
	FROM review_repractice_requests
`

func getRetryRequestByKey(
	ctx context.Context,
	database queryer,
	ownerUserID string,
	idempotencyKey string,
	suffix string,
) (RepracticeRequest, []byte, error) {
	return scanRetryRequest(database.QueryRow(ctx, retryRequestSelect+`
		WHERE owner_user_id = $1
		  AND idempotency_key = $2
	`+suffix, ownerUserID, idempotencyKey))
}

func getRetryRequestByID(
	ctx context.Context,
	database queryer,
	ownerUserID string,
	retryRequestID string,
	suffix string,
) (RepracticeRequest, []byte, error) {
	return scanRetryRequest(database.QueryRow(ctx, retryRequestSelect+`
		WHERE owner_user_id = $1
		  AND retry_request_id = $2
	`+suffix, ownerUserID, retryRequestID))
}

func scanRetryRequest(
	row rowScanner,
) (RepracticeRequest, []byte, error) {
	var request RepracticeRequest
	var (
		failureCode      string
		failureRetryable sql.NullBool
		completedAt      sql.NullTime
		fingerprint      []byte
	)
	err := row.Scan(
		&request.RetryRequestID,
		&request.FeedbackItemID,
		&request.PracticeSessionID,
		&request.OriginalTurnID,
		&request.QuestionID,
		&request.NewTurnID,
		&request.RetryStatus,
		&failureCode,
		&failureRetryable,
		&request.CreatedAt,
		&request.UpdatedAt,
		&completedAt,
		&fingerprint,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepracticeRequest{}, nil, ErrRetryRequestNotFound
	}
	if err != nil {
		return RepracticeRequest{}, nil,
			fmt.Errorf("scan RepracticeRequest: %w", err)
	}
	if len(fingerprint) != sha256.Size {
		return RepracticeRequest{}, nil, ErrRetryRequestInvalid
	}
	if failureCode != "" && failureRetryable.Valid {
		request.StableFailure = &RetryRequestStableFailure{
			ReasonCode: RetryRequestFailureCode(failureCode),
			Retryable:  failureRetryable.Bool,
		}
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		request.CompletedAt = &value
	}
	request.CreatedAt = request.CreatedAt.UTC()
	request.UpdatedAt = request.UpdatedAt.UTC()
	request.StatusURL = RetryRequestStatusURL(request.RetryRequestID)
	if request.RetryStatus == RetryRequestTurnCreated {
		request.NewTurnStatus = "ANSWERING"
		request.AnswerPath = RetryTurnAnswerPath(request.NewTurnID)
	}
	if !request.valid() {
		return RepracticeRequest{}, nil, ErrRetryRequestInvalid
	}
	return request, fingerprint, nil
}

func lockRetryRequestKey(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	idempotencyKey string,
) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		ownerUserID+"\x1fretry-request\x1f"+idempotencyKey,
	)
	if err != nil {
		return fmt.Errorf("lock RepracticeRequest idempotency key: %w", err)
	}
	return nil
}

func classifyRetryRequestWrite(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrRetryRequestNotFound
		case "23505", "23514":
			return ErrRetryRequestConflict
		}
	}
	return fmt.Errorf("persist RepracticeRequest: %w", err)
}

func mapRetryRequestAccountError(err error) error {
	if errors.Is(err, ErrAccountDeleted) {
		return ErrRetryRequestNotFound
	}
	return err
}

var _ RetryRequestRepository = (*PostgresRepository)(nil)
