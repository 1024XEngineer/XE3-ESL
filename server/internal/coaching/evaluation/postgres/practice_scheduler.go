package postgres

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/jackc/pgx/v5"
)

type SessionScheduler struct {
	store   *Store
	builder *evaluation.SessionCommandBuilder
}

func NewSessionScheduler(
	store *Store,
	builder *evaluation.SessionCommandBuilder,
) (*SessionScheduler, error) {
	if store == nil || builder == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &SessionScheduler{store: store, builder: builder}, nil
}

func (scheduler *SessionScheduler) ScheduleCompletedSession(
	ctx context.Context,
	tx pgx.Tx,
	evidence practice.SessionEvidence,
) error {
	if scheduler == nil || scheduler.store == nil || scheduler.builder == nil ||
		ctx == nil || tx == nil {
		return evaluation.ErrInvalidRequest
	}
	command, err := scheduler.builder.Build(evidence)
	if err != nil {
		return err
	}
	_, _, err = scheduler.store.QueueInTx(ctx, tx, command)
	return err
}

type TurnFeedbackScheduler struct {
	store   *Store
	builder *evaluation.TurnFeedbackCommandBuilder
}

func NewTurnFeedbackScheduler(
	store *Store,
	builder *evaluation.TurnFeedbackCommandBuilder,
) (*TurnFeedbackScheduler, error) {
	if store == nil || builder == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &TurnFeedbackScheduler{store: store, builder: builder}, nil
}

func (scheduler *TurnFeedbackScheduler) ScheduleConfirmedTurn(
	ctx context.Context,
	tx pgx.Tx,
	evidence practice.TurnFeedbackEvidence,
) error {
	if scheduler == nil || scheduler.store == nil || scheduler.builder == nil ||
		ctx == nil || tx == nil {
		return evaluation.ErrInvalidRequest
	}
	command, err := scheduler.builder.Build(evidence)
	if err != nil {
		return err
	}
	_, _, err = scheduler.store.QueueInTx(ctx, tx, command)
	return err
}

var _ practicepostgres.CompletionScheduler = (*SessionScheduler)(nil)
var _ practicepostgres.TurnFeedbackScheduler = (*TurnFeedbackScheduler)(nil)
