// Package postgres implements Practice's production PostgreSQL repository.
package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	persistence "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateSession(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.CreateSessionCommand,
) (persistence.Session, error) {
	if r == nil || r.pool == nil || !validActor(actor) ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.PlanID) == "" ||
		!validSnapshot(command.Snapshot) {
		return persistence.Session{}, persistence.ErrInvalidArgument
	}

	targets, err := json.Marshal(command.Snapshot.TargetIDs)
	if err != nil {
		return persistence.Session{}, persistence.ErrInvalidArgument
	}
	participants, err := json.Marshal(command.Snapshot.Participants)
	if err != nil {
		return persistence.Session{}, persistence.ErrInvalidArgument
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.Session{}, fmt.Errorf("begin create practice session: %w", err)
	}
	defer rollback(ctx, tx)

	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.Session{}, err
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, status,
			version, effective_turns, started_at
		)
		VALUES ($1, $2, $3, 'active', 1, 0, transaction_timestamp())
		RETURNING created_at, updated_at, started_at
	`, actor.UserID, command.SessionID, command.PlanID)

	session := persistence.Session{
		ID:          command.SessionID,
		OwnerUserID: actor.UserID,
		PlanID:      command.PlanID,
		Status:      persistence.SessionStatusActive,
		Version:     1,
		Snapshot:    cloneSnapshot(command.Snapshot),
	}
	if err := row.Scan(
		&session.CreatedAt,
		&session.UpdatedAt,
		&session.StartedAt,
	); err != nil {
		return persistence.Session{}, classifyWriteError(
			"insert practice session",
			err,
		)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, mode, target_ids,
			participants, turn_limit
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, actor.UserID, command.SessionID, command.Snapshot.Mode, targets,
		participants, command.Snapshot.TurnLimit); err != nil {
		return persistence.Session{}, classifyWriteError(
			"insert practice session snapshot",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return persistence.Session{}, fmt.Errorf("commit practice session: %w", err)
	}
	return session, nil
}

func (r *Repository) GetSession(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
) (persistence.Session, error) {
	if r == nil || r.pool == nil || !validActor(actor) ||
		strings.TrimSpace(sessionID) == "" {
		return persistence.Session{}, persistence.ErrInvalidArgument
	}

	return scanSession(r.pool.QueryRow(ctx, sessionSelect+`
		WHERE s.owner_user_id = $1 AND s.session_id = $2
	`, actor.UserID, sessionID))
}

func (r *Repository) ListSessions(
	ctx context.Context,
	actor persistence.Actor,
) ([]persistence.Session, error) {
	if r == nil || r.pool == nil || !validActor(actor) {
		return nil, persistence.ErrInvalidArgument
	}

	rows, err := r.pool.Query(ctx, sessionSelect+`
		WHERE s.owner_user_id = $1
		ORDER BY s.created_at, s.session_id
	`, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list practice sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]persistence.Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list practice sessions: %w", err)
	}
	return sessions, nil
}

func (r *Repository) ConsumeTurn(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.ConsumeTurnCommand,
) (persistence.TurnResult, error) {
	if r == nil || r.pool == nil || !validActor(actor) ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.TurnID) == "" ||
		len(command.Payload) == 0 {
		return persistence.TurnResult{}, persistence.ErrInvalidArgument
	}
	fingerprint := sha256.Sum256(command.Payload)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.TurnResult{}, fmt.Errorf("begin consume practice turn: %w", err)
	}
	defer rollback(ctx, tx)

	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.TurnResult{}, err
	}

	var status persistence.SessionStatus
	var version, effectiveTurns, turnLimit int
	err = tx.QueryRow(ctx, `
		SELECT s.status, s.version, s.effective_turns, snapshot.turn_limit
		FROM practice_sessions AS s
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = s.owner_user_id
		 AND snapshot.session_id = s.session_id
		WHERE s.owner_user_id = $1 AND s.session_id = $2
		FOR UPDATE OF s
	`, actor.UserID, command.SessionID).Scan(
		&status,
		&version,
		&effectiveTurns,
		&turnLimit,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.TurnResult{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.TurnResult{}, fmt.Errorf("lock practice session: %w", err)
	}

	result, storedFingerprint, err := loadTurnResult(
		ctx,
		tx,
		actor.UserID,
		command.SessionID,
		command.TurnID,
	)
	if err == nil {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return persistence.TurnResult{}, persistence.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.TurnResult{}, fmt.Errorf("commit replayed practice turn: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return persistence.TurnResult{}, err
	}
	if status == persistence.SessionStatusCompleted || effectiveTurns >= turnLimit {
		return persistence.TurnResult{}, persistence.ErrSessionCompleted
	}

	round := effectiveTurns + 1
	completed := round == turnLimit
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
			"insert practice turn result",
			err,
		)
	}

	nextStatus := persistence.SessionStatusActive
	if completed {
		nextStatus = persistence.SessionStatusCompleted
	}
	tag, err := tx.Exec(ctx, `
		UPDATE practice_sessions
		SET status = $3,
		    version = $4,
		    effective_turns = $5,
		    updated_at = transaction_timestamp(),
		    completed_at = CASE
		        WHEN $3 = 'completed' THEN transaction_timestamp()
		        ELSE NULL
		    END
		WHERE owner_user_id = $1
		  AND session_id = $2
		  AND version = $6
		  AND effective_turns = $7
	`, actor.UserID, command.SessionID, nextStatus, nextVersion, round,
		version, effectiveTurns)
	if err != nil {
		return persistence.TurnResult{}, fmt.Errorf("advance practice session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return persistence.TurnResult{}, persistence.ErrConflict
	}

	if err := tx.Commit(ctx); err != nil {
		return persistence.TurnResult{}, fmt.Errorf("commit practice turn: %w", err)
	}
	return result, nil
}

func (r *Repository) DeleteSession(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
) error {
	if r == nil || r.pool == nil || !validActor(actor) ||
		strings.TrimSpace(sessionID) == "" {
		return persistence.ErrInvalidArgument
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin delete practice session: %w", err)
	}
	defer rollback(ctx, tx)

	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `
		DELETE FROM practice_sessions
		WHERE owner_user_id = $1 AND session_id = $2
	`, actor.UserID, sessionID)
	if err != nil {
		return fmt.Errorf("delete practice session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persistence.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deleted practice session: %w", err)
	}
	return nil
}

func (r *Repository) DeleteUserData(
	ctx context.Context,
	deletion persistence.DeletionContext,
) error {
	if r == nil || r.pool == nil || !validUserID(deletion.UserID) ||
		deletion.Generation == 0 {
		return persistence.ErrInvalidArgument
	}

	if _, err := r.pool.Exec(ctx, `
		DELETE FROM practice_sessions WHERE owner_user_id = $1
	`, deletion.UserID); err != nil {
		return fmt.Errorf("delete practice user data: %w", err)
	}
	return nil
}

const sessionSelect = `
	SELECT
		s.session_id, s.owner_user_id, s.plan_id, s.status,
		s.version, s.effective_turns, s.created_at, s.updated_at,
		s.started_at, s.completed_at,
		snapshot.mode, snapshot.target_ids, snapshot.participants,
		snapshot.turn_limit
	FROM practice_sessions AS s
	JOIN practice_session_snapshots AS snapshot
	  ON snapshot.owner_user_id = s.owner_user_id
	 AND snapshot.session_id = s.session_id
`

type rowScanner interface {
	Scan(...any) error
}

func scanSession(row rowScanner) (persistence.Session, error) {
	var session persistence.Session
	var targets, participants []byte
	err := row.Scan(
		&session.ID,
		&session.OwnerUserID,
		&session.PlanID,
		&session.Status,
		&session.Version,
		&session.EffectiveTurns,
		&session.CreatedAt,
		&session.UpdatedAt,
		&session.StartedAt,
		&session.CompletedAt,
		&session.Snapshot.Mode,
		&targets,
		&participants,
		&session.Snapshot.TurnLimit,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.Session{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.Session{}, fmt.Errorf("read practice session: %w", err)
	}
	if err := json.Unmarshal(targets, &session.Snapshot.TargetIDs); err != nil {
		return persistence.Session{}, fmt.Errorf("decode practice target snapshot: %w", err)
	}
	if err := json.Unmarshal(participants, &session.Snapshot.Participants); err != nil {
		return persistence.Session{}, fmt.Errorf("decode practice participant snapshot: %w", err)
	}
	return session, nil
}

func loadTurnResult(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	sessionID string,
	turnID string,
) (persistence.TurnResult, []byte, error) {
	var result persistence.TurnResult
	var fingerprint []byte
	err := tx.QueryRow(ctx, `
		SELECT
			session_id, turn_id, payload_fingerprint, round_number,
			effective_turns, session_version, completed,
			completion_token, created_at
		FROM practice_turn_results
		WHERE owner_user_id = $1 AND session_id = $2 AND turn_id = $3
	`, ownerUserID, sessionID, turnID).Scan(
		&result.SessionID,
		&result.TurnID,
		&fingerprint,
		&result.Round,
		&result.EffectiveTurns,
		&result.SessionVersion,
		&result.Completed,
		&result.CompletionToken,
		&result.CreatedAt,
	)
	if err != nil {
		return persistence.TurnResult{}, nil, err
	}
	return result, fingerprint, nil
}

// lockActiveActor joins a Practice write to Identity's account-deletion
// fence without copying account state into Practice. FOR SHARE conflicts with
// the coordinator's account-status update, so either this write commits first
// or it observes the durable non-active status and fails closed.
func lockActiveActor(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
) error {
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT true
		FROM identity_users
		WHERE id = $1 AND account_status = 'active'
		FOR SHARE
	`, userID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("verify active practice actor: %w", err)
	}
	return nil
}

func validActor(actor persistence.Actor) bool {
	return validUserID(actor.UserID) && validUserID(actor.SessionID)
}

func validUserID(userID string) bool {
	var parsed pgtype.UUID
	return parsed.Scan(strings.TrimSpace(userID)) == nil && parsed.Valid
}

func validSnapshot(snapshot persistence.SessionSnapshot) bool {
	if strings.TrimSpace(snapshot.Mode) == "" || snapshot.TurnLimit <= 0 ||
		snapshot.TargetIDs == nil || snapshot.Participants == nil {
		return false
	}
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	subjects := make(map[string]struct{}, len(snapshot.Participants))
	for _, participant := range snapshot.Participants {
		if strings.TrimSpace(participant.ParticipantID) == "" ||
			strings.TrimSpace(participant.ParticipantRole) == "" ||
			strings.TrimSpace(participant.SubjectRef.Namespace) == "" ||
			strings.TrimSpace(participant.SubjectRef.SubjectID) == "" ||
			participant.Order < 0 {
			return false
		}
		if _, exists := participantIDs[participant.ParticipantID]; exists {
			return false
		}
		participantIDs[participant.ParticipantID] = struct{}{}
		subjectKey := participant.SubjectRef.Namespace + "\x00" +
			participant.SubjectRef.SubjectID
		if _, exists := subjects[subjectKey]; exists {
			return false
		}
		subjects[subjectKey] = struct{}{}
	}
	return true
}

func cloneSnapshot(snapshot persistence.SessionSnapshot) persistence.SessionSnapshot {
	cloned := snapshot
	cloned.TargetIDs = append([]string(nil), snapshot.TargetIDs...)
	cloned.Participants = append(
		[]persistence.ParticipantSnapshot(nil),
		snapshot.Participants...,
	)
	return cloned
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func classifyWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return persistence.ErrNotFound
		case "23505":
			return persistence.ErrConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ persistence.Repository = (*Repository)(nil)
