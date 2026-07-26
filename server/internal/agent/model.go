package agent

import (
	"context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type MessageRole string

const (
	MessageRoleUser MessageRole = "user"
)

type Thread struct {
	ID             string
	OwnerID        string
	ActiveMatterID string
	NextMessageSeq int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ThreadMatterLink struct {
	OwnerID   string
	ThreadID  string
	MatterID  string
	Active    bool
	LinkedAt  time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID              string
	OwnerID         string
	ThreadID        string
	Sequence        int64
	Role            MessageRole
	ClientMessageID string
	Content         string
	CreatedAt       time.Time
}

type Repository interface {
	CreateThread(
		ctx context.Context,
		ownerID string,
		activeMatterID string,
	) (Thread, error)
	ListThreads(ctx context.Context, ownerID string) ([]Thread, error)
	FindThread(ctx context.Context, ownerID, threadID string) (Thread, error)
	SetActiveMatter(
		ctx context.Context,
		ownerID string,
		threadID string,
		matterID string,
	) (ThreadMatterLink, error)
	AppendUserMessage(
		ctx context.Context,
		ownerID string,
		threadID string,
		clientMessageID string,
		content string,
	) (Message, error)
	ListMessages(
		ctx context.Context,
		ownerID string,
		threadID string,
	) ([]Message, error)
}

type Application interface {
	CreateThread(
		ctx context.Context,
		actor requestcontext.Actor,
		activeMatterID string,
	) (Thread, error)
	ListThreads(
		ctx context.Context,
		actor requestcontext.Actor,
	) ([]Thread, error)
	GetThread(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
	) (Thread, error)
	SetActiveMatter(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
		matterID string,
	) (ThreadMatterLink, error)
	AppendUserMessage(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
		clientMessageID string,
		content string,
	) (Message, error)
	ListMessages(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
	) ([]Message, error)
}

type IDGenerator interface {
	NewID() (string, error)
}
