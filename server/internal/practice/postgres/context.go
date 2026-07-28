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

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func (r *Repository) ReplayPlan(
	ctx context.Context,
	actor persistence.Actor,
	intent persistence.ContextIdempotencyIntent,
) (persistence.Plan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextIntent(intent) {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	record, found, err := loadContextIdempotency(
		ctx,
		r.pool,
		actor.UserID,
		intent,
		true,
	)
	if err != nil || !found {
		return persistence.Plan{}, false, err
	}
	if record.ResourceKind != "plan" {
		return persistence.Plan{}, false,
			persistence.ErrIdempotencyConflict
	}
	var plan persistence.Plan
	if err := json.Unmarshal(record.ResponseBody, &plan); err != nil ||
		plan.ID != record.ResourceID ||
		plan.UserID != actor.UserID {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	return plan, true, nil
}

func (r *Repository) CreatePlan(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.CreatePlanCommand,
) (persistence.Plan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validCreatePlanCommand(command) {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	selectedRoles, err := json.Marshal(command.SelectedRoleIDs)
	if err != nil {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.Plan{}, false,
			fmt.Errorf("begin create practice plan: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.Plan{}, false, err
	}
	if err := lockContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return persistence.Plan{}, false, err
	}
	replayed, found, err := loadContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		false,
	)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if found {
		if replayed.ResourceKind != "plan" {
			return persistence.Plan{}, false,
				persistence.ErrIdempotencyConflict
		}
		var plan persistence.Plan
		if err := json.Unmarshal(replayed.ResponseBody, &plan); err != nil ||
			plan.ID != replayed.ResourceID ||
			plan.UserID != actor.UserID {
			return persistence.Plan{}, false, persistence.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.Plan{}, false,
				fmt.Errorf("commit replayed practice plan: %w", err)
		}
		return plan, true, nil
	}

	if err := lockPlanDependencies(
		ctx,
		tx,
		actor.UserID,
		command.AgentThreadID,
		command.MatterID,
		command.PreparationProfileID,
	); err != nil {
		return persistence.Plan{}, false, err
	}

	plan := persistence.Plan{
		ID:                        command.PlanID,
		UserID:                    actor.UserID,
		AgentThreadID:             command.AgentThreadID,
		MatterID:                  command.MatterID,
		ScenarioDefinitionID:      command.ScenarioDefinitionID,
		ScenarioDefinitionVersion: command.ScenarioDefinitionVersion,
		ScenarioType:              command.ScenarioType,
		ScenarioConfigID:          command.ScenarioConfigID,
		ScenarioConfigVersion:     command.ScenarioConfigVersion,
		PreparationProfileID:      command.PreparationProfileID,
		SelectedRoleIDs:           cloneContextStrings(command.SelectedRoleIDs),
		Revision:                  1,
		Status:                    persistence.PlanStatusReady,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_plans (
			owner_user_id, plan_id, agent_thread_id, matter_id,
			scenario_definition_id, scenario_definition_version,
			scenario_type, scenario_config_id, scenario_config_version,
			preparation_profile_id, selected_role_ids,
			plan_revision, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			1, 'ready'
		)
		RETURNING created_at, updated_at
	`,
		actor.UserID,
		command.PlanID,
		command.AgentThreadID,
		command.MatterID,
		command.ScenarioDefinitionID,
		command.ScenarioDefinitionVersion,
		command.ScenarioType,
		command.ScenarioConfigID,
		command.ScenarioConfigVersion,
		command.PreparationProfileID,
		selectedRoles,
	).Scan(&plan.CreatedAt, &plan.UpdatedAt)
	if err != nil {
		return persistence.Plan{}, false,
			classifyContextWriteError("insert practice plan", err)
	}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	if err := saveContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"plan",
		plan.ID,
		201,
		plan,
	); err != nil {
		return persistence.Plan{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.Plan{}, false,
			fmt.Errorf("commit practice plan: %w", err)
	}
	return plan, false, nil
}

func (r *Repository) GetPlan(
	ctx context.Context,
	actor persistence.Actor,
	planID string,
) (persistence.Plan, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(planID) {
		return persistence.Plan{}, persistence.ErrInvalidArgument
	}
	return scanContextPlan(r.pool.QueryRow(ctx, contextPlanSelect+`
		WHERE plan.owner_user_id = $1
		  AND plan.plan_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, planID))
}

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
	var bootstrap persistence.ContextSessionBootstrap
	if err := json.Unmarshal(record.ResponseBody, &bootstrap); err != nil ||
		bootstrap.Session.ID != record.ResourceID ||
		!validStoredContextBootstrap(bootstrap) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
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
			fmt.Errorf("begin create context session: %w", err)
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
	replayed, found, err := loadContextIdempotency(
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
		if replayed.ResourceKind != "session" {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrIdempotencyConflict
		}
		var bootstrap persistence.ContextSessionBootstrap
		if err := json.Unmarshal(
			replayed.ResponseBody,
			&bootstrap,
		); err != nil ||
			bootstrap.Session.ID != replayed.ResourceID ||
			!validStoredContextBootstrap(bootstrap) {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.ContextSessionBootstrap{}, false,
				fmt.Errorf("commit replayed context session: %w", err)
		}
		return bootstrap, true, nil
	}

	plan, err := lockContextPlan(
		ctx,
		tx,
		actor.UserID,
		command.PlanID,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if plan.Status != persistence.PlanStatusReady ||
		plan.Revision != command.ExpectedPlanRevision {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	if err := lockPlanDependencies(
		ctx,
		tx,
		actor.UserID,
		plan.AgentThreadID,
		plan.MatterID,
		plan.PreparationProfileID,
	); err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	var storedPreparation persistence.PreparationSnapshot
	var resumeSnapshot, jobDescriptionSnapshot pgtype.Text
	err = tx.QueryRow(ctx, `
		SELECT
			snapshot.snapshot_id,
			snapshot.source_profile_id,
			snapshot.source_version,
			snapshot.resume_snapshot,
			snapshot.job_description_snapshot,
			snapshot.background_snapshot,
			snapshot.created_at
		FROM preparation_snapshots AS snapshot
		WHERE snapshot.owner_user_id = $1
		  AND snapshot.snapshot_id = $2
		FOR KEY SHARE OF snapshot
	`, actor.UserID, command.PreparationSnapshotID).Scan(
		&storedPreparation.ID,
		&storedPreparation.SourceProfileID,
		&storedPreparation.SourceVersion,
		&resumeSnapshot,
		&jobDescriptionSnapshot,
		&storedPreparation.BackgroundSnapshot,
		&storedPreparation.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrNotFound
	}
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			fmt.Errorf("lock preparation snapshot: %w", err)
	}
	storedPreparation.CreatedAt = storedPreparation.CreatedAt.UTC()
	if resumeSnapshot.Valid {
		storedPreparation.ResumeSnapshot = resumeSnapshot.String
	}
	if jobDescriptionSnapshot.Valid {
		storedPreparation.JobDescriptionSnapshot =
			jobDescriptionSnapshot.String
	}
	if storedPreparation.SourceProfileID != plan.PreparationProfileID ||
		!equalPreparationSnapshot(
			storedPreparation,
			command.Snapshot.Preparation,
		) ||
		!contextSnapshotMatchesPlan(actor, command.Snapshot, plan) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}

	session := persistence.ContextSession{
		ID:           command.SessionID,
		PlanID:       plan.ID,
		ScenarioType: plan.ScenarioType,
		SnapshotID:   command.SnapshotID,
		Status:       persistence.ContextSessionStarting,
		Version:      1,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, context_plan_id,
			agent_thread_id, matter_id, snapshot_id, scenario_type,
			status, version, effective_turns, started_at
		) VALUES (
			$1, $2, $3, $3, $4, $5, $6, $7,
			'starting', 1, 0, NULL
		)
		RETURNING created_at
	`,
		actor.UserID,
		command.SessionID,
		plan.ID,
		plan.AgentThreadID,
		plan.MatterID,
		command.SnapshotID,
		plan.ScenarioType,
	).Scan(&session.CreatedAt)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			classifyContextWriteError("insert context session", err)
	}
	session.CreatedAt = session.CreatedAt.UTC()

	snapshot := cloneContextSessionSnapshot(command.Snapshot)
	snapshot.ID = command.SnapshotID
	snapshot.SessionID = command.SessionID
	snapshot.CreatedAt = session.CreatedAt
	document, err := json.Marshal(snapshot)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	targets, err := json.Marshal([]string{plan.MatterID})
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	legacyParticipants, err := legacyContextParticipantProjection(
		snapshot.Participants,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	participants, err := json.Marshal(legacyParticipants)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, mode, target_ids, participants,
			turn_limit, snapshot_id, context_plan_id,
			preparation_snapshot_id, snapshot_document
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
	`,
		actor.UserID,
		command.SessionID,
		snapshot.ScenarioType,
		targets,
		participants,
		snapshot.SessionPolicy.MaxEffectiveTurns,
		command.SnapshotID,
		plan.ID,
		command.PreparationSnapshotID,
		document,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			classifyContextWriteError("insert context session snapshot", err)
	}
	bootstrap := persistence.ContextSessionBootstrap{
		Session:  session,
		Snapshot: snapshot,
	}
	if err := saveContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"session",
		session.ID,
		201,
		bootstrap,
	); err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.ContextSessionBootstrap{}, false,
			classifyContextWriteError("commit context session", err)
	}
	return bootstrap, false, nil
}

func equalPreparationSnapshot(
	left persistence.PreparationSnapshot,
	right persistence.PreparationSnapshot,
) bool {
	return left.ID == right.ID &&
		left.SourceProfileID == right.SourceProfileID &&
		left.SourceVersion == right.SourceVersion &&
		left.ResumeSnapshot == right.ResumeSnapshot &&
		left.JobDescriptionSnapshot == right.JobDescriptionSnapshot &&
		left.BackgroundSnapshot == right.BackgroundSnapshot &&
		left.CreatedAt.Equal(right.CreatedAt)
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
		  AND session.context_plan_id IS NOT NULL
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
	var document []byte
	var storedSnapshotID, storedPlanID string
	err := r.pool.QueryRow(ctx, `
		SELECT
			snapshot.snapshot_id,
			snapshot.context_plan_id,
			snapshot.snapshot_document
		FROM practice_session_snapshots AS snapshot
		JOIN practice_sessions AS session
		  ON session.owner_user_id = snapshot.owner_user_id
		 AND session.session_id = snapshot.session_id
		 AND session.context_plan_id = snapshot.context_plan_id
		 AND session.snapshot_id = snapshot.snapshot_id
		JOIN identity_users AS owner
		  ON owner.id = snapshot.owner_user_id
		LEFT JOIN practice_deletion_fences AS fence
		  ON fence.owner_user_id = snapshot.owner_user_id
		WHERE snapshot.owner_user_id = $1
		  AND snapshot.session_id = $2
		  AND snapshot.context_plan_id IS NOT NULL
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, sessionID).Scan(
		&storedSnapshotID,
		&storedPlanID,
		&document,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ContextSessionSnapshot{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.ContextSessionSnapshot{},
			fmt.Errorf("read context session snapshot: %w", err)
	}
	snapshot, err := decodeContextSnapshot(document)
	if err != nil || snapshot.ID != storedSnapshotID ||
		snapshot.SessionID != sessionID ||
		storedPlanID == "" {
		return persistence.ContextSessionSnapshot{}, persistence.ErrConflict
	}
	return snapshot, nil
}

func (r *Repository) ResolveContextSessionByThread(
	ctx context.Context,
	actor persistence.Actor,
	threadID string,
) (persistence.ContextSessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(threadID) {
		return persistence.ContextSessionBootstrap{},
			persistence.ErrInvalidArgument
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			session.session_id,
			session.context_plan_id,
			session.scenario_type,
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
		JOIN practice_plans AS plan
		  ON plan.owner_user_id = session.owner_user_id
		 AND plan.plan_id = session.context_plan_id
		 AND plan.agent_thread_id = session.agent_thread_id
		 AND plan.matter_id = session.matter_id
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = session.owner_user_id
		 AND snapshot.session_id = session.session_id
		 AND snapshot.context_plan_id = session.context_plan_id
		 AND snapshot.snapshot_id = session.snapshot_id
		JOIN agent_thread_matter_links AS link
		  ON link.owner_user_id = plan.owner_user_id
		 AND link.thread_id = plan.agent_thread_id
		 AND link.matter_id = plan.matter_id
		JOIN matters AS matter
		  ON matter.owner_user_id = plan.owner_user_id
		 AND matter.id = plan.matter_id
		JOIN identity_users AS owner
		  ON owner.id = session.owner_user_id
		LEFT JOIN practice_deletion_fences AS fence
		  ON fence.owner_user_id = session.owner_user_id
		WHERE session.owner_user_id = $1
		  AND session.agent_thread_id = $2
		  AND session.status IN (
		      'starting',
		      'in_progress',
		      'paused',
		      'completed'
		  )
		  AND plan.status = 'ready'
		  AND matter.status = 'active'
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		ORDER BY
		  CASE
		    WHEN session.status IN ('starting', 'in_progress', 'paused')
		      THEN 0
		    ELSE 1
		  END,
		  session.created_at,
		  session.session_id
		LIMIT 2
	`, actor.UserID, threadID)
	if err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("resolve context session by Thread: %w", err)
	}
	defer rows.Close()

	results := make([]persistence.ContextSessionBootstrap, 0, 2)
	for rows.Next() {
		session, document, err := scanResolvedContextSession(rows)
		if err != nil {
			return persistence.ContextSessionBootstrap{}, err
		}
		snapshot, err := decodeContextSnapshot(document)
		if err != nil ||
			snapshot.ID != session.SnapshotID ||
			snapshot.SessionID != session.ID {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrConflict
		}
		results = append(results, persistence.ContextSessionBootstrap{
			Session:  session,
			Snapshot: snapshot,
		})
	}
	if err := rows.Err(); err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("iterate resolved context Sessions: %w", err)
	}
	if len(results) == 0 {
		return persistence.ContextSessionBootstrap{}, persistence.ErrNotFound
	}
	if isEffectiveContextSessionStatus(results[0].Session.Status) {
		if len(results) == 2 &&
			isEffectiveContextSessionStatus(results[1].Session.Status) {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrConflict
		}
		return results[0], nil
	}
	if len(results) != 1 ||
		results[0].Session.Status != persistence.ContextSessionCompleted {
		return persistence.ContextSessionBootstrap{}, persistence.ErrConflict
	}
	return results[0], nil
}

// ResolveContextSession resolves the formal Session bound to the exact Actor +
// Thread + Matter tuple. A unique effective Session wins over completed
// history. Without an effective Session, exactly one completed Session may be
// restored; ambiguous effective or completed rows are rejected.
func (r *Repository) ResolveContextSession(
	ctx context.Context,
	actor persistence.Actor,
	threadID string,
	matterID string,
) (persistence.ContextSessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(threadID) ||
		!validContextResourceID(matterID) {
		return persistence.ContextSessionBootstrap{},
			persistence.ErrInvalidArgument
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			session.session_id,
			session.context_plan_id,
			session.scenario_type,
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
		JOIN practice_plans AS plan
		  ON plan.owner_user_id = session.owner_user_id
		 AND plan.plan_id = session.context_plan_id
		 AND plan.agent_thread_id = session.agent_thread_id
		 AND plan.matter_id = session.matter_id
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = session.owner_user_id
		 AND snapshot.session_id = session.session_id
		 AND snapshot.context_plan_id = session.context_plan_id
		 AND snapshot.snapshot_id = session.snapshot_id
		JOIN agent_thread_matter_links AS link
		  ON link.owner_user_id = plan.owner_user_id
		 AND link.thread_id = plan.agent_thread_id
		 AND link.matter_id = plan.matter_id
		JOIN matters AS matter
		  ON matter.owner_user_id = plan.owner_user_id
		 AND matter.id = plan.matter_id
		JOIN identity_users AS owner
		  ON owner.id = session.owner_user_id
		LEFT JOIN practice_deletion_fences AS fence
		  ON fence.owner_user_id = session.owner_user_id
		WHERE session.owner_user_id = $1
		  AND session.agent_thread_id = $2
		  AND session.matter_id = $3
		  AND session.status IN (
		      'starting',
		      'in_progress',
		      'paused',
		      'completed'
		  )
		  AND plan.status = 'ready'
		  AND matter.status = 'active'
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		ORDER BY
		  CASE
		    WHEN session.status IN ('starting', 'in_progress', 'paused')
		      THEN 0
		    ELSE 1
		  END,
		  session.created_at,
		  session.session_id
		LIMIT 2
	`, actor.UserID, threadID, matterID)
	if err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("resolve exact context Session: %w", err)
	}
	defer rows.Close()

	results := make([]persistence.ContextSessionBootstrap, 0, 2)
	for rows.Next() {
		session, document, scanErr := scanResolvedContextSession(rows)
		if scanErr != nil {
			return persistence.ContextSessionBootstrap{}, scanErr
		}
		snapshot, decodeErr := decodeContextSnapshot(document)
		if decodeErr != nil ||
			snapshot.ID != session.SnapshotID ||
			snapshot.SessionID != session.ID {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrConflict
		}
		results = append(results, persistence.ContextSessionBootstrap{
			Session:  session,
			Snapshot: snapshot,
		})
	}
	if err := rows.Err(); err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("iterate exact context Sessions: %w", err)
	}
	if len(results) == 0 {
		return persistence.ContextSessionBootstrap{}, persistence.ErrNotFound
	}
	if isEffectiveContextSessionStatus(results[0].Session.Status) {
		if len(results) == 2 &&
			isEffectiveContextSessionStatus(results[1].Session.Status) {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrConflict
		}
		return results[0], nil
	}
	if len(results) != 1 ||
		results[0].Session.Status != persistence.ContextSessionCompleted {
		return persistence.ContextSessionBootstrap{}, persistence.ErrConflict
	}
	return results[0], nil
}

func isEffectiveContextSessionStatus(
	status persistence.ContextSessionStatus,
) bool {
	switch status {
	case persistence.ContextSessionStarting,
		persistence.ContextSessionProgress,
		persistence.ContextSessionPaused:
		return true
	default:
		return false
	}
}

func (r *Repository) ReplayContextVoiceStart(
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
	var original persistence.ContextSessionBootstrap
	if err := json.Unmarshal(record.ResponseBody, &original); err != nil ||
		original.Session.ID != record.ResourceID ||
		!validStoredContextBootstrap(original) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	session, err := r.GetContextSession(ctx, actor, record.ResourceID)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	snapshot, err := r.GetContextSessionSnapshot(
		ctx,
		actor,
		record.ResourceID,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	return persistence.ContextSessionBootstrap{
		Session:  session,
		Snapshot: snapshot,
	}, true, nil
}

// ActivateContextSession atomically changes a formal starting Session to
// in_progress after re-validating its exact immutable Thread + Matter binding.
// Replaying an already in_progress Session is idempotent; paused Sessions must
// use the formal lifecycle resume command.
func (r *Repository) ActivateContextSession(
	ctx context.Context,
	actor persistence.Actor,
	sessionID string,
	threadID string,
	matterID string,
	intent persistence.ContextIdempotencyIntent,
) (persistence.ContextSessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(sessionID) ||
		!validContextResourceID(threadID) ||
		!validContextResourceID(matterID) ||
		!validContextIntent(intent) {
		return persistence.ContextSessionBootstrap{},
			persistence.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("begin activate context Session: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	if err := lockContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		intent,
	); err != nil {
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
		var replayed persistence.ContextSessionBootstrap
		if err := json.Unmarshal(record.ResponseBody, &replayed); err != nil ||
			replayed.Session.ID != record.ResourceID ||
			!validStoredContextBootstrap(replayed) {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.ContextSessionBootstrap{},
				fmt.Errorf("commit replayed voice Start: %w", err)
		}
		return replayed, nil
	}

	session, err := scanContextSession(tx.QueryRow(ctx, contextSessionSelect+`
		JOIN practice_plans AS plan
		  ON plan.owner_user_id = session.owner_user_id
		 AND plan.plan_id = session.context_plan_id
		 AND plan.agent_thread_id = session.agent_thread_id
		 AND plan.matter_id = session.matter_id
		JOIN agent_thread_matter_links AS link
		  ON link.owner_user_id = plan.owner_user_id
		 AND link.thread_id = plan.agent_thread_id
		 AND link.matter_id = plan.matter_id
		JOIN matters AS matter
		  ON matter.owner_user_id = plan.owner_user_id
		 AND matter.id = plan.matter_id
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND session.agent_thread_id = $3
		  AND session.matter_id = $4
		  AND session.context_plan_id IS NOT NULL
		  AND plan.status = 'ready'
		  AND matter.status = 'active'
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		FOR UPDATE OF session
	`, actor.UserID, sessionID, threadID, matterID))
	if err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	switch session.Status {
	case persistence.ContextSessionStarting:
		var startedAt pgtype.Timestamptz
		tag, updateErr := tx.Exec(ctx, `
			UPDATE practice_sessions
			SET status = 'in_progress',
			    version = version + 1,
			    started_at = transaction_timestamp(),
			    updated_at = transaction_timestamp()
			WHERE owner_user_id = $1
			  AND session_id = $2
			  AND context_plan_id IS NOT NULL
			  AND status = 'starting'
			  AND version = $3
		`, actor.UserID, sessionID, session.Version)
		if updateErr != nil {
			return persistence.ContextSessionBootstrap{},
				classifyContextWriteError(
					"activate context Session",
					updateErr,
				)
		}
		if tag.RowsAffected() != 1 {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrConflict
		}
		if scanErr := tx.QueryRow(ctx, `
			SELECT started_at
			FROM practice_sessions
			WHERE owner_user_id = $1 AND session_id = $2
		`, actor.UserID, sessionID).Scan(&startedAt); scanErr != nil ||
			!startedAt.Valid {
			if scanErr != nil {
				return persistence.ContextSessionBootstrap{},
					fmt.Errorf(
						"read activated context Session: %w",
						scanErr,
					)
			}
			return persistence.ContextSessionBootstrap{},
				persistence.ErrConflict
		}
		value := startedAt.Time.UTC()
		session.Status = persistence.ContextSessionProgress
		session.Version++
		session.StartedAt = &value
	case persistence.ContextSessionProgress:
	case persistence.ContextSessionPaused:
		return persistence.ContextSessionBootstrap{},
			persistence.ErrConflict
	default:
		return persistence.ContextSessionBootstrap{},
			persistence.ErrConflict
	}

	var document []byte
	var storedSnapshotID, storedPlanID string
	if err := tx.QueryRow(ctx, `
		SELECT snapshot.snapshot_id, snapshot.context_plan_id,
		       snapshot.snapshot_document
		FROM practice_session_snapshots AS snapshot
		WHERE snapshot.owner_user_id = $1
		  AND snapshot.session_id = $2
		  AND snapshot.context_plan_id = $3
		  AND snapshot.snapshot_id = $4
	`, actor.UserID, session.ID, session.PlanID, session.SnapshotID).Scan(
		&storedSnapshotID,
		&storedPlanID,
		&document,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return persistence.ContextSessionBootstrap{},
				persistence.ErrNotFound
		}
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("read activated context Snapshot: %w", err)
	}
	snapshot, err := decodeContextSnapshot(document)
	if err != nil || storedSnapshotID != session.SnapshotID ||
		storedPlanID != session.PlanID ||
		snapshot.ID != session.SnapshotID ||
		snapshot.SessionID != session.ID {
		return persistence.ContextSessionBootstrap{},
			persistence.ErrConflict
	}
	bootstrap := persistence.ContextSessionBootstrap{
		Session:  session,
		Snapshot: snapshot,
	}
	if !validStoredContextBootstrap(bootstrap) {
		return persistence.ContextSessionBootstrap{},
			persistence.ErrConflict
	}
	if err := saveContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		intent,
		"session",
		session.ID,
		201,
		bootstrap,
	); err != nil {
		return persistence.ContextSessionBootstrap{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.ContextSessionBootstrap{},
			fmt.Errorf("commit activated context Session: %w", err)
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
			fmt.Errorf("begin context Session transition: %w", err)
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
			session.ID != record.ResourceID {
			return persistence.ContextSession{}, false,
				persistence.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.ContextSession{}, false,
				fmt.Errorf("commit replayed context Session transition: %w", err)
		}
		return session, true, nil
	}
	session, err := scanContextSession(tx.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND session.context_plan_id IS NOT NULL
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
	nextVersion := session.Version + 1
	var startedAtExpression, completedAtExpression, endReasonExpression string
	switch command.Transition {
	case persistence.ContextSessionPause:
		startedAtExpression = "started_at"
		completedAtExpression = "NULL"
		endReasonExpression = "NULL"
	case persistence.ContextSessionResume:
		startedAtExpression = "started_at"
		completedAtExpression = "NULL"
		endReasonExpression = "NULL"
	case persistence.ContextSessionEndEarly:
		startedAtExpression = "COALESCE(started_at, transaction_timestamp())"
		completedAtExpression = "transaction_timestamp()"
		endReasonExpression = "'USER_ENDED'"
	default:
		return persistence.ContextSession{}, false,
			persistence.ErrInvalidArgument
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
		  AND context_plan_id IS NOT NULL
		  AND version = $5
	`, startedAtExpression, completedAtExpression, endReasonExpression)
	tag, err := tx.Exec(
		ctx,
		query,
		actor.UserID,
		command.SessionID,
		nextStatus,
		nextVersion,
		session.Version,
	)
	if err != nil {
		return persistence.ContextSession{}, false,
			classifyContextWriteError("transition context Session", err)
	}
	if tag.RowsAffected() != 1 {
		return persistence.ContextSession{}, false, persistence.ErrConflict
	}
	session, err = scanContextSession(tx.QueryRow(ctx, contextSessionSelect+`
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND session.context_plan_id IS NOT NULL
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
			fmt.Errorf("commit context Session transition: %w", err)
	}
	return session, false, nil
}

const contextPlanSelect = `
	SELECT
		plan.plan_id,
		plan.owner_user_id::text,
		plan.agent_thread_id::text,
		plan.matter_id::text,
		plan.scenario_definition_id,
		plan.scenario_definition_version,
		plan.scenario_type,
		plan.scenario_config_id,
		plan.scenario_config_version,
		plan.preparation_profile_id,
		plan.selected_role_ids,
		plan.plan_revision,
		plan.status,
		plan.created_at,
		plan.updated_at
	FROM practice_plans AS plan
	JOIN identity_users AS owner
	  ON owner.id = plan.owner_user_id
	LEFT JOIN practice_deletion_fences AS fence
	  ON fence.owner_user_id = plan.owner_user_id
`

const contextSessionSelect = `
	SELECT
		session.session_id,
		session.context_plan_id,
		session.scenario_type,
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

type contextRowScanner interface {
	Scan(...any) error
}

type contextQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type contextIdempotencyRecord struct {
	ResourceKind string
	ResourceID   string
	ResponseBody []byte
}

func scanContextPlan(row contextRowScanner) (persistence.Plan, error) {
	var plan persistence.Plan
	var selectedRoles []byte
	err := row.Scan(
		&plan.ID,
		&plan.UserID,
		&plan.AgentThreadID,
		&plan.MatterID,
		&plan.ScenarioDefinitionID,
		&plan.ScenarioDefinitionVersion,
		&plan.ScenarioType,
		&plan.ScenarioConfigID,
		&plan.ScenarioConfigVersion,
		&plan.PreparationProfileID,
		&selectedRoles,
		&plan.Revision,
		&plan.Status,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.Plan{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.Plan{}, fmt.Errorf("read practice plan: %w", err)
	}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	if err := json.Unmarshal(selectedRoles, &plan.SelectedRoleIDs); err != nil ||
		!validUniqueContextIDs(plan.SelectedRoleIDs) {
		return persistence.Plan{}, persistence.ErrConflict
	}
	return plan, nil
}

func lockContextPlan(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	planID string,
) (persistence.Plan, error) {
	return scanContextPlan(tx.QueryRow(ctx, contextPlanSelect+`
		WHERE plan.owner_user_id = $1
		  AND plan.plan_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		FOR UPDATE OF plan
	`, ownerUserID, planID))
}

func scanContextSession(row contextRowScanner) (persistence.ContextSession, error) {
	var session persistence.ContextSession
	var startedAt, completedAt pgtype.Timestamptz
	var endReason pgtype.Text
	err := row.Scan(
		&session.ID,
		&session.PlanID,
		&session.ScenarioType,
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
			fmt.Errorf("read context Session: %w", err)
	}
	session.CreatedAt = session.CreatedAt.UTC()
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

func scanResolvedContextSession(
	row contextRowScanner,
) (persistence.ContextSession, []byte, error) {
	var session persistence.ContextSession
	var startedAt, completedAt pgtype.Timestamptz
	var endReason pgtype.Text
	var document []byte
	err := row.Scan(
		&session.ID,
		&session.PlanID,
		&session.ScenarioType,
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
	if err != nil {
		return persistence.ContextSession{}, nil,
			fmt.Errorf("scan resolved context Session: %w", err)
	}
	session.CreatedAt = session.CreatedAt.UTC()
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
		return persistence.ContextSession{}, nil, persistence.ErrConflict
	}
	return session, document, nil
}

func lockPlanDependencies(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	threadID string,
	matterID string,
	profileID string,
) error {
	var valid bool
	err := tx.QueryRow(ctx, `
		SELECT true
		FROM agent_threads AS thread
		JOIN agent_thread_matter_links AS link
		  ON link.owner_user_id = thread.owner_user_id
		 AND link.thread_id = thread.id
		JOIN matters AS matter
		  ON matter.owner_user_id = link.owner_user_id
		 AND matter.id = link.matter_id
		JOIN preparation_profiles AS profile
		  ON profile.owner_user_id = thread.owner_user_id
		LEFT JOIN preparation_deletion_fences AS preparation_fence
		  ON preparation_fence.owner_user_id = profile.owner_user_id
		WHERE thread.owner_user_id = $1
		  AND thread.id = $2
		  AND link.matter_id = $3
		  AND link.is_active
		  AND matter.status = 'active'
		  AND profile.profile_id = $4
		  AND preparation_fence.owner_user_id IS NULL
		FOR SHARE OF thread, link, matter, profile
	`, ownerUserID, threadID, matterID, profileID).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock practice plan dependencies: %w", err)
	}
	return nil
}

func lockContextIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	intent persistence.ContextIdempotencyIntent,
) error {
	scopeBytes, err := json.Marshal([]string{
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
	`, string(scopeBytes)); err != nil {
		return fmt.Errorf("lock Practice idempotency scope: %w", err)
	}
	return nil
}

func loadContextIdempotency(
	ctx context.Context,
	query contextQueryRow,
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
	row := query.QueryRow(ctx, `
		SELECT
			record.payload_fingerprint,
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
	)
	var record contextIdempotencyRecord
	var storedFingerprint []byte
	err := row.Scan(
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

func decodeContextSnapshot(
	document []byte,
) (persistence.ContextSessionSnapshot, error) {
	var snapshot persistence.ContextSessionSnapshot
	if err := json.Unmarshal(document, &snapshot); err != nil {
		return persistence.ContextSessionSnapshot{}, err
	}
	return snapshot, nil
}

func contextSnapshotMatchesPlan(
	actor persistence.Actor,
	snapshot persistence.ContextSessionSnapshot,
	plan persistence.Plan,
) bool {
	validOptionType := snapshot.PracticeOption.Type == "FULL_SIMULATION" ||
		snapshot.PracticeOption.Type == "FOCUS"
	if snapshot.PlanRevision != plan.Revision ||
		snapshot.ScenarioType != plan.ScenarioType ||
		snapshot.ScenarioDefinition.ID != plan.ScenarioDefinitionID ||
		snapshot.ScenarioDefinition.Version !=
			plan.ScenarioDefinitionVersion ||
		snapshot.ScenarioDefinition.Type != plan.ScenarioType ||
		snapshot.ScenarioDefinition.Status != "active" ||
		strings.TrimSpace(snapshot.ScenarioDefinition.Name) == "" ||
		snapshot.ScenarioConfig.ID != plan.ScenarioConfigID ||
		snapshot.ScenarioConfig.Version != plan.ScenarioConfigVersion ||
		snapshot.ScenarioConfig.ScenarioDefinitionID !=
			plan.ScenarioDefinitionID ||
		snapshot.ScenarioConfig.Type != plan.ScenarioType ||
		strings.TrimSpace(snapshot.ScenarioConfig.JobTitle) == "" ||
		strings.TrimSpace(snapshot.ScenarioConfig.JobDescription) == "" ||
		!validUniqueContextIDs(snapshot.ScenarioConfig.FocusAreas) ||
		snapshot.Preparation.ID == "" ||
		snapshot.Preparation.SourceProfileID !=
			plan.PreparationProfileID ||
		snapshot.Preparation.SourceVersion < 1 ||
		strings.TrimSpace(snapshot.Preparation.BackgroundSnapshot) == "" ||
		snapshot.Preparation.CreatedAt.IsZero() ||
		snapshot.PracticeOption.ScenarioDefinitionID !=
			plan.ScenarioDefinitionID ||
		snapshot.PracticeOption.Version < 1 ||
		!validOptionType ||
		len(snapshot.Participants) != 2 ||
		!validContextPolicy(snapshot.SessionPolicy, snapshot.PracticeOption.Type) ||
		!validContextObjectives(snapshot.PracticeFocuses, false) {
		return false
	}
	selectedRoles := make(map[string]struct{}, len(plan.SelectedRoleIDs))
	for _, roleID := range plan.SelectedRoleIDs {
		selectedRoles[roleID] = struct{}{}
	}
	interviewers := 0
	candidates := 0
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	participantOrders := make(map[int]struct{}, len(snapshot.Participants))
	selectedInterviewerRole := ""
	for _, participant := range snapshot.Participants {
		if !validContextResourceID(participant.ID) ||
			participant.SessionID != snapshot.SessionID ||
			participant.Order < 1 {
			return false
		}
		if _, duplicate := participantIDs[participant.ID]; duplicate {
			return false
		}
		participantIDs[participant.ID] = struct{}{}
		if _, duplicate := participantOrders[participant.Order]; duplicate {
			return false
		}
		participantOrders[participant.Order] = struct{}{}
		switch participant.Role {
		case "INTERVIEWER":
			interviewers++
			if participant.RoleSnapshot == nil ||
				participant.RoleDefinitionID == "" ||
				participant.RoleSnapshot.ID !=
					participant.RoleDefinitionID {
				return false
			}
			roleID := participant.RoleDefinitionID
			if _, selected := selectedRoles[roleID]; !selected ||
				participant.RoleSnapshot.ScenarioDefinitionID !=
					plan.ScenarioDefinitionID ||
				participant.RoleSnapshot.Version < 1 ||
				strings.TrimSpace(
					participant.RoleSnapshot.DisplayName,
				) == "" ||
				!validUniqueContextIDs(
					participant.RoleSnapshot.FocusAreas,
				) {
				return false
			}
			selectedInterviewerRole = participant.RoleDefinitionID
		case "CANDIDATE":
			candidates++
			if participant.SubjectRef.Namespace != "speakup.user" ||
				participant.SubjectRef.SubjectID != actor.UserID ||
				participant.RoleSnapshot != nil ||
				participant.RoleDefinitionID != "" {
				return false
			}
		default:
			return false
		}
	}
	if interviewers != 1 || candidates != 1 {
		return false
	}
	if snapshot.PracticeOption.Type == "FOCUS" {
		return snapshot.PracticeOption.RoleDefinitionID ==
			selectedInterviewerRole
	}
	return snapshot.PracticeOption.RoleDefinitionID == ""
}

func validCreatePlanCommand(command persistence.CreatePlanCommand) bool {
	return validContextResourceID(command.PlanID) &&
		validContextResourceID(command.AgentThreadID) &&
		validContextResourceID(command.MatterID) &&
		validContextResourceID(command.ScenarioDefinitionID) &&
		command.ScenarioDefinitionVersion > 0 &&
		validContextResourceID(command.ScenarioType) &&
		validContextResourceID(command.ScenarioConfigID) &&
		command.ScenarioConfigVersion > 0 &&
		validContextResourceID(command.PreparationProfileID) &&
		validUniqueContextIDs(command.SelectedRoleIDs) &&
		validContextIntent(command.Intent)
}

func validCreateContextSessionCommand(
	actor persistence.Actor,
	command persistence.CreateContextSessionCommand,
) bool {
	snapshot := command.Snapshot
	return validContextResourceID(command.SessionID) &&
		validContextResourceID(command.SnapshotID) &&
		validContextResourceID(command.PlanID) &&
		command.ExpectedPlanRevision > 0 &&
		validContextResourceID(command.PreparationSnapshotID) &&
		snapshot.ID == command.SnapshotID &&
		snapshot.SessionID == command.SessionID &&
		snapshot.PlanRevision == command.ExpectedPlanRevision &&
		snapshot.Preparation.ID == command.PreparationSnapshotID &&
		snapshot.CreatedAt.IsZero() &&
		contextSnapshotMatchesBasicActor(actor, snapshot) &&
		validContextIntent(command.Intent)
}

func contextSnapshotMatchesBasicActor(
	actor persistence.Actor,
	snapshot persistence.ContextSessionSnapshot,
) bool {
	if !validContextResourceID(snapshot.ScenarioType) ||
		!validContextResourceID(snapshot.ScenarioDefinition.ID) ||
		snapshot.ScenarioDefinition.Version < 1 ||
		!validContextResourceID(snapshot.ScenarioConfig.ID) ||
		snapshot.ScenarioConfig.Version < 1 ||
		!validContextResourceID(snapshot.Preparation.ID) ||
		snapshot.Preparation.SourceVersion < 1 ||
		len(snapshot.Participants) < 2 ||
		!validContextResourceID(snapshot.PracticeOption.ID) {
		return false
	}
	candidateFound := false
	for _, participant := range snapshot.Participants {
		if participant.Role == "CANDIDATE" &&
			participant.SubjectRef.Namespace == "speakup.user" &&
			participant.SubjectRef.SubjectID == actor.UserID {
			candidateFound = true
		}
	}
	return candidateFound
}

func validContextPolicy(
	policy persistence.ContextSessionPolicy,
	optionType string,
) bool {
	if policy.SuggestedDurationSeconds < 1 ||
		policy.MinEffectiveTurns < 1 ||
		policy.MinEffectiveTurns > policy.CoverageCheckpointTurn ||
		policy.CoverageCheckpointTurn > policy.MaxEffectiveTurns ||
		policy.MaxFollowUpsPerQuestion < 0 ||
		!validContextObjectives(policy.TargetObjectives, true) ||
		strings.TrimSpace(policy.EarlyCompletionRule) == "" {
		return false
	}
	switch optionType {
	case "FULL_SIMULATION":
		return policy.MinEffectiveTurns == 4 &&
			policy.CoverageCheckpointTurn == 4 &&
			policy.MaxEffectiveTurns == 6
	case "FOCUS":
		return policy.MinEffectiveTurns == 1 &&
			policy.CoverageCheckpointTurn == 1 &&
			policy.MaxEffectiveTurns == 3
	default:
		return false
	}
}

func validContextObjectives(
	objectives []persistence.PracticeObjective,
	required bool,
) bool {
	if required && len(objectives) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(objectives))
	for _, objective := range objectives {
		if !validContextResourceID(objective.ID) ||
			strings.TrimSpace(objective.Description) == "" {
			return false
		}
		if _, duplicate := seen[objective.ID]; duplicate {
			return false
		}
		seen[objective.ID] = struct{}{}
	}
	return true
}

func validContextIntent(intent persistence.ContextIdempotencyIntent) bool {
	return intent.Method == "POST" &&
		strings.HasPrefix(intent.CanonicalPath, "/") &&
		len(intent.CanonicalPath) <= 1024 &&
		strings.TrimSpace(intent.CanonicalPath) ==
			intent.CanonicalPath &&
		len(intent.Key) >= 8 &&
		len(intent.Key) <= 128 &&
		strings.TrimSpace(intent.Key) == intent.Key &&
		!strings.ContainsRune(intent.Key, '\x00')
}

func validContextResourceID(value string) bool {
	return value != "" &&
		len(value) <= 128 &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsRune(value, '\x00')
}

func validUniqueContextIDs(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validContextResourceID(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validStoredContextSession(session persistence.ContextSession) bool {
	if !validContextResourceID(session.ID) ||
		!validContextResourceID(session.PlanID) ||
		!validContextResourceID(session.ScenarioType) ||
		!validContextResourceID(session.SnapshotID) ||
		session.Version < 1 ||
		session.EffectiveTurns < 0 ||
		session.EffectiveTurns > 6 ||
		session.CreatedAt.IsZero() {
		return false
	}
	switch session.Status {
	case persistence.ContextSessionStarting:
		return session.EffectiveTurns == 0 &&
			session.StartedAt == nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case persistence.ContextSessionProgress,
		persistence.ContextSessionPaused:
		return session.StartedAt != nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case persistence.ContextSessionCompleted,
		persistence.ContextSessionEndedEarly:
		return session.StartedAt != nil &&
			session.EndedAt != nil &&
			session.EndReason != "" &&
			(session.Status != persistence.ContextSessionCompleted ||
				session.EffectiveTurns > 0)
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
		bootstrap.Snapshot.ScenarioType ==
			bootstrap.Session.ScenarioType &&
		bootstrap.Snapshot.PlanRevision > 0 &&
		!bootstrap.Snapshot.CreatedAt.IsZero()
}

func validTransitionCommand(
	command persistence.TransitionContextSessionCommand,
) bool {
	if !validContextResourceID(command.SessionID) ||
		command.ExpectedSessionVersion < 1 ||
		!validContextIntent(command.Intent) {
		return false
	}
	return command.Transition == persistence.ContextSessionPause ||
		command.Transition == persistence.ContextSessionResume ||
		command.Transition == persistence.ContextSessionEndEarly
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

func cloneContextStrings(values []string) []string {
	return append([]string(nil), values...)
}

func legacyContextParticipantProjection(
	participants []persistence.ContextParticipant,
) ([]persistence.ParticipantSnapshot, error) {
	projected := make(
		[]persistence.ParticipantSnapshot,
		len(participants),
	)
	for index, participant := range participants {
		projected[index] = persistence.ParticipantSnapshot{
			ParticipantID:   participant.ID,
			ParticipantRole: participant.Role,
			SubjectRef:      participant.SubjectRef,
			Order:           participant.Order,
		}
		if participant.RoleSnapshot == nil {
			continue
		}
		document, err := json.Marshal(participant.RoleSnapshot)
		if err != nil {
			return nil, err
		}
		var roleDefinition map[string]any
		if err := json.Unmarshal(document, &roleDefinition); err != nil {
			return nil, err
		}
		projected[index].RoleDefinition = roleDefinition
	}
	return projected, nil
}

func cloneContextSessionSnapshot(
	snapshot persistence.ContextSessionSnapshot,
) persistence.ContextSessionSnapshot {
	body, err := json.Marshal(snapshot)
	if err != nil {
		return persistence.ContextSessionSnapshot{}
	}
	var cloned persistence.ContextSessionSnapshot
	if err := json.Unmarshal(body, &cloned); err != nil {
		return persistence.ContextSessionSnapshot{}
	}
	return cloned
}

func classifyContextWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23001":
			return persistence.ErrNotFound
		case "23505":
			if postgresError.ConstraintName ==
				"practice_idempotency_records_pkey" {
				return persistence.ErrIdempotencyConflict
			}
			return persistence.ErrConflict
		case "23514", "55000":
			return persistence.ErrConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ persistence.ContextRepository = (*Repository)(nil)
