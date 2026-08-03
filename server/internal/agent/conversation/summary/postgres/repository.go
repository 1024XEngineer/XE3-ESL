package postgres

import (
	"context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollbackTimeout = 5 * time.Second

type database interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type IDGenerator interface {
	NewID() (string, error)
}

type Repository struct {
	database database
	ids      IDGenerator
}

func New(database database, ids IDGenerator) (*Repository, error) {
	if database == nil || ids == nil {
		return nil, conversation.ErrRepository
	}
	return &Repository{database: database, ids: ids}, nil
}

type rowScanner interface {
	Scan(...any) error
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}
