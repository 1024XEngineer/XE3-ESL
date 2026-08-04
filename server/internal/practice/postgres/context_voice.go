package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

// AdvanceContextVoiceTurn is the formal Context-owned voice progression
// boundary. It intentionally does not reuse the legacy Session state machine:
// formal lifecycle values, frozen policy, and completion reason are updated in
// the same transaction as the owner-scoped Turn result.
func (r *Repository) AdvanceContextVoiceTurn(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.ConsumeTurnCommand,
) (persistence.TurnResult, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.TurnID) == "" ||
		len(command.Payload) == 0 {
		return persistence.TurnResult{}, persistence.ErrInvalidArgument
	}
	fingerprint := sha256.Sum256(command.Payload)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.TurnResult{},
			fmt.Errorf("begin context voice Turn: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.TurnResult{}, err
	}

	var status persistence.ContextSessionStatus
	var snapshotID string
	var snapshotDocument []byte
	var version, effectiveTurns, turnLimit int
	err = tx.QueryRow(ctx, `
		SELECT session.status, session.version, session.effective_turns,
		       snapshot.turn_limit, snapshot.snapshot_id,
		       snapshot.snapshot_document
		FROM practice_sessions AS session
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = session.owner_user_id
		 AND snapshot.session_id = session.session_id
		 AND snapshot.snapshot_id = session.snapshot_id
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		FOR UPDATE OF session
	`, actor.UserID, command.SessionID).Scan(
		&status,
		&version,
		&effectiveTurns,
		&turnLimit,
		&snapshotID,
		&snapshotDocument,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.TurnResult{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.TurnResult{},
			fmt.Errorf("lock context voice Session: %w", err)
	}

	result, storedFingerprint, err := loadTurnResult(
		ctx,
		tx,
		actor.UserID,
		command.TurnID,
	)
	if err == nil {
		if result.SessionID != command.SessionID ||
			!bytes.Equal(storedFingerprint, fingerprint[:]) {
			return persistence.TurnResult{},
				persistence.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.TurnResult{},
				fmt.Errorf("commit replayed context voice Turn: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return persistence.TurnResult{}, err
	}

	snapshot, err := decodeContextSnapshot(snapshotDocument)
	if err != nil ||
		snapshot.ID != snapshotID ||
		snapshot.SessionID != command.SessionID ||
		snapshot.SessionPolicy.MaxEffectiveTurns != turnLimit ||
		turnLimit < 1 || turnLimit > 14 ||
		effectiveTurns < 0 || effectiveTurns > turnLimit {
		return persistence.TurnResult{}, persistence.ErrConflict
	}
	switch status {
	case persistence.ContextSessionStarting:
		if effectiveTurns != 0 {
			return persistence.TurnResult{}, persistence.ErrConflict
		}
	case persistence.ContextSessionProgress:
	case persistence.ContextSessionCompleted,
		persistence.ContextSessionEndedEarly:
		return persistence.TurnResult{}, persistence.ErrSessionCompleted
	default:
		// paused and unknown lifecycle states cannot consume a Turn.
		return persistence.TurnResult{}, persistence.ErrConflict
	}
	if effectiveTurns >= turnLimit {
		return persistence.TurnResult{}, persistence.ErrSessionCompleted
	}

	round := effectiveTurns
	if command.CountsTowardTurnLimit {
		round++
	}
	if round < 1 {
		return persistence.TurnResult{}, persistence.ErrConflict
	}
	completed := command.CountsTowardTurnLimit && round == turnLimit
	nextVersion := version + 1
	completionToken := ""
	if completed {
		completionToken = fmt.Sprintf(
			"practice-session:%s:completed:v%d",
			command.SessionID,
			nextVersion,
		)
	}
	result = persistence.TurnResult{
		SessionID:       command.SessionID,
		TurnID:          command.TurnID,
		Round:           round,
		EffectiveTurns:  round,
		SessionVersion:  nextVersion,
		TurnLimit:       turnLimit,
		Completed:       completed,
		CompletionToken: completionToken,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_turn_results (
			owner_user_id, session_id, turn_id, payload_fingerprint,
			round_number, effective_turns, session_version,
			completed, completion_token
		)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8)
		RETURNING created_at
	`, actor.UserID, command.SessionID, command.TurnID, fingerprint[:],
		round, nextVersion, completed, completionToken).Scan(&result.CreatedAt)
	if err != nil {
		return persistence.TurnResult{}, classifyWriteError(
			"insert context voice Turn result",
			err,
		)
	}

	nextStatus := persistence.ContextSessionProgress
	if completed {
		nextStatus = persistence.ContextSessionCompleted
	}
	tag, err := tx.Exec(ctx, `
		UPDATE practice_sessions
		SET status = $3,
		    version = $4,
		    effective_turns = $5,
		    started_at = COALESCE(
		        started_at,
		        transaction_timestamp()
		    ),
		    completed_at = CASE
		        WHEN $6 THEN transaction_timestamp()
		        ELSE NULL
		    END,
		    end_reason = CASE
		        WHEN $6 THEN 'TURN_LIMIT_REACHED'
		        ELSE NULL
		    END,
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND session_id = $2
		  AND version = $7
		  AND effective_turns = $8
		  AND status = $9
	`, actor.UserID, command.SessionID, nextStatus, nextVersion, round,
		completed, version, effectiveTurns, status)
	if err != nil {
		return persistence.TurnResult{},
			classifyContextWriteError(
				"advance context voice Session",
				err,
			)
	}
	if tag.RowsAffected() != 1 {
		return persistence.TurnResult{}, persistence.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.TurnResult{},
			fmt.Errorf("commit context voice Turn: %w", err)
	}
	return result, nil
}

var _ persistence.ContextVoiceRepository = (*Repository)(nil)
