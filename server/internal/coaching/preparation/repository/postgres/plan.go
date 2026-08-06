package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const selectPracticePlanColumns = `
	plan.plan_id,
	plan.owner_user_id::text,
	COALESCE(plan.source_thread_id::text, ''),
	COALESCE(revision.goal_id::text, ''),
	COALESCE(revision.goal_version, 0),
	revision.goal_snapshot,
	revision.preparation_snapshot_id,
	revision.preparation_snapshot,
	revision.scene_id,
	revision.scene_version,
		revision.scene_selection,
		revision.session_policy,
		revision.practice_objectives,
		revision.ielts_assignment,
		revision.revision,
	plan.status,
	plan.created_at,
	plan.updated_at`

// PostgresPlanRepository stores Plan identities, immutable revisions, and
// exact idempotency responses in Preparation's PostgreSQL authority.
type PostgresPlanRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPlanRepository binds the Plan persistence Port to one pool.
func NewPostgresPlanRepository(pool *pgxpool.Pool) *PostgresPlanRepository {
	return &PostgresPlanRepository{pool: pool}
}

func (r *PostgresPlanRepository) ReplayPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	intent IdempotencyIntent,
) (PracticePlan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) || !validPlanReplayIntent(intent) {
		return PracticePlan{}, false, ErrPlanInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PracticePlan{}, false, planDatabaseFailure("begin Plan replay")
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActivePlanActor(ctx, tx, actor.UserID); err != nil {
		return PracticePlan{}, false, err
	}
	plan, found, err := replayPracticePlan(
		ctx,
		tx,
		actor,
		intent,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PracticePlan{}, false, planDatabaseFailure("commit Plan replay")
	}
	return plan, found, nil
}

func (r *PostgresPlanRepository) CreatePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreatePlanCommand,
) (PracticePlan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		!validCreatePlanCommand(actor, command) {
		return PracticePlan{}, false, ErrPlanInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PracticePlan{}, false, planDatabaseFailure("begin Plan creation")
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActivePlanActor(ctx, tx, actor.UserID); err != nil {
		return PracticePlan{}, false, err
	}
	if err := lockPreparationIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return PracticePlan{}, false, planPersistenceError(err)
	}
	replayed, found, err := replayPracticePlan(
		ctx,
		tx,
		actor,
		command.Intent,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return PracticePlan{}, false,
				planDatabaseFailure("commit Plan creation replay")
		}
		return replayed, true, nil
	}
	storedSelection, err := canonicalStoredSceneSelection(
		command.SceneSelection,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}

	plan := PracticePlan{
		ID:                  command.PlanID,
		UserID:              actor.UserID,
		SourceThreadID:      command.SourceThreadID,
		GoalSnapshot:        cloneGoalSnapshot(command.GoalSnapshot),
		PreparationSnapshot: clonePlanPreparationSnapshot(command.PreparationSnapshot),
		SceneSelection:      storedSelection,
		SessionPolicy:       command.SessionPolicy,
		PracticeObjectives:  clonePlanObjectives(command.PracticeObjectives),
		IELTSAssignment:     cloneIELTSAssignment(command.IELTSAssignment),
		Revision:            1,
		Status:              PlanStatusReady,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_practice_plans (
			owner_user_id,
			plan_id,
			current_revision,
			status,
			source_thread_id
		)
		VALUES ($1, $2, 1, 'ready', $3)
		RETURNING created_at, updated_at
	`,
		actor.UserID,
		plan.ID,
		nullablePreparationText(plan.SourceThreadID),
	).Scan(&plan.CreatedAt, &plan.UpdatedAt)
	if err != nil {
		return PracticePlan{}, false, classifyPlanWriteError(err)
	}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	if err := insertPlanRevision(ctx, tx, plan); err != nil {
		return PracticePlan{}, false, err
	}
	if err := persistPlanIdempotency(
		ctx,
		tx,
		command.Intent,
		plan,
		httpStatusForPlanIntent(command.Intent),
	); err != nil {
		return PracticePlan{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PracticePlan{}, false, planDatabaseFailure("commit Plan creation")
	}
	return clonePracticePlan(plan), false, nil
}

func (r *PostgresPlanRepository) ReadCurrentPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) (PracticePlan, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) || !validPlanResourceID(planID) {
		return PracticePlan{}, ErrPlanNotFound
	}
	plan, err := scanStoredPracticePlan(r.pool.QueryRow(ctx, `
		SELECT `+selectPracticePlanColumns+`
		FROM preparation_practice_plans AS plan
		JOIN preparation_practice_plan_revisions AS revision
		  ON revision.owner_user_id = plan.owner_user_id
		 AND revision.plan_id = plan.plan_id
		 AND revision.revision = plan.current_revision
		JOIN identity_users AS owner
		  ON owner.id = plan.owner_user_id
		LEFT JOIN preparation_deletion_fences AS fence
		  ON fence.owner_user_id = plan.owner_user_id
		WHERE plan.owner_user_id = $1
		  AND plan.plan_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, planID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PracticePlan{}, ErrPlanNotFound
	}
	if err != nil {
		return PracticePlan{}, planPersistenceError(err)
	}
	if !validReturnedPlan(plan, actor, planID) {
		return PracticePlan{}, planDatabaseFailure("validate current Plan")
	}
	return plan, nil
}

func (r *PostgresPlanRepository) RevisePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	command RevisePlanCommand,
) (PracticePlan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) ||
		!validRevisePlanCommand(command) {
		return PracticePlan{}, false, ErrPlanInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PracticePlan{}, false, planDatabaseFailure("begin Plan revision")
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActivePlanActor(ctx, tx, actor.UserID); err != nil {
		return PracticePlan{}, false, err
	}
	if err := lockPreparationIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return PracticePlan{}, false, planPersistenceError(err)
	}
	replayed, found, err := replayPracticePlan(
		ctx,
		tx,
		actor,
		command.Intent,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return PracticePlan{}, false,
				planDatabaseFailure("commit Plan revision replay")
		}
		return replayed, true, nil
	}

	current, err := scanStoredPracticePlan(tx.QueryRow(ctx, `
		SELECT `+selectPracticePlanColumns+`
		FROM preparation_practice_plans AS plan
		JOIN preparation_practice_plan_revisions AS revision
		  ON revision.owner_user_id = plan.owner_user_id
		 AND revision.plan_id = plan.plan_id
		 AND revision.revision = plan.current_revision
		WHERE plan.owner_user_id = $1
		  AND plan.plan_id = $2
		FOR UPDATE OF plan
	`, actor.UserID, command.PlanID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PracticePlan{}, false, ErrPlanNotFound
	}
	if err != nil {
		return PracticePlan{}, false, planPersistenceError(err)
	}
	if !validReturnedPlan(current, actor, command.PlanID) {
		return PracticePlan{}, false, planDatabaseFailure("validate locked Plan")
	}
	if current.Status != PlanStatusReady ||
		current.Revision != command.ExpectedPlanRevision {
		return PracticePlan{}, false, ErrPlanConflict
	}
	if !sameFrozenScene(
		current.SceneSelection,
		command.SceneSelection,
	) || !reflect.DeepEqual(
		current.IELTSAssignment,
		command.IELTSAssignment,
	) {
		return PracticePlan{}, false, ErrPlanConflict
	}

	storedSelection, err := canonicalStoredSceneSelection(
		command.SceneSelection,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	revised := current
	revised.SceneSelection = storedSelection
	revised.SessionPolicy = command.SessionPolicy
	revised.PracticeObjectives = clonePlanObjectives(command.PracticeObjectives)
	revised.IELTSAssignment = cloneIELTSAssignment(command.IELTSAssignment)
	revised.Revision++
	if err := insertPlanRevision(ctx, tx, revised); err != nil {
		return PracticePlan{}, false, err
	}
	err = tx.QueryRow(ctx, `
		UPDATE preparation_practice_plans
		SET current_revision = $3,
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND plan_id = $2
		  AND current_revision = $4
		  AND status = 'ready'
		RETURNING updated_at
	`,
		actor.UserID,
		revised.ID,
		revised.Revision,
		command.ExpectedPlanRevision,
	).Scan(&revised.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PracticePlan{}, false, ErrPlanConflict
	}
	if err != nil {
		return PracticePlan{}, false, classifyPlanWriteError(err)
	}
	revised.UpdatedAt = revised.UpdatedAt.UTC()
	if err := persistPlanIdempotency(
		ctx,
		tx,
		command.Intent,
		revised,
		httpStatusForPlanIntent(command.Intent),
	); err != nil {
		return PracticePlan{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PracticePlan{}, false, planDatabaseFailure("commit Plan revision")
	}
	return clonePracticePlan(revised), false, nil
}

func (r *PostgresPlanRepository) ReadExecutablePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	exactRevision int,
) (PracticePlan, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validPreparationActor(actor) || !validPlanResourceID(planID) ||
		exactRevision < 1 {
		return PracticePlan{}, ErrPlanNotFound
	}
	plan, err := scanStoredPracticePlan(r.pool.QueryRow(ctx, `
		SELECT `+selectPracticePlanColumns+`
		FROM preparation_practice_plans AS plan
		JOIN preparation_practice_plan_revisions AS revision
		  ON revision.owner_user_id = plan.owner_user_id
		 AND revision.plan_id = plan.plan_id
		 AND revision.revision = plan.current_revision
		JOIN identity_users AS owner
		  ON owner.id = plan.owner_user_id
		LEFT JOIN preparation_deletion_fences AS fence
		  ON fence.owner_user_id = plan.owner_user_id
		WHERE plan.owner_user_id = $1
		  AND plan.plan_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		  AND plan.current_revision = $3
		  AND plan.status = 'ready'
	`, actor.UserID, planID, exactRevision))
	if err == nil {
		if !validReturnedPlan(plan, actor, planID) ||
			plan.Revision != exactRevision || plan.Status != PlanStatusReady {
			return PracticePlan{}, planDatabaseFailure("validate executable Plan")
		}
		return plan, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PracticePlan{}, planPersistenceError(err)
	}

	var currentRevision int
	var status PlanStatus
	err = r.pool.QueryRow(ctx, `
		SELECT plan.current_revision, plan.status
		FROM preparation_practice_plans AS plan
		JOIN identity_users AS owner
		  ON owner.id = plan.owner_user_id
		LEFT JOIN preparation_deletion_fences AS fence
		  ON fence.owner_user_id = plan.owner_user_id
		WHERE plan.owner_user_id = $1
		  AND plan.plan_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, actor.UserID, planID).Scan(&currentRevision, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PracticePlan{}, ErrPlanNotFound
	}
	if err != nil {
		return PracticePlan{}, planPersistenceError(err)
	}
	return PracticePlan{}, ErrPlanConflict
}

func insertPlanRevision(
	ctx context.Context,
	tx pgx.Tx,
	plan PracticePlan,
) error {
	goalDocument, preparationDocument, selectionDocument, policyDocument,
		objectivesDocument, ieltsDocument, err := encodePlanRevision(plan)
	if err != nil {
		return err
	}
	var goalID any
	var goalVersion any
	if plan.GoalSnapshot != nil {
		goalID = plan.GoalSnapshot.ID
		goalVersion = plan.GoalSnapshot.Version
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO preparation_practice_plan_revisions (
			owner_user_id,
			plan_id,
			revision,
			goal_id,
			goal_version,
			goal_snapshot,
			preparation_snapshot_id,
			preparation_snapshot,
			scene_id,
			scene_version,
			scene_selection,
			session_policy,
			practice_objectives,
			ielts_assignment
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14
		)
	`,
		plan.UserID,
		plan.ID,
		plan.Revision,
		goalID,
		goalVersion,
		goalDocument,
		plan.PreparationSnapshot.ID,
		preparationDocument,
		plan.SceneSelection.Scene.ID,
		plan.SceneSelection.Scene.Version,
		selectionDocument,
		policyDocument,
		objectivesDocument,
		ieltsDocument,
	)
	if err != nil {
		return classifyPlanWriteError(err)
	}
	return nil
}

func encodePlanRevision(
	plan PracticePlan,
) (goalDocument any, preparationDocument, selectionDocument,
	policyDocument, objectivesDocument []byte, ieltsDocument any, err error) {
	if plan.GoalSnapshot != nil {
		encoded, encodeErr := json.Marshal(plan.GoalSnapshot)
		if encodeErr != nil {
			return nil, nil, nil, nil, nil, nil,
				planDatabaseFailure("encode Goal snapshot")
		}
		goalDocument = encoded
	}
	preparationDocument, err = json.Marshal(plan.PreparationSnapshot)
	if err != nil {
		return nil, nil, nil, nil, nil, nil,
			planDatabaseFailure("encode Preparation snapshot")
	}
	selectionDocument, err = json.Marshal(plan.SceneSelection)
	if err != nil {
		return nil, nil, nil, nil, nil, nil,
			planDatabaseFailure("encode Scene selection")
	}
	policyDocument, err = json.Marshal(plan.SessionPolicy)
	if err != nil {
		return nil, nil, nil, nil, nil, nil,
			planDatabaseFailure("encode Session policy")
	}
	objectivesDocument, err = json.Marshal(plan.PracticeObjectives)
	if err != nil {
		return nil, nil, nil, nil, nil, nil,
			planDatabaseFailure("encode Practice objectives")
	}
	if plan.IELTSAssignment != nil {
		encoded, encodeErr := json.Marshal(plan.IELTSAssignment)
		if encodeErr != nil {
			return nil, nil, nil, nil, nil, nil,
				planDatabaseFailure("encode IELTS assignment")
		}
		ieltsDocument = encoded
	}
	return goalDocument, preparationDocument, selectionDocument,
		policyDocument, objectivesDocument, ieltsDocument, nil
}

func scanStoredPracticePlan(row preparationRowScanner) (PracticePlan, error) {
	var plan PracticePlan
	var goalID string
	var goalVersion int64
	var sceneID string
	var sceneVersion int
	var goalDocument, preparationDocument, selectionDocument,
		policyDocument, objectivesDocument, ieltsDocument []byte
	err := row.Scan(
		&plan.ID,
		&plan.UserID,
		&plan.SourceThreadID,
		&goalID,
		&goalVersion,
		&goalDocument,
		&plan.PreparationSnapshot.ID,
		&preparationDocument,
		&sceneID,
		&sceneVersion,
		&selectionDocument,
		&policyDocument,
		&objectivesDocument,
		&ieltsDocument,
		&plan.Revision,
		&plan.Status,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)
	if err != nil {
		return PracticePlan{}, err
	}
	if (goalID == "") != (goalVersion == 0) ||
		(goalID == "") != (len(goalDocument) == 0) {
		return PracticePlan{}, planDatabaseFailure("decode Goal snapshot columns")
	}
	if goalID != "" {
		var snapshot GoalSnapshot
		if err := decodeStrictPlanJSON(goalDocument, &snapshot); err != nil ||
			snapshot.ID != goalID || snapshot.Version != goalVersion {
			return PracticePlan{}, planDatabaseFailure("decode Goal snapshot")
		}
		plan.GoalSnapshot = &snapshot
	}
	var preparation Snapshot
	if err := decodeStrictPlanJSON(
		preparationDocument,
		&preparation,
	); err != nil || preparation.ID != plan.PreparationSnapshot.ID {
		return PracticePlan{}, planDatabaseFailure("decode Preparation snapshot")
	}
	plan.PreparationSnapshot = preparation
	var selection scene.SelectionSnapshot
	if err := decodeStrictPlanJSON(selectionDocument, &selection); err != nil {
		return PracticePlan{}, planDatabaseFailure("decode Scene selection")
	}
	plan.SceneSelection = selection
	if plan.SceneSelection.Scene.ID != sceneID ||
		plan.SceneSelection.Scene.Version != sceneVersion {
		return PracticePlan{}, planDatabaseFailure("validate Scene selection columns")
	}
	var policy SessionPolicy
	if err := decodeStrictPlanJSON(policyDocument, &policy); err != nil {
		return PracticePlan{}, planDatabaseFailure("decode Session policy")
	}
	plan.SessionPolicy = policy
	if err := decodeStrictPlanJSON(
		objectivesDocument,
		&plan.PracticeObjectives,
	); err != nil {
		return PracticePlan{}, planDatabaseFailure("decode Practice objectives")
	}
	if len(ieltsDocument) > 0 {
		var assignment IELTSAssignmentSnapshot
		if err := decodeStrictPlanJSON(
			ieltsDocument,
			&assignment,
		); err != nil {
			return PracticePlan{}, planDatabaseFailure("decode IELTS assignment")
		}
		plan.IELTSAssignment = &assignment
	}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	return plan, nil
}

func decodeStrictPlanJSON(document []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("preparation: multiple JSON values")
		}
		return err
	}
	return nil
}

func canonicalStoredSceneSelection(
	selection scene.SelectionSnapshot,
) (scene.SelectionSnapshot, error) {
	document, err := json.Marshal(selection)
	if err != nil {
		return scene.SelectionSnapshot{},
			planDatabaseFailure("encode canonical Scene selection")
	}
	var stored scene.SelectionSnapshot
	if err := decodeStrictPlanJSON(document, &stored); err != nil {
		return scene.SelectionSnapshot{},
			planDatabaseFailure("decode canonical Scene selection")
	}
	return stored, nil
}

func replayPracticePlan(
	ctx context.Context,
	query preparationQueryRow,
	actor requestcontext.Actor,
	intent IdempotencyIntent,
) (PracticePlan, bool, error) {
	var fingerprint []byte
	var resourceKind, resourceID string
	var resourceRevision, status int
	var body []byte
	err := query.QueryRow(ctx, `
		SELECT
			payload_fingerprint,
			resource_kind,
			resource_id,
			resource_revision,
			response_status,
			response_body
		FROM preparation_idempotency_records
		WHERE owner_user_id = $1
		  AND method = $2
		  AND canonical_path = $3
		  AND idempotency_key = $4
	`,
		actor.UserID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
	).Scan(
		&fingerprint,
		&resourceKind,
		&resourceID,
		&resourceRevision,
		&status,
		&body,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PracticePlan{}, false, nil
	}
	if err != nil {
		return PracticePlan{}, false, planPersistenceError(err)
	}
	if !bytes.Equal(fingerprint, intent.PayloadFingerprint[:]) ||
		resourceKind != "plan" {
		return PracticePlan{}, false, ErrPlanIdempotencyConflict
	}
	if status != httpStatusForPlanIntent(intent) || resourceRevision < 1 ||
		!validPlanResourceID(resourceID) {
		return PracticePlan{}, false,
			planDatabaseFailure("validate Plan idempotency replay")
	}
	var plan PracticePlan
	if err := decodeStrictPlanJSON(body, &plan); err != nil ||
		plan.ID != resourceID || plan.Revision != resourceRevision ||
		!validReturnedPlan(plan, actor, resourceID) {
		return PracticePlan{}, false,
			planDatabaseFailure("decode Plan idempotency replay")
	}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	return plan, true, nil
}

func persistPlanIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	intent IdempotencyIntent,
	plan PracticePlan,
	responseStatus int,
) error {
	body, err := json.Marshal(plan)
	if err != nil {
		return planDatabaseFailure("encode Plan idempotency response")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO preparation_idempotency_records (
			owner_user_id,
			method,
			canonical_path,
			idempotency_key,
			payload_fingerprint,
			resource_kind,
			resource_id,
			resource_revision,
			response_status,
			response_body
		)
		VALUES ($1, $2, $3, $4, $5, 'plan', $6, $7, $8, $9)
	`,
		plan.UserID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
		intent.PayloadFingerprint[:],
		plan.ID,
		plan.Revision,
		responseStatus,
		body,
	)
	if err != nil {
		return classifyPlanWriteError(err)
	}
	return nil
}

func validCreatePlanCommand(
	actor requestcontext.Actor,
	command CreatePlanCommand,
) bool {
	if !validPlanResourceID(command.PlanID) ||
		(command.SourceThreadID != "" &&
			!validPreparationUUID(command.SourceThreadID)) ||
		!validPlanIntentForResource(
			command.Intent,
			"POST",
			"/v1/practice-plans",
		) {
		return false
	}
	if command.GoalSnapshot != nil &&
		(!validPreparationUUID(command.GoalSnapshot.ID) ||
			command.GoalSnapshot.Version < 1 ||
			!validPlanText(command.GoalSnapshot.Title)) {
		return false
	}
	plan := PracticePlan{
		ID:                  command.PlanID,
		UserID:              actor.UserID,
		SourceThreadID:      command.SourceThreadID,
		GoalSnapshot:        command.GoalSnapshot,
		PreparationSnapshot: command.PreparationSnapshot,
		SceneSelection:      command.SceneSelection,
		SessionPolicy:       command.SessionPolicy,
		PracticeObjectives:  command.PracticeObjectives,
		IELTSAssignment:     command.IELTSAssignment,
		Revision:            1,
		Status:              PlanStatusReady,
		CreatedAt:           planValidationTime,
		UpdatedAt:           planValidationTime,
	}
	return validReturnedPlan(plan, actor, command.PlanID)
}

func validRevisePlanCommand(command RevisePlanCommand) bool {
	if !validPlanResourceID(command.PlanID) ||
		command.ExpectedPlanRevision < 1 ||
		!validPlanIntentForResource(
			command.Intent,
			"PUT",
			"/v1/practice-plans/"+command.PlanID,
		) {
		return false
	}
	selection := command.SceneSelection
	roles, err := selection.SelectedRoles()
	if err != nil || len(roles) == 0 ||
		!validUniquePlanIDs(selection.SelectedRoleIDs) {
		return false
	}
	option, err := selection.PracticeOption()
	return err == nil && validSelectedPlanOption(selection, roles, option) &&
		validStoredSessionPolicy(command.SessionPolicy) &&
		validPracticeObjectives(command.PracticeObjectives) &&
		validPlanIELTSAssignment(selection, command.IELTSAssignment)
}

func validPlanReplayIntent(intent IdempotencyIntent) bool {
	switch intent.Method {
	case "POST":
		return validPlanIntentForResource(
			intent,
			"POST",
			"/v1/practice-plans",
		)
	case "PUT":
		const prefix = "/v1/practice-plans/"
		return strings.HasPrefix(intent.CanonicalPath, prefix) &&
			validPlanResourceID(strings.TrimPrefix(intent.CanonicalPath, prefix)) &&
			validPlanIntentForResource(
				intent,
				"PUT",
				intent.CanonicalPath,
			)
	default:
		return false
	}
}

func validPlanIntentForResource(
	intent IdempotencyIntent,
	method string,
	path string,
) bool {
	var zeroFingerprint [32]byte
	return intent.Method == method && intent.CanonicalPath == path &&
		validCanonicalPath(path) && validIdempotencyKey(intent.Key) &&
		intent.PayloadFingerprint != zeroFingerprint
}

func sameFrozenScene(left, right scene.SelectionSnapshot) bool {
	return reflect.DeepEqual(left.Scene, right.Scene)
}

func httpStatusForPlanIntent(intent IdempotencyIntent) int {
	if intent.Method == "PUT" {
		return 200
	}
	return 201
}

func lockActivePlanActor(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
) error {
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT true
		FROM identity_users AS owner
		WHERE owner.id = $1
		  AND owner.account_status = 'active'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM preparation_deletion_fences AS fence
		      WHERE fence.owner_user_id = owner.id
		  )
		FOR SHARE OF owner
	`, userID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPlanNotFound
	}
	if err != nil {
		return planPersistenceError(err)
	}
	return nil
}

func classifyPlanWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrPlanNotFound
		case "23505", "23514":
			return ErrPlanConflict
		}
	}
	return planPersistenceError(err)
}

func planPersistenceError(err error) error {
	if errors.Is(err, ErrPlanInvalid) || errors.Is(err, ErrPlanNotFound) ||
		errors.Is(err, ErrPlanConflict) ||
		errors.Is(err, ErrPlanIdempotencyConflict) ||
		errors.Is(err, ErrPlanRepository) {
		return err
	}
	return planDatabaseFailure("persist Plan data")
}

func planDatabaseFailure(operation string) error {
	return fmt.Errorf("%w: %s", ErrPlanRepository, operation)
}

var planValidationTime = time.Unix(1, 0).UTC()

var _ PlanRepository = (*PostgresPlanRepository)(nil)
