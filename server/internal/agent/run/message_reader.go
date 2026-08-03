package run

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

// MessageReader is the Conversation capability required to replay a Run
// submission to a streaming client.
type MessageReader interface {
	FindMessage(
		context.Context,
		string,
		string,
		string,
	) (conversation.Message, error)
}
