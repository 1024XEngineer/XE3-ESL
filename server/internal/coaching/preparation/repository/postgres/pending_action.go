package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pendingActionColumns = `pending_action_id::text, owner_id::text, thread_id::text,
source_run_id::text, source_input_message_id::text, source_input_sequence,
proposal, proposal_fingerprint, state,
COALESCE(resolution_input_message_id::text, ''),
COALESCE(resolved_plan_id::text, ''), created_at, resolved_at`

type PostgresPendingActionRepository struct{ pool *pgxpool.Pool }

func NewPostgresPendingActionRepository(
	pool *pgxpool.Pool,
) *PostgresPendingActionRepository {
	return &PostgresPendingActionRepository{pool: pool}
}

func (repository *PostgresPendingActionRepository) HasOpenForReply(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	inputSequence int64,
) (bool, error) {
	if repository == nil || repository.pool == nil || ctx == nil ||
		!actor.Valid() || !preparation.ValidAggregateID(threadID) ||
		inputSequence < 3 {
		return false, preparation.ErrPendingActionInvalid
	}
	var exists bool
	if err := repository.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM pending_practice_actions
WHERE owner_id=$1 AND thread_id=$2 AND state='OPEN'
  AND source_input_sequence + 2 = $3)`,
		actor.UserID, threadID, inputSequence).Scan(&exists); err != nil {
		return false, preparation.ErrPendingActionRepository
	}
	return exists, nil
}

func (repository *PostgresPendingActionRepository) CreateOrReplay(
	ctx context.Context,
	actor requestcontext.Actor,
	command preparation.CreatePendingActionCommand,
) (preparation.PendingPracticeAction, bool, error) {
	if repository == nil || repository.pool == nil || ctx == nil ||
		!actor.Valid() || !validPendingCreate(command) {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	existing, err := readPendingBySourceMessage(
		ctx, tx, actor.UserID, command.ThreadID, command.SourceInputMessageID,
	)
	if err == nil {
		if !bytes.Equal(existing.ProposalFingerprint[:], command.ProposalFingerprint[:]) {
			return preparation.PendingPracticeAction{}, false,
				preparation.ErrPendingActionConflict
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return preparation.PendingPracticeAction{}, false,
				preparation.ErrPendingActionRepository
		}
		return existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	if _, err = tx.Exec(ctx, `UPDATE pending_practice_actions
SET state='SUPERSEDED', resolved_at=transaction_timestamp()
WHERE owner_id=$1 AND thread_id=$2 AND state='OPEN'`,
		actor.UserID, command.ThreadID); err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	created, err := scanPending(tx.QueryRow(ctx, `INSERT INTO pending_practice_actions (
owner_id, thread_id, source_run_id, source_input_message_id,
source_input_sequence, proposal, proposal_fingerprint, state)
VALUES ($1,$2,$3,$4,$5,$6,$7,'OPEN') RETURNING `+pendingActionColumns,
		actor.UserID, command.ThreadID, command.SourceRunID,
		command.SourceInputMessageID, command.SourceInputSequence,
		command.Proposal, command.ProposalFingerprint[:]))
	if err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	if err = tx.Commit(ctx); err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	return created, false, nil
}

func (repository *PostgresPendingActionRepository) ClaimForReply(
	ctx context.Context,
	actor requestcontext.Actor,
	command preparation.ResolvePendingActionCommand,
) (preparation.PendingPracticeAction, bool, error) {
	if repository == nil || repository.pool == nil || ctx == nil ||
		!actor.Valid() || !validPendingResolve(command) {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	item, err := scanPending(tx.QueryRow(ctx, `SELECT `+pendingActionColumns+`
FROM pending_practice_actions
WHERE owner_id=$1 AND thread_id=$2
  AND resolution_input_message_id=$3
  AND state IN ('CONFIRMING','CONFIRMED','REJECTED')
FOR UPDATE`, actor.UserID, command.ThreadID,
		command.ResolutionInputMessageID))
	if err == nil {
		if (command.Confirm && item.State == preparation.PendingActionRejected) ||
			(!command.Confirm && item.State != preparation.PendingActionRejected) {
			return preparation.PendingPracticeAction{}, false,
				preparation.ErrPendingActionConflict
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return preparation.PendingPracticeAction{}, false,
				preparation.ErrPendingActionRepository
		}
		return item, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	item, err = scanPending(tx.QueryRow(ctx, `SELECT `+pendingActionColumns+`
FROM pending_practice_actions
WHERE owner_id=$1 AND thread_id=$2 AND state='OPEN'
  AND source_input_sequence + 2 = $3
FOR UPDATE`, actor.UserID, command.ThreadID,
		command.ResolutionInputSequence))
	if errors.Is(err, pgx.ErrNoRows) {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionNotFound
	}
	if err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	state := preparation.PendingActionConfirming
	resolvedAt := "NULL"
	if !command.Confirm {
		state = preparation.PendingActionRejected
		resolvedAt = "transaction_timestamp()"
	}
	item, err = scanPending(tx.QueryRow(ctx, `UPDATE pending_practice_actions
SET state=$1, resolution_input_message_id=$2, resolved_at=`+resolvedAt+`
WHERE pending_action_id=$3 RETURNING `+pendingActionColumns,
		string(state), command.ResolutionInputMessageID, item.ID))
	if err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	if err = tx.Commit(ctx); err != nil {
		return preparation.PendingPracticeAction{}, false,
			preparation.ErrPendingActionRepository
	}
	return item, false, nil
}

func (repository *PostgresPendingActionRepository) CompleteConfirmation(
	ctx context.Context,
	actor requestcontext.Actor,
	pendingID string,
	resolutionMessageID string,
	planID string,
) (preparation.PendingPracticeAction, error) {
	if repository == nil || repository.pool == nil || ctx == nil ||
		!actor.Valid() || !preparation.ValidAggregateID(pendingID) ||
		!preparation.ValidAggregateID(resolutionMessageID) ||
		!preparation.ValidAggregateID(planID) {
		return preparation.PendingPracticeAction{}, preparation.ErrPendingActionInvalid
	}
	item, err := scanPending(repository.pool.QueryRow(ctx, `UPDATE pending_practice_actions
SET state='CONFIRMED', resolved_plan_id=$1, resolved_at=transaction_timestamp()
WHERE pending_action_id=$2 AND owner_id=$3
  AND state='CONFIRMING' AND resolution_input_message_id=$4
RETURNING `+pendingActionColumns, planID, pendingID, actor.UserID,
		resolutionMessageID))
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = scanPending(repository.pool.QueryRow(ctx, `SELECT `+pendingActionColumns+`
FROM pending_practice_actions
WHERE pending_action_id=$1 AND owner_id=$2 AND state='CONFIRMED'
  AND resolution_input_message_id=$3 AND resolved_plan_id=$4`,
			pendingID, actor.UserID, resolutionMessageID, planID))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return preparation.PendingPracticeAction{}, preparation.ErrPendingActionConflict
	}
	if err != nil {
		return preparation.PendingPracticeAction{}, preparation.ErrPendingActionRepository
	}
	return item, nil
}

func readPendingBySourceMessage(
	ctx context.Context,
	query interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	ownerID string,
	threadID string,
	messageID string,
) (preparation.PendingPracticeAction, error) {
	return scanPending(query.QueryRow(ctx, `SELECT `+pendingActionColumns+`
FROM pending_practice_actions
WHERE owner_id=$1 AND thread_id=$2 AND source_input_message_id=$3
FOR UPDATE`, ownerID, threadID, messageID))
}

func scanPending(row pgx.Row) (preparation.PendingPracticeAction, error) {
	var item preparation.PendingPracticeAction
	var fingerprint []byte
	if err := row.Scan(
		&item.ID, &item.OwnerID, &item.ThreadID, &item.SourceRunID,
		&item.SourceInputMessageID, &item.SourceInputSequence, &item.Proposal,
		&fingerprint, &item.State, &item.ResolutionInputMessageID,
		&item.ResolvedPlanID, &item.CreatedAt, &item.ResolvedAt,
	); err != nil {
		return preparation.PendingPracticeAction{}, err
	}
	if len(fingerprint) != len(item.ProposalFingerprint) {
		return preparation.PendingPracticeAction{},
			preparation.ErrPendingActionRepository
	}
	copy(item.ProposalFingerprint[:], fingerprint)
	if !item.Valid() {
		return preparation.PendingPracticeAction{},
			preparation.ErrPendingActionRepository
	}
	return item, nil
}

func validPendingCreate(command preparation.CreatePendingActionCommand) bool {
	var proposal map[string]any
	return preparation.ValidAggregateID(command.ThreadID) &&
		preparation.ValidAggregateID(command.SourceRunID) &&
		preparation.ValidAggregateID(command.SourceInputMessageID) &&
		command.SourceInputSequence > 0 && len(command.Proposal) > 0 &&
		json.Unmarshal(command.Proposal, &proposal) == nil && proposal != nil
}

func validPendingResolve(command preparation.ResolvePendingActionCommand) bool {
	return preparation.ValidAggregateID(command.ThreadID) &&
		preparation.ValidAggregateID(command.ResolutionInputMessageID) &&
		command.ResolutionInputSequence > 2
}
