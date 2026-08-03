package conversation

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Repository interface {
	CreateThread(context.Context, string, string) (Thread, error)
	ListThreads(context.Context, string) ([]Thread, error)
	PageThreads(context.Context, string, int, *ThreadPageCursor) ([]Thread, error)
	FindThread(context.Context, string, string) (Thread, error)
	FindFocusedThread(context.Context, string) (Thread, bool, error)
	SetFocusedThread(context.Context, string, string) (Thread, error)
	ClearFocusedThread(context.Context, string) error
	SetActiveMatter(context.Context, string, string, string) (ThreadMatterLink, error)
	AppendUserMessage(context.Context, string, string, string, string) (Message, error)
	ListMessages(context.Context, string, string) ([]Message, error)
	PageMessages(context.Context, string, string, int, *MessagePageCursor) ([]Message, error)
	DeleteThread(context.Context, string, string) error
}

type Application interface {
	CreateThread(context.Context, requestcontext.Actor, string) (Thread, error)
	ListThreads(context.Context, requestcontext.Actor) ([]Thread, error)
	PageThreads(context.Context, requestcontext.Actor, int, string) (ThreadPage, error)
	GetThread(context.Context, requestcontext.Actor, string) (Thread, error)
	GetFocusedThread(context.Context, requestcontext.Actor) (Thread, bool, error)
	SetFocusedThread(context.Context, requestcontext.Actor, string) (Thread, error)
	ClearFocusedThread(context.Context, requestcontext.Actor) error
	SetActiveMatter(context.Context, requestcontext.Actor, string, string) (ThreadMatterLink, error)
	AppendUserMessage(context.Context, requestcontext.Actor, string, string, string) (Message, error)
	ListMessages(context.Context, requestcontext.Actor, string) ([]Message, error)
	PageMessages(context.Context, requestcontext.Actor, string, int, string) (MessagePage, error)
	DeleteThread(context.Context, requestcontext.Actor, string) error
}
