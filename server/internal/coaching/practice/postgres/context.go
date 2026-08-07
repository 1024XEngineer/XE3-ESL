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

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func (r *Repository) ReplaySession(
	ctx context.Context,
	actor practice.Actor,
	intent practice.IdempotencyIntent,
) (practice.SessionBootstrap, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextIntent(intent) {
		return practice.SessionBootstrap{}, false,
			practice.ErrInvalidArgument
	}
	record, found, err := loadContextIdempotency(
		ctx,
		r.pool,
		actor.UserID,
		intent,
		true,
	)
	if err != nil || !found {
		return practice.SessionBootstrap{}, false, err
	}
	if record.ResourceKind != "session" {
		return practice.SessionBootstrap{}, false,
			practice.ErrIdempotencyConflict
	}
	bootstrap, err := r.readStoredContextBootstrap(
		ctx,
		actor,
		record.ResourceID,
	)
	if err != nil {
		return practice.SessionBootstrap{}, false, err
	}
	return bootstrap, true, nil
}

func (r *Repository) CreateSession(
	ctx context.Context,
	actor practice.Actor,
	command practice.CreateSessionCommand,
) (practice.SessionBootstrap, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validCreateSessionCommand(actor, command) {
		return practice.SessionBootstrap{}, false,
			practice.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practice.SessionBootstrap{}, false,
			fmt.Errorf("begin create Practice Session: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return practice.SessionBootstrap{}, false, err
	}
	if err := lockContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return practice.SessionBootstrap{}, false, err
	}
	record, found, err := loadContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		false,
	)
	if err != nil {
		return practice.SessionBootstrap{}, false, err
	}
	if found {
		if record.ResourceKind != "session" {
			return practice.SessionBootstrap{}, false,
				practice.ErrIdempotencyConflict
		}
		bootstrap, err := readContextBootstrapWithQuery(
			ctx,
			tx,
			actor.UserID,
			record.ResourceID,
			false,
		)
		if err != nil {
			return practice.SessionBootstrap{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return practice.SessionBootstrap{}, false,
				fmt.Errorf("commit replayed Practice Session: %w", err)
		}
		return bootstrap, true, nil
	}

	snapshot := command.Snapshot
	option, err := snapshot.SceneSelection.PracticeOption()
	if err != nil {
		return practice.SessionBootstrap{}, false, practice.ErrConflict
	}
	var createdAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, plan_revision,
			status, version, effective_turns, snapshot_id,
			practice_experience, scene_category, practice_mode,
			evaluation_policy_ref
		) VALUES (
			$1, $2, $3, $4, 'starting', 1, 0, $5, $6, $7, $8, $9
		)
		RETURNING created_at
	`,
		actor.UserID,
		command.SessionID,
		command.PlanID,
		command.PlanRevision,
		command.SnapshotID,
		snapshot.Experience,
		snapshot.Category,
		snapshot.PracticeMode,
		option.EvaluationPolicyRef,
	).Scan(&createdAt)
	if err != nil {
		return practice.SessionBootstrap{}, false,
			classifyContextWriteError("insert Practice Session", err)
	}
	if !createdAt.Valid {
		return practice.SessionBootstrap{}, false,
			practice.ErrConflict
	}
	snapshot.CreatedAt = createdAt.Time.UTC()
	document, err := json.Marshal(snapshot)
	if err != nil {
		return practice.SessionBootstrap{}, false,
			practice.ErrInvalidArgument
	}
	targetIDs, err := json.Marshal([]string{command.PlanID})
	if err != nil {
		return practice.SessionBootstrap{}, false,
			practice.ErrInvalidArgument
	}
	participants, err := json.Marshal(snapshot.Participants)
	if err != nil {
		return practice.SessionBootstrap{}, false,
			practice.ErrInvalidArgument
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, practice_mode, target_ids,
			participants, turn_limit, snapshot_id, snapshot_document
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		actor.UserID,
		command.SessionID,
		snapshot.PracticeMode,
		targetIDs,
		participants,
		snapshot.SessionPolicy.MaxEffectiveTurns,
		command.SnapshotID,
		document,
	)
	if err != nil {
		return practice.SessionBootstrap{}, false,
			classifyContextWriteError("insert Practice Session snapshot", err)
	}
	bootstrap := practice.SessionBootstrap{
		Session: practice.Session{
			ID:                  command.SessionID,
			PlanID:              command.PlanID,
			PlanRevision:        command.PlanRevision,
			Experience:          snapshot.Experience,
			Category:            snapshot.Category,
			PracticeMode:        snapshot.PracticeMode,
			EvaluationPolicyRef: option.EvaluationPolicyRef,
			SnapshotID:          command.SnapshotID,
			Status:              practice.SessionStarting,
			Version:             1,
			CreatedAt:           createdAt.Time.UTC(),
		},
		Snapshot: snapshot,
	}
	if !validStoredContextBootstrap(bootstrap) {
		return practice.SessionBootstrap{}, false,
			practice.ErrConflict
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
		return practice.SessionBootstrap{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.SessionBootstrap{}, false,
			fmt.Errorf("commit created Practice Session: %w", err)
	}
	return bootstrap, false, nil
}

func (r *Repository) GetSession(
	ctx context.Context,
	actor practice.Actor,
	sessionID string,
) (practice.Session, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(sessionID) {
		return practice.Session{}, practice.ErrInvalidArgument
	}
	return scanSession(r.pool.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, sessionID))
}

func (r *Repository) GetSessionSnapshot(
	ctx context.Context,
	actor practice.Actor,
	sessionID string,
) (practice.SessionSnapshot, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(sessionID) {
		return practice.SessionSnapshot{},
			practice.ErrInvalidArgument
	}
	bootstrap, err := r.readStoredContextBootstrap(ctx, actor, sessionID)
	if err != nil {
		return practice.SessionSnapshot{}, err
	}
	return bootstrap.Snapshot, nil
}

func (r *Repository) GetCompletedSession(
	ctx context.Context,
	ownerUserID string,
	sessionID string,
) (practice.Session, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUserID(ownerUserID) || !validContextResourceID(sessionID) {
		return practice.Session{}, practice.ErrInvalidArgument
	}
	return scanSession(r.pool.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND session.status = 'completed'
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, ownerUserID, sessionID))
}

func (r *Repository) GetCompletedSessionSnapshot(
	ctx context.Context,
	ownerUserID string,
	sessionID string,
) (practice.SessionSnapshot, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUserID(ownerUserID) || !validContextResourceID(sessionID) {
		return practice.SessionSnapshot{}, practice.ErrInvalidArgument
	}
	bootstrap, err := readContextBootstrapWithQuery(
		ctx,
		r.pool,
		ownerUserID,
		sessionID,
		false,
	)
	if err != nil {
		return practice.SessionSnapshot{}, err
	}
	if bootstrap.Session.Status != practice.SessionCompleted {
		return practice.SessionSnapshot{}, practice.ErrNotFound
	}
	return bootstrap.Snapshot, nil
}

func (r *Repository) ResolveSessionByPlan(
	ctx context.Context,
	actor practice.Actor,
	planID string,
) (practice.SessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(planID) {
		return practice.SessionBootstrap{},
			practice.ErrInvalidArgument
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
		return practice.SessionBootstrap{},
			fmt.Errorf("resolve Practice Session by Plan: %w", err)
	}
	defer rows.Close()
	values := make([]practice.SessionBootstrap, 0, 2)
	for rows.Next() {
		value, err := scanContextBootstrap(rows)
		if err != nil {
			return practice.SessionBootstrap{}, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return practice.SessionBootstrap{},
			fmt.Errorf("resolve Practice Session rows: %w", err)
	}
	if len(values) == 0 {
		return practice.SessionBootstrap{}, practice.ErrNotFound
	}
	if isEffectiveSessionStatus(values[0].Session.Status) {
		return values[0], nil
	}
	if len(values) != 1 {
		return practice.SessionBootstrap{}, practice.ErrConflict
	}
	return values[0], nil
}

func (r *Repository) ReplayVoiceStart(
	ctx context.Context,
	actor practice.Actor,
	intent practice.IdempotencyIntent,
) (practice.SessionBootstrap, bool, error) {
	return r.ReplaySession(ctx, actor, intent)
}

func (r *Repository) ActivateSession(
	ctx context.Context,
	actor practice.Actor,
	sessionID string,
	planID string,
	intent practice.IdempotencyIntent,
) (practice.SessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(sessionID) ||
		!validContextResourceID(planID) || !validContextIntent(intent) {
		return practice.SessionBootstrap{},
			practice.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practice.SessionBootstrap{},
			fmt.Errorf("begin activate Practice Session: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return practice.SessionBootstrap{}, err
	}
	if err := lockContextIdempotency(ctx, tx, actor.UserID, intent); err != nil {
		return practice.SessionBootstrap{}, err
	}
	record, found, err := loadContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		intent,
		false,
	)
	if err != nil {
		return practice.SessionBootstrap{}, err
	}
	if found {
		if record.ResourceKind != "session" {
			return practice.SessionBootstrap{},
				practice.ErrIdempotencyConflict
		}
		bootstrap, err := readContextBootstrapWithQuery(
			ctx,
			tx,
			actor.UserID,
			record.ResourceID,
			false,
		)
		if err != nil {
			return practice.SessionBootstrap{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return practice.SessionBootstrap{},
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
		return practice.SessionBootstrap{}, err
	}
	if bootstrap.Session.PlanID != planID {
		return practice.SessionBootstrap{}, practice.ErrNotFound
	}
	switch bootstrap.Session.Status {
	case practice.SessionStarting:
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
			return practice.SessionBootstrap{},
				classifyContextWriteError("activate Practice Session", err)
		}
		if tag.RowsAffected() != 1 {
			return practice.SessionBootstrap{}, practice.ErrConflict
		}
		bootstrap, err = readContextBootstrapWithQuery(
			ctx,
			tx,
			actor.UserID,
			sessionID,
			false,
		)
		if err != nil {
			return practice.SessionBootstrap{}, err
		}
	case practice.SessionInProgress:
	case practice.SessionPaused:
		return practice.SessionBootstrap{}, practice.ErrConflict
	default:
		return practice.SessionBootstrap{}, practice.ErrConflict
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
		return practice.SessionBootstrap{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.SessionBootstrap{},
			fmt.Errorf("commit activated Practice Session: %w", err)
	}
	return bootstrap, nil
}

func (r *Repository) TransitionSession(
	ctx context.Context,
	actor practice.Actor,
	command practice.TransitionSessionCommand,
) (practice.Session, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validTransitionCommand(command) {
		return practice.Session{}, false,
			practice.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practice.Session{}, false,
			fmt.Errorf("begin Practice Session transition: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return practice.Session{}, false, err
	}
	if err := lockContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return practice.Session{}, false, err
	}
	record, found, err := loadContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		false,
	)
	if err != nil {
		return practice.Session{}, false, err
	}
	resourceKind := string(command.Transition)
	if found {
		if record.ResourceKind != resourceKind {
			return practice.Session{}, false,
				practice.ErrIdempotencyConflict
		}
		var session practice.Session
		if err := json.Unmarshal(record.ResponseBody, &session); err != nil ||
			session.ID != record.ResourceID || !validStoredSession(session) {
			return practice.Session{}, false,
				practice.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return practice.Session{}, false,
				fmt.Errorf("commit replayed Practice transition: %w", err)
		}
		return session, true, nil
	}
	session, err := scanSession(tx.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		FOR UPDATE OF session
	`, actor.UserID, command.SessionID))
	if err != nil {
		return practice.Session{}, false, err
	}
	if session.Version != command.ExpectedSessionVersion {
		return practice.Session{}, false, practice.ErrConflict
	}
	if command.Transition == practice.SessionComplete {
		if session.EffectiveTurns < 1 {
			return practice.Session{}, false, practice.ErrConflict
		}
		var snapshotDocument []byte
		if err := tx.QueryRow(ctx, `
			SELECT snapshot_document
			FROM practice_session_snapshots
			WHERE owner_user_id = $1 AND session_id = $2
		`, actor.UserID, command.SessionID).Scan(&snapshotDocument); err != nil {
			return practice.Session{}, false, classifyContextWriteError(
				"read completion policy", err,
			)
		}
		snapshot, err := decodeContextSnapshot(snapshotDocument)
		if err != nil || snapshot.SessionPolicy.CompletionMode !=
			practice.CompletionModeUserControlled {
			return practice.Session{}, false, practice.ErrConflict
		}
	}
	nextStatus, allowed := contextTransitionStatus(
		session.Status,
		command.Transition,
	)
	if !allowed {
		return practice.Session{}, false, practice.ErrConflict
	}
	startedAtExpression := "started_at"
	completedAtExpression := "NULL"
	endReasonExpression := "NULL"
	if command.Transition == practice.SessionEndEarly ||
		command.Transition == practice.SessionComplete {
		startedAtExpression = "COALESCE(started_at, transaction_timestamp())"
		completedAtExpression = "transaction_timestamp()"
		endReasonExpression = "'USER_ENDED'"
		if command.Transition == practice.SessionComplete {
			endReasonExpression = "'USER_COMPLETED'"
		}
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
		return practice.Session{}, false,
			classifyContextWriteError("transition Practice Session", err)
	}
	if tag.RowsAffected() != 1 {
		return practice.Session{}, false, practice.ErrConflict
	}
	session, err = scanSession(tx.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, command.SessionID))
	if err != nil {
		return practice.Session{}, false, err
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
		return practice.Session{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Session{}, false,
			fmt.Errorf("commit Practice Session transition: %w", err)
	}
	return session, false, nil
}

func (r *Repository) readStoredContextBootstrap(
	ctx context.Context,
	actor practice.Actor,
	sessionID string,
) (practice.SessionBootstrap, error) {
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
) (practice.SessionBootstrap, error) {
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
			session.practice_experience,
			session.scene_category,
			session.practice_mode,
		session.evaluation_policy_ref,
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
			session.practice_experience,
			session.scene_category,
			session.practice_mode,
		session.evaluation_policy_ref,
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

func scanSession(row contextRowScanner) (practice.Session, error) {
	var session practice.Session
	var startedAt, completedAt pgtype.Timestamptz
	var endReason pgtype.Text
	err := row.Scan(
		&session.ID,
		&session.PlanID,
		&session.PlanRevision,
		&session.Experience,
		&session.Category,
		&session.PracticeMode,
		&session.EvaluationPolicyRef,
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
		return practice.Session{}, practice.ErrNotFound
	}
	if err != nil {
		return practice.Session{},
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
	if !validStoredSession(session) {
		return practice.Session{}, practice.ErrConflict
	}
	return session, nil
}

func scanContextBootstrap(
	row contextRowScanner,
) (practice.SessionBootstrap, error) {
	var session practice.Session
	var startedAt, completedAt pgtype.Timestamptz
	var endReason pgtype.Text
	var document []byte
	err := row.Scan(
		&session.ID,
		&session.PlanID,
		&session.PlanRevision,
		&session.Experience,
		&session.Category,
		&session.PracticeMode,
		&session.EvaluationPolicyRef,
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
		return practice.SessionBootstrap{}, practice.ErrNotFound
	}
	if err != nil {
		return practice.SessionBootstrap{},
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
	var snapshot practice.SessionSnapshot
	if err := json.Unmarshal(document, &snapshot); err != nil {
		return practice.SessionBootstrap{},
			fmt.Errorf("decode Practice Session snapshot: %w", err)
	}
	bootstrap := practice.SessionBootstrap{
		Session:  session,
		Snapshot: snapshot,
	}
	if !validStoredContextBootstrap(bootstrap) {
		return practice.SessionBootstrap{}, practice.ErrConflict
	}
	return bootstrap, nil
}

func decodeContextSnapshot(
	document []byte,
) (practice.SessionSnapshot, error) {
	var snapshot practice.SessionSnapshot
	if err := json.Unmarshal(document, &snapshot); err != nil {
		return practice.SessionSnapshot{}, err
	}
	return snapshot, nil
}

func lockContextIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	intent practice.IdempotencyIntent,
) error {
	scope, err := json.Marshal([]string{
		ownerUserID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
	})
	if err != nil {
		return practice.ErrInvalidArgument
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
	intent practice.IdempotencyIntent,
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
			practice.ErrIdempotencyConflict
	}
	return record, true, nil
}

func saveContextIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	intent practice.IdempotencyIntent,
	resourceKind string,
	resourceID string,
	status int,
	response any,
) error {
	body, err := json.Marshal(response)
	if err != nil {
		return practice.ErrInvalidArgument
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

func validCreateSessionCommand(
	actor practice.Actor,
	command practice.CreateSessionCommand,
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
	snapshot practice.SessionSnapshot,
	actorUserID string,
) bool {
	if !validContextResourceID(snapshot.ID) ||
		!validContextResourceID(snapshot.SessionID) ||
		snapshot.PlanRevision < 1 ||
		snapshot.SceneSelection.Scene.Experience != snapshot.Experience ||
		snapshot.SceneSelection.Scene.Category != snapshot.Category ||
		snapshot.SceneSelection.Scene.Status != practice.SceneStatusActive ||
		!validContextResourceID(snapshot.Preparation.ID) ||
		!validContextResourceID(snapshot.Preparation.SourceProfileID) ||
		snapshot.Preparation.SourceVersion < 1 ||
		snapshot.Preparation.CreatedAt.IsZero() ||
		!validContextObjectives(snapshot.PracticeObjectives) {
		return false
	}
	roles, err := snapshot.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return false
	}
	option, err := snapshot.SceneSelection.PracticeOption()
	if err != nil || option.Mode != snapshot.PracticeMode ||
		!validEvaluationPolicyRef(option.EvaluationPolicyRef) ||
		!practice.ValidSessionPolicy(
			option.SessionPolicyRef,
			option.Mode,
			len(snapshot.SceneSelection.Scene.Prompt.TurnBlueprints),
			option.SuggestedDurationSeconds,
			snapshot.SessionPolicy,
		) {
		return false
	}
	if _, err := practice.ResolveTurnPolicy(
		option.TurnPolicyRef,
	); err != nil {
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
	snapshot practice.SessionSnapshot,
) bool {
	option, err := snapshot.SceneSelection.PracticeOption()
	if err != nil {
		return false
	}
	turnPolicy, err := practice.ResolveTurnPolicy(
		option.TurnPolicyRef,
	)
	if err != nil {
		return false
	}
	if turnPolicy.Kind != practice.TurnPolicyFrozenIELTS {
		return snapshot.IELTSAssignment == nil
	}
	return practice.ValidIELTSAssignment(
		snapshot.IELTSAssignment,
		turnPolicy.Mode,
		snapshot.SceneSelection.Scene.Prompt.TurnBlueprints,
	)
}

func validContextObjectives(values []practice.PracticeObjective) bool {
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

func validContextIntent(intent practice.IdempotencyIntent) bool {
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

func validEvaluationPolicyRef(value string) bool {
	return validContextResourceID(value) &&
		strings.HasSuffix(value, ".evaluation.v1")
}

func validStoredSession(session practice.Session) bool {
	if !validContextResourceID(session.ID) ||
		!validContextResourceID(session.PlanID) || session.PlanRevision < 1 ||
		!validContextResourceID(session.SnapshotID) || session.Version < 1 ||
		!validEvaluationPolicyRef(session.EvaluationPolicyRef) ||
		session.EffectiveTurns < 0 || session.CreatedAt.IsZero() {
		return false
	}
	switch session.Status {
	case practice.SessionStarting:
		return session.StartedAt == nil && session.EndedAt == nil &&
			session.EndReason == "" && session.EffectiveTurns == 0
	case practice.SessionInProgress,
		practice.SessionPaused:
		return session.StartedAt != nil && session.EndedAt == nil &&
			session.EndReason == ""
	case practice.SessionCompleted,
		practice.SessionEndedEarly:
		return session.StartedAt != nil && session.EndedAt != nil &&
			strings.TrimSpace(session.EndReason) != ""
	default:
		return false
	}
}

func validStoredContextBootstrap(
	bootstrap practice.SessionBootstrap,
) bool {
	return validStoredSession(bootstrap.Session) &&
		bootstrap.Snapshot.ID == bootstrap.Session.SnapshotID &&
		bootstrap.Snapshot.SessionID == bootstrap.Session.ID &&
		bootstrap.Snapshot.PlanRevision == bootstrap.Session.PlanRevision &&
		bootstrap.Snapshot.Experience == bootstrap.Session.Experience &&
		bootstrap.Snapshot.Category == bootstrap.Session.Category &&
		bootstrap.Snapshot.PracticeMode == bootstrap.Session.PracticeMode &&
		selectedEvaluationPolicyRef(bootstrap.Snapshot) ==
			bootstrap.Session.EvaluationPolicyRef &&
		!bootstrap.Snapshot.CreatedAt.IsZero() &&
		validContextSnapshot(bootstrap.Snapshot, learnerUserID(bootstrap.Snapshot))
}

func selectedEvaluationPolicyRef(snapshot practice.SessionSnapshot) string {
	option, err := snapshot.SceneSelection.PracticeOption()
	if err != nil {
		return ""
	}
	return option.EvaluationPolicyRef
}

func learnerUserID(snapshot practice.SessionSnapshot) string {
	for _, participant := range snapshot.Participants {
		if participant.Role == "LEARNER" &&
			participant.SubjectRef.Namespace == "speakup.user" {
			return participant.SubjectRef.SubjectID
		}
	}
	return ""
}

func validTransitionCommand(
	command practice.TransitionSessionCommand,
) bool {
	return validContextResourceID(command.SessionID) &&
		command.ExpectedSessionVersion > 0 && validContextIntent(command.Intent) &&
		(command.Transition == practice.SessionPause ||
			command.Transition == practice.SessionResume ||
			command.Transition == practice.SessionComplete ||
			command.Transition == practice.SessionEndEarly)
}

func contextTransitionStatus(
	current practice.SessionStatus,
	transition practice.SessionTransition,
) (practice.SessionStatus, bool) {
	switch transition {
	case practice.SessionPause:
		return practice.SessionPaused,
			current == practice.SessionInProgress
	case practice.SessionResume:
		return practice.SessionInProgress,
			current == practice.SessionPaused
	case practice.SessionComplete:
		return practice.SessionCompleted,
			current == practice.SessionInProgress ||
				current == practice.SessionPaused
	case practice.SessionEndEarly:
		return practice.SessionEndedEarly,
			current == practice.SessionStarting ||
				current == practice.SessionInProgress ||
				current == practice.SessionPaused
	default:
		return "", false
	}
}

func isEffectiveSessionStatus(status practice.SessionStatus) bool {
	return status == practice.SessionStarting ||
		status == practice.SessionInProgress ||
		status == practice.SessionPaused
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
			return practice.ErrNotFound
		case "23514":
			return practice.ErrConflict
		case "23505":
			switch postgresError.ConstraintName {
			case "practice_one_active_session_per_plan",
				"practice_one_effective_session_per_plan":
				return practice.ErrActiveSessionConflict
			case "practice_idempotency_records_pkey":
				return practice.ErrIdempotencyConflict
			default:
				return practice.ErrConflict
			}
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ practice.SessionRepository = (*Repository)(nil)
var _ practice.VoiceSessionRepository = (*Repository)(nil)
