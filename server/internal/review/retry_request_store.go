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

func (r *PostgresRepository) ReserveRetryRequest(
	ctx context.Context,
	ownerUserID string,
	feedbackItemID string,
	idempotencyKey string,
) (SpeechFeedbackRetryRequest, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(feedbackItemID) ||
		!validRetryRequestIdempotencyKey(idempotencyKey) {
		return SpeechFeedbackRetryRequest{}, false, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, false,
			fmt.Errorf("begin SpeechFeedbackRetryRequest reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return SpeechFeedbackRetryRequest{}, false, mapRetryRequestAccountError(err)
	}
	if err := lockRetryRequestKey(
		ctx,
		tx,
		ownerUserID,
		idempotencyKey,
	); err != nil {
		return SpeechFeedbackRetryRequest{}, false, err
	}

	fingerprint := retryRequestFingerprint(feedbackItemID)
	existing, storedFingerprint, err := getRetryRequestByKey(
		ctx,
		tx,
		ownerUserID,
		idempotencyKey,
		" FOR UPDATE",
	)
	if err == nil {
		if existing.FeedbackItemID != feedbackItemID ||
			!bytes.Equal(storedFingerprint, fingerprint[:]) {
			return SpeechFeedbackRetryRequest{}, false, ErrRetryRequestConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return SpeechFeedbackRetryRequest{}, false,
				fmt.Errorf("commit replayed SpeechFeedbackRetryRequest: %w", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrRetryRequestNotFound) {
		return SpeechFeedbackRetryRequest{}, false, err
	}

	var (
		speechFeedbackID   string
		practiceSessionID  string
		originalTurnID     string
		questionID         string
		deletionGeneration int64
	)
	err = tx.QueryRow(ctx, `
		SELECT
			feedback.id::text,
			feedback.practice_session_id,
			feedback.turn_id,
			turn.question_id,
			feedback.deletion_generation
		FROM review_speech_feedback_items AS item
		JOIN review_speech_feedbacks AS feedback
		  ON feedback.id = item.speech_feedback_id
		 AND feedback.owner_user_id = item.owner_user_id
		JOIN conversation_confirmed_turns AS turn
		  ON turn.owner_user_id = feedback.owner_user_id
		 AND turn.turn_id = feedback.turn_id
		 AND turn.practice_session_id =
		     feedback.practice_session_id
		WHERE item.owner_user_id = $1
		  AND item.id = $2
		  AND item.repractice_mode = 'SAME_QUESTION'
		  AND item.turn_id = feedback.turn_id
		  AND feedback.source_kind = 'CONVERSATION_TURN'
		  AND feedback.feedback_status = 'READY'
		  AND feedback.scoreability_status = 'PROVISIONAL'
		  AND feedback.gate_status = 'FEEDBACK_ONLY'
		  AND turn.turn_kind = 'EFFECTIVE'
		  AND turn.counts_toward_effective_turn_limit
		FOR SHARE OF item, feedback, turn
	`, ownerUserID, feedbackItemID).Scan(
		&speechFeedbackID,
		&practiceSessionID,
		&originalTurnID,
		&questionID,
		&deletionGeneration,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SpeechFeedbackRetryRequest{}, false, ErrRetryRequestNotFound
	}
	if err != nil {
		return SpeechFeedbackRetryRequest{}, false,
			fmt.Errorf("read eligible SpeechFeedback retry item: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO review_speech_feedback_retry_requests (
			owner_user_id,
			feedback_item_id,
			speech_feedback_id,
			idempotency_key,
			request_fingerprint,
			deletion_generation,
			practice_session_id,
			original_turn_id,
			question_id,
			retry_status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'PENDING')
	`, ownerUserID, feedbackItemID, speechFeedbackID,
		idempotencyKey, fingerprint[:], deletionGeneration,
		practiceSessionID, originalTurnID, questionID); err != nil {
		return SpeechFeedbackRetryRequest{}, false, classifyRetryRequestWrite(err)
	}
	request, _, err := getRetryRequestByKey(
		ctx,
		tx,
		ownerUserID,
		idempotencyKey,
		"",
	)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedbackRetryRequest{}, false,
			fmt.Errorf("commit SpeechFeedbackRetryRequest reservation: %w", err)
	}
	return request, true, nil
}

func (r *PostgresRepository) GetRetryRequest(
	ctx context.Context,
	ownerUserID string,
	retryRequestID string,
) (SpeechFeedbackRetryRequest, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(retryRequestID) {
		return SpeechFeedbackRetryRequest{}, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, fmt.Errorf("begin SpeechFeedbackRetryRequest read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return SpeechFeedbackRetryRequest{}, mapRetryRequestAccountError(err)
	}
	request, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		"",
	)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedbackRetryRequest{}, fmt.Errorf("commit SpeechFeedbackRetryRequest read: %w", err)
	}
	return request, nil
}

func (r *PostgresRepository) CompleteRetryRequest(
	ctx context.Context,
	ownerUserID string,
	retryRequestID string,
	newTurnID string,
) (SpeechFeedbackRetryRequest, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(retryRequestID) ||
		!validRetryRequestResourceID(newTurnID) {
		return SpeechFeedbackRetryRequest{}, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedbackRetryRequest{},
			fmt.Errorf("begin SpeechFeedbackRetryRequest completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return SpeechFeedbackRetryRequest{}, mapRetryRequestAccountError(err)
	}
	request, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		" FOR UPDATE",
	)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, err
	}
	switch request.RetryStatus {
	case RetryRequestTurnCreated:
		if request.NewTurnID != newTurnID {
			return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
		}
	case RetryRequestFailed:
		return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
	case RetryRequestPending:
		tag, updateErr := tx.Exec(ctx, `
			UPDATE review_speech_feedback_retry_requests
			SET retry_status = 'TURN_CREATED',
			    new_turn_id = $3,
			    updated_at = transaction_timestamp(),
			    completed_at = transaction_timestamp()
			WHERE owner_user_id = $1
			  AND retry_request_id = $2
			  AND retry_status = 'PENDING'
		`, ownerUserID, retryRequestID, newTurnID)
		if updateErr != nil {
			return SpeechFeedbackRetryRequest{},
				classifyRetryRequestWrite(updateErr)
		}
		if tag.RowsAffected() != 1 {
			return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
		}
	default:
		return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
	}
	completed, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		"",
	)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedbackRetryRequest{},
			fmt.Errorf("commit SpeechFeedbackRetryRequest completion: %w", err)
	}
	return completed, nil
}

func (r *PostgresRepository) FailRetryRequest(
	ctx context.Context,
	ownerUserID string,
	retryRequestID string,
	failure RetryRequestStableFailure,
) (SpeechFeedbackRetryRequest, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(retryRequestID) ||
		!failure.valid() {
		return SpeechFeedbackRetryRequest{}, ErrRetryRequestInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, fmt.Errorf("begin SpeechFeedbackRetryRequest failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return SpeechFeedbackRetryRequest{}, mapRetryRequestAccountError(err)
	}
	request, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		" FOR UPDATE",
	)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, err
	}
	switch request.RetryStatus {
	case RetryRequestFailed:
		if request.StableFailure == nil ||
			*request.StableFailure != failure {
			return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
		}
	case RetryRequestTurnCreated:
		return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
	case RetryRequestPending:
		tag, updateErr := tx.Exec(ctx, `
			UPDATE review_speech_feedback_retry_requests
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
			return SpeechFeedbackRetryRequest{},
				classifyRetryRequestWrite(updateErr)
		}
		if tag.RowsAffected() != 1 {
			return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
		}
	default:
		return SpeechFeedbackRetryRequest{}, ErrRetryRequestConflict
	}
	failed, _, err := getRetryRequestByID(
		ctx,
		tx,
		ownerUserID,
		retryRequestID,
		"",
	)
	if err != nil {
		return SpeechFeedbackRetryRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedbackRetryRequest{}, fmt.Errorf("commit SpeechFeedbackRetryRequest failure: %w", err)
	}
	return failed, nil
}

const retryRequestSelect = `
	SELECT
		retry_request_id::text,
		feedback_item_id::text,
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
	FROM review_speech_feedback_retry_requests
`

func getRetryRequestByKey(
	ctx context.Context,
	database queryer,
	ownerUserID string,
	idempotencyKey string,
	suffix string,
) (SpeechFeedbackRetryRequest, []byte, error) {
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
) (SpeechFeedbackRetryRequest, []byte, error) {
	return scanRetryRequest(database.QueryRow(ctx, retryRequestSelect+`
		WHERE owner_user_id = $1
		  AND retry_request_id = $2
	`+suffix, ownerUserID, retryRequestID))
}

func scanRetryRequest(
	row rowScanner,
) (SpeechFeedbackRetryRequest, []byte, error) {
	var request SpeechFeedbackRetryRequest
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
		return SpeechFeedbackRetryRequest{}, nil, ErrRetryRequestNotFound
	}
	if err != nil {
		return SpeechFeedbackRetryRequest{}, nil,
			fmt.Errorf("scan SpeechFeedbackRetryRequest: %w", err)
	}
	if len(fingerprint) != sha256.Size {
		return SpeechFeedbackRetryRequest{}, nil, ErrRetryRequestInvalid
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
		return SpeechFeedbackRetryRequest{}, nil, ErrRetryRequestInvalid
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
		return fmt.Errorf("lock SpeechFeedbackRetryRequest idempotency key: %w", err)
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
	return fmt.Errorf("persist SpeechFeedbackRetryRequest: %w", err)
}

func mapRetryRequestAccountError(err error) error {
	if errors.Is(err, ErrAccountDeleted) {
		return ErrRetryRequestNotFound
	}
	return err
}

var _ RetryRequestRepository = (*PostgresRepository)(nil)
