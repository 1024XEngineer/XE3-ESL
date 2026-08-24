package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

type Repository struct {
	pool               *pgxpool.Pool
	now                func() time.Time
	afterRecordingLock func()
	completion         CompletionScheduler
	turnFeedback       TurnFeedbackScheduler
	profile            IELTSProfileScheduler
	ids                practice.PracticeResourceIDGenerator
}

// CompletionScheduler is the transactional integration port consumed only by
// this PostgreSQL adapter. Practice's domain remains infrastructure-agnostic.
type CompletionScheduler interface {
	ScheduleCompletedSession(context.Context, pgx.Tx, practice.SessionEvidence) error
}

type TurnFeedbackScheduler interface {
	ScheduleConfirmedTurn(context.Context, pgx.Tx, practice.TurnFeedbackEvidence) error
}

type IELTSProfileScheduler interface {
	ScheduleCompletedPart(
		context.Context,
		pgx.Tx,
		practice.IELTSPartProfileEvidence,
	) error
}

func New(
	pool *pgxpool.Pool,
	completion CompletionScheduler,
	turnFeedback TurnFeedbackScheduler,
	profile IELTSProfileScheduler,
	ids practice.PracticeResourceIDGenerator,
) (*Repository, error) {
	if pool == nil || completion == nil || turnFeedback == nil ||
		profile == nil || ids == nil {
		return nil, errors.New("practice postgres dependencies are required")
	}
	return &Repository{
		pool:         pool,
		now:          time.Now,
		completion:   completion,
		turnFeedback: turnFeedback,
		profile:      profile,
		ids:          ids,
	}, nil
}

func lockActiveActor(ctx context.Context, tx pgx.Tx, userID string) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT true FROM users WHERE id=$1 AND status='active' FOR UPDATE`, userID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("verify active practice actor: %w", err)
	}
	return nil
}

func validActor(actor practice.Actor) bool {
	return validUserID(actor.UserID) && validUserID(actor.SessionID)
}
func validUserID(value string) bool {
	var parsed pgtype.UUID
	return parsed.Scan(strings.TrimSpace(value)) == nil && parsed.Valid
}
func rollback(ctx context.Context, tx pgx.Tx) { _ = tx.Rollback(ctx) }

func classifyWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return practice.ErrNotFound
		case "23505", "23514":
			return practice.ErrConflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
