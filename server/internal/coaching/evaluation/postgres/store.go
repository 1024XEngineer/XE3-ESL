package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the only Evaluation persistence implementation. It owns both
// evaluations and evaluation_feedback_items; report and Review reads project
// directly from these two tables.
type Store struct {
	pool *pgxpool.Pool
}

type rowScanner interface {
	Scan(...any) error
}

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &Store{pool: pool}, nil
}

func (store *Store) Queue(
	ctx context.Context,
	command evaluation.QueueCommand,
) (evaluation.Record, bool, error) {
	if store == nil || store.pool == nil || ctx == nil || !command.Valid() {
		return evaluation.Record{}, false, evaluation.ErrInvalidRequest
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return evaluation.Record{}, false, fmt.Errorf("begin Evaluation queue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, replayed, err := store.QueueInTx(ctx, tx, command)
	if err != nil {
		return evaluation.Record{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return evaluation.Record{}, false, fmt.Errorf("commit Evaluation queue: %w", err)
	}
	return record, replayed, nil
}

// QueueInTx inserts a queue row without opening or committing a transaction.
// The Practice completion workflow uses this method after updating the Session
// and before committing that exact transaction.
func (store *Store) QueueInTx(
	ctx context.Context,
	tx pgx.Tx,
	command evaluation.QueueCommand,
) (evaluation.Record, bool, error) {
	if store == nil || ctx == nil || tx == nil || !command.Valid() {
		return evaluation.Record{}, false, evaluation.ErrInvalidRequest
	}
	if err := lockActiveUser(ctx, tx, command.UserID); err != nil {
		return evaluation.Record{}, false, err
	}
	record, err := scanRecord(tx.QueryRow(ctx, `INSERT INTO evaluations (
			user_id, kind, source_id, context_id, status,
			input_snapshot, input_hash, config_lineage, config_hash,
			attempt_count, available_at
		)
		VALUES ($1, $2, $3, $4, 'QUEUED', $5::json, $6, $7::json, $8, 0, $9)
		ON CONFLICT (user_id, kind, source_id) DO NOTHING
		RETURNING `+evaluationColumns,
		command.UserID, command.Kind, command.SourceID, command.ContextID,
		command.InputSnapshot, command.InputHash[:], command.ConfigLineage,
		command.ConfigHash[:], command.AvailableAt.UTC()))
	if err == nil {
		return record, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return evaluation.Record{}, false, fmt.Errorf("insert Evaluation queue: %w", err)
	}
	record, err = scanRecord(tx.QueryRow(ctx, `SELECT `+evaluationColumns+`
		FROM evaluations
		WHERE user_id = $1 AND kind = $2 AND source_id = $3
		FOR UPDATE`, command.UserID, command.Kind, command.SourceID))
	if err != nil {
		return evaluation.Record{}, false, fmt.Errorf("read Evaluation queue replay: %w", err)
	}
	if record.ContextID != command.ContextID {
		return evaluation.Record{}, false, evaluation.ErrIdempotencyConflict
	}
	return record, true, nil
}

func (store *Store) GetRecordBySource(
	ctx context.Context,
	userID string,
	kind evaluation.Kind,
	sourceID string,
) (evaluation.Record, error) {
	if store == nil || store.pool == nil || ctx == nil ||
		!validUUID(userID) || !kind.Valid() || !validUUID(sourceID) {
		return evaluation.Record{}, evaluation.ErrInvalidRequest
	}
	record, err := scanRecord(store.pool.QueryRow(ctx, `SELECT `+evaluationColumns+`
		FROM evaluations
		WHERE user_id = $1 AND kind = $2 AND source_id = $3`,
		userID, kind, sourceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.Record{}, evaluation.ErrNotFound
	}
	if err != nil {
		return evaluation.Record{}, fmt.Errorf("read Evaluation by source: %w", err)
	}
	return record, nil
}

func (store *Store) ClaimNext(
	ctx context.Context,
	lane evaluation.ClaimLane,
) (evaluation.Claim, error) {
	if store == nil || store.pool == nil || ctx == nil || !lane.Valid() {
		return evaluation.Claim{}, evaluation.ErrInvalidRequest
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return evaluation.Claim{}, fmt.Errorf("begin Evaluation claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	kinds := kindStrings(lane.Kinds)
	var userID string
	err = tx.QueryRow(ctx, `SELECT evaluation.user_id::text
		FROM evaluations AS evaluation
		JOIN users AS owner ON owner.id = evaluation.user_id
		WHERE owner.status = 'active'
		  AND evaluation.kind = ANY($1::text[])
		  AND (
			(evaluation.attempt_count < $2 AND (
			  (evaluation.status = 'QUEUED'
			   AND evaluation.available_at <= transaction_timestamp())
			  OR (evaluation.status = 'RUNNING'
			      AND evaluation.lease_expires_at <= transaction_timestamp())
			))
			OR (evaluation.status = 'RUNNING'
			    AND evaluation.lease_expires_at <= transaction_timestamp()
			    AND evaluation.attempt_count >= $2)
		  )
		ORDER BY evaluation.available_at, evaluation.created_at, evaluation.id
		LIMIT 1`, kinds, lane.MaxAttempts).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.Claim{}, evaluation.ErrNotFound
	}
	if err != nil {
		return evaluation.Claim{}, fmt.Errorf("select Evaluation claim owner: %w", err)
	}
	// Account deletion locks this same row FOR UPDATE before deleting domain
	// data. Taking the user lock before the Evaluation row prevents deadlocks
	// and closes the active -> deleting race without a deletion fence table.
	if err := lockActiveUser(ctx, tx, userID); err != nil {
		return evaluation.Claim{}, err
	}
	exhaustedError, _, err := evaluation.EncodeStrict(evaluation.JobError{
		Code:      "LEASE_EXPIRED",
		Retryable: false,
		Message:   "Evaluation worker lease expired after the final attempt.",
	})
	if err != nil {
		return evaluation.Claim{}, err
	}
	// Keep the same user -> Evaluation lock order as account deletion and all
	// other Evaluation writes. The final-attempt crash is made terminal before
	// another claim is selected for this owner.
	if _, err := tx.Exec(ctx, `UPDATE evaluations
		SET status = 'FAILED', lease_token = NULL, lease_expires_at = NULL,
			error = $4::jsonb, updated_at = transaction_timestamp(),
			finished_at = transaction_timestamp()
		WHERE user_id = $1 AND kind = ANY($2::text[]) AND status = 'RUNNING'
		  AND lease_expires_at <= transaction_timestamp()
		  AND attempt_count >= $3`, userID, kinds, lane.MaxAttempts, exhaustedError); err != nil {
		return evaluation.Claim{}, fmt.Errorf("fail exhausted Evaluation claims: %w", err)
	}
	record, err := scanRecord(tx.QueryRow(ctx, `WITH candidate AS (
		SELECT id
		FROM evaluations
		WHERE kind = ANY($1::text[]) AND user_id = $4
		  AND attempt_count < $2
		  AND (
			(status = 'QUEUED' AND available_at <= transaction_timestamp())
			OR (status = 'RUNNING' AND lease_expires_at <= transaction_timestamp())
		  )
		ORDER BY available_at, created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	)
	UPDATE evaluations AS evaluation
	SET status = 'RUNNING',
		attempt_count = attempt_count + 1,
		lease_token = gen_random_uuid(),
		lease_expires_at = transaction_timestamp() + ($3 * interval '1 millisecond'),
		error = NULL,
		updated_at = transaction_timestamp(),
		started_at = COALESCE(started_at, transaction_timestamp()),
		finished_at = NULL
	FROM candidate
	WHERE evaluation.id = candidate.id
	RETURNING `+prefixedEvaluationColumns("evaluation"),
		kinds, lane.MaxAttempts, lane.LeaseDuration.Milliseconds(), userID))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return evaluation.Claim{}, fmt.Errorf(
				"commit exhausted Evaluation claims: %w",
				commitErr,
			)
		}
		return evaluation.Claim{}, evaluation.ErrNotFound
	}
	if err != nil {
		return evaluation.Claim{}, fmt.Errorf("claim Evaluation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return evaluation.Claim{}, fmt.Errorf("commit Evaluation claim: %w", err)
	}
	return evaluation.Claim{Record: record, LeaseDuration: lane.LeaseDuration}, nil
}

func (store *Store) CheckpointSnapshot(
	ctx context.Context,
	checkpoint evaluation.SnapshotCheckpoint,
) (evaluation.Record, error) {
	if store == nil || store.pool == nil || ctx == nil || !checkpoint.Valid() {
		return evaluation.Record{}, evaluation.ErrInvalidRequest
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return evaluation.Record{}, fmt.Errorf("begin Evaluation checkpoint: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveUser(ctx, tx, checkpoint.UserID); err != nil {
		return evaluation.Record{}, err
	}
	record, err := scanRecord(tx.QueryRow(ctx, `UPDATE evaluations
		SET input_snapshot = $3::json, input_hash = $4,
			updated_at = transaction_timestamp()
		WHERE id = $1 AND user_id = $5 AND status = 'RUNNING'
		  AND lease_token = $2 AND lease_expires_at > transaction_timestamp()
		RETURNING `+evaluationColumns,
		checkpoint.ID, checkpoint.LeaseToken,
		checkpoint.InputSnapshot, checkpoint.InputHash[:], checkpoint.UserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.Record{}, evaluation.ErrClaimLost
	}
	if err != nil {
		return evaluation.Record{}, fmt.Errorf("checkpoint Evaluation input: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return evaluation.Record{}, fmt.Errorf("commit Evaluation checkpoint: %w", err)
	}
	return record, nil
}

func (store *Store) DeferClaim(
	ctx context.Context,
	deferral evaluation.Deferral,
) error {
	if store == nil || store.pool == nil || ctx == nil || !deferral.Valid() {
		return evaluation.ErrInvalidRequest
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Evaluation deferral: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveUser(ctx, tx, deferral.UserID); err != nil {
		return err
	}
	commandTag, err := tx.Exec(ctx, `UPDATE evaluations
		SET status = 'QUEUED', attempt_count = attempt_count - 1,
			lease_token = NULL, lease_expires_at = NULL, available_at = $4,
			error = NULL, updated_at = transaction_timestamp(), finished_at = NULL
		WHERE id = $1 AND user_id = $2
		  AND status = 'RUNNING' AND attempt_count > 0
		  AND lease_token = $3 AND lease_expires_at > transaction_timestamp()`,
		deferral.ID, deferral.UserID, deferral.LeaseToken,
		deferral.AvailableAt.UTC())
	if err != nil {
		return fmt.Errorf("defer Evaluation claim: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return evaluation.ErrClaimLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Evaluation deferral: %w", err)
	}
	return nil
}

func (store *Store) ReadSessionAcoustics(
	ctx context.Context,
	userID string,
	sessionID string,
	turnIDs []string,
) (evaluation.SessionAcousticRead, error) {
	if store == nil || store.pool == nil || ctx == nil ||
		!validUUID(userID) || !validUUID(sessionID) ||
		len(turnIDs) == 0 || len(turnIDs) > 128 {
		return evaluation.SessionAcousticRead{}, evaluation.ErrInvalidRequest
	}
	expected := make(map[string]struct{}, len(turnIDs))
	for _, turnID := range turnIDs {
		if !validUUID(turnID) {
			return evaluation.SessionAcousticRead{}, evaluation.ErrInvalidRequest
		}
		if _, duplicate := expected[turnID]; duplicate {
			return evaluation.SessionAcousticRead{}, evaluation.ErrInvalidRequest
		}
		expected[turnID] = struct{}{}
	}
	rows, err := store.pool.Query(ctx, `SELECT source_id::text, status, input_snapshot
		FROM evaluations
		WHERE user_id = $1 AND kind = 'PRACTICE_TURN_FEEDBACK'
		  AND context_id = $2 AND source_id = ANY($3::uuid[])`,
		userID, sessionID, turnIDs)
	if err != nil {
		return evaluation.SessionAcousticRead{}, fmt.Errorf(
			"read IELTS acoustic dependencies: %w", err,
		)
	}
	defer rows.Close()
	result := evaluation.SessionAcousticRead{
		Checkpoints: make(map[string]evaluation.AcousticCheckpoint, len(turnIDs)),
	}
	found := make(map[string]struct{}, len(turnIDs))
	for rows.Next() {
		var turnID string
		var status evaluation.JobStatus
		var input []byte
		if err := rows.Scan(&turnID, &status, &input); err != nil {
			return evaluation.SessionAcousticRead{}, fmt.Errorf(
				"scan IELTS acoustic dependency: %w", err,
			)
		}
		if _, exists := expected[turnID]; !exists {
			return evaluation.SessionAcousticRead{}, evaluation.ErrInvalidRequest
		}
		found[turnID] = struct{}{}
		var snapshot evaluation.SpeechInputSnapshot
		if evaluation.DecodeStrict(input, &snapshot) != nil ||
			!snapshot.Valid(evaluation.KindPracticeTurnFeedback) ||
			snapshot.EvidenceRefID != turnID {
			return evaluation.SessionAcousticRead{}, evaluation.ErrAcousticDependencyFailed
		}
		if snapshot.Acoustic != nil {
			result.Checkpoints[turnID] = *snapshot.Acoustic
			continue
		}
		switch status {
		case evaluation.JobQueued, evaluation.JobRunning:
			result.Pending = true
		case evaluation.JobFailed, evaluation.JobReady:
			if status == evaluation.JobReady {
				return evaluation.SessionAcousticRead{}, evaluation.ErrAcousticDependencyFailed
			}
			result.Checkpoints[turnID] = evaluation.AcousticCheckpoint{
				Status: evaluation.AcousticNotAssessed,
				Reason: "ACOUSTIC_ASSESSMENT_FAILED",
			}
		default:
			return evaluation.SessionAcousticRead{}, evaluation.ErrInvalidRequest
		}
	}
	if err := rows.Err(); err != nil {
		return evaluation.SessionAcousticRead{}, fmt.Errorf(
			"iterate IELTS acoustic dependencies: %w", err,
		)
	}
	if len(found) != len(expected) {
		return evaluation.SessionAcousticRead{}, evaluation.ErrAcousticDependencyFailed
	}
	return result, nil
}

func (store *Store) CompleteClaim(
	ctx context.Context,
	completion evaluation.Completion,
) error {
	if store == nil || store.pool == nil || ctx == nil || !completion.Valid() {
		return evaluation.ErrInvalidRequest
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Evaluation completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveUser(ctx, tx, completion.UserID); err != nil {
		return err
	}
	var kind evaluation.Kind
	err = tx.QueryRow(ctx, `SELECT kind FROM evaluations
		WHERE id = $1 AND user_id = $3 AND status = 'RUNNING'
		  AND lease_token = $2 AND lease_expires_at > transaction_timestamp()
		FOR UPDATE`, completion.ID, completion.LeaseToken, completion.UserID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.ErrClaimLost
	}
	if err != nil {
		return fmt.Errorf("lock Evaluation completion: %w", err)
	}
	if kind == evaluation.KindSessionReport && len(completion.Items) != 0 {
		return evaluation.ErrInvalidRequest
	}
	for position, item := range completion.Items {
		evidenceJSON, _, err := evaluation.EncodeStrict(item.Evidence)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO evaluation_feedback_items (
			id, evaluation_id, position, category, severity, evidence,
			recommendation, correction, repractice_mode
		) VALUES (gen_random_uuid(), $1, $2, $3, NULLIF($4, ''), $5::jsonb,
			$6, NULLIF($7, ''), $8)`, completion.ID, position+1,
			item.Category, item.Severity, evidenceJSON, item.Recommendation,
			item.Correction, item.RepracticeMode); err != nil {
			return fmt.Errorf("insert Evaluation feedback item: %w", err)
		}
	}
	commandTag, err := tx.Exec(ctx, `UPDATE evaluations
		SET status = 'READY', result = $3::jsonb, lease_token = NULL,
			lease_expires_at = NULL, error = NULL,
			updated_at = transaction_timestamp(), finished_at = transaction_timestamp()
		WHERE id = $1 AND status = 'RUNNING'
		  AND lease_token = $2 AND lease_expires_at > transaction_timestamp()`,
		completion.ID, completion.LeaseToken, completion.Result)
	if err != nil {
		return fmt.Errorf("complete Evaluation claim: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return evaluation.ErrClaimLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Evaluation completion: %w", err)
	}
	return nil
}

func (store *Store) FailClaim(
	ctx context.Context,
	failure evaluation.Failure,
) error {
	if store == nil || store.pool == nil || ctx == nil || !failure.Valid() {
		return evaluation.ErrInvalidRequest
	}
	failureJSON, _, err := evaluation.EncodeStrict(failure.Error)
	if err != nil {
		return err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Evaluation failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveUser(ctx, tx, failure.UserID); err != nil {
		return err
	}
	commandTag, err := tx.Exec(ctx, `UPDATE evaluations
		SET status = CASE
				WHEN attempt_count >= $4 OR NOT $5 THEN 'FAILED'
				ELSE 'QUEUED'
			END,
			lease_token = NULL,
			lease_expires_at = NULL,
			available_at = CASE
				WHEN attempt_count >= $4 OR NOT $5 THEN available_at
				ELSE $3
			END,
			error = CASE
				WHEN attempt_count >= $4 OR NOT $5 THEN $6::jsonb
				ELSE NULL
			END,
			updated_at = transaction_timestamp(),
			finished_at = CASE
				WHEN attempt_count >= $4 OR NOT $5 THEN transaction_timestamp()
				ELSE NULL
			END
		WHERE id = $1 AND user_id = $7 AND status = 'RUNNING'
		  AND lease_token = $2 AND lease_expires_at > transaction_timestamp()`,
		failure.ID, failure.LeaseToken, failure.RetryAt.UTC(),
		failure.MaxAttempts, failure.Error.Retryable, failureJSON, failure.UserID)
	if err != nil {
		return fmt.Errorf("fail Evaluation claim: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return evaluation.ErrClaimLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Evaluation failure: %w", err)
	}
	return nil
}

func (store *Store) ListFeedbackItems(
	ctx context.Context,
	userID string,
	evaluationID string,
) ([]evaluation.FeedbackItem, error) {
	if store == nil || store.pool == nil || ctx == nil ||
		!validUUID(userID) || !validUUID(evaluationID) {
		return nil, evaluation.ErrInvalidRequest
	}
	rows, err := store.pool.Query(ctx, feedbackItemSelect+`
		WHERE evaluation.user_id = $1 AND item.evaluation_id = $2
		  AND evaluation.status = 'READY'
		ORDER BY item.position`, userID, evaluationID)
	if err != nil {
		return nil, fmt.Errorf("list Evaluation feedback items: %w", err)
	}
	defer rows.Close()
	items := make([]evaluation.FeedbackItem, 0, 4)
	for rows.Next() {
		item, err := scanFeedbackItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Evaluation feedback item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Evaluation feedback items: %w", err)
	}
	return items, nil
}

func (store *Store) GetFeedbackItem(
	ctx context.Context,
	userID string,
	feedbackItemID string,
) (evaluation.FeedbackItem, error) {
	if store == nil || store.pool == nil || ctx == nil ||
		!validUUID(userID) || !validUUID(feedbackItemID) {
		return evaluation.FeedbackItem{}, evaluation.ErrInvalidRequest
	}
	item, err := scanFeedbackItem(store.pool.QueryRow(ctx, feedbackItemSelect+`
		WHERE evaluation.user_id = $1 AND item.id = $2
		  AND evaluation.status = 'READY'`, userID, feedbackItemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.FeedbackItem{}, evaluation.ErrNotFound
	}
	if err != nil {
		return evaluation.FeedbackItem{}, fmt.Errorf("read Evaluation feedback item: %w", err)
	}
	return item, nil
}

// GetRetrySourceInTx locks the READY Evaluation before its feedback item.
// Re-evaluation takes the same Evaluation lock before replacing feedback,
// making retry Turn creation and source validation one atomic decision.
func (store *Store) GetRetrySourceInTx(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	feedbackItemID string,
) (evaluation.RetrySource, error) {
	if store == nil || ctx == nil || tx == nil ||
		!validUUID(userID) || !validUUID(feedbackItemID) {
		return evaluation.RetrySource{}, evaluation.ErrInvalidRequest
	}
	if err := lockActiveUser(ctx, tx, userID); err != nil {
		return evaluation.RetrySource{}, err
	}
	record, err := scanRecord(tx.QueryRow(ctx, `SELECT `+
		prefixedEvaluationColumns("evaluation")+`
		FROM evaluations AS evaluation
		JOIN evaluation_feedback_items AS item
		  ON item.evaluation_id = evaluation.id
		WHERE evaluation.user_id = $1 AND item.id = $2
		  AND evaluation.kind = 'PRACTICE_TURN_FEEDBACK'
		  AND evaluation.status = 'READY'
		FOR UPDATE OF evaluation`, userID, feedbackItemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.RetrySource{}, evaluation.ErrNotFound
	}
	if err != nil {
		return evaluation.RetrySource{}, fmt.Errorf("lock Evaluation retry source: %w", err)
	}
	item, err := scanFeedbackItem(tx.QueryRow(ctx, feedbackItemSelect+`
		WHERE evaluation.user_id = $1 AND item.id = $2
		  AND evaluation.id = $3 AND evaluation.status = 'READY'
		FOR UPDATE OF item`, userID, feedbackItemID, record.ID))
	if err != nil {
		return evaluation.RetrySource{}, fmt.Errorf("lock Evaluation feedback item: %w", err)
	}
	source := evaluation.RetrySource{Evaluation: record, Item: item}
	if !source.Valid() {
		return evaluation.RetrySource{}, evaluation.ErrInvalidRequest
	}
	return source, nil
}

const evaluationColumns = `
	id::text, user_id::text, kind, source_id::text, context_id::text, status,
	input_snapshot, input_hash, config_lineage, config_hash, result,
	attempt_count, lease_token::text, lease_expires_at, available_at, error,
	created_at, updated_at, started_at, finished_at`

func prefixedEvaluationColumns(prefix string) string {
	return prefix + `.id::text, ` + prefix + `.user_id::text, ` + prefix +
		`.kind, ` + prefix + `.source_id::text, ` + prefix + `.context_id::text, ` +
		prefix + `.status, ` + prefix +
		`.input_snapshot, ` + prefix + `.input_hash, ` + prefix +
		`.config_lineage, ` + prefix + `.config_hash, ` + prefix +
		`.result, ` + prefix + `.attempt_count, ` + prefix +
		`.lease_token::text, ` + prefix + `.lease_expires_at, ` + prefix +
		`.available_at, ` + prefix + `.error, ` + prefix + `.created_at, ` +
		prefix + `.updated_at, ` + prefix + `.started_at, ` + prefix + `.finished_at`
}

func scanRecord(row rowScanner) (evaluation.Record, error) {
	var (
		record     evaluation.Record
		inputHash  []byte
		configHash []byte
		leaseToken *string
		errorJSON  []byte
	)
	if err := row.Scan(
		&record.ID, &record.UserID, &record.Kind, &record.SourceID,
		&record.ContextID, &record.Status,
		&record.InputSnapshot, &inputHash, &record.ConfigLineage, &configHash,
		&record.Result, &record.AttemptCount, &leaseToken,
		&record.LeaseExpiresAt, &record.AvailableAt, &errorJSON,
		&record.CreatedAt, &record.UpdatedAt, &record.StartedAt,
		&record.FinishedAt,
	); err != nil {
		return evaluation.Record{}, err
	}
	if len(inputHash) != sha256.Size || len(configHash) != sha256.Size {
		return evaluation.Record{}, evaluation.ErrInvalidRequest
	}
	copy(record.InputHash[:], inputHash)
	copy(record.ConfigHash[:], configHash)
	if leaseToken != nil {
		record.LeaseToken = *leaseToken
	}
	if len(errorJSON) != 0 {
		var failure evaluation.JobError
		if err := evaluation.DecodeStrict(errorJSON, &failure); err != nil || !failure.Valid() {
			return evaluation.Record{}, evaluation.ErrInvalidRequest
		}
		record.Error = &failure
	}
	record.AvailableAt = record.AvailableAt.UTC()
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	if record.LeaseExpiresAt != nil {
		value := record.LeaseExpiresAt.UTC()
		record.LeaseExpiresAt = &value
	}
	if record.StartedAt != nil {
		value := record.StartedAt.UTC()
		record.StartedAt = &value
	}
	if record.FinishedAt != nil {
		value := record.FinishedAt.UTC()
		record.FinishedAt = &value
	}
	if !record.Valid() {
		return evaluation.Record{}, evaluation.ErrInvalidRequest
	}
	return record, nil
}

const feedbackItemSelect = `SELECT
	item.id::text, item.evaluation_id::text, item.position, item.category,
	COALESCE(item.severity, ''), item.evidence, item.recommendation,
	COALESCE(item.correction, ''), item.repractice_mode, item.created_at
	FROM evaluation_feedback_items AS item
	JOIN evaluations AS evaluation ON evaluation.id = item.evaluation_id `

func scanFeedbackItem(row rowScanner) (evaluation.FeedbackItem, error) {
	var item evaluation.FeedbackItem
	var evidenceJSON []byte
	if err := row.Scan(
		&item.ID, &item.EvaluationID, &item.Position, &item.Category,
		&item.Severity, &evidenceJSON, &item.Recommendation, &item.Correction,
		&item.RepracticeMode, &item.CreatedAt,
	); err != nil {
		return evaluation.FeedbackItem{}, err
	}
	if err := evaluation.DecodeStrict(evidenceJSON, &item.Evidence); err != nil {
		return evaluation.FeedbackItem{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	if !item.Valid() {
		return evaluation.FeedbackItem{}, evaluation.ErrInvalidRequest
	}
	return item, nil
}

func kindStrings(kinds []evaluation.Kind) []string {
	values := make([]string, len(kinds))
	for index, kind := range kinds {
		values[index] = string(kind)
	}
	return values
}

func lockActiveUser(ctx context.Context, tx pgx.Tx, userID string) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status
		FROM users
		WHERE id = $1
		FOR UPDATE`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return evaluation.ErrAccountUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock active Evaluation user: %w", err)
	}
	return nil
}

var _ evaluation.Store = (*Store)(nil)
