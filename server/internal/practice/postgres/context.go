package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func (r *Repository) ReplayContextSession(
	ctx context.Context,
	actor persistence.Actor,
	intent persistence.ContextIdempotencyIntent,
) (persistence.ContextSessionBootstrap, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextIntent(intent) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	record, found, err := loadContextIdempotency(
		ctx,
		r.pool,
		actor.UserID,
		intent,
		true,
	)
	if err != nil || !found {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if record.ResourceKind != "session" {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrIdempotencyConflict
	}
	bootstrap, err := r.readStoredContextBootstrap(
		ctx,
		actor,
		record.ResourceID,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	return bootstrap, true, nil
}

func (r *Repository) CreateContextSession(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.CreateContextSessionCommand,
) (persistence.ContextSessionBootstrap, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validCreateContextSessionCommand(actor, command) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			fmt.Errorf("begin create Practice Session: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if err := lockContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	record, found, err := loadContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		false,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if found {
		if record.ResourceKind != "session" {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrIdempotencyConflict
		}
		bootstrap, err := readContextBootstrapWithQuery(
			ctx,
			tx,
			actor.UserID,
			record.ResourceID,
			false,
		)
		if err != nil {
			return persistence.ContextSessionBootstrap{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.ContextSessionBootstrap{}, false,
				fmt.Errorf("commit replayed Practice Session: %w", err)
		}
		return bootstrap, true, nil
	}

	snapshot := command.Snapshot
	var createdAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, plan_revision,
			status, version, effective_turns, snapshot_id,
			scene_family, scene_model
		) VALUES (
			$1, $2, $3, $4, 'starting', 1, 0, $5, $6, $7
		)
		RETURNING created_at
	`,
		actor.UserID,
		command.SessionID,
		command.PlanID,
		command.PlanRevision,
		command.SnapshotID,
		snapshot.SceneFamily,
		snapshot.SceneModel,
	).Scan(&createdAt)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			classifyContextWriteError("insert Practice Session", err)
	}
	if !createdAt.Valid {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	snapshot.CreatedAt = createdAt.Time.UTC()
	document, err := json.Marshal(snapshot)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	targetIDs, err := json.Marshal([]string{command.PlanID})
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	participants, err := json.Marshal(snapshot.Participants)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, mode, target_ids,
			participants, turn_limit, snapshot_id, snapshot_document
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		actor.UserID,
		command.SessionID,
		snapshot.SceneFamily,
		targetIDs,
		participants,
		snapshot.SessionPolicy.MaxEffectiveTurns,
		command.SnapshotID,
		document,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			classifyContextWriteError("insert Practice Session snapshot", err)
	}
	bootstrap := persistence.ContextSessionBootstrap{
		Session: persistence.ContextSession{
			ID:           command.SessionID,
			PlanID:       command.PlanID,
			PlanRevision: command.PlanRevision,
			SceneFamily:  snapshot.SceneFamily,
			SceneModel:   snapshot.SceneModel,
			SnapshotID:   command.SnapshotID,
			Status:       persistence.ContextSessionStarting,
			Version:      1,
			CreatedAt:    createdAt.Time.UTC(),
		},
		Snapshot: snapshot,
	}
	if !validStoredContextBootstrap(bootstrap) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	if err := saveContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"session",
		command.SessionID,
		201,
		bootstrap,
	); err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			fmt.Errorf("commit created Practice Session: %w", err)
	}
	return bootstrap, false, nil
}

func (r *Repository) GetContextSession(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
) (persistence.ContextSession, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(sessionID) {
		return persistence.ContextSession{}, persistence.ErrInvalidArgument
	}
	return scanContextSession(r.pool.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, sessionID))
}

func (r *Repository) GetContextSessionSnapshot(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
) (persistence.ContextSessionSnapshot, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(sessionID) {
		return persistence.ContextSessionSnapshot{},
			persistence.ErrInvalidArgument
	}
	bootstrap, err := r.readStoredContextBootstrap(ctx, actor, sessionID)
	if err != nil {
		return persistence.ContextSessionSnapshot{}, err
	}
	return bootstrap.Snapshot, nil
}

func (r *Repository) ResolveContextSessionByPlan(
	ctx context.Context,
	actor persistence.Actor,
	planID string,
) (persistence.ContextSessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(planID) {
		return persistence.ContextSessionBootstrap{},
			persistence.ErrInvalidArgument
	}
	rows, err := r.pool.Query(ctx, contextBootstrapSelect+`
		WHERE session.owner_user_id = $1
		  AND session.plan_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		ORDER BY
			CASE session.status
				WHEN 'starting' THEN 0
				WHEN 'in_progress' THEN 0
				WHEN 'paused' THEN 0
				ELSE 1
			END,
			session.updated_at DESC,
			session.session_id
	`, actor.UserID, planID)
	if err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("resolve Practice Session by Plan: %w", err)
	}
	defer rows.Close()
	values := make([]persistence.ContextSessionBootstrap, 0, 2)
	for rows.Next() {
		value, err := scanContextBootstrap(rows)
		if err != nil {
			return persistence.ContextSessionBootstrap{}, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("resolve Practice Session rows: %w", err)
	}
	if len(values) == 0 {
		return persistence.ContextSessionBootstrap{}, persistence.ErrNotFound
	}
	if isEffectiveContextSessionStatus(values[0].Session.Status) {
		return values[0], nil
	}
	if len(values) != 1 {
		return persistence.ContextSessionBootstrap{}, persistence.ErrConflict
	}
	return values[0], nil
}

func (r *Repository) ReplayContextVoiceStart(
	ctx context.Context,
	actor persistence.Actor,
	intent persistence.ContextIdempotencyIntent,
) (persistence.ContextSessionBootstrap, bool, error) {
	return r.ReplayContextSession(ctx, actor, intent)
}

func (r *Repository) ActivateContextSession(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
	planID string,
	intent persistence.ContextIdempotencyIntent,
) (persistence.ContextSessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(sessionID) ||
		!validContextResourceID(planID) || !validContextIntent(intent) {
		return persistence.ContextSessionBootstrap{},
			persistence.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("begin activate Practice Session: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	if err := lockContextIdempotency(ctx, tx, actor.UserID, intent); err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	record, found, err := loadContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		intent,
		false,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	if found {
		if record.ResourceKind != "session" {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrIdempotencyConflict
		}
		bootstrap, err := readContextBootstrapWithQuery(
			ctx,
			tx,
			actor.UserID,
			record.ResourceID,
			false,
		)
		if err != nil {
			return persistence.ContextSessionBootstrap{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.ContextSessionBootstrap{},
				fmt.Errorf("commit replayed Practice activation: %w", err)
		}
		return bootstrap, nil
	}
	bootstrap, err := readContextBootstrapWithQuery(
		ctx,
		tx,
		actor.UserID,
		sessionID,
		true,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	if bootstrap.Session.PlanID != planID {
		return persistence.ContextSessionBootstrap{}, persistence.ErrNotFound
	}
	switch bootstrap.Session.Status {
	case persistence.ContextSessionStarting:
		tag, err := tx.Exec(ctx, `
			UPDATE practice_sessions
			SET status = 'in_progress',
			    version = version + 1,
			    started_at = transaction_timestamp(),
			    updated_at = transaction_timestamp()
			WHERE owner_user_id = $1
			  AND session_id = $2
			  AND plan_id = $3
			  AND status = 'starting'
			  AND version = $4
		`, actor.UserID, sessionID, planID, bootstrap.Session.Version)
		if err != nil {
			return persistence.ContextSessionBootstrap{},
				classifyContextWriteError("activate Practice Session", err)
		}
		if tag.RowsAffected() != 1 {
			return persistence.ContextSessionBootstrap{}, persistence.ErrConflict
		}
		bootstrap, err = readContextBootstrapWithQuery(
			ctx,
			tx,
			actor.UserID,
			sessionID,
			false,
		)
		if err != nil {
			return persistence.ContextSessionBootstrap{}, err
		}
	case persistence.ContextSessionProgress:
	case persistence.ContextSessionPaused:
		return persistence.ContextSessionBootstrap{}, persistence.ErrConflict
	default:
		return persistence.ContextSessionBootstrap{}, persistence.ErrConflict
	}
	if err := saveContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		intent,
		"session",
		bootstrap.Session.ID,
		201,
		bootstrap,
	); err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("commit activated Practice Session: %w", err)
	}
	return bootstrap, nil
}

func (r *Repository) TransitionContextSession(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.TransitionContextSessionCommand,
) (persistence.ContextSession, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validTransitionCommand(command) {
		return persistence.ContextSession{}, false,
			persistence.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.ContextSession{}, false,
			fmt.Errorf("begin Practice Session transition: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.ContextSession{}, false, err
	}
	if err := lockContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return persistence.ContextSession{}, false, err
	}
	record, found, err := loadContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		false,
	)
	if err != nil {
		return persistence.ContextSession{}, false, err
	}
	resourceKind := string(command.Transition)
	if found {
		if record.ResourceKind != resourceKind {
			return persistence.ContextSession{}, false,
				persistence.ErrIdempotencyConflict
		}
		var session persistence.ContextSession
		if err := json.Unmarshal(record.ResponseBody, &session); err != nil ||
			session.ID != record.ResourceID || !validStoredContextSession(session) {
			return persistence.ContextSession{}, false,
				persistence.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.ContextSession{}, false,
				fmt.Errorf("commit replayed Practice transition: %w", err)
		}
		return session, true, nil
	}
	session, err := scanContextSession(tx.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		FOR UPDATE OF session
	`, actor.UserID, command.SessionID))
	if err != nil {
		return persistence.ContextSession{}, false, err
	}
	if session.Version != command.ExpectedSessionVersion {
		return persistence.ContextSession{}, false, persistence.ErrConflict
	}
	nextStatus, allowed := contextTransitionStatus(
		session.Status,
		command.Transition,
	)
	if !allowed {
		return persistence.ContextSession{}, false, persistence.ErrConflict
	}
	startedAtExpression := "started_at"
	completedAtExpression := "NULL"
	endReasonExpression := "NULL"
	if command.Transition == persistence.ContextSessionEndEarly {
		startedAtExpression = "COALESCE(started_at, transaction_timestamp())"
		completedAtExpression = "transaction_timestamp()"
		endReasonExpression = "'USER_ENDED'"
	}
	query := fmt.Sprintf(`
		UPDATE practice_sessions
		SET status = $3,
		    version = $4,
		    started_at = %s,
		    completed_at = %s,
		    end_reason = %s,
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND session_id = $2
		  AND version = $5
	`, startedAtExpression, completedAtExpression, endReasonExpression)
	tag, err := tx.Exec(
		ctx,
		query,
		actor.UserID,
		command.SessionID,
		nextStatus,
		session.Version+1,
		session.Version,
	)
	if err != nil {
		return persistence.ContextSession{}, false,
			classifyContextWriteError("transition Practice Session", err)
	}
	if tag.RowsAffected() != 1 {
		return persistence.ContextSession{}, false, persistence.ErrConflict
	}
	session, err = scanContextSession(tx.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, command.SessionID))
	if err != nil {
		return persistence.ContextSession{}, false, err
	}
	if err := saveContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		resourceKind,
		session.ID,
		200,
		session,
	); err != nil {
		return persistence.ContextSession{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.ContextSession{}, false,
			fmt.Errorf("commit Practice Session transition: %w", err)
	}
	return session, false, nil
}

func (r *Repository) readStoredContextBootstrap(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
) (persistence.ContextSessionBootstrap, error) {
	return readContextBootstrapWithQuery(
		ctx,
		r.pool,
		actor.UserID,
		sessionID,
		false,
	)
}

func readContextBootstrapWithQuery(
	ctx context.Context,
	query contextQuery,
	ownerUserID string,
	sessionID string,
	forUpdate bool,
) (persistence.ContextSessionBootstrap, error) {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE OF session"
	}
	return scanContextBootstrap(query.QueryRow(ctx, contextBootstrapSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`+suffix, ownerUserID, sessionID))
}

const contextSessionSelect = `
	SELECT
		session.session_id,
		session.plan_id,
		session.plan_revision,
		session.scene_family,
		session.scene_model,
		session.snapshot_id,
		session.status,
		session.version,
		session.effective_turns,
		session.started_at,
		session.completed_at,
		session.end_reason,
		session.created_at
	FROM practice_sessions AS session
	JOIN identity_users AS owner
	  ON owner.id = session.owner_user_id
	LEFT JOIN practice_deletion_fences AS fence
	  ON fence.owner_user_id = session.owner_user_id
`

const contextBootstrapSelect = `
	SELECT
		session.session_id,
		session.plan_id,
		session.plan_revision,
		session.scene_family,
		session.scene_model,
		session.snapshot_id,
		session.status,
		session.version,
		session.effective_turns,
		session.started_at,
		session.completed_at,
		session.end_reason,
		session.created_at,
		snapshot.snapshot_document
	FROM practice_sessions AS session
	JOIN practice_session_snapshots AS snapshot
	  ON snapshot.owner_user_id = session.owner_user_id
	 AND snapshot.session_id = session.session_id
	 AND snapshot.snapshot_id = session.snapshot_id
	JOIN identity_users AS owner
	  ON owner.id = session.owner_user_id
	LEFT JOIN practice_deletion_fences AS fence
	  ON fence.owner_user_id = session.owner_user_id
`

type contextRowScanner interface {
	Scan(...any) error
}

type contextQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type contextIdempotencyRecord struct {
	ResourceKind string
	ResourceID   string
	ResponseBody []byte
}

func scanContextSession(row contextRowScanner) (persistence.ContextSession, error) {
	var session persistence.ContextSession
	var startedAt, completedAt pgtype.Timestamptz
	var endReason pgtype.Text
	err := row.Scan(
		&session.ID,
		&session.PlanID,
		&session.PlanRevision,
		&session.SceneFamily,
		&session.SceneModel,
		&session.SnapshotID,
		&session.Status,
		&session.Version,
		&session.EffectiveTurns,
		&startedAt,
		&completedAt,
		&endReason,
		&session.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ContextSession{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.ContextSession{},
			fmt.Errorf("read Practice Session: %w", err)
	}
	if startedAt.Valid {
		value := startedAt.Time.UTC()
		session.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		session.EndedAt = &value
	}
	if endReason.Valid {
		session.EndReason = endReason.String
	}
	if !validStoredContextSession(session) {
		return persistence.ContextSession{}, persistence.ErrConflict
	}
	return session, nil
}

func scanContextBootstrap(
	row contextRowScanner,
) (persistence.ContextSessionBootstrap, error) {
	var session persistence.ContextSession
	var startedAt, completedAt pgtype.Timestamptz
	var endReason pgtype.Text
	var document []byte
	err := row.Scan(
		&session.ID,
		&session.PlanID,
		&session.PlanRevision,
		&session.SceneFamily,
		&session.SceneModel,
		&session.SnapshotID,
		&session.Status,
		&session.Version,
		&session.EffectiveTurns,
		&startedAt,
		&completedAt,
		&endReason,
		&session.CreatedAt,
		&document,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ContextSessionBootstrap{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("read Practice Session snapshot: %w", err)
	}
	if startedAt.Valid {
		value := startedAt.Time.UTC()
		session.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		session.EndedAt = &value
	}
	if endReason.Valid {
		session.EndReason = endReason.String
	}
	var snapshot persistence.ContextSessionSnapshot
	if err := json.Unmarshal(document, &snapshot); err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("decode Practice Session snapshot: %w", err)
	}
	bootstrap := persistence.ContextSessionBootstrap{
		Session:  session,
		Snapshot: snapshot,
	}
	if !validStoredContextBootstrap(bootstrap) {
		return persistence.ContextSessionBootstrap{}, persistence.ErrConflict
	}
	return bootstrap, nil
}

func decodeContextSnapshot(
	document []byte,
) (persistence.ContextSessionSnapshot, error) {
	var snapshot persistence.ContextSessionSnapshot
	if err := json.Unmarshal(document, &snapshot); err != nil {
		return persistence.ContextSessionSnapshot{}, err
	}
	return snapshot, nil
}

func lockContextIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	intent persistence.ContextIdempotencyIntent,
) error {
	scope, err := json.Marshal([]string{
		ownerUserID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
	})
	if err != nil {
		return persistence.ErrInvalidArgument
	}
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
	`, string(scope)); err != nil {
		return fmt.Errorf("lock Practice idempotency scope: %w", err)
	}
	return nil
}

func loadContextIdempotency(
	ctx context.Context,
	query contextQuery,
	ownerUserID string,
	intent persistence.ContextIdempotencyIntent,
	requireActiveActor bool,
) (contextIdempotencyRecord, bool, error) {
	activePredicate := ""
	if requireActiveActor {
		activePredicate = `
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL`
	}
	var record contextIdempotencyRecord
	var storedFingerprint []byte
	err := query.QueryRow(ctx, `
		SELECT record.payload_fingerprint,
		       record.resource_kind,
		       record.resource_id,
		       record.response_body
		FROM practice_idempotency_records AS record
		JOIN identity_users AS owner
		  ON owner.id = record.owner_user_id
		LEFT JOIN practice_deletion_fences AS fence
		  ON fence.owner_user_id = record.owner_user_id
		WHERE record.owner_user_id = $1
		  AND record.method = $2
		  AND record.canonical_path = $3
		  AND record.idempotency_key = $4`+activePredicate,
		ownerUserID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
	).Scan(
		&storedFingerprint,
		&record.ResourceKind,
		&record.ResourceID,
		&record.ResponseBody,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return contextIdempotencyRecord{}, false, nil
	}
	if err != nil {
		return contextIdempotencyRecord{}, false,
			fmt.Errorf("read Practice idempotency record: %w", err)
	}
	if !bytes.Equal(storedFingerprint, intent.PayloadFingerprint[:]) {
		return contextIdempotencyRecord{}, false,
			persistence.ErrIdempotencyConflict
	}
	return record, true, nil
}

func saveContextIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	intent persistence.ContextIdempotencyIntent,
	resourceKind string,
	resourceID string,
	status int,
	response any,
) error {
	body, err := json.Marshal(response)
	if err != nil {
		return persistence.ErrInvalidArgument
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO practice_idempotency_records (
			owner_user_id, method, canonical_path, idempotency_key,
			payload_fingerprint, resource_kind, resource_id,
			response_status, response_body
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		ownerUserID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
		intent.PayloadFingerprint[:],
		resourceKind,
		resourceID,
		status,
		body,
	)
	if err != nil {
		return classifyContextWriteError(
			"insert Practice idempotency record",
			err,
		)
	}
	return nil
}

func validCreateContextSessionCommand(
	actor persistence.Actor,
	command persistence.CreateContextSessionCommand,
) bool {
	return validContextResourceID(command.SessionID) &&
		validContextResourceID(command.SnapshotID) &&
		validContextResourceID(command.PlanID) && command.PlanRevision > 0 &&
		command.Snapshot.ID == command.SnapshotID &&
		command.Snapshot.SessionID == command.SessionID &&
		command.Snapshot.PlanRevision == command.PlanRevision &&
		command.Snapshot.CreatedAt.IsZero() &&
		validContextSnapshot(command.Snapshot, actor.UserID) &&
		validContextIntent(command.Intent)
}

func validContextSnapshot(
	snapshot persistence.ContextSessionSnapshot,
	actorUserID string,
) bool {
	if !validContextResourceID(snapshot.ID) ||
		!validContextResourceID(snapshot.SessionID) ||
		snapshot.PlanRevision < 1 ||
		snapshot.SceneSelection.Scene.Family != snapshot.SceneFamily ||
		snapshot.SceneSelection.Scene.Model != snapshot.SceneModel ||
		snapshot.SceneSelection.Scene.Status != scene.SceneStatusActive ||
		!validContextResourceID(snapshot.Preparation.ID) ||
		!validContextResourceID(snapshot.Preparation.SourceProfileID) ||
		snapshot.Preparation.SourceVersion < 1 ||
		snapshot.Preparation.CreatedAt.IsZero() ||
		snapshot.SessionPolicy.SuggestedDurationSeconds < 1 ||
		snapshot.SessionPolicy.MinEffectiveTurns < 1 ||
		snapshot.SessionPolicy.MaxEffectiveTurns <
			snapshot.SessionPolicy.MinEffectiveTurns ||
		snapshot.SessionPolicy.MaxEffectiveTurns > 14 ||
		snapshot.SessionPolicy.CoverageCheckpointTurn < 1 ||
		snapshot.SessionPolicy.CoverageCheckpointTurn >
			snapshot.SessionPolicy.MaxEffectiveTurns ||
		snapshot.SessionPolicy.MaxFollowUpsPerQuestion < 0 ||
		snapshot.SessionPolicy.EarlyCompletionRule !=
			preparation.EarlyCompletionCoverageSatisfiedAfterCheckpoint ||
		!validContextObjectives(snapshot.PracticeObjectives) {
		return false
	}
	roles, err := snapshot.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return false
	}
	if _, err := snapshot.SceneSelection.PracticeOption(); err != nil {
		return false
	}
	if len(snapshot.Participants) != len(roles)+1 {
		return false
	}
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	orders := make(map[int]struct{}, len(snapshot.Participants))
	facilitators := make(map[string]struct{}, len(roles))
	learnerCount := 0
	for _, participant := range snapshot.Participants {
		if !validContextResourceID(participant.ID) ||
			participant.SessionID != snapshot.SessionID || participant.Order < 1 {
			return false
		}
		if _, duplicate := participantIDs[participant.ID]; duplicate {
			return false
		}
		participantIDs[participant.ID] = struct{}{}
		if _, duplicate := orders[participant.Order]; duplicate {
			return false
		}
		orders[participant.Order] = struct{}{}
		switch participant.Role {
		case "FACILITATOR":
			if participant.SubjectRef.Namespace != "speakup.role" ||
				participant.SubjectRef.SubjectID != participant.RoleDefinitionID ||
				participant.RoleSnapshot == nil ||
				participant.RoleSnapshot.ID != participant.RoleDefinitionID {
				return false
			}
			facilitators[participant.RoleDefinitionID] = struct{}{}
		case "LEARNER":
			if learnerCount != 0 ||
				participant.SubjectRef.Namespace != "speakup.user" ||
				participant.SubjectRef.SubjectID != actorUserID ||
				participant.RoleDefinitionID != "" ||
				participant.RoleSnapshot != nil {
				return false
			}
			learnerCount++
		default:
			return false
		}
	}
	if learnerCount != 1 || len(facilitators) != len(roles) {
		return false
	}
	for _, role := range roles {
		if _, selected := facilitators[role.ID]; !selected {
			return false
		}
	}
	return validCreatedIELTSAssignment(snapshot)
}

func validCreatedIELTSAssignment(
	snapshot persistence.ContextSessionSnapshot,
) bool {
	expectedMode, isIELTS := ieltsModeForScene(snapshot.SceneSelection.Scene)
	if !isIELTS {
		return snapshot.IELTSAssignment == nil
	}
	assignment := snapshot.IELTSAssignment
	if assignment == nil || assignment.Mode != expectedMode ||
		!validContextResourceID(assignment.BankID) ||
		strings.TrimSpace(assignment.Season) == "" ||
		len(assignment.TurnBlueprints) == 0 ||
		!equalContextStrings(
			assignment.TurnBlueprints,
			snapshot.SceneSelection.Scene.Prompt.TurnBlueprints,
		) {
		return false
	}
	switch expectedMode {
	case scene.IELTSPracticeModeFullMock:
		return validContextResourceID(assignment.Part1SetID) &&
			validContextResourceID(assignment.TopicGroupID) &&
			assignment.Part1Questions == 8 && assignment.Part2Questions == 1 &&
			assignment.Part3Questions >= 1 && assignment.Part3Questions <= 5
	case scene.IELTSPracticeModePart1:
		return validContextResourceID(assignment.Part1SetID) &&
			assignment.TopicGroupID == "" && assignment.Part1Questions == 8 &&
			assignment.Part2Questions == 0 && assignment.Part3Questions == 0
	case scene.IELTSPracticeModePart2:
		return assignment.Part1SetID == "" &&
			validContextResourceID(assignment.TopicGroupID) &&
			assignment.Part1Questions == 0 && assignment.Part2Questions == 1 &&
			assignment.Part3Questions >= 1 && assignment.Part3Questions <= 5
	case scene.IELTSPracticeModePart3:
		return assignment.Part1SetID == "" &&
			validContextResourceID(assignment.TopicGroupID) &&
			assignment.Part1Questions == 0 && assignment.Part2Questions == 0 &&
			assignment.Part3Questions >= 1 && assignment.Part3Questions <= 5
	default:
		return false
	}
}

func validContextObjectives(values []preparation.PracticeObjective) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validContextResourceID(value.ID) ||
			strings.TrimSpace(value.Description) == "" {
			return false
		}
		if _, duplicate := seen[value.ID]; duplicate {
			return false
		}
		seen[value.ID] = struct{}{}
	}
	return true
}

func validContextIntent(intent persistence.ContextIdempotencyIntent) bool {
	return intent.Method == "POST" &&
		strings.HasPrefix(intent.CanonicalPath, "/") &&
		len(intent.CanonicalPath) <= 1024 &&
		strings.TrimSpace(intent.CanonicalPath) == intent.CanonicalPath &&
		len(intent.Key) >= 8 && len(intent.Key) <= 128 &&
		strings.TrimSpace(intent.Key) == intent.Key
}

func validContextResourceID(value string) bool {
	return value != "" && len(value) <= 128 &&
		strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func validStoredContextSession(session persistence.ContextSession) bool {
	if !validContextResourceID(session.ID) ||
		!validContextResourceID(session.PlanID) || session.PlanRevision < 1 ||
		!validContextResourceID(session.SnapshotID) || session.Version < 1 ||
		session.EffectiveTurns < 0 || session.CreatedAt.IsZero() {
		return false
	}
	switch session.Status {
	case persistence.ContextSessionStarting:
		return session.StartedAt == nil && session.EndedAt == nil &&
			session.EndReason == "" && session.EffectiveTurns == 0
	case persistence.ContextSessionProgress,
		persistence.ContextSessionPaused:
		return session.StartedAt != nil && session.EndedAt == nil &&
			session.EndReason == ""
	case persistence.ContextSessionCompleted,
		persistence.ContextSessionEndedEarly:
		return session.StartedAt != nil && session.EndedAt != nil &&
			strings.TrimSpace(session.EndReason) != ""
	default:
		return false
	}
}

func validStoredContextBootstrap(
	bootstrap persistence.ContextSessionBootstrap,
) bool {
	return validStoredContextSession(bootstrap.Session) &&
		bootstrap.Snapshot.ID == bootstrap.Session.SnapshotID &&
		bootstrap.Snapshot.SessionID == bootstrap.Session.ID &&
		bootstrap.Snapshot.PlanRevision == bootstrap.Session.PlanRevision &&
		bootstrap.Snapshot.SceneFamily == bootstrap.Session.SceneFamily &&
		bootstrap.Snapshot.SceneModel == bootstrap.Session.SceneModel &&
		!bootstrap.Snapshot.CreatedAt.IsZero() &&
		validContextSnapshot(bootstrap.Snapshot, learnerUserID(bootstrap.Snapshot))
}

func learnerUserID(snapshot persistence.ContextSessionSnapshot) string {
	for _, participant := range snapshot.Participants {
		if participant.Role == "LEARNER" &&
			participant.SubjectRef.Namespace == "speakup.user" {
			return participant.SubjectRef.SubjectID
		}
	}
	return ""
}

func validTransitionCommand(
	command persistence.TransitionContextSessionCommand,
) bool {
	return validContextResourceID(command.SessionID) &&
		command.ExpectedSessionVersion > 0 && validContextIntent(command.Intent) &&
		(command.Transition == persistence.ContextSessionPause ||
			command.Transition == persistence.ContextSessionResume ||
			command.Transition == persistence.ContextSessionEndEarly)
}

func contextTransitionStatus(
	current persistence.ContextSessionStatus,
	transition persistence.ContextSessionTransition,
) (persistence.ContextSessionStatus, bool) {
	switch transition {
	case persistence.ContextSessionPause:
		return persistence.ContextSessionPaused,
			current == persistence.ContextSessionProgress
	case persistence.ContextSessionResume:
		return persistence.ContextSessionProgress,
			current == persistence.ContextSessionPaused
	case persistence.ContextSessionEndEarly:
		return persistence.ContextSessionEndedEarly,
			current == persistence.ContextSessionStarting ||
				current == persistence.ContextSessionProgress ||
				current == persistence.ContextSessionPaused
	default:
		return "", false
	}
}

func isEffectiveContextSessionStatus(status persistence.ContextSessionStatus) bool {
	return status == persistence.ContextSessionStarting ||
		status == persistence.ContextSessionProgress ||
		status == persistence.ContextSessionPaused
}

func ieltsModeForScene(
	definition scene.SceneDefinition,
) (scene.IELTSPracticeMode, bool) {
	if definition.Family != scene.SceneFamilyExam {
		return "", false
	}
	switch definition.Model {
	case scene.SceneModelIELTSSpeakingFullMock:
		return scene.IELTSPracticeModeFullMock, true
	case scene.SceneModelIELTSSpeakingPart1:
		return scene.IELTSPracticeModePart1, true
	case scene.SceneModelIELTSSpeakingPart2:
		return scene.IELTSPracticeModePart2, true
	case scene.SceneModelIELTSSpeakingPart3:
		return scene.IELTSPracticeModePart3, true
	default:
		return "", false
	}
}

func equalContextStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func classifyContextWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return persistence.ErrNotFound
		case "23514":
			return persistence.ErrConflict
		case "23505":
			switch postgresError.ConstraintName {
			case "practice_one_active_session_per_plan",
				"practice_one_effective_session_per_plan":
				return persistence.ErrActiveSessionConflict
			case "practice_idempotency_records_pkey":
				return persistence.ErrIdempotencyConflict
			default:
				return persistence.ErrConflict
			}
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ persistence.SessionRepository = (*Repository)(nil)
var _ persistence.ContextVoiceRepository = (*Repository)(nil)
