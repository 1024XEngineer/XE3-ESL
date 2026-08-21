package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func (r *Repository) AdvanceTurn(ctx context.Context, actor practice.Actor, command practice.ConsumeTurnCommand) (practice.TurnResult, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) || !validResourceID(command.SessionID) || !validResourceID(command.TurnID) || len(command.Payload) == 0 {
		return practice.TurnResult{}, practice.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return practice.TurnResult{}, err
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return practice.TurnResult{}, err
	}
	result, err := r.advanceTurnInTransaction(ctx, tx, actor, command)
	if err != nil {
		return practice.TurnResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.TurnResult{}, err
	}
	return result, nil
}

func (r *Repository) AdvanceTurnInTransaction(ctx context.Context, tx pgx.Tx, actor practice.Actor, command practice.ConsumeTurnCommand) (practice.TurnResult, error) {
	if r == nil || tx == nil || ctx == nil || !validUserID(actor.UserID) || !validResourceID(command.SessionID) || !validResourceID(command.TurnID) || len(command.Payload) == 0 {
		return practice.TurnResult{}, practice.ErrInvalidArgument
	}
	return r.advanceTurnInTransaction(ctx, tx, actor, command)
}

func (r *Repository) advanceTurnInTransaction(ctx context.Context, tx pgx.Tx, actor practice.Actor, command practice.ConsumeTurnCommand) (practice.TurnResult, error) {
	fingerprint := sha256.Sum256(command.Payload)
	var status practice.SessionStatus
	var version, effective int
	var snapshotJSON []byte
	err := tx.QueryRow(ctx, `SELECT status,version,effective_turns,plan_snapshot FROM practice_sessions WHERE user_id=$1 AND session_id=$2 FOR UPDATE`, actor.UserID, command.SessionID).Scan(&status, &version, &effective, &snapshotJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.TurnResult{}, practice.ErrNotFound
	}
	if err != nil {
		return practice.TurnResult{}, err
	}
	var stored []byte
	var progressedAt *time.Time
	var storedEffective, storedVersion *int
	err = tx.QueryRow(ctx, `SELECT t.progress_fingerprint,t.progressed_at,t.effective_turns_after,t.session_version_after FROM practice_turns t JOIN practice_sessions s ON s.session_id=t.session_id WHERE s.user_id=$1 AND t.session_id=$2 AND t.turn_id=$3 FOR UPDATE OF t`, actor.UserID, command.SessionID, command.TurnID).Scan(&stored, &progressedAt, &storedEffective, &storedVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.TurnResult{}, practice.ErrNotFound
	}
	if err != nil {
		return practice.TurnResult{}, err
	}
	var snapshot practice.SessionSnapshot
	if decodeStrictJSON(snapshotJSON, &snapshot) != nil {
		return practice.TurnResult{}, practice.ErrConflict
	}
	turnLimit := snapshot.SessionPolicy.MaxEffectiveTurns
	if progressedAt != nil {
		if !bytes.Equal(stored, fingerprint[:]) ||
			storedEffective == nil || storedVersion == nil {
			return practice.TurnResult{}, practice.ErrIdempotencyConflict
		}
		return practice.TurnResult{SessionID: command.SessionID, TurnID: command.TurnID, Round: *storedEffective, EffectiveTurns: *storedEffective, SessionVersion: *storedVersion, TurnLimit: turnLimit, Completed: status == practice.SessionCompleted, CreatedAt: progressedAt.UTC()}, nil
	}
	if status != practice.SessionStarting && status != practice.SessionInProgress {
		return practice.TurnResult{}, practice.ErrConflict
	}
	completionMode := snapshot.SessionPolicy.CompletionMode
	nextEffective := effective
	if command.CountsTowardTurnLimit {
		nextEffective++
	}
	if nextEffective < 1 {
		return practice.TurnResult{}, practice.ErrConflict
	}
	completed := completionMode == practice.CompletionModeTurnLimited && command.CountsTowardTurnLimit && nextEffective == turnLimit
	if completionMode == practice.CompletionModeTurnLimited && (turnLimit < 1 || nextEffective > turnLimit) {
		return practice.TurnResult{}, practice.ErrSessionCompleted
	}
	nextStatus := practice.SessionInProgress
	if completed {
		nextStatus = practice.SessionCompleted
	}
	var progressed time.Time
	err = tx.QueryRow(ctx, `UPDATE practice_turns t SET progress_fingerprint=$4,effective_turns_after=$5,session_version_after=$6,progressed_at=transaction_timestamp() FROM practice_sessions s WHERE s.session_id=t.session_id AND s.user_id=$1 AND t.session_id=$2 AND t.turn_id=$3 AND t.progressed_at IS NULL RETURNING t.progressed_at`, actor.UserID, command.SessionID, command.TurnID, fingerprint[:], nextEffective, version+1).Scan(&progressed)
	if err != nil {
		return practice.TurnResult{}, classifyWriteError("advance practice turn", err)
	}
	endSQL := "ended_at"
	reason := ""
	if completed {
		endSQL = "transaction_timestamp()"
		reason = "turn_limit_reached"
	}
	tag, err := tx.Exec(ctx, `UPDATE practice_sessions SET status=$3,version=version+1,effective_turns=$4,started_at=COALESCE(started_at,transaction_timestamp()),ended_at=`+endSQL+`,end_reason=NULLIF($5,''),updated_at=transaction_timestamp() WHERE user_id=$1 AND session_id=$2 AND version=$6`, actor.UserID, command.SessionID, string(nextStatus), nextEffective, reason, version)
	if err != nil {
		return practice.TurnResult{}, classifyWriteError("advance practice session", err)
	}
	if tag.RowsAffected() != 1 {
		return practice.TurnResult{}, practice.ErrConflict
	}
	if completed {
		evidence, err := r.ReadSessionEvidence(ctx, tx, actor.UserID, command.SessionID)
		if err != nil {
			return practice.TurnResult{}, err
		}
		if err := r.completion.ScheduleCompletedSession(ctx, tx, evidence); err != nil {
			return practice.TurnResult{}, err
		}
	}
	return practice.TurnResult{SessionID: command.SessionID, TurnID: command.TurnID, Round: nextEffective, EffectiveTurns: nextEffective, SessionVersion: version + 1, TurnLimit: turnLimit, Completed: completed, CreatedAt: progressed.UTC()}, nil
}
