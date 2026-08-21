package postgres

import (
	"context"
	"errors"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct {
	database database
}

func New(database database) (*Repository, error) {
	if database == nil {
		return nil, agentcontext.ErrRepository
	}
	return &Repository{database: database}, nil
}

func mapSourcePostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return conversation.ErrNotFound
		case "23505":
			return conversation.ErrConflict
		case "23514":
			return conversation.ErrInvalidRequest
		}
	}
	return conversation.ErrRepository
}

var (
	_ agentcontext.Repository = (*Repository)(nil)
)
