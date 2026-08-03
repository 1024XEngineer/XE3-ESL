package postgres

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5"
)

// FindMessageInTransaction reads the Conversation-owned Message projection
// without taking ownership of the caller's transaction.
func FindMessageInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	messageID string,
) (conversation.Message, error) {
	return findMessage(ctx, tx, ownerID, threadID, messageID)
}

// FindMessageByClientIDInTransaction reads the Conversation-owned Message
// projection without taking ownership of the caller's transaction.
func FindMessageByClientIDInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	clientMessageID string,
) (conversation.Message, bool, error) {
	return findMessageByClientID(
		ctx,
		tx,
		ownerID,
		threadID,
		clientMessageID,
	)
}
