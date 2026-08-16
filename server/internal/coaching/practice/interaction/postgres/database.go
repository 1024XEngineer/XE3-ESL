package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
)

const evidenceSourceLockNamespace = "evidence-source"

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type rowScanner interface {
	Scan(...any) error
}

func (r *Repository) beginActorRead(
	ctx context.Context,
	actor practiceinteraction.Actor,
) (pgx.Tx, error) {
	return r.beginOwnerRead(ctx, actor.UserID)
}

func (r *Repository) beginOwnerRead(
	ctx context.Context,
	ownerUserID string,
) (pgx.Tx, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	if _, err := ensureActorWritable(ctx, tx, ownerUserID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// Deletion generations belonged to the removed cleanup graph. Active-account
// ownership is now the only write fence and generation zero is the sole value
// exposed to in-flight voice jobs.
func ensureActorWritable(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
) (int64, error) {
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT true FROM users WHERE id = $1 AND status = 'active' FOR SHARE
	`, ownerUserID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return 0, safeDatabaseError(err)
	}
	return 0, nil
}

// confirm operations enqueue an Evaluation in the same transaction. Taking
// the user row exclusively at the transaction boundary keeps the global
// user -> evaluation lock order and avoids a concurrent SHARE -> UPDATE
// upgrade deadlock.
func ensureActorWritableForEvaluation(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
) error {
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT true FROM users WHERE id = $1 AND status = 'active' FOR UPDATE
	`, ownerUserID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

func ensureJobWritable(
	ctx context.Context,
	tx pgx.Tx,
	job practiceinteraction.JobContext,
) error {
	if job.DeletionGeneration != 0 {
		return practiceinteraction.ErrPersistenceConflict
	}
	_, err := ensureActorWritable(ctx, tx, job.OwnerUserID)
	return err
}

func lockKey(ctx context.Context, tx pgx.Tx, parts ...string) error {
	key := strings.Join(parts, "\x1f")
	if _, err := tx.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		key,
	); err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

func lockEvidenceSourceSession(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	sessionID string,
) error {
	return lockKey(ctx, tx, ownerUserID, evidenceSourceLockNamespace, sessionID)
}

func lockCandidateEvidenceSourceSession(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	candidateID string,
) (string, error) {
	var sessionID string
	err := tx.QueryRow(ctx, `
		SELECT t.session_id FROM practice_turns t
		JOIN practice_sessions s ON s.session_id=t.session_id
		WHERE s.user_id = $1 AND t.candidate_id = $2
	`, ownerUserID, candidateID).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return "", safeDatabaseError(err)
	}
	if err := lockEvidenceSourceSession(ctx, tx, ownerUserID, sessionID); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (r *Repository) reachedWriteFence() {
	if r.afterWriteFence != nil {
		r.afterWriteFence()
	}
}

func transactionTime(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&now); err != nil {
		return time.Time{}, safeDatabaseError(err)
	}
	return now.UTC(), nil
}

func validInputActor(actor practiceinteraction.Actor) bool {
	return actor.Valid() && validUUID(actor.UserID)
}

func validJob(job practiceinteraction.JobContext) bool {
	return job.Valid() && validUUID(job.OwnerUserID) && job.DeletionGeneration == 0
}

func validProcessingFailureCode(code string) bool {
	switch code {
	case "invalid_request", "configuration", "authentication",
		"authorization", "quota_exhausted", "rate_limited", "timeout",
		"provider_timeout", "provider_unavailable", "invalid_response",
		"cancelled", "legacy_provider_failure":
		return true
	default:
		return false
	}
}

func validUUID(value string) bool {
	var identifier pgtype.UUID
	return identifier.Scan(value) == nil && identifier.Valid
}

func containsParticipant(participants []string, participantID string) bool {
	for _, candidate := range participants {
		if candidate == participantID {
			return true
		}
	}
	return false
}

func confirmationFingerprint(command practiceinteraction.ConfirmTurnCommand) []byte {
	payload := command.CandidateID + "\x00" +
		strings.TrimSpace(command.ConfirmedText) + "\x00" + command.RetryTurnID
	digest := sha256.Sum256([]byte(payload))
	return digest[:]
}

func databaseTime(now func() time.Time) time.Time {
	return now().UTC().Truncate(time.Microsecond)
}

func safeDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23505", "23514":
			return practiceinteraction.ErrPersistenceConflict
		}
	}
	return practiceinteraction.ErrPersistenceUnavailable
}

func mapPracticeError(err error) error {
	switch {
	case errors.Is(err, practice.ErrNotFound):
		return practiceinteraction.ErrPersistenceNotFound
	case errors.Is(err, practice.ErrConflict),
		errors.Is(err, practice.ErrIdempotencyConflict),
		errors.Is(err, practice.ErrSessionCompleted):
		return practiceinteraction.ErrPersistenceConflict
	case errors.Is(err, practice.ErrInvalidArgument):
		return practiceinteraction.ErrPersistenceInvalid
	default:
		return err
	}
}
