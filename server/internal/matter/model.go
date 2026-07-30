package matter

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

type Matter struct {
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
	Create(ctx context.Context, ownerID, title string) (Matter, error)
	CreateIdempotent(
		ctx context.Context,
		ownerID string,
		requestID string,
		title string,
	) (Matter, error)
	ListOwned(ctx context.Context, ownerID string) ([]Matter, error)
	SearchOwned(
		ctx context.Context,
		ownerID string,
		query SearchQuery,
	) ([]Matter, error)
	FindOwned(ctx context.Context, ownerID, matterID string) (Matter, error)
	UpdateStatus(
		ctx context.Context,
		ownerID string,
		matterID string,
		expectedVersion int64,
		status Status,
	) (Matter, error)
}

// Reader is Matter's public ownership and state port for other modules.
// Implementations resolve ownership from a trusted Actor, never request data.
type Reader interface {
	ReadOwned(
		ctx context.Context,
		actor requestcontext.Actor,
		matterID string,
	) (Matter, error)
}

type Application interface {
	Reader
	Create(
		ctx context.Context,
		actor requestcontext.Actor,
		title string,
	) (Matter, error)
	CreateIdempotent(
		ctx context.Context,
		actor requestcontext.Actor,
		requestID string,
		title string,
	) (Matter, error)
	List(
		ctx context.Context,
		actor requestcontext.Actor,
	) ([]Matter, error)
	Search(
		ctx context.Context,
		actor requestcontext.Actor,
		query SearchQuery,
	) ([]Matter, error)
	ChangeStatus(
		ctx context.Context,
		actor requestcontext.Actor,
		matterID string,
		expectedVersion int64,
		status Status,
	) (Matter, error)
}

type IDGenerator interface {
	NewID() (string, error)
}
