package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
	if record.ResourceKind != planResourceKind(intent) {
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
	preparationSnapshotID, catalogSnapshot, sessionPolicy, practiceFocuses,
		err := encodePlanPreview(command)
	if err != nil {
		return persistence.Plan{}, false, err
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
		preparationSnapshotID,
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
		ScenarioModel:             command.ScenarioModel,
		ScenarioConfigID:          command.ScenarioConfigID,
		ScenarioConfigVersion:     command.ScenarioConfigVersion,
		PreparationProfileID:      command.PreparationProfileID,
		SelectedRoleIDs:           cloneContextStrings(command.SelectedRoleIDs),
		PreparationSnapshot:       cloneOptionalPreparationSnapshot(command.PreparationSnapshot),
		CatalogSnapshot:           cloneOptionalPlanCatalogSnapshot(command.CatalogSnapshot),
		SessionPolicy:             cloneOptionalContextSessionPolicy(command.SessionPolicy),
		PracticeFocuses:           cloneContextObjectives(command.PracticeFocuses),
		Revision:                  1,
		Status:                    persistence.PlanStatusReady,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_plans (
			owner_user_id, plan_id, agent_thread_id, matter_id,
			scenario_definition_id, scenario_definition_version,
			scenario_type, scenario_model,
			scenario_config_id, scenario_config_version,
			preparation_profile_id, selected_role_ids,
			preparation_snapshot_id, catalog_snapshot,
			session_policy, practice_focuses,
			plan_revision, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16,
			1, 'ready'
		)
		RETURNING created_at, updated_at
	`,
		actor.UserID,
		command.PlanID,
		command.AgentThreadID,
		nullableContextText(command.MatterID),
		command.ScenarioDefinitionID,
		command.ScenarioDefinitionVersion,
		command.ScenarioType,
		command.ScenarioModel,
		command.ScenarioConfigID,
		command.ScenarioConfigVersion,
		command.PreparationProfileID,
		selectedRoles,
		nullableContextText(preparationSnapshotID),
		catalogSnapshot,
		sessionPolicy,
		practiceFocuses,
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

func (r *Repository) UpdatePlan(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.UpdatePlanCommand,
) (persistence.Plan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validUpdatePlanCommand(command) {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	selectedRoles, err := json.Marshal(command.SelectedRoleIDs)
	if err != nil {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	catalogSnapshot, err := json.Marshal(command.CatalogSnapshot)
	if err != nil {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	sessionPolicy, err := json.Marshal(command.SessionPolicy)
	if err != nil {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	practiceFocuses, err := json.Marshal(command.PracticeFocuses)
	if err != nil {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.Plan{}, false,
			fmt.Errorf("begin update practice plan: %w", err)
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
		if replayed.ResourceKind != "plan_revision" {
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
				fmt.Errorf("commit replayed practice plan revision: %w", err)
		}
		return plan, true, nil
	}

	plan, err := lockContextPlan(ctx, tx, actor.UserID, command.PlanID)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if plan.Status != persistence.PlanStatusReady ||
		plan.Revision != command.ExpectedPlanRevision ||
		!completeStoredPlanPreview(plan) ||
		command.CatalogSnapshot.ScenarioDefinition.ID !=
			plan.ScenarioDefinitionID ||
		command.CatalogSnapshot.ScenarioDefinition.Version !=
			plan.ScenarioDefinitionVersion ||
		command.CatalogSnapshot.ScenarioConfig.ID !=
			plan.ScenarioConfigID ||
		command.CatalogSnapshot.ScenarioConfig.Version !=
			plan.ScenarioConfigVersion {
		return persistence.Plan{}, false, persistence.ErrConflict
	}

	tag, err := tx.Exec(ctx, `
		UPDATE practice_plans
		SET selected_role_ids = $3,
		    catalog_snapshot = $4,
		    session_policy = $5,
		    practice_focuses = $6,
		    plan_revision = plan_revision + 1,
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND plan_id = $2
		  AND plan_revision = $7
		  AND status = 'ready'
		  AND preparation_snapshot_id IS NOT NULL
	`,
		actor.UserID,
		command.PlanID,
		selectedRoles,
		catalogSnapshot,
		sessionPolicy,
		practiceFocuses,
		command.ExpectedPlanRevision,
	)
	if err != nil {
		return persistence.Plan{}, false,
			classifyContextWriteError("update practice plan revision", err)
	}
	if tag.RowsAffected() != 1 {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	plan, err = scanContextPlan(tx.QueryRow(ctx, contextPlanSelect+`
		WHERE plan.owner_user_id = $1
		  AND plan.plan_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, command.PlanID))
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if err := saveContextIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"plan_revision",
		plan.ID,
		200,
		plan,
	); err != nil {
		return persistence.Plan{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.Plan{}, false,
			fmt.Errorf("commit practice plan revision: %w", err)
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
		contextPlanPreparationSnapshotID(plan),
	); err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	var storedPreparation persistence.PreparationSnapshot
	var resumeSnapshot, jobDescriptionSnapshot pgtype.Text
	var targetInput, targetCandidate []byte
	err = tx.QueryRow(ctx, `
		SELECT
			snapshot.snapshot_id,
			snapshot.source_profile_id,
			snapshot.source_version,
			snapshot.resume_snapshot,
			snapshot.job_description_snapshot,
			snapshot.background_snapshot,
			COALESCE(snapshot.source_job_target_id, ''),
			COALESCE(
			    snapshot.source_job_target_confirmation_version,
			    0
			),
			snapshot.job_target_input_snapshot,
			snapshot.job_target_candidate_snapshot,
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
		&storedPreparation.SourceJobTargetID,
		&storedPreparation.SourceJobTargetConfirmationVersion,
		&targetInput,
		&targetCandidate,
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
	if err := decodeStoredPreparationTarget(
		&storedPreparation,
		targetInput,
		targetCandidate,
	); err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
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
		ID:            command.SessionID,
		PlanID:        plan.ID,
		ScenarioType:  plan.ScenarioType,
		ScenarioModel: plan.ScenarioModel,
		SnapshotID:    command.SnapshotID,
		Status:        persistence.ContextSessionStarting,
		Version:       1,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, context_plan_id,
			agent_thread_id, matter_id, snapshot_id, scenario_type,
			scenario_model,
			status, version, effective_turns, started_at
		) VALUES (
			$1, $2, $3, $3, $4, $5, $6, $7, $8,
			'starting', 1, 0, NULL
		)
		RETURNING created_at
	`,
		actor.UserID,
		command.SessionID,
		plan.ID,
		plan.AgentThreadID,
		nullableContextText(plan.MatterID),
		command.SnapshotID,
		plan.ScenarioType,
		plan.ScenarioModel,
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
	targetIDs := []string{}
	if plan.MatterID != "" {
		targetIDs = append(targetIDs, plan.MatterID)
	}
	targets, err := json.Marshal(targetIDs)
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
		left.SourceJobTargetID == right.SourceJobTargetID &&
		left.SourceJobTargetConfirmationVersion ==
			right.SourceJobTargetConfirmationVersion &&
		reflect.DeepEqual(
			left.JobTargetInputSnapshot,
			right.JobTargetInputSnapshot,
		) &&
		reflect.DeepEqual(
			left.JobTargetCandidateSnapshot,
			right.JobTargetCandidateSnapshot,
		) &&
		left.ResumeSnapshot == right.ResumeSnapshot &&
		left.JobDescriptionSnapshot == right.JobDescriptionSnapshot &&
		left.BackgroundSnapshot == right.BackgroundSnapshot &&
		left.CreatedAt.Equal(right.CreatedAt)
}

func encodePlanPreview(
	command persistence.CreatePlanCommand,
) (string, []byte, []byte, []byte, error) {
	if command.PreparationSnapshot == nil &&
		command.CatalogSnapshot == nil &&
		command.SessionPolicy == nil &&
		len(command.PracticeFocuses) == 0 {
		return "", nil, nil, nil, nil
	}
	if command.CatalogSnapshot == nil ||
		command.SessionPolicy == nil ||
		len(command.PracticeFocuses) == 0 {
		return "", nil, nil, nil, persistence.ErrInvalidArgument
	}
	catalog, err := json.Marshal(command.CatalogSnapshot)
	if err != nil {
		return "", nil, nil, nil, persistence.ErrInvalidArgument
	}
	policy, err := json.Marshal(command.SessionPolicy)
	if err != nil {
		return "", nil, nil, nil, persistence.ErrInvalidArgument
	}
	focuses, err := json.Marshal(command.PracticeFocuses)
	if err != nil {
		return "", nil, nil, nil, persistence.ErrInvalidArgument
	}
	preparationSnapshotID := ""
	if command.PreparationSnapshot != nil {
		preparationSnapshotID = command.PreparationSnapshot.ID
	}
	return preparationSnapshotID, catalog, policy, focuses, nil
}

func decodeStoredPreparationTarget(
	snapshot *persistence.PreparationSnapshot,
	targetInput []byte,
	targetCandidate []byte,
) error {
	if snapshot == nil {
		return persistence.ErrConflict
	}
	if snapshot.SourceJobTargetID == "" {
		if snapshot.SourceJobTargetConfirmationVersion != 0 ||
			len(targetInput) != 0 || len(targetCandidate) != 0 {
			return persistence.ErrConflict
		}
		return nil
	}
	if snapshot.SourceJobTargetConfirmationVersion < 1 ||
		len(targetInput) == 0 || len(targetCandidate) == 0 {
		return persistence.ErrConflict
	}
	var input persistence.JobTargetInputSnapshot
	var candidate persistence.JobTargetCandidateSnapshot
	if err := json.Unmarshal(targetInput, &input); err != nil ||
		json.Unmarshal(targetCandidate, &candidate) != nil {
		return persistence.ErrConflict
	}
	snapshot.JobTargetInputSnapshot = &input
	snapshot.JobTargetCandidateSnapshot = &candidate
	return nil
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
			session.scenario_model,
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
		 AND plan.matter_id IS NOT DISTINCT FROM session.matter_id
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = session.owner_user_id
		 AND snapshot.session_id = session.session_id
		 AND snapshot.context_plan_id = session.context_plan_id
		 AND snapshot.snapshot_id = session.snapshot_id
		LEFT JOIN agent_thread_matter_links AS link
		  ON link.owner_user_id = plan.owner_user_id
		 AND link.thread_id = plan.agent_thread_id
		 AND link.matter_id = plan.matter_id
		LEFT JOIN matters AS matter
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
		  AND (
		      plan.matter_id IS NULL
		      OR matter.status = 'active'
		  )
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
			session.scenario_model,
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
// in_progress after re-validating its immutable Thread and optional Matter.
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
		(matterID != "" && !validContextResourceID(matterID)) ||
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
		 AND plan.matter_id IS NOT DISTINCT FROM session.matter_id
		LEFT JOIN agent_thread_matter_links AS link
		  ON link.owner_user_id = plan.owner_user_id
		 AND link.thread_id = plan.agent_thread_id
		 AND link.matter_id = plan.matter_id
		LEFT JOIN matters AS matter
		  ON matter.owner_user_id = plan.owner_user_id
		 AND matter.id = plan.matter_id
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND session.agent_thread_id = $3
		  AND session.matter_id IS NOT DISTINCT FROM $4::uuid
		  AND session.context_plan_id IS NOT NULL
		  AND plan.status = 'ready'
		  AND (
		      plan.matter_id IS NULL
		      OR matter.status = 'active'
		  )
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		FOR UPDATE OF session
	`,
		actor.UserID,
		sessionID,
		threadID,
		nullableContextText(matterID),
	))
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
		COALESCE(plan.matter_id::text, ''),
		plan.scenario_definition_id,
		plan.scenario_definition_version,
		plan.scenario_type,
		plan.scenario_model,
		plan.scenario_config_id,
		plan.scenario_config_version,
		plan.preparation_profile_id,
		plan.selected_role_ids,
		COALESCE(plan.preparation_snapshot_id, ''),
		COALESCE(snapshot.source_profile_id, ''),
		COALESCE(snapshot.source_version, 0),
		snapshot.resume_snapshot,
		snapshot.job_description_snapshot,
		COALESCE(snapshot.background_snapshot, ''),
		COALESCE(snapshot.source_job_target_id, ''),
		COALESCE(
		    snapshot.source_job_target_confirmation_version,
		    0
		),
		snapshot.job_target_input_snapshot,
		snapshot.job_target_candidate_snapshot,
		snapshot.created_at,
		plan.catalog_snapshot,
		plan.session_policy,
		plan.practice_focuses,
		plan.plan_revision,
		plan.status,
		plan.created_at,
		plan.updated_at
	FROM practice_plans AS plan
	LEFT JOIN preparation_snapshots AS snapshot
	  ON snapshot.owner_user_id = plan.owner_user_id
	 AND snapshot.snapshot_id = plan.preparation_snapshot_id
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
		session.scenario_model,
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
	var preparation persistence.PreparationSnapshot
	var resumeSnapshot, jobDescriptionSnapshot pgtype.Text
	var preparationCreatedAt pgtype.Timestamptz
	var targetInput, targetCandidate []byte
	var catalogSnapshot, sessionPolicy, practiceFocuses []byte
	err := row.Scan(
		&plan.ID,
		&plan.UserID,
		&plan.AgentThreadID,
		&plan.MatterID,
		&plan.ScenarioDefinitionID,
		&plan.ScenarioDefinitionVersion,
		&plan.ScenarioType,
		&plan.ScenarioModel,
		&plan.ScenarioConfigID,
		&plan.ScenarioConfigVersion,
		&plan.PreparationProfileID,
		&selectedRoles,
		&preparation.ID,
		&preparation.SourceProfileID,
		&preparation.SourceVersion,
		&resumeSnapshot,
		&jobDescriptionSnapshot,
		&preparation.BackgroundSnapshot,
		&preparation.SourceJobTargetID,
		&preparation.SourceJobTargetConfirmationVersion,
		&targetInput,
		&targetCandidate,
		&preparationCreatedAt,
		&catalogSnapshot,
		&sessionPolicy,
		&practiceFocuses,
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
	if preparation.ID == "" {
		if len(catalogSnapshot) == 0 && len(sessionPolicy) == 0 &&
			len(practiceFocuses) == 0 {
			return plan, nil
		}
		if len(catalogSnapshot) == 0 || len(sessionPolicy) == 0 ||
			len(practiceFocuses) == 0 {
			return persistence.Plan{}, persistence.ErrConflict
		}
		if err := decodeStoredPlanConfiguration(
			&plan,
			catalogSnapshot,
			sessionPolicy,
			practiceFocuses,
		); err != nil {
			return persistence.Plan{}, err
		}
		if !completeStoredPlanConfiguration(plan) {
			return persistence.Plan{}, persistence.ErrConflict
		}
		return plan, nil
	}
	if !preparationCreatedAt.Valid || len(catalogSnapshot) == 0 ||
		len(sessionPolicy) == 0 || len(practiceFocuses) == 0 {
		return persistence.Plan{}, persistence.ErrConflict
	}
	if resumeSnapshot.Valid {
		preparation.ResumeSnapshot = resumeSnapshot.String
	}
	if jobDescriptionSnapshot.Valid {
		preparation.JobDescriptionSnapshot = jobDescriptionSnapshot.String
	}
	preparation.CreatedAt = preparationCreatedAt.Time.UTC()
	if err := decodeStoredPreparationTarget(
		&preparation,
		targetInput,
		targetCandidate,
	); err != nil {
		return persistence.Plan{}, err
	}
	plan.PreparationSnapshot = &preparation
	if err := decodeStoredPlanConfiguration(
		&plan,
		catalogSnapshot,
		sessionPolicy,
		practiceFocuses,
	); err != nil {
		return persistence.Plan{}, err
	}
	if !completeStoredPlanPreview(plan) {
		return persistence.Plan{}, persistence.ErrConflict
	}
	return plan, nil
}

func decodeStoredPlanConfiguration(
	plan *persistence.Plan,
	catalogSnapshot []byte,
	sessionPolicy []byte,
	practiceFocuses []byte,
) error {
	if plan == nil {
		return persistence.ErrConflict
	}
	var catalog persistence.PlanCatalogSnapshot
	var policy persistence.ContextSessionPolicy
	var focuses []persistence.PracticeObjective
	if err := json.Unmarshal(catalogSnapshot, &catalog); err != nil ||
		json.Unmarshal(sessionPolicy, &policy) != nil ||
		json.Unmarshal(practiceFocuses, &focuses) != nil {
		return persistence.ErrConflict
	}
	plan.CatalogSnapshot = &catalog
	plan.SessionPolicy = &policy
	plan.PracticeFocuses = focuses
	return nil
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
		&session.ScenarioModel,
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
		&session.ScenarioModel,
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
	preparationSnapshotID string,
) error {
	var valid bool
	if matterID == "" {
		err := tx.QueryRow(ctx, `
			SELECT true
			FROM agent_threads AS thread
			JOIN preparation_profiles AS profile
			  ON profile.owner_user_id = thread.owner_user_id
			LEFT JOIN preparation_deletion_fences AS preparation_fence
			  ON preparation_fence.owner_user_id = profile.owner_user_id
			WHERE thread.owner_user_id = $1
			  AND thread.id = $2
			  AND profile.profile_id = $3
			  AND preparation_fence.owner_user_id IS NULL
			FOR SHARE OF thread, profile
		`, ownerUserID, threadID, profileID).Scan(&valid)
		if errors.Is(err, pgx.ErrNoRows) {
			return persistence.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf(
				"lock Matter-free practice plan dependencies: %w",
				err,
			)
		}
		return lockPreparationSnapshotDependency(
			ctx,
			tx,
			ownerUserID,
			profileID,
			preparationSnapshotID,
		)
	}
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
	return lockPreparationSnapshotDependency(
		ctx,
		tx,
		ownerUserID,
		profileID,
		preparationSnapshotID,
	)
}

func lockPreparationSnapshotDependency(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	profileID string,
	preparationSnapshotID string,
) error {
	if preparationSnapshotID == "" {
		return nil
	}
	var valid bool
	err := tx.QueryRow(ctx, `
		SELECT true
		FROM preparation_snapshots AS snapshot
		WHERE snapshot.owner_user_id = $1
		  AND snapshot.snapshot_id = $2
		  AND snapshot.source_profile_id = $3
		FOR KEY SHARE OF snapshot
	`, ownerUserID, preparationSnapshotID, profileID).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock Practice Preparation Snapshot: %w", err)
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
	normalizeLegacyScenarioModel(&snapshot)
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
		snapshot.ScenarioModel != plan.ScenarioModel ||
		snapshot.ScenarioDefinition.ID != plan.ScenarioDefinitionID ||
		snapshot.ScenarioDefinition.Version !=
			plan.ScenarioDefinitionVersion ||
		snapshot.ScenarioDefinition.Type != plan.ScenarioType ||
		snapshot.ScenarioDefinition.Model != plan.ScenarioModel ||
		snapshot.ScenarioDefinition.Status != "active" ||
		strings.TrimSpace(snapshot.ScenarioDefinition.Name) == "" ||
		snapshot.ScenarioConfig.ID != plan.ScenarioConfigID ||
		snapshot.ScenarioConfig.Version != plan.ScenarioConfigVersion ||
		snapshot.ScenarioConfig.ScenarioDefinitionID !=
			plan.ScenarioDefinitionID ||
		snapshot.ScenarioConfig.Type != plan.ScenarioType ||
		snapshot.ScenarioConfig.Model != plan.ScenarioModel ||
		!validScenarioCompatibilityFields(snapshot.ScenarioConfig) ||
		!validScenarioPromptModel(snapshot.ScenarioConfig.PromptModel) ||
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
		!validContextPolicy(
			snapshot.SessionPolicy,
			snapshot.PracticeOption.Type,
			snapshot.ScenarioModel,
		) ||
		!validContextObjectives(snapshot.PracticeFocuses, false) {
		return false
	}
	if plan.PreparationSnapshot != nil {
		if plan.CatalogSnapshot == nil || plan.SessionPolicy == nil ||
			!equalPreparationSnapshot(
				*plan.PreparationSnapshot,
				snapshot.Preparation,
			) ||
			!reflect.DeepEqual(
				plan.CatalogSnapshot.ScenarioDefinition,
				snapshot.ScenarioDefinition,
			) ||
			!reflect.DeepEqual(
				plan.CatalogSnapshot.ScenarioConfig,
				snapshot.ScenarioConfig,
			) ||
			!reflect.DeepEqual(
				plan.CatalogSnapshot.PracticeOption,
				snapshot.PracticeOption,
			) ||
			!reflect.DeepEqual(*plan.SessionPolicy, snapshot.SessionPolicy) ||
			!reflect.DeepEqual(
				plan.PracticeFocuses,
				snapshot.PracticeFocuses,
			) {
			return false
		}
	}
	selectedRoles := make(map[string]struct{}, len(plan.SelectedRoleIDs))
	for _, roleID := range plan.SelectedRoleIDs {
		selectedRoles[roleID] = struct{}{}
	}
	facilitators := 0
	learners := 0
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	participantOrders := make(map[int]struct{}, len(snapshot.Participants))
	selectedFacilitatorRole := ""
	var selectedFacilitatorSnapshot *persistence.RoleSnapshot
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
		case "FACILITATOR", "INTERVIEWER":
			facilitators++
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
			selectedFacilitatorRole = participant.RoleDefinitionID
			selectedFacilitatorSnapshot = participant.RoleSnapshot
		case "LEARNER", "CANDIDATE":
			learners++
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
	if facilitators != 1 || learners != 1 {
		return false
	}
	if plan.CatalogSnapshot != nil {
		if len(plan.CatalogSnapshot.SelectedRoles) != 1 ||
			selectedFacilitatorSnapshot == nil ||
			!reflect.DeepEqual(
				plan.CatalogSnapshot.SelectedRoles[0],
				*selectedFacilitatorSnapshot,
			) {
			return false
		}
	}
	if snapshot.PracticeOption.Type == "FOCUS" {
		return snapshot.PracticeOption.RoleDefinitionID ==
			selectedFacilitatorRole
	}
	return snapshot.PracticeOption.RoleDefinitionID == ""
}

func completeStoredPlanPreview(plan persistence.Plan) bool {
	if plan.PreparationSnapshot == nil ||
		!validStoredPreparationSnapshot(
			*plan.PreparationSnapshot,
		) ||
		plan.PreparationSnapshot.SourceProfileID !=
			plan.PreparationProfileID {
		return false
	}
	return completeStoredPlanConfiguration(plan)
}

func completeStoredPlanConfiguration(plan persistence.Plan) bool {
	if plan.CatalogSnapshot == nil ||
		plan.SessionPolicy == nil ||
		len(plan.PracticeFocuses) == 0 ||
		!validStoredPlanCatalog(
			*plan.CatalogSnapshot,
			plan.SelectedRoleIDs,
		) ||
		plan.CatalogSnapshot.ScenarioDefinition.ID !=
			plan.ScenarioDefinitionID ||
		plan.CatalogSnapshot.ScenarioDefinition.Version !=
			plan.ScenarioDefinitionVersion ||
		plan.CatalogSnapshot.ScenarioDefinition.Type !=
			plan.ScenarioType ||
		plan.CatalogSnapshot.ScenarioDefinition.Model !=
			plan.ScenarioModel ||
		plan.CatalogSnapshot.ScenarioConfig.ID !=
			plan.ScenarioConfigID ||
		plan.CatalogSnapshot.ScenarioConfig.Version !=
			plan.ScenarioConfigVersion ||
		!validContextPolicy(
			*plan.SessionPolicy,
			plan.CatalogSnapshot.PracticeOption.Type,
			plan.ScenarioModel,
		) ||
		!validContextObjectives(plan.PracticeFocuses, true) {
		return false
	}
	return true
}

func validStoredPreparationSnapshot(
	snapshot persistence.PreparationSnapshot,
) bool {
	if !validContextResourceID(snapshot.ID) ||
		!validContextResourceID(snapshot.SourceProfileID) ||
		snapshot.SourceVersion < 1 ||
		strings.TrimSpace(snapshot.BackgroundSnapshot) == "" ||
		snapshot.CreatedAt.IsZero() {
		return false
	}
	targetEmpty := snapshot.SourceJobTargetID == "" &&
		snapshot.SourceJobTargetConfirmationVersion == 0 &&
		snapshot.JobTargetInputSnapshot == nil &&
		snapshot.JobTargetCandidateSnapshot == nil
	return targetEmpty || validStoredTargetedPreparationSnapshot(snapshot)
}

func validStoredTargetedPreparationSnapshot(
	snapshot persistence.PreparationSnapshot,
) bool {
	if !validContextResourceID(snapshot.ID) ||
		!validContextResourceID(snapshot.SourceProfileID) ||
		snapshot.SourceVersion < 1 ||
		!validContextResourceID(snapshot.SourceJobTargetID) ||
		snapshot.SourceJobTargetConfirmationVersion < 1 ||
		snapshot.JobTargetInputSnapshot == nil ||
		snapshot.JobTargetCandidateSnapshot == nil ||
		strings.TrimSpace(snapshot.BackgroundSnapshot) == "" ||
		snapshot.CreatedAt.IsZero() {
		return false
	}
	input := snapshot.JobTargetInputSnapshot
	candidate := snapshot.JobTargetCandidateSnapshot
	recommendation := candidate.CatalogRecommendation
	sourceShape := (candidate.Source == "quick_start" &&
		candidate.GeneralAdviceOnly &&
		strings.TrimSpace(input.JobTitle) != "" &&
		input.JobDescription == "") ||
		(candidate.Source == "job_description" &&
			!candidate.GeneralAdviceOnly &&
			strings.TrimSpace(input.JobDescription) != "")
	return input.Source == candidate.Source &&
		(input.Source == "job_description" ||
			input.Source == "quick_start") &&
		sourceShape &&
		strings.TrimSpace(candidate.JobTitle) != "" &&
		strings.TrimSpace(candidate.Seniority) != "" &&
		strings.TrimSpace(candidate.ScopeNotice) != "" &&
		validNonBlankContextTexts(candidate.Responsibilities) &&
		validNonBlankContextTexts(candidate.CoreSkills) &&
		validNonBlankContextTexts(candidate.CommunicationFocus) &&
		validNonBlankContextTexts(candidate.PracticeGoals) &&
		validContextResourceID(recommendation.ScenarioDefinitionID) &&
		recommendation.ScenarioDefinitionVersion > 0 &&
		validUniqueContextIDs(recommendation.SelectedRoleIDs) &&
		validContextResourceID(recommendation.PracticeOptionID) &&
		recommendation.PracticeOptionVersion > 0
}

func validStoredPlanCatalog(
	catalog persistence.PlanCatalogSnapshot,
	selectedRoleIDs []string,
) bool {
	definition := catalog.ScenarioDefinition
	config := catalog.ScenarioConfig
	option := catalog.PracticeOption
	if !validContextResourceID(definition.ID) ||
		!validContextResourceID(string(definition.Type)) ||
		!validContextResourceID(string(definition.Model)) ||
		!validScenarioFamilyModel(definition.Type, definition.Model) ||
		strings.TrimSpace(definition.Name) == "" ||
		definition.Version < 1 ||
		definition.Status != "active" ||
		!validContextResourceID(config.ID) ||
		config.ScenarioDefinitionID != definition.ID ||
		config.Type != definition.Type ||
		config.Model != definition.Model ||
		config.Version < 1 ||
		!validScenarioCompatibilityFields(config) ||
		!validScenarioPromptModel(config.PromptModel) ||
		!validUniqueContextIDs(selectedRoleIDs) ||
		len(catalog.SelectedRoles) != len(selectedRoleIDs) ||
		(definition.Type == "INTERVIEW" &&
			len(catalog.SelectedRoles) != 1) ||
		!validContextResourceID(option.ID) ||
		option.ScenarioDefinitionID != definition.ID ||
		strings.TrimSpace(option.DisplayName) == "" ||
		option.Version < 1 {
		return false
	}
	for index, role := range catalog.SelectedRoles {
		if role.ID != selectedRoleIDs[index] ||
			role.ScenarioDefinitionID != definition.ID ||
			!validContextResourceID(role.Type) ||
			strings.TrimSpace(role.DisplayName) == "" ||
			strings.TrimSpace(role.Responsibilities) == "" ||
			strings.TrimSpace(role.Style) == "" ||
			!validUniqueContextIDs(role.FocusAreas) ||
			role.Version < 1 {
			return false
		}
	}
	switch option.Type {
	case "FOCUS":
		return len(catalog.SelectedRoles) == 1 &&
			option.RoleDefinitionID == catalog.SelectedRoles[0].ID
	case "FULL_SIMULATION":
		return option.RoleDefinitionID == ""
	default:
		return false
	}
}

func validScenarioFamilyModel(
	family persistence.ScenarioFamily,
	model persistence.ScenarioModel,
) bool {
	switch family {
	case persistence.ScenarioFamilyInterview:
		return model == persistence.ScenarioModelProjectExperienceDeepDive ||
			model == persistence.ScenarioModelInterviewBasicDialogue
	case persistence.ScenarioFamilyExam:
		return model == persistence.ScenarioModelIELTSSpeakingPart2 ||
			model == persistence.ScenarioModelIELTSSpeakingFullMock ||
			model == persistence.ScenarioModelExamBasicDialogue
	case persistence.ScenarioFamilyWorkplace:
		return model == persistence.ScenarioModelProgressAndRiskUpdate ||
			model == persistence.ScenarioModelWorkplaceBasicDialogue
	case persistence.ScenarioFamilyDaily:
		return model ==
			persistence.ScenarioModelHotelCheckinAndIssueHandling ||
			model == persistence.ScenarioModelDailyBasicDialogue
	default:
		return false
	}
}

func validScenarioCompatibilityFields(
	config persistence.ScenarioConfigSnapshot,
) bool {
	if config.Model ==
		persistence.ScenarioModelProjectExperienceDeepDive {
		return strings.TrimSpace(config.JobTitle) != "" &&
			strings.TrimSpace(config.JobTitle) == config.JobTitle &&
			strings.TrimSpace(config.JobDescription) != "" &&
			strings.TrimSpace(config.JobDescription) ==
				config.JobDescription
	}
	return config.JobTitle == "" && config.JobDescription == ""
}

func validScenarioPromptModel(
	model persistence.ScenarioPromptModel,
) bool {
	return strings.TrimSpace(model.PublicSceneBrief) != "" &&
		strings.TrimSpace(model.PublicSceneBrief) ==
			model.PublicSceneBrief &&
		strings.TrimSpace(model.PracticeGoal) != "" &&
		strings.TrimSpace(model.PracticeGoal) == model.PracticeGoal &&
		strings.TrimSpace(model.UserRole) != "" &&
		strings.TrimSpace(model.UserRole) == model.UserRole &&
		strings.TrimSpace(model.AIRole) != "" &&
		strings.TrimSpace(model.AIRole) == model.AIRole &&
		strings.TrimSpace(model.PersonaSummary) != "" &&
		strings.TrimSpace(model.PersonaSummary) == model.PersonaSummary &&
		validUniqueNonBlankContextTexts(model.FocusAreas) &&
		validUniqueNonBlankContextTexts(model.TurnBlueprints) &&
		model.SuggestedDurationSeconds > 0
}

func validUniqueNonBlankContextTexts(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" ||
			strings.TrimSpace(value) != value {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func normalizeLegacyScenarioModel(
	snapshot *persistence.ContextSessionSnapshot,
) {
	if snapshot == nil ||
		snapshot.ScenarioType != persistence.ScenarioFamilyInterview {
		return
	}
	if snapshot.ScenarioModel == "" {
		snapshot.ScenarioModel =
			persistence.ScenarioModelProjectExperienceDeepDive
	}
	if snapshot.ScenarioDefinition.Model == "" {
		snapshot.ScenarioDefinition.Model =
			persistence.ScenarioModelProjectExperienceDeepDive
	}
	if snapshot.ScenarioConfig.Model == "" {
		snapshot.ScenarioConfig.Model =
			persistence.ScenarioModelProjectExperienceDeepDive
	}
	if snapshot.ScenarioConfig.PromptModel.PublicSceneBrief == "" &&
		len(snapshot.ScenarioConfig.FocusAreas) > 0 {
		snapshot.ScenarioConfig.PromptModel =
			persistence.ScenarioPromptModel{
				PublicSceneBrief: "Legacy interview practice session.",
				PracticeGoal:     "Practice clear, evidence-based interview answers.",
				UserRole:         "Candidate",
				AIRole:           "Interviewer",
				PersonaSummary:   "An English interviewer using the frozen legacy interview context.",
				FocusAreas: cloneContextStrings(
					snapshot.ScenarioConfig.FocusAreas,
				),
				TurnBlueprints: []string{
					"Clarify the candidate background and responsibility",
					"Probe one important challenge",
					"Discuss a decision and trade-off",
					"Confirm the result and reflection",
				},
				SuggestedDurationSeconds: 900,
			}
	}
}

func validNonBlankContextTexts(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" ||
			strings.TrimSpace(value) != value {
			return false
		}
	}
	return true
}

func validCreatePlanCommand(command persistence.CreatePlanCommand) bool {
	baseValid := validContextResourceID(command.PlanID) &&
		validContextResourceID(command.AgentThreadID) &&
		(command.MatterID == "" ||
			validContextResourceID(command.MatterID)) &&
		validContextResourceID(command.ScenarioDefinitionID) &&
		command.ScenarioDefinitionVersion > 0 &&
		validContextResourceID(string(command.ScenarioType)) &&
		validContextResourceID(string(command.ScenarioModel)) &&
		validScenarioFamilyModel(command.ScenarioType, command.ScenarioModel) &&
		validContextResourceID(command.ScenarioConfigID) &&
		command.ScenarioConfigVersion > 0 &&
		validContextResourceID(command.PreparationProfileID) &&
		validUniqueContextIDs(command.SelectedRoleIDs) &&
		validContextIntent(command.Intent) &&
		command.Intent.Method == "POST"
	if !baseValid {
		return false
	}
	legacy := command.PreparationSnapshot == nil &&
		command.CatalogSnapshot == nil &&
		command.SessionPolicy == nil &&
		len(command.PracticeFocuses) == 0
	if legacy {
		return true
	}
	if command.CatalogSnapshot == nil ||
		command.SessionPolicy == nil ||
		len(command.PracticeFocuses) == 0 {
		return false
	}
	if command.PreparationSnapshot == nil {
		return completeStoredPlanConfiguration(persistence.Plan{
			ScenarioDefinitionID:      command.ScenarioDefinitionID,
			ScenarioDefinitionVersion: command.ScenarioDefinitionVersion,
			ScenarioType:              command.ScenarioType,
			ScenarioModel:             command.ScenarioModel,
			ScenarioConfigID:          command.ScenarioConfigID,
			ScenarioConfigVersion:     command.ScenarioConfigVersion,
			SelectedRoleIDs:           command.SelectedRoleIDs,
			CatalogSnapshot:           command.CatalogSnapshot,
			SessionPolicy:             command.SessionPolicy,
			PracticeFocuses:           command.PracticeFocuses,
		})
	}
	return completeStoredPlanPreview(persistence.Plan{
		ScenarioDefinitionID:      command.ScenarioDefinitionID,
		ScenarioDefinitionVersion: command.ScenarioDefinitionVersion,
		ScenarioType:              command.ScenarioType,
		ScenarioModel:             command.ScenarioModel,
		ScenarioConfigID:          command.ScenarioConfigID,
		ScenarioConfigVersion:     command.ScenarioConfigVersion,
		PreparationProfileID:      command.PreparationProfileID,
		SelectedRoleIDs:           command.SelectedRoleIDs,
		PreparationSnapshot:       command.PreparationSnapshot,
		CatalogSnapshot:           command.CatalogSnapshot,
		SessionPolicy:             command.SessionPolicy,
		PracticeFocuses:           command.PracticeFocuses,
	})
}

func validUpdatePlanCommand(command persistence.UpdatePlanCommand) bool {
	if !validContextResourceID(command.PlanID) ||
		command.ExpectedPlanRevision < 1 ||
		!validUniqueContextIDs(command.SelectedRoleIDs) ||
		!validContextIntent(command.Intent) ||
		command.Intent.Method != "PUT" {
		return false
	}
	return validStoredPlanCatalog(
		command.CatalogSnapshot,
		command.SelectedRoleIDs,
	) &&
		validContextPolicy(
			command.SessionPolicy,
			command.CatalogSnapshot.PracticeOption.Type,
			command.CatalogSnapshot.ScenarioDefinition.Model,
		) &&
		validContextObjectives(command.PracticeFocuses, true)
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
		validContextIntent(command.Intent) &&
		command.Intent.Method == "POST"
}

func contextSnapshotMatchesBasicActor(
	actor persistence.Actor,
	snapshot persistence.ContextSessionSnapshot,
) bool {
	if !validContextResourceID(string(snapshot.ScenarioType)) ||
		!validContextResourceID(string(snapshot.ScenarioModel)) ||
		!validScenarioFamilyModel(
			snapshot.ScenarioType,
			snapshot.ScenarioModel,
		) ||
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
		if (participant.Role == "CANDIDATE" ||
			participant.Role == "LEARNER") &&
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
	scenarioModel persistence.ScenarioModel,
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
		if scenarioModel == persistence.ScenarioModelIELTSSpeakingFullMock {
			return policy.MinEffectiveTurns == 14 &&
				policy.CoverageCheckpointTurn == 14 &&
				policy.MaxEffectiveTurns == 14 &&
				policy.MaxFollowUpsPerQuestion == 0
		}
		return policy.MinEffectiveTurns == 4 &&
			policy.CoverageCheckpointTurn == 4 &&
			policy.MaxEffectiveTurns >= 4 &&
			policy.MaxEffectiveTurns <= 6
	case "FOCUS":
		return policy.MinEffectiveTurns == 1 &&
			policy.CoverageCheckpointTurn == 1 &&
			policy.MaxEffectiveTurns >= 1 &&
			policy.MaxEffectiveTurns <= 3
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
	return (intent.Method == "POST" || intent.Method == "PUT") &&
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
		!validContextResourceID(string(session.ScenarioType)) ||
		!validContextResourceID(string(session.ScenarioModel)) ||
		!validScenarioFamilyModel(
			session.ScenarioType,
			session.ScenarioModel,
		) ||
		!validContextResourceID(session.SnapshotID) ||
		session.Version < 1 ||
		session.EffectiveTurns < 0 ||
		session.EffectiveTurns > 14 ||
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
		!validContextIntent(command.Intent) ||
		command.Intent.Method != "POST" {
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

func cloneContextObjectives(
	values []persistence.PracticeObjective,
) []persistence.PracticeObjective {
	return append([]persistence.PracticeObjective(nil), values...)
}

func cloneOptionalPreparationSnapshot(
	source *persistence.PreparationSnapshot,
) *persistence.PreparationSnapshot {
	if source == nil {
		return nil
	}
	result := *source
	if source.JobTargetInputSnapshot != nil {
		input := *source.JobTargetInputSnapshot
		result.JobTargetInputSnapshot = &input
	}
	if source.JobTargetCandidateSnapshot != nil {
		candidate := *source.JobTargetCandidateSnapshot
		candidate.Responsibilities = cloneContextStrings(
			source.JobTargetCandidateSnapshot.Responsibilities,
		)
		candidate.CoreSkills = cloneContextStrings(
			source.JobTargetCandidateSnapshot.CoreSkills,
		)
		candidate.CommunicationFocus = cloneContextStrings(
			source.JobTargetCandidateSnapshot.CommunicationFocus,
		)
		candidate.PracticeGoals = cloneContextStrings(
			source.JobTargetCandidateSnapshot.PracticeGoals,
		)
		candidate.CatalogRecommendation.SelectedRoleIDs =
			cloneContextStrings(
				source.JobTargetCandidateSnapshot.
					CatalogRecommendation.SelectedRoleIDs,
			)
		result.JobTargetCandidateSnapshot = &candidate
	}
	return &result
}

func cloneOptionalPlanCatalogSnapshot(
	source *persistence.PlanCatalogSnapshot,
) *persistence.PlanCatalogSnapshot {
	if source == nil {
		return nil
	}
	result := *source
	result.ScenarioConfig.PromptModel.FocusAreas = cloneContextStrings(
		source.ScenarioConfig.PromptModel.FocusAreas,
	)
	result.ScenarioConfig.PromptModel.TurnBlueprints = cloneContextStrings(
		source.ScenarioConfig.PromptModel.TurnBlueprints,
	)
	result.ScenarioConfig.FocusAreas = cloneContextStrings(
		source.ScenarioConfig.FocusAreas,
	)
	result.SelectedRoles = make(
		[]persistence.RoleSnapshot,
		len(source.SelectedRoles),
	)
	for index, role := range source.SelectedRoles {
		result.SelectedRoles[index] = role
		result.SelectedRoles[index].FocusAreas =
			cloneContextStrings(role.FocusAreas)
	}
	return &result
}

func cloneOptionalContextSessionPolicy(
	source *persistence.ContextSessionPolicy,
) *persistence.ContextSessionPolicy {
	if source == nil {
		return nil
	}
	result := *source
	result.TargetObjectives = cloneContextObjectives(
		source.TargetObjectives,
	)
	return &result
}

func contextPlanPreparationSnapshotID(plan persistence.Plan) string {
	if plan.PreparationSnapshot == nil {
		return ""
	}
	return plan.PreparationSnapshot.ID
}

func nullableContextText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func planResourceKind(intent persistence.ContextIdempotencyIntent) string {
	if intent.Method == "PUT" {
		return "plan_revision"
	}
	return "plan"
}

func legacyContextParticipantProjection(
	participants []persistence.ContextParticipant,
) ([]persistence.ParticipantSnapshot, error) {
	projected := make(
		[]persistence.ParticipantSnapshot,
		len(participants),
	)
	for index, participant := range participants {
		legacyRole := participant.Role
		switch legacyRole {
		case "FACILITATOR":
			legacyRole = "INTERVIEWER"
		case "LEARNER":
			legacyRole = "CANDIDATE"
		}
		projected[index] = persistence.ParticipantSnapshot{
			ParticipantID:   participant.ID,
			ParticipantRole: legacyRole,
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
			if postgresError.ConstraintName ==
				"practice_one_effective_session_per_context_plan" ||
				postgresError.ConstraintName ==
					"practice_one_effective_session_per_agent_thread" {
				return persistence.ErrActiveSessionConflict
			}
			return persistence.ErrConflict
		case "23514", "55000":
			return persistence.ErrConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ persistence.ContextRepository = (*Repository)(nil)
