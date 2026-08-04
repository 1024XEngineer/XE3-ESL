package context

import (
	stdcontext "context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
)

type Repository interface {
	FindThread(stdcontext.Context, string, string) (conversation.Thread, error)
	FindLatestCheckpoint(stdcontext.Context, string, string, int64) (summary.Checkpoint, error)
	ListMessagesForContext(
		stdcontext.Context,
		string,
		string,
		int64,
		int64,
		int,
	) ([]conversation.Message, int, error)
	FindMessage(stdcontext.Context, string, string, string) (conversation.Message, error)
}

type ManifestRepository interface {
	SaveManifest(stdcontext.Context, Manifest) (Manifest, error)
	FindManifest(stdcontext.Context, string, string) (Manifest, error)
	SaveToolSnapshot(stdcontext.Context, Manifest) (Manifest, error)
}
