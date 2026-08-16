package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const planColumns = `plan_id, user_id::text, COALESCE(source_thread_id::text, ''),
preparation_snapshot, scene_selection, session_policy, practice_objectives,
ielts_assignment, version, status, created_at, updated_at`

type PostgresPlanRepository struct{ pool *pgxpool.Pool }

func NewPostgresPlanRepository(pool *pgxpool.Pool) *PostgresPlanRepository {
	return &PostgresPlanRepository{pool: pool}
}

func (r *PostgresPlanRepository) CreatePlan(ctx context.Context, actor requestcontext.Actor, command preparation.CreatePlanCommand) (preparation.PracticePlan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!validAggregateID(command.PlanID) || !validIdempotencyKey(command.ClientRequestID) ||
		(command.Status != preparation.PlanStatusDraft && command.Status != preparation.PlanStatusReady) {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	preparationJSON, err := json.Marshal(command.PreparationSnapshot)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	selectionJSON, err := json.Marshal(command.SceneSelection)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	policyJSON, err := json.Marshal(command.SessionPolicy)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	objectivesJSON, err := json.Marshal(command.PracticeObjectives)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	assignmentJSON, err := nullableJSON(command.IELTSAssignment)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return preparation.PracticePlan{}, false, planDB(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		if errors.Is(err, errInactiveActor) {
			return preparation.PracticePlan{}, false, preparation.ErrPlanNotFound
		}
		return preparation.PracticePlan{}, false, planDB(err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO practice_plans (
user_id, plan_id, source_thread_id, preparation_snapshot, scene_selection,
session_policy, practice_objectives, ielts_assignment, practice_experience,
status, version, initial_client_request_id, initial_request_fingerprint)
VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,1,$11,$12)
ON CONFLICT (user_id, initial_client_request_id) DO NOTHING`, actor.UserID,
		command.PlanID, command.SourceThreadID, preparationJSON, selectionJSON,
		policyJSON, objectivesJSON, assignmentJSON,
		string(command.SceneSelection.Scene.Experience), string(command.Status),
		command.ClientRequestID, command.RequestFingerprint[:])
	if err != nil {
		return preparation.PracticePlan{}, false, classifyPlanError(err)
	}
	if tag.RowsAffected() == 0 {
		var existingID string
		var fingerprint []byte
		err = tx.QueryRow(ctx, `SELECT plan_id, initial_request_fingerprint FROM practice_plans WHERE user_id=$1 AND initial_client_request_id=$2`, actor.UserID, command.ClientRequestID).Scan(&existingID, &fingerprint)
		if err != nil {
			return preparation.PracticePlan{}, false, planDB(err)
		}
		if !bytes.Equal(fingerprint, command.RequestFingerprint[:]) {
			return preparation.PracticePlan{}, false, preparation.ErrPlanIdempotencyConflict
		}
		plan, err := readPlan(ctx, tx, actor.UserID, existingID, "")
		if err != nil {
			return preparation.PracticePlan{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return preparation.PracticePlan{}, false, planDB(err)
		}
		return plan, true, nil
	}
	plan, err := readPlan(ctx, tx, actor.UserID, command.PlanID, "")
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return preparation.PracticePlan{}, false, planDB(err)
	}
	return plan, false, nil
}

func (r *PostgresPlanRepository) ReadCurrentPlan(ctx context.Context, actor requestcontext.Actor, planID string) (preparation.PracticePlan, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() || !validAggregateID(planID) {
		return preparation.PracticePlan{}, preparation.ErrPlanNotFound
	}
	return readPlan(ctx, r.pool, actor.UserID, planID, "")
}

func (r *PostgresPlanRepository) ListCurrentPlans(ctx context.Context, actor requestcontext.Actor, experience scene.PracticeExperience) ([]preparation.PracticePlan, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() {
		return nil, preparation.ErrPlanInvalid
	}
	rows, err := r.pool.Query(ctx, `SELECT `+planColumns+` FROM practice_plans WHERE user_id=$1 AND practice_experience=$2 ORDER BY created_at DESC, plan_id DESC`, actor.UserID, string(experience))
	if err != nil {
		return nil, planDB(err)
	}
	defer rows.Close()
	plans := make([]preparation.PracticePlan, 0)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, planDB(err)
	}
	return plans, nil
}

func (r *PostgresPlanRepository) ConfirmPlan(ctx context.Context, actor requestcontext.Actor, command preparation.ConfirmPlanCommand) (preparation.PracticePlan, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() || !validAggregateID(command.PlanID) || command.ExpectedVersion < 1 || !validIdempotencyKey(command.ClientRequestID) {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return preparation.PracticePlan{}, false, planDB(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		if errors.Is(err, errInactiveActor) {
			return preparation.PracticePlan{}, false, preparation.ErrPlanNotFound
		}
		return preparation.PracticePlan{}, false, planDB(err)
	}
	var lastID string
	var lastFingerprint []byte
	err = tx.QueryRow(ctx, `SELECT COALESCE(last_client_request_id,''), COALESCE(last_request_fingerprint,'') FROM practice_plans WHERE user_id=$1 AND plan_id=$2 FOR UPDATE`, actor.UserID, command.PlanID).Scan(&lastID, &lastFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return preparation.PracticePlan{}, false, preparation.ErrPlanNotFound
	}
	if err != nil {
		return preparation.PracticePlan{}, false, planDB(err)
	}
	if lastID == command.ClientRequestID {
		if !bytes.Equal(lastFingerprint, command.RequestFingerprint[:]) {
			return preparation.PracticePlan{}, false, preparation.ErrPlanIdempotencyConflict
		}
		plan, err := readPlan(ctx, tx, actor.UserID, command.PlanID, "")
		if err != nil {
			return preparation.PracticePlan{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return preparation.PracticePlan{}, false, planDB(err)
		}
		return plan, true, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE practice_plans SET status='ready', version=version+1, last_client_request_id=$4, last_request_fingerprint=$5, updated_at=transaction_timestamp() WHERE user_id=$1 AND plan_id=$2 AND version=$3 AND status='draft'`, actor.UserID, command.PlanID, command.ExpectedVersion, command.ClientRequestID, command.RequestFingerprint[:])
	if err != nil {
		return preparation.PracticePlan{}, false, classifyPlanError(err)
	}
	if tag.RowsAffected() != 1 {
		return preparation.PracticePlan{}, false, preparation.ErrPlanConflict
	}
	plan, err := readPlan(ctx, tx, actor.UserID, command.PlanID, "")
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return preparation.PracticePlan{}, false, planDB(err)
	}
	return plan, false, nil
}

func (r *PostgresPlanRepository) ReadExecutablePlan(ctx context.Context, actor requestcontext.Actor, planID string, version int) (preparation.PracticePlan, error) {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() || !validAggregateID(planID) || version < 1 {
		return preparation.PracticePlan{}, preparation.ErrPlanNotFound
	}
	return readPlan(ctx, r.pool, actor.UserID, planID, ` AND version=$3 AND status='ready'`, version)
}

type planRow interface{ Scan(...any) error }
type planQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readPlan(ctx context.Context, query planQuery, ownerID, planID, suffix string, extra ...any) (preparation.PracticePlan, error) {
	args := []any{ownerID, planID}
	args = append(args, extra...)
	plan, err := scanPlan(query.QueryRow(ctx, `SELECT `+planColumns+` FROM practice_plans WHERE user_id=$1 AND plan_id=$2`+suffix, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return preparation.PracticePlan{}, preparation.ErrPlanNotFound
	}
	return plan, err
}

func scanPlan(row planRow) (preparation.PracticePlan, error) {
	var plan preparation.PracticePlan
	var preparationJSON, selectionJSON, policyJSON, objectivesJSON []byte
	var assignmentJSON []byte
	err := row.Scan(&plan.ID, &plan.UserID, &plan.SourceThreadID, &preparationJSON, &selectionJSON, &policyJSON, &objectivesJSON, &assignmentJSON, &plan.Version, &plan.Status, &plan.CreatedAt, &plan.UpdatedAt)
	if err != nil {
		return preparation.PracticePlan{}, err
	}
	if decodeStrict(preparationJSON, &plan.PreparationSnapshot) != nil || decodeStrict(selectionJSON, &plan.SceneSelection) != nil || decodeStrict(policyJSON, &plan.SessionPolicy) != nil || decodeStrict(objectivesJSON, &plan.PracticeObjectives) != nil {
		return preparation.PracticePlan{}, preparation.ErrPlanRepository
	}
	if len(assignmentJSON) > 0 && string(assignmentJSON) != "null" {
		var assignment preparation.IELTSAssignmentSnapshot
		if decodeStrict(assignmentJSON, &assignment) != nil {
			return preparation.PracticePlan{}, preparation.ErrPlanRepository
		}
		plan.IELTSAssignment = &assignment
	}
	plan.CreatedAt, plan.UpdatedAt = plan.CreatedAt.UTC(), plan.UpdatedAt.UTC()
	return plan, nil
}

func nullableJSON[T any](value *T) (any, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func classifyPlanError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503" || pgErr.Code == "23514") {
		return preparation.ErrPlanConflict
	}
	return planDB(err)
}

func planDB(err error) error { return fmt.Errorf("%w: %v", preparation.ErrPlanRepository, err) }

var _ preparation.PlanRepository = (*PostgresPlanRepository)(nil)
