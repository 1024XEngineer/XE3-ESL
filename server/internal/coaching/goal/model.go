package goal

import (
	"context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusArchived  Status = "archived"
)

type Goal struct {
	ID        string
	OwnerID   string
	Title     string
	Status    Status
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SearchQuery struct {
	Query string
	Limit int
}

type Repository interface {
	Create(ctx context.Context, ownerID, title string) (Goal, error)
	CreateIdempotent(
		ctx context.Context,
		ownerID string,
		requestID string,
		title string,
	) (Goal, error)
	ListOwned(ctx context.Context, ownerID string) ([]Goal, error)
	SearchOwned(
		ctx context.Context,
		ownerID string,
		query SearchQuery,
	) ([]Goal, error)
	FindOwned(ctx context.Context, ownerID, goalID string) (Goal, error)
	UpdateStatus(
		ctx context.Context,
		ownerID string,
		goalID string,
		expectedVersion int64,
		status Status,
	) (Goal, error)
}

// Reader is Goal's public ownership and state port for other modules.
// Implementations resolve ownership from a trusted Actor, never request data.
type Reader interface {
	ReadOwned(
		ctx context.Context,
		actor requestcontext.Actor,
		goalID string,
	) (Goal, error)
}

type Application interface {
	Reader
	Create(
		ctx context.Context,
		actor requestcontext.Actor,
		title string,
	) (Goal, error)
	CreateIdempotent(
		ctx context.Context,
		actor requestcontext.Actor,
		requestID string,
		title string,
	) (Goal, error)
	List(
		ctx context.Context,
		actor requestcontext.Actor,
	) ([]Goal, error)
	Search(
		ctx context.Context,
		actor requestcontext.Actor,
		query SearchQuery,
	) ([]Goal, error)
	ChangeStatus(
		ctx context.Context,
		actor requestcontext.Actor,
		goalID string,
		expectedVersion int64,
		status Status,
	) (Goal, error)
}

type IDGenerator interface {
	NewID() (string, error)
}
