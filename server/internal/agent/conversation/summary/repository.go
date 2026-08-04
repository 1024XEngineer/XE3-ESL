package summary

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

type CheckpointRepository interface {
	CreateCheckpoint(context.Context, CreateCheckpointCommand) (Checkpoint, error)
	FindLatestCheckpoint(context.Context, string, string, int64) (Checkpoint, error)
}

type Repository interface {
	CheckpointRepository
	ListMessagesForSummary(
		context.Context,
		string,
		string,
		int64,
		int64,
	) ([]conversation.Message, error)
}
