package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const interviewPreparationColumns = `interview_preparation_id, user_id::text,
input, candidate, resume_content, status, version, created_at, updated_at`

type PostgresInterviewPreparationRepository struct{ pool *pgxpool.Pool }

func NewPostgresInterviewPreparationRepository(pool *pgxpool.Pool) *PostgresInterviewPreparationRepository {
	return &PostgresInterviewPreparationRepository{pool: pool}
}

func (r *PostgresInterviewPreparationRepository) ReplayCreate(ctx context.Context, actor requestcontext.Actor, clientRequestID string, fingerprint [sha256.Size]byte) (preparation.InterviewPreparation, bool, error) {
	if !validInterviewRepository(r, ctx, actor) || !validIdempotencyKey(clientRequestID) {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationInvalid
	}
	var id string
	var stored []byte
	err := r.pool.QueryRow(ctx, `SELECT interview_preparation_id, initial_request_fingerprint FROM interview_preparations WHERE user_id=$1 AND initial_client_request_id=$2`, actor.UserID, clientRequestID).Scan(&id, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return preparation.InterviewPreparation{}, false, nil
	}
	if err != nil {
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	if !bytes.Equal(stored, fingerprint[:]) {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationRequestReuse
	}
	value, err := readInterviewPreparation(ctx, r.pool, actor.UserID, id)
	return value, err == nil, err
}

func (r *PostgresInterviewPreparationRepository) Create(ctx context.Context, actor requestcontext.Actor, command preparation.CreateInterviewPreparationCommand) (preparation.InterviewPreparation, bool, error) {
	if !validInterviewRepository(r, ctx, actor) || !validAggregateID(command.ID) || !validIdempotencyKey(command.ClientRequestID) || !validJobTargetInput(command.Input) || !validJobTargetCandidateShape(command.Candidate, command.Input.Source) ||
		(command.ResumeContent != nil && !preparation.ValidResumeMaterial(*command.ResumeContent)) {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationInvalid
	}
	inputJSON, _ := json.Marshal(command.Input)
	candidateJSON, _ := json.Marshal(command.Candidate)
	resumeJSON, err := nullableJSON(command.ResumeContent)
	if err != nil {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		if errors.Is(err, errInactiveActor) {
			return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationNotFound
		}
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO interview_preparations (
user_id, interview_preparation_id, input, candidate, resume_content,
status, version, initial_client_request_id, initial_request_fingerprint)
VALUES ($1,$2,$3,$4,$5,'draft',1,$6,$7)
ON CONFLICT (user_id, initial_client_request_id) DO NOTHING`, actor.UserID,
		command.ID, inputJSON, candidateJSON, resumeJSON, command.ClientRequestID,
		command.RequestFingerprint[:])
	if err != nil {
		return preparation.InterviewPreparation{}, false, classifyInterviewError(err)
	}
	id := command.ID
	replayed := tag.RowsAffected() == 0
	if replayed {
		var stored []byte
		if err := tx.QueryRow(ctx, `SELECT interview_preparation_id, initial_request_fingerprint FROM interview_preparations WHERE user_id=$1 AND initial_client_request_id=$2`, actor.UserID, command.ClientRequestID).Scan(&id, &stored); err != nil {
			return preparation.InterviewPreparation{}, false, interviewDB(err)
		}
		if !bytes.Equal(stored, command.RequestFingerprint[:]) {
			return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationRequestReuse
		}
	}
	value, err := readInterviewPreparation(ctx, tx, actor.UserID, id)
	if err != nil {
		return preparation.InterviewPreparation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	return value, replayed, nil
}

func (r *PostgresInterviewPreparationRepository) Get(ctx context.Context, actor requestcontext.Actor, id string) (preparation.InterviewPreparation, error) {
	if !validInterviewRepository(r, ctx, actor) || !validAggregateID(id) {
		return preparation.InterviewPreparation{}, preparation.ErrInterviewPreparationNotFound
	}
	return readInterviewPreparation(ctx, r.pool, actor.UserID, id)
}

func (r *PostgresInterviewPreparationRepository) Patch(ctx context.Context, actor requestcontext.Actor, command preparation.PatchInterviewPreparationCommand) (preparation.InterviewPreparation, bool, error) {
	if !validInterviewRepository(r, ctx, actor) || !validAggregateID(command.ID) || command.ExpectedVersion < 1 || !validIdempotencyKey(command.ClientRequestID) {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		if errors.Is(err, errInactiveActor) {
			return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationNotFound
		}
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	var currentVersion int
	var currentStatus preparation.InterviewPreparationStatus
	var lastID string
	var lastFingerprint []byte
	err = tx.QueryRow(ctx, `SELECT version,status,COALESCE(last_client_request_id,''),COALESCE(last_request_fingerprint,'') FROM interview_preparations WHERE user_id=$1 AND interview_preparation_id=$2 FOR UPDATE`, actor.UserID, command.ID).Scan(&currentVersion, &currentStatus, &lastID, &lastFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationNotFound
	}
	if err != nil {
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	if lastID == command.ClientRequestID {
		if !bytes.Equal(lastFingerprint, command.RequestFingerprint[:]) {
			return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationRequestReuse
		}
		value, err := readInterviewPreparation(ctx, tx, actor.UserID, command.ID)
		if err != nil {
			return preparation.InterviewPreparation{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return preparation.InterviewPreparation{}, false, interviewDB(err)
		}
		return value, true, nil
	}
	if currentVersion != command.ExpectedVersion || currentStatus == preparation.InterviewPreparationDiscarded {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationConflict
	}
	var tag pgconn.CommandTag
	switch command.Action {
	case preparation.InterviewPreparationRegenerate:
		if command.Input == nil || command.Candidate == nil || !validJobTargetInput(*command.Input) || !validJobTargetCandidateShape(*command.Candidate, command.Input.Source) {
			return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationInvalid
		}
		inputJSON, _ := json.Marshal(command.Input)
		candidateJSON, _ := json.Marshal(command.Candidate)
		tag, err = tx.Exec(ctx, `UPDATE interview_preparations SET input=$4,candidate=$5,status='draft',version=version+1,last_client_request_id=$6,last_request_fingerprint=$7,updated_at=transaction_timestamp() WHERE user_id=$1 AND interview_preparation_id=$2 AND version=$3`, actor.UserID, command.ID, command.ExpectedVersion, inputJSON, candidateJSON, command.ClientRequestID, command.RequestFingerprint[:])
	case preparation.InterviewPreparationConfirm:
		if command.Candidate == nil || currentStatus != preparation.InterviewPreparationDraft {
			return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationConflict
		}
		candidateJSON, _ := json.Marshal(command.Candidate)
		tag, err = tx.Exec(ctx, `UPDATE interview_preparations SET candidate=$4,status='confirmed',version=version+1,last_client_request_id=$5,last_request_fingerprint=$6,updated_at=transaction_timestamp() WHERE user_id=$1 AND interview_preparation_id=$2 AND version=$3`, actor.UserID, command.ID, command.ExpectedVersion, candidateJSON, command.ClientRequestID, command.RequestFingerprint[:])
	case preparation.InterviewPreparationDiscard:
		tag, err = tx.Exec(ctx, `UPDATE interview_preparations SET status='discarded',version=version+1,last_client_request_id=$4,last_request_fingerprint=$5,updated_at=transaction_timestamp() WHERE user_id=$1 AND interview_preparation_id=$2 AND version=$3`, actor.UserID, command.ID, command.ExpectedVersion, command.ClientRequestID, command.RequestFingerprint[:])
	default:
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationInvalid
	}
	if err != nil {
		return preparation.InterviewPreparation{}, false, classifyInterviewError(err)
	}
	if tag.RowsAffected() != 1 {
		return preparation.InterviewPreparation{}, false, preparation.ErrInterviewPreparationConflict
	}
	value, err := readInterviewPreparation(ctx, tx, actor.UserID, command.ID)
	if err != nil {
		return preparation.InterviewPreparation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return preparation.InterviewPreparation{}, false, interviewDB(err)
	}
	return value, false, nil
}

type interviewQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readInterviewPreparation(ctx context.Context, query interviewQuery, ownerID, id string) (preparation.InterviewPreparation, error) {
	var value preparation.InterviewPreparation
	var inputJSON, candidateJSON, resumeJSON []byte
	err := query.QueryRow(ctx, `SELECT `+interviewPreparationColumns+` FROM interview_preparations WHERE user_id=$1 AND interview_preparation_id=$2`, ownerID, id).Scan(&value.ID, &value.UserID, &inputJSON, &candidateJSON, &resumeJSON, &value.Status, &value.Version, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return preparation.InterviewPreparation{}, preparation.ErrInterviewPreparationNotFound
	}
	if err != nil {
		return preparation.InterviewPreparation{}, interviewDB(err)
	}
	if decodeStrict(inputJSON, &value.Input) != nil || decodeStrict(candidateJSON, &value.Candidate) != nil {
		return preparation.InterviewPreparation{}, preparation.ErrInterviewPreparationConflict
	}
	if len(resumeJSON) > 0 && string(resumeJSON) != "null" {
		var resume preparation.ResumeMaterial
		if decodeStrict(resumeJSON, &resume) != nil {
			return preparation.InterviewPreparation{}, preparation.ErrInterviewPreparationConflict
		}
		value.ResumeContent = &resume
	}
	value.CreatedAt, value.UpdatedAt = value.CreatedAt.UTC(), value.UpdatedAt.UTC()
	return value, nil
}

func validInterviewRepository(r *PostgresInterviewPreparationRepository, ctx context.Context, actor requestcontext.Actor) bool {
	return r != nil && r.pool != nil && ctx != nil && actor.Valid()
}

func classifyInterviewError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503" || pgErr.Code == "23514") {
		return preparation.ErrInterviewPreparationConflict
	}
	return interviewDB(err)
}

func interviewDB(err error) error { return fmt.Errorf("interview preparation repository: %w", err) }

var _ preparation.InterviewPreparationRepository = (*PostgresInterviewPreparationRepository)(nil)
