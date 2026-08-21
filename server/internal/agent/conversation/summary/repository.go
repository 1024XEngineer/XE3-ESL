package summary

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

type Repository interface {
	FindSummary(context.Context, string, string, int64) (State, error)
	ListMessagesForSummary(
		context.Context,
		string,
		string,
		int64,
		int64,
	) ([]conversation.Message, error)
	Claim(context.Context, WorkerConfiguration) (Claim, bool, error)
	Complete(context.Context, Claim, int64, Content) error
	Skip(context.Context, Claim) error
	Fail(
		context.Context,
		Claim,
		string,
		bool,
		WorkerConfiguration,
	) (bool, error)
}
