package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type PostgresJobTargetRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresJobTargetRepository(
	pool *pgxpool.Pool,
) *PostgresJobTargetRepository {
	return &PostgresJobTargetRepository{pool: pool}
}

func (r *PostgresJobTargetRepository) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreateJobTargetCommand,
) (JobTarget, bool, error) {
	if !r.valid() || ctx == nil || !validPreparationActor(actor) ||
		!validResourceIdentifier(command.TargetID) ||
		!validJobTargetInput(command.Request.Input()) ||
		!validJobTargetOperationIntent(
			command.Intent,
			"POST",
			"/v1/job-targets",
			command.Request,
		) {
		return JobTarget{}, false, ErrJobTargetInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"begin target creation",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActiveJobTargetActor(ctx, tx, actor.UserID); err != nil {
		return JobTarget{}, false, err
	}
	if err := lockJobTargetIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return JobTarget{}, false, err
	}
	replayed, found, err := readJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target",
		"",
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return JobTarget{}, false, jobTargetDatabaseFailure(
				"commit target replay",
			)
		}
		return replayed, true, nil
	}

	input := command.Request.Input()
	_, err = tx.Exec(ctx, `
		INSERT INTO preparation_job_targets (
			owner_user_id,
			target_id,
			source_kind,
			job_title,
			job_description,
			company,
			seniority,
			candidate_background,
			practice_focus
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		actor.UserID,
		command.TargetID,
		input.Source,
		nullablePreparationText(input.JobTitle),
		nullablePreparationText(input.JobDescription),
		nullablePreparationText(input.Company),
		nullablePreparationText(input.Seniority),
		nullablePreparationText(input.CandidateBackground),
		nullablePreparationText(input.PracticeFocus),
	)
	if err != nil {
		return JobTarget{}, false, classifyJobTargetWriteError(err)
	}
	target, err := readJobTargetInTransaction(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
		false,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if err := persistJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target",
		target.ID,
		httpStatusCreated,
		target,
	); err != nil {
		return JobTarget{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"commit target creation",
		)
	}
	return target, false, nil
}

func (r *PostgresJobTargetRepository) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
) (JobTarget, error) {
	if !r.valid() || ctx == nil || !validPreparationActor(actor) ||
		!validResourceIdentifier(targetID) {
		return JobTarget{}, ErrJobTargetNotFound
	}
	target, err := scanJobTarget(r.pool.QueryRow(
		ctx,
		jobTargetSelect+`
		WHERE target.owner_user_id = $1
		  AND target.target_id = $2
		  AND target.stage <> 'discarded'
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`,
		actor.UserID,
		targetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return JobTarget{}, ErrJobTargetNotFound
	}
	if err != nil {
		return JobTarget{}, jobTargetDatabaseFailure("read target")
	}
	return target, nil
}

func (r *PostgresJobTargetRepository) Update(
	ctx context.Context,
	actor requestcontext.Actor,
	command UpdateJobTargetCommand,
) (JobTarget, bool, error) {
	expectedPath := "/v1/job-targets/" + command.TargetID
	if !r.valid() || ctx == nil || !validPreparationActor(actor) ||
		!validResourceIdentifier(command.TargetID) ||
		command.Request.ExpectedInputVersion < 1 ||
		!validJobTargetInput(command.Request.Input()) ||
		!validJobTargetOperationIntent(
			command.Intent,
			"PUT",
			expectedPath,
			command.Request,
		) {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"begin target update",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActiveJobTargetActor(ctx, tx, actor.UserID); err != nil {
		return JobTarget{}, false, err
	}
	if err := lockJobTargetIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return JobTarget{}, false, err
	}
	replayed, found, err := readJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target_update",
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return JobTarget{}, false, jobTargetDatabaseFailure(
				"commit target update replay",
			)
		}
		return replayed, true, nil
	}

	current, err := lockJobTarget(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if current.InputVersion != command.Request.ExpectedInputVersion {
		return JobTarget{}, false, ErrJobTargetConflict
	}
	if current.Stage == JobTargetStageDiscarded {
		return JobTarget{}, false, ErrJobTargetNotFound
	}
	nextInput := command.Request.Input()
	if current.Input != nextInput {
		if _, err := tx.Exec(ctx, `
			UPDATE preparation_job_target_analysis_attempts
			SET status = 'failed',
			    stable_error_category = 'input_superseded',
			    finished_at = transaction_timestamp()
			WHERE owner_user_id = $1
			  AND target_id = $2
			  AND status = 'running'
		`, actor.UserID, command.TargetID); err != nil {
			return JobTarget{}, false, jobTargetDatabaseFailure(
				"fence superseded target analysis",
			)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE preparation_job_targets
			SET source_kind = $3,
			    job_title = $4,
			    job_description = $5,
			    company = $6,
			    seniority = $7,
			    candidate_background = $8,
			    practice_focus = $9,
			    input_version = input_version + 1,
			    stage = 'draft',
			    updated_at = transaction_timestamp()
			WHERE owner_user_id = $1
			  AND target_id = $2
			  AND input_version = $10
		`,
			actor.UserID,
			command.TargetID,
			nextInput.Source,
			nullablePreparationText(nextInput.JobTitle),
			nullablePreparationText(nextInput.JobDescription),
			nullablePreparationText(nextInput.Company),
			nullablePreparationText(nextInput.Seniority),
			nullablePreparationText(nextInput.CandidateBackground),
			nullablePreparationText(nextInput.PracticeFocus),
			command.Request.ExpectedInputVersion,
		)
		if err != nil {
			return JobTarget{}, false, classifyJobTargetWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return JobTarget{}, false, ErrJobTargetConflict
		}
	}
	target, err := readJobTargetInTransaction(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
		false,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if err := persistJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target_update",
		target.ID,
		httpStatusOK,
		target,
	); err != nil {
		return JobTarget{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"commit target update",
		)
	}
	return target, false, nil
}

func (r *PostgresJobTargetRepository) ClaimAnalysis(
	ctx context.Context,
	actor requestcontext.Actor,
	command AnalyzeJobTargetCommand,
) (JobTarget, JobTargetAnalysisClaim, bool, bool, error) {
	expectedPath := "/v1/job-targets/" +
		command.TargetID + "/analyses"
	if !r.valid() || ctx == nil || !validPreparationActor(actor) ||
		!validResourceIdentifier(command.TargetID) ||
		command.Request.ExpectedInputVersion < 1 ||
		command.Lease <= 0 ||
		!validJobTargetOperationIntent(
			command.Intent,
			"POST",
			expectedPath,
			command.Request,
		) {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, ErrJobTargetInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, jobTargetDatabaseFailure(
				"begin analysis claim",
			)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActiveJobTargetActor(ctx, tx, actor.UserID); err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, err
	}
	if err := lockJobTargetIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, err
	}
	replayed, found, err := readJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target_analysis",
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return JobTarget{}, JobTargetAnalysisClaim{},
				false, false, jobTargetDatabaseFailure(
					"commit analysis replay",
				)
		}
		return replayed, JobTargetAnalysisClaim{},
			false, true, nil
	}

	current, err := lockJobTarget(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, err
	}
	if current.InputVersion != command.Request.ExpectedInputVersion {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, ErrJobTargetConflict
	}
	if current.Stage == JobTargetStageDiscarded {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, ErrJobTargetNotFound
	}
	if current.Stage == JobTargetStageAwaitingConfirmation ||
		current.Stage == JobTargetStageConfirmed {
		if err := persistJobTargetReplay(
			ctx,
			tx,
			actor.UserID,
			command.Intent,
			"job_target_analysis",
			current.ID,
			httpStatusOK,
			current,
		); err != nil {
			return JobTarget{}, JobTargetAnalysisClaim{},
				false, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return JobTarget{}, JobTargetAnalysisClaim{},
				false, false, jobTargetDatabaseFailure(
					"commit completed analysis recovery",
				)
		}
		return current, JobTargetAnalysisClaim{},
			false, false, nil
	}

	var (
		runningAttemptID string
		leaseUntil       time.Time
		leaseValid       bool
	)
	err = tx.QueryRow(ctx, `
		SELECT
			attempt_id::text,
			lease_until,
			lease_until > transaction_timestamp()
		FROM preparation_job_target_analysis_attempts
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND input_version = $3
		  AND status = 'running'
		ORDER BY attempt_number DESC
		LIMIT 1
		FOR UPDATE
	`,
		actor.UserID,
		command.TargetID,
		current.InputVersion,
	).Scan(&runningAttemptID, &leaseUntil, &leaseValid)
	switch {
	case err == nil && leaseValid:
		current, err = readJobTargetInTransaction(
			ctx,
			tx,
			actor.UserID,
			command.TargetID,
			false,
		)
		if err != nil {
			return JobTarget{}, JobTargetAnalysisClaim{},
				false, false, err
		}
		// A live lease is a transient observation, not the terminal result of
		// this idempotent operation. Persisting this 202 projection would stop
		// the same key from re-evaluating an expired lease after a worker crash.
		if err := tx.Commit(ctx); err != nil {
			return JobTarget{}, JobTargetAnalysisClaim{},
				false, false, jobTargetDatabaseFailure(
					"commit live analysis recovery",
				)
		}
		return current, JobTargetAnalysisClaim{},
			false, false, nil
	case err == nil:
		if _, err := tx.Exec(ctx, `
			UPDATE preparation_job_target_analysis_attempts
			SET status = 'failed',
			    stable_error_category = 'lease_expired',
			    finished_at = transaction_timestamp()
			WHERE attempt_id = $1
			  AND owner_user_id = $2
		`, runningAttemptID, actor.UserID); err != nil {
			return JobTarget{}, JobTargetAnalysisClaim{},
				false, false, jobTargetDatabaseFailure(
					"expire analysis claim",
				)
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, jobTargetDatabaseFailure(
				"read running analysis claim",
			)
	}

	var claim JobTargetAnalysisClaim
	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_job_target_analysis_attempts (
			owner_user_id,
			target_id,
			input_version,
			attempt_number,
			status,
			lease_until
		)
		SELECT
			$1,
			$2,
			$3,
			coalesce(max(attempt_number), 0) + 1,
			'running',
			transaction_timestamp() + make_interval(secs => $4)
		FROM preparation_job_target_analysis_attempts
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND input_version = $3
		RETURNING
			attempt_id::text,
			target_id,
			owner_user_id::text,
			input_version,
			attempt_number,
			worker_token::text,
			lease_until
	`,
		actor.UserID,
		command.TargetID,
		current.InputVersion,
		command.Lease.Seconds(),
	).Scan(
		&claim.AttemptID,
		&claim.TargetID,
		&claim.OwnerUserID,
		&claim.InputVersion,
		&claim.AnalysisVersion,
		&claim.WorkerToken,
		&claim.LeaseUntil,
	)
	if err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, classifyJobTargetWriteError(err)
	}
	claim.Input = current.Input
	claim.Intent = command.Intent
	claim.LeaseUntil = claim.LeaseUntil.UTC()

	if _, err := tx.Exec(ctx, `
		UPDATE preparation_job_targets
		SET stage = 'parsing',
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND input_version = $3
	`,
		actor.UserID,
		command.TargetID,
		current.InputVersion,
	); err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, classifyJobTargetWriteError(err)
	}
	current, err = readJobTargetInTransaction(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
		false,
	)
	if err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobTarget{}, JobTargetAnalysisClaim{},
			false, false, jobTargetDatabaseFailure(
				"commit analysis claim",
			)
	}
	return current, claim, true, false, nil
}

func (r *PostgresJobTargetRepository) CompleteAnalysis(
	ctx context.Context,
	claim JobTargetAnalysisClaim,
	candidate JobTargetCandidate,
) (JobTarget, error) {
	if !r.valid() || ctx == nil || !validJobTargetAnalysisClaim(claim) ||
		!validJobTargetCandidateShape(candidate, claim.Input.Source) {
		return JobTarget{}, ErrJobTargetInvalid
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return JobTarget{}, ErrJobTargetInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return JobTarget{}, jobTargetDatabaseFailure(
			"begin analysis completion",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActiveJobTargetActor(
		ctx,
		tx,
		claim.OwnerUserID,
	); err != nil {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	if err := lockJobTargetIdempotency(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.Intent,
	); err != nil {
		return JobTarget{}, err
	}
	valid, err := lockAndValidateJobTargetClaim(ctx, tx, claim)
	if err != nil {
		return JobTarget{}, err
	}
	if !valid {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	tag, err := tx.Exec(ctx, `
		UPDATE preparation_job_target_analysis_attempts
		SET status = 'succeeded',
		    candidate = $2,
		    finished_at = transaction_timestamp()
		WHERE attempt_id = $1
		  AND owner_user_id = $3
		  AND worker_token = $4
		  AND status = 'running'
	`, claim.AttemptID, encoded, claim.OwnerUserID, claim.WorkerToken)
	if err != nil {
		return JobTarget{}, classifyJobTargetWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	tag, err = tx.Exec(ctx, `
		UPDATE preparation_job_targets
		SET stage = 'awaiting_confirmation',
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND input_version = $3
		  AND stage = 'parsing'
	`, claim.OwnerUserID, claim.TargetID, claim.InputVersion)
	if err != nil {
		return JobTarget{}, classifyJobTargetWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	target, err := readJobTargetInTransaction(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.TargetID,
		false,
	)
	if err != nil {
		return JobTarget{}, err
	}
	if err := persistJobTargetReplay(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.Intent,
		"job_target_analysis",
		claim.TargetID,
		httpStatusOK,
		target,
	); err != nil {
		return JobTarget{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobTarget{}, jobTargetDatabaseFailure(
			"commit analysis completion",
		)
	}
	return target, nil
}

func (r *PostgresJobTargetRepository) FailAnalysis(
	ctx context.Context,
	claim JobTargetAnalysisClaim,
	stableErrorCategory string,
) (JobTarget, error) {
	category := strings.TrimSpace(stableErrorCategory)
	if !r.valid() || ctx == nil || !validJobTargetAnalysisClaim(claim) ||
		!ValidStableJobTargetCategory(category) {
		return JobTarget{}, ErrJobTargetInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return JobTarget{}, jobTargetDatabaseFailure(
			"begin analysis failure",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActiveJobTargetActor(
		ctx,
		tx,
		claim.OwnerUserID,
	); err != nil {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	if err := lockJobTargetIdempotency(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.Intent,
	); err != nil {
		return JobTarget{}, err
	}
	valid, err := lockAndValidateJobTargetClaim(ctx, tx, claim)
	if err != nil {
		return JobTarget{}, err
	}
	if !valid {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	tag, err := tx.Exec(ctx, `
		UPDATE preparation_job_target_analysis_attempts
		SET status = 'failed',
		    stable_error_category = $2,
		    finished_at = transaction_timestamp()
		WHERE attempt_id = $1
		  AND owner_user_id = $3
		  AND worker_token = $4
		  AND status = 'running'
	`, claim.AttemptID, category, claim.OwnerUserID, claim.WorkerToken)
	if err != nil {
		return JobTarget{}, classifyJobTargetWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	tag, err = tx.Exec(ctx, `
		UPDATE preparation_job_targets
		SET stage = 'analysis_failed',
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND input_version = $3
		  AND stage = 'parsing'
	`, claim.OwnerUserID, claim.TargetID, claim.InputVersion)
	if err != nil {
		return JobTarget{}, classifyJobTargetWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return JobTarget{}, ErrJobTargetAnalysisClaimLost
	}
	target, err := readJobTargetInTransaction(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.TargetID,
		false,
	)
	if err != nil {
		return JobTarget{}, err
	}
	if err := persistJobTargetReplay(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.Intent,
		"job_target_analysis",
		claim.TargetID,
		httpStatusOK,
		target,
	); err != nil {
		return JobTarget{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobTarget{}, jobTargetDatabaseFailure(
			"commit analysis failure",
		)
	}
	return target, nil
}

func (r *PostgresJobTargetRepository) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmJobTargetCommand,
) (JobTarget, bool, error) {
	expectedPath := "/v1/job-targets/" +
		command.TargetID + "/confirmations"
	if !r.valid() || ctx == nil || !validPreparationActor(actor) ||
		!validResourceIdentifier(command.TargetID) ||
		command.Request.ExpectedInputVersion < 1 ||
		command.Request.ExpectedAnalysisVersion < 1 ||
		!validJobTargetCandidateShape(
			command.Request.Candidate,
			command.Request.Candidate.Source,
		) ||
		!validJobTargetOperationIntent(
			command.Intent,
			"POST",
			expectedPath,
			command.Request,
		) {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	encoded, err := json.Marshal(command.Request.Candidate)
	if err != nil {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"begin target confirmation",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActiveJobTargetActor(ctx, tx, actor.UserID); err != nil {
		return JobTarget{}, false, err
	}
	if err := lockJobTargetIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return JobTarget{}, false, err
	}
	replayed, found, err := readJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target_confirmation",
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return JobTarget{}, false, jobTargetDatabaseFailure(
				"commit confirmation replay",
			)
		}
		return replayed, true, nil
	}
	current, err := lockJobTarget(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if current.Stage == JobTargetStageDiscarded {
		return JobTarget{}, false, ErrJobTargetNotFound
	}
	if current.InputVersion != command.Request.ExpectedInputVersion ||
		current.Stage != JobTargetStageAwaitingConfirmation ||
		current.Analysis == nil ||
		current.Analysis.Status != JobTargetAnalysisSucceeded ||
		current.Analysis.AnalysisVersion !=
			command.Request.ExpectedAnalysisVersion {
		return JobTarget{}, false, ErrJobTargetConflict
	}
	if command.Request.Candidate.Source != current.Input.Source {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	inputSnapshot, err := json.Marshal(current.Input)
	if err != nil {
		return JobTarget{}, false, ErrJobTargetInvalid
	}

	var confirmationVersion int
	err = tx.QueryRow(ctx, `
		INSERT INTO preparation_job_target_confirmations (
			owner_user_id,
			target_id,
			input_version,
				analysis_version,
				confirmation_version,
				input_snapshot,
				candidate
			)
		SELECT
			$1,
			$2,
				$3,
				$4,
				coalesce(max(confirmation_version), 0) + 1,
				$5,
				$6
		FROM preparation_job_target_confirmations
		WHERE owner_user_id = $1
		  AND target_id = $2
		RETURNING confirmation_version
	`,
		actor.UserID,
		command.TargetID,
		current.InputVersion,
		command.Request.ExpectedAnalysisVersion,
		inputSnapshot,
		encoded,
	).Scan(&confirmationVersion)
	if err != nil {
		return JobTarget{}, false, classifyJobTargetWriteError(err)
	}
	if confirmationVersion < 1 {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"validate confirmation version",
		)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE preparation_job_targets
		SET stage = 'confirmed',
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND input_version = $3
		  AND stage = 'awaiting_confirmation'
	`,
		actor.UserID,
		command.TargetID,
		current.InputVersion,
	)
	if err != nil {
		return JobTarget{}, false, classifyJobTargetWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return JobTarget{}, false, ErrJobTargetConflict
	}
	target, err := readJobTargetInTransaction(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
		false,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if err := persistJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target_confirmation",
		target.ID,
		httpStatusOK,
		target,
	); err != nil {
		return JobTarget{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"commit target confirmation",
		)
	}
	return target, false, nil
}

func (r *PostgresJobTargetRepository) Discard(
	ctx context.Context,
	actor requestcontext.Actor,
	command DiscardJobTargetCommand,
) (JobTarget, bool, error) {
	expectedPath := "/v1/job-targets/" +
		command.TargetID + "/discard"
	if !r.valid() || ctx == nil || !validPreparationActor(actor) ||
		!validResourceIdentifier(command.TargetID) ||
		command.Request.ExpectedInputVersion < 1 ||
		!validJobTargetOperationIntent(
			command.Intent,
			"POST",
			expectedPath,
			command.Request,
		) {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"begin target discard",
		)
	}
	defer rollbackPreparationTransaction(ctx, tx)
	if err := lockActiveJobTargetActor(ctx, tx, actor.UserID); err != nil {
		return JobTarget{}, false, err
	}
	if err := lockJobTargetIdempotency(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
	); err != nil {
		return JobTarget{}, false, err
	}
	replayed, found, err := readJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target_discard",
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return JobTarget{}, false, jobTargetDatabaseFailure(
				"commit discard replay",
			)
		}
		return replayed, true, nil
	}
	current, err := lockJobTarget(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if current.Stage == JobTargetStageDiscarded {
		return JobTarget{}, false, ErrJobTargetNotFound
	}
	if current.InputVersion != command.Request.ExpectedInputVersion ||
		current.Stage == JobTargetStageConfirmed {
		return JobTarget{}, false, ErrJobTargetConflict
	}
	if _, err := tx.Exec(ctx, `
		UPDATE preparation_job_target_analysis_attempts
		SET status = 'failed',
		    stable_error_category = 'target_discarded',
		    finished_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND status = 'running'
	`, actor.UserID, command.TargetID); err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"fence discarded target analysis",
		)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE preparation_job_targets
		SET stage = 'discarded',
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND target_id = $2
		  AND input_version = $3
		  AND stage <> 'confirmed'
		  AND stage <> 'discarded'
	`,
		actor.UserID,
		command.TargetID,
		current.InputVersion,
	)
	if err != nil {
		return JobTarget{}, false, classifyJobTargetWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return JobTarget{}, false, ErrJobTargetConflict
	}
	target, err := readJobTargetInTransaction(
		ctx,
		tx,
		actor.UserID,
		command.TargetID,
		true,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	if err := persistJobTargetReplay(
		ctx,
		tx,
		actor.UserID,
		command.Intent,
		"job_target_discard",
		target.ID,
		httpStatusOK,
		target,
	); err != nil {
		return JobTarget{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"commit target discard",
		)
	}
	return target, false, nil
}

const (
	httpStatusOK      = 200
	httpStatusCreated = 201
)

const jobTargetSelect = `
	SELECT
		target.target_id,
		target.owner_user_id::text,
		target.source_kind,
		coalesce(target.job_title, ''),
		coalesce(target.job_description, ''),
		coalesce(target.company, ''),
		coalesce(target.seniority, ''),
		coalesce(target.candidate_background, ''),
		coalesce(target.practice_focus, ''),
		target.input_version,
		target.stage,
		target.created_at,
		target.updated_at,
		analysis.input_version,
		analysis.attempt_number,
		analysis.status,
		analysis.candidate,
		analysis.stable_error_category,
		analysis.started_at,
		analysis.finished_at,
		confirmation.input_version,
		confirmation.analysis_version,
		confirmation.confirmation_version,
		confirmation.candidate,
		confirmation.confirmed_at
	FROM preparation_job_targets AS target
	JOIN identity_users AS owner
	  ON owner.id = target.owner_user_id
	LEFT JOIN preparation_deletion_fences AS fence
	  ON fence.owner_user_id = target.owner_user_id
	LEFT JOIN LATERAL (
		SELECT
			attempt.input_version,
			attempt.attempt_number,
			attempt.status,
			attempt.candidate,
			attempt.stable_error_category,
			attempt.started_at,
			attempt.finished_at
		FROM preparation_job_target_analysis_attempts AS attempt
		WHERE attempt.owner_user_id = target.owner_user_id
		  AND attempt.target_id = target.target_id
		  AND attempt.input_version = target.input_version
		ORDER BY attempt.attempt_number DESC
		LIMIT 1
	) AS analysis ON true
	LEFT JOIN preparation_job_target_confirmations AS confirmation
	  ON confirmation.owner_user_id = target.owner_user_id
	 AND confirmation.target_id = target.target_id
	 AND confirmation.input_version = target.input_version
`

type jobTargetRowScanner interface {
	Scan(...any) error
}

func scanJobTarget(row jobTargetRowScanner) (JobTarget, error) {
	var (
		target                  JobTarget
		source                  string
		stage                   string
		analysisInputVersion    pgtype.Int4
		analysisVersion         pgtype.Int4
		analysisStatus          pgtype.Text
		analysisCandidate       []byte
		analysisError           pgtype.Text
		analysisStartedAt       pgtype.Timestamptz
		analysisFinishedAt      pgtype.Timestamptz
		confirmationInput       pgtype.Int4
		confirmationAnalysis    pgtype.Int4
		confirmationVersion     pgtype.Int4
		confirmationCandidate   []byte
		confirmationConfirmedAt pgtype.Timestamptz
	)
	err := row.Scan(
		&target.ID,
		&target.UserID,
		&source,
		&target.Input.JobTitle,
		&target.Input.JobDescription,
		&target.Input.Company,
		&target.Input.Seniority,
		&target.Input.CandidateBackground,
		&target.Input.PracticeFocus,
		&target.InputVersion,
		&stage,
		&target.CreatedAt,
		&target.UpdatedAt,
		&analysisInputVersion,
		&analysisVersion,
		&analysisStatus,
		&analysisCandidate,
		&analysisError,
		&analysisStartedAt,
		&analysisFinishedAt,
		&confirmationInput,
		&confirmationAnalysis,
		&confirmationVersion,
		&confirmationCandidate,
		&confirmationConfirmedAt,
	)
	if err != nil {
		return JobTarget{}, err
	}
	target.Input.Source = JobTargetSource(source)
	target.Stage = JobTargetStage(stage)
	target.CreatedAt = target.CreatedAt.UTC()
	target.UpdatedAt = target.UpdatedAt.UTC()

	if analysisVersion.Valid {
		analysis := JobTargetAnalysis{
			InputVersion:    int(analysisInputVersion.Int32),
			AnalysisVersion: int(analysisVersion.Int32),
			Attempt:         int(analysisVersion.Int32),
			Status: JobTargetAnalysisStatus(
				analysisStatus.String,
			),
			StableErrorCategory: analysisError.String,
			StartedAt:           analysisStartedAt.Time.UTC(),
		}
		if analysisFinishedAt.Valid {
			finishedAt := analysisFinishedAt.Time.UTC()
			analysis.FinishedAt = &finishedAt
		}
		if len(analysisCandidate) > 0 {
			var candidate JobTargetCandidate
			if json.Unmarshal(analysisCandidate, &candidate) != nil ||
				!validJobTargetCandidateShape(
					candidate,
					target.Input.Source,
				) {
				return JobTarget{}, errors.New(
					"invalid persisted job target candidate",
				)
			}
			candidate = cloneJobTargetCandidate(candidate)
			analysis.Candidate = &candidate
		}
		target.Analysis = &analysis
	}
	if confirmationVersion.Valid {
		var candidate JobTargetCandidate
		if json.Unmarshal(confirmationCandidate, &candidate) != nil ||
			!validJobTargetCandidateShape(
				candidate,
				target.Input.Source,
			) {
			return JobTarget{}, errors.New(
				"invalid persisted job target confirmation",
			)
		}
		target.Confirmation = &JobTargetConfirmation{
			InputVersion: int(confirmationInput.Int32),
			AnalysisVersion: int(
				confirmationAnalysis.Int32,
			),
			ConfirmationVersion: int(
				confirmationVersion.Int32,
			),
			Candidate:   cloneJobTargetCandidate(candidate),
			ConfirmedAt: confirmationConfirmedAt.Time.UTC(),
		}
	}
	if !validPersistedJobTarget(target) {
		return JobTarget{}, errors.New("invalid persisted job target")
	}
	return target, nil
}

func validPersistedJobTarget(target JobTarget) bool {
	if !validResourceIdentifier(target.ID) ||
		!validPreparationUUID(target.UserID) ||
		!validJobTargetInput(target.Input) ||
		target.InputVersion < 1 ||
		target.CreatedAt.IsZero() ||
		target.UpdatedAt.Before(target.CreatedAt) {
		return false
	}
	switch target.Stage {
	case JobTargetStageDraft:
		return target.Analysis == nil && target.Confirmation == nil
	case JobTargetStageParsing:
		return target.Analysis != nil &&
			target.Analysis.Status == JobTargetAnalysisRunning &&
			target.Confirmation == nil
	case JobTargetStageAnalysisFailed:
		return target.Analysis != nil &&
			target.Analysis.Status == JobTargetAnalysisFailed &&
			target.Confirmation == nil
	case JobTargetStageAwaitingConfirmation:
		return target.Analysis != nil &&
			target.Analysis.Status == JobTargetAnalysisSucceeded &&
			target.Analysis.Candidate != nil &&
			target.Confirmation == nil
	case JobTargetStageConfirmed:
		return target.Analysis != nil &&
			target.Analysis.Status == JobTargetAnalysisSucceeded &&
			target.Confirmation != nil &&
			target.Confirmation.InputVersion == target.InputVersion &&
			target.Confirmation.AnalysisVersion ==
				target.Analysis.AnalysisVersion
	case JobTargetStageDiscarded:
		return true
	default:
		return false
	}
}

func readJobTargetInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	targetID string,
	includeDiscarded bool,
) (JobTarget, error) {
	discardPredicate := " AND target.stage <> 'discarded'"
	if includeDiscarded {
		discardPredicate = ""
	}
	target, err := scanJobTarget(tx.QueryRow(
		ctx,
		jobTargetSelect+`
		WHERE target.owner_user_id = $1
		  AND target.target_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		`+discardPredicate,
		userID,
		targetID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return JobTarget{}, ErrJobTargetNotFound
	}
	if err != nil {
		return JobTarget{}, jobTargetDatabaseFailure(
			"read target transaction state",
		)
	}
	return target, nil
}

func lockJobTarget(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	targetID string,
) (JobTarget, error) {
	var (
		target JobTarget
		source string
		stage  string
	)
	err := tx.QueryRow(ctx, `
		SELECT
			target.target_id,
			target.owner_user_id::text,
			target.source_kind,
			coalesce(target.job_title, ''),
			coalesce(target.job_description, ''),
			coalesce(target.company, ''),
			coalesce(target.seniority, ''),
			coalesce(target.candidate_background, ''),
			coalesce(target.practice_focus, ''),
			target.input_version,
			target.stage,
			target.created_at,
			target.updated_at
		FROM preparation_job_targets AS target
		WHERE target.owner_user_id = $1
		  AND target.target_id = $2
		FOR UPDATE
	`, userID, targetID).Scan(
		&target.ID,
		&target.UserID,
		&source,
		&target.Input.JobTitle,
		&target.Input.JobDescription,
		&target.Input.Company,
		&target.Input.Seniority,
		&target.Input.CandidateBackground,
		&target.Input.PracticeFocus,
		&target.InputVersion,
		&stage,
		&target.CreatedAt,
		&target.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return JobTarget{}, ErrJobTargetNotFound
	}
	if err != nil {
		return JobTarget{}, jobTargetDatabaseFailure("lock target")
	}
	target.Input.Source = JobTargetSource(source)
	target.Stage = JobTargetStage(stage)
	target.CreatedAt = target.CreatedAt.UTC()
	target.UpdatedAt = target.UpdatedAt.UTC()
	if target.Stage == JobTargetStageAwaitingConfirmation ||
		target.Stage == JobTargetStageConfirmed {
		full, err := readJobTargetInTransaction(
			ctx,
			tx,
			userID,
			targetID,
			false,
		)
		if err != nil {
			return JobTarget{}, err
		}
		return full, nil
	}
	return target, nil
}

func lockAndValidateJobTargetClaim(
	ctx context.Context,
	tx pgx.Tx,
	claim JobTargetAnalysisClaim,
) (bool, error) {
	var (
		targetInputVersion int
		targetStage        string
		status             string
		leaseValid         bool
		latestAttemptID    string
	)
	err := tx.QueryRow(ctx, `
		SELECT
			target.input_version,
			target.stage,
			attempt.status,
			attempt.lease_until > transaction_timestamp(),
			(
				SELECT latest.attempt_id::text
				FROM preparation_job_target_analysis_attempts AS latest
				WHERE latest.owner_user_id = attempt.owner_user_id
				  AND latest.target_id = attempt.target_id
				  AND latest.input_version = attempt.input_version
				ORDER BY latest.attempt_number DESC
				LIMIT 1
			)
		FROM preparation_job_target_analysis_attempts AS attempt
		JOIN preparation_job_targets AS target
		  ON target.owner_user_id = attempt.owner_user_id
		 AND target.target_id = attempt.target_id
		WHERE attempt.attempt_id = $1
		  AND attempt.owner_user_id = $2
		  AND attempt.target_id = $3
		  AND attempt.input_version = $4
		  AND attempt.attempt_number = $5
		  AND attempt.worker_token = $6
		FOR UPDATE OF attempt, target
	`,
		claim.AttemptID,
		claim.OwnerUserID,
		claim.TargetID,
		claim.InputVersion,
		claim.AnalysisVersion,
		claim.WorkerToken,
	).Scan(
		&targetInputVersion,
		&targetStage,
		&status,
		&leaseValid,
		&latestAttemptID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, jobTargetDatabaseFailure(
			"validate analysis claim",
		)
	}
	return targetInputVersion == claim.InputVersion &&
		targetStage == string(JobTargetStageParsing) &&
		status == string(JobTargetAnalysisRunning) &&
		leaseValid &&
		latestAttemptID == claim.AttemptID, nil
}

func readJobTargetReplay(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent JobTargetOperationIntent,
	expectedKind string,
	expectedTargetID string,
) (JobTarget, bool, error) {
	var (
		fingerprint []byte
		kind        string
		resourceID  string
		status      int
		body        []byte
	)
	err := tx.QueryRow(ctx, `
		SELECT
			payload_fingerprint,
			resource_kind,
			resource_id,
			response_status,
			response_body
		FROM preparation_job_target_idempotency_records
		WHERE owner_user_id = $1
		  AND method = $2
		  AND canonical_path = $3
		  AND idempotency_key = $4
	`,
		userID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
	).Scan(&fingerprint, &kind, &resourceID, &status, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return JobTarget{}, false, nil
	}
	if err != nil {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"read target idempotency replay",
		)
	}
	if !bytes.Equal(fingerprint, intent.PayloadFingerprint[:]) ||
		kind != expectedKind {
		return JobTarget{}, false, ErrJobTargetIdempotencyConflict
	}
	if status < 200 || status > 299 ||
		(expectedTargetID != "" && resourceID != expectedTargetID) {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"validate target idempotency replay",
		)
	}
	var target JobTarget
	if json.Unmarshal(body, &target) != nil ||
		target.ID != resourceID ||
		target.UserID != userID ||
		!validPersistedJobTarget(target) {
		return JobTarget{}, false, jobTargetDatabaseFailure(
			"decode target idempotency replay",
		)
	}
	return target, true, nil
}

func persistJobTargetReplay(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent JobTargetOperationIntent,
	resourceKind string,
	resourceID string,
	responseStatus int,
	target JobTarget,
) error {
	body, err := json.Marshal(target)
	if err != nil {
		return jobTargetDatabaseFailure(
			"encode target idempotency response",
		)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO preparation_job_target_idempotency_records (
			owner_user_id,
			method,
			canonical_path,
			idempotency_key,
			payload_fingerprint,
			resource_kind,
			resource_id,
			response_status,
			response_body
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (
			owner_user_id,
			method,
			canonical_path,
			idempotency_key
		) DO UPDATE
		SET response_status = EXCLUDED.response_status,
		    response_body = EXCLUDED.response_body
		WHERE preparation_job_target_idempotency_records.payload_fingerprint =
		        EXCLUDED.payload_fingerprint
		  AND preparation_job_target_idempotency_records.resource_kind =
		        EXCLUDED.resource_kind
		  AND preparation_job_target_idempotency_records.resource_id =
		        EXCLUDED.resource_id
	`,
		userID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
		intent.PayloadFingerprint[:],
		resourceKind,
		resourceID,
		responseStatus,
		body,
	)
	if err != nil {
		return classifyJobTargetWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return ErrJobTargetIdempotencyConflict
	}
	return nil
}

func lockJobTargetIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	intent JobTargetOperationIntent,
) error {
	document, err := json.Marshal([]string{
		userID,
		intent.Method,
		intent.CanonicalPath,
		intent.Key,
	})
	if err != nil {
		return jobTargetDatabaseFailure(
			"encode target idempotency lock",
		)
	}
	if _, err := tx.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		string(document),
	); err != nil {
		return jobTargetDatabaseFailure(
			"lock target idempotency",
		)
	}
	return nil
}

func lockActiveJobTargetActor(
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
		return ErrJobTargetNotFound
	}
	if err != nil {
		return jobTargetDatabaseFailure(
			"lock active target actor",
		)
	}
	return nil
}

func validJobTargetOperationIntent(
	intent JobTargetOperationIntent,
	method string,
	path string,
	payload any,
) bool {
	if intent.Method != method ||
		intent.CanonicalPath != path ||
		!validCanonicalPath(path) ||
		!validIdempotencyKey(intent.Key) {
		return false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	expected := sha256.Sum256(encoded)
	return bytes.Equal(
		intent.PayloadFingerprint[:],
		expected[:],
	)
}

func validJobTargetAnalysisClaim(
	claim JobTargetAnalysisClaim,
) bool {
	return validPreparationUUID(claim.AttemptID) &&
		validResourceIdentifier(claim.TargetID) &&
		validPreparationUUID(claim.OwnerUserID) &&
		claim.InputVersion > 0 &&
		claim.AnalysisVersion > 0 &&
		validPreparationUUID(claim.WorkerToken) &&
		!claim.LeaseUntil.IsZero() &&
		validJobTargetInput(claim.Input) &&
		validCanonicalPath(claim.Intent.CanonicalPath) &&
		validIdempotencyKey(claim.Intent.Key)
}

func (r *PostgresJobTargetRepository) valid() bool {
	return r != nil && r.pool != nil
}

func classifyJobTargetWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrJobTargetNotFound
		case "23505":
			return ErrJobTargetConflict
		case "23514", "22001":
			return ErrJobTargetInvalid
		}
	}
	return jobTargetDatabaseFailure("write target data")
}

func jobTargetDatabaseFailure(operation string) error {
	return fmt.Errorf("%w: %s", ErrJobTargetRepository, operation)
}

var _ JobTargetRepository = (*PostgresJobTargetRepository)(nil)
