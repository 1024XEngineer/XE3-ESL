package postgres

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/jackc/pgx/v5"
)

// InitialPendingRunInTransaction is the complete input required to create an
// initial pending Run inside a transaction owned by another business operation.
type InitialPendingRunInTransaction struct {
	ID             string
	OwnerID        string
	ThreadID       string
	InputMessageID string
	Configuration  agentrun.Configuration
}

// CreateInitialPendingRunInTransaction writes a Run without committing or
// rolling back the transaction supplied by the caller.
func CreateInitialPendingRunInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	command InitialPendingRunInTransaction,
) (agentrun.Run, error) {
	return insertPendingRun(
		ctx,
		tx,
		command.ID,
		command.OwnerID,
		command.ThreadID,
		command.InputMessageID,
		1,
		"",
		"",
		command.Configuration,
	)
}

// FindRunInTransaction reads a Run without taking ownership of the caller's
// transaction.
func FindRunInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	runID string,
) (agentrun.Run, error) {
	return findRun(ctx, tx, ownerID, runID)
}

type rowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findRun(
	ctx context.Context,
	queryer rowQueryer,
	ownerID string,
	runID string,
) (agentrun.Run, error) {
	run, err := scanRun(queryer.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads ON threads.id = runs.thread_id
WHERE runs.id = $1
  AND threads.user_id = $2
  AND threads.deleted_at IS NULL`,
		runID,
		ownerID,
	))
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	return run, nil
}

func findInputMessageByClientIDInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	clientMessageID string,
) (conversation.Message, bool, error) {
	message, found, err := conversationpostgres.
		FindMessageByClientIDInTransaction(
			ctx,
			tx,
			ownerID,
			threadID,
			clientMessageID,
		)
	if err != nil {
		return conversation.Message{}, false, mapConversationError(err)
	}
	return message, found, nil
}

func mapConversationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, conversation.ErrInvalidRequest):
		return agentrun.ErrInvalidRequest
	case errors.Is(err, conversation.ErrNotFound):
		return agentrun.ErrNotFound
	case errors.Is(err, conversation.ErrConflict):
		return agentrun.ErrConflict
	case errors.Is(err, conversation.ErrIdempotencyConflict):
		return agentrun.ErrIdempotencyConflict
	default:
		return agentrun.ErrRepository
	}
}
