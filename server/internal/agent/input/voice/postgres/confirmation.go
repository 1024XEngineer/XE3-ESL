package postgres

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ConfirmDraft(
	ctx context.Context,
	ownerID string,
	command agentvoice.ConfirmDraftCommand,
) (agentvoice.Confirmation, error) {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(command.DraftID) || command.Version < 1 ||
		!agentvoice.ValidClientMessageID(command.ClientMessageID) ||
		!agentvoice.ValidMessageContent(command.ConfirmedText) ||
		!command.Configuration.Valid() {
		return agentvoice.Confirmation{}, agentvoice.ErrInvalidRequest
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	defer rollback(tx)
	if err := lockActiveOwner(ctx, tx, ownerID); err != nil {
		return agentvoice.Confirmation{}, err
	}
	threadID, err := draftThreadID(ctx, tx, ownerID, command.DraftID)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	nextSequence, err := lockOwnedThread(ctx, tx, ownerID, threadID)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	draft, err := findDraftForUpdate(ctx, tx, ownerID, command.DraftID)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	if draft.Version != command.Version {
		return agentvoice.Confirmation{}, agentvoice.ErrDraftStale
	}
	if draft.Status == agentvoice.StatusConfirmed {
		return replayConfirmation(ctx, tx, ownerID, command, draft)
	}
	if draft.Status == agentvoice.StatusTranscribing {
		return agentvoice.Confirmation{}, agentvoice.ErrDraftProcessing
	}
	if draft.Status != agentvoice.StatusReady {
		return agentvoice.Confirmation{}, agentvoice.ErrConflict
	}
	_, found, err := conversationpostgres.FindMessageByClientIDInTransaction(
		ctx, tx, ownerID, threadID, command.ClientMessageID,
	)
	if err != nil {
		return agentvoice.Confirmation{}, mapConversationError(err)
	}
	if found {
		return agentvoice.Confirmation{}, agentvoice.ErrIdempotencyConflict
	}
	if err := mediapostgres.LockAttachableInTransaction(
		ctx, tx, ownerID, sharedmedia.KindAudio, []string{draft.ID},
	); err != nil {
		return agentvoice.Confirmation{}, mapMediaError(err)
	}
	messageID, err := repository.ids.NewID()
	if err != nil {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	runID, err := repository.ids.NewID()
	if err != nil {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO agent_messages (
    id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at
) VALUES ($1, $2, $3, 'user', $4, 'voice', $5, CURRENT_TIMESTAMP)`,
		messageID,
		threadID,
		nextSequence,
		command.ClientMessageID,
		command.ConfirmedText,
	); err != nil {
		return agentvoice.Confirmation{}, mapError(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO agent_message_attachments (
    message_id,
    asset_id,
    position,
    created_at
) VALUES ($1, $2, 0, CURRENT_TIMESTAMP)`, messageID, draft.ID); err != nil {
		return agentvoice.Confirmation{}, mapError(err)
	}
	if err := mediapostgres.RetainInTransaction(
		ctx, tx, ownerID, sharedmedia.KindAudio, []string{draft.ID},
	); err != nil {
		return agentvoice.Confirmation{}, mapMediaError(err)
	}
	if err := advanceThread(
		ctx, tx, ownerID, threadID, command.ConfirmedText,
	); err != nil {
		return agentvoice.Confirmation{}, err
	}
	pendingRun, err := runpostgres.CreateInitialPendingRunInTransaction(
		ctx,
		tx,
		runpostgres.InitialPendingRunInTransaction{
			ID: runID, OwnerID: ownerID, ThreadID: threadID,
			InputMessageID: messageID, Configuration: command.Configuration,
		},
	)
	if err != nil {
		return agentvoice.Confirmation{}, mapRunError(err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE agent_voice_drafts
SET
    status = 'confirmed',
    confirmed_message_id = $2,
    confirmed_run_id = $3,
    confirmed_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND status = 'ready' AND version = $4`,
		draft.ID,
		messageID,
		runID,
		command.Version,
	)
	if err != nil {
		return agentvoice.Confirmation{}, mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentvoice.Confirmation{}, agentvoice.ErrConflict
	}
	draft, err = findDraftForUpdate(ctx, tx, ownerID, draft.ID)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	message, err := conversationpostgres.FindMessageInTransaction(
		ctx, tx, ownerID, threadID, messageID,
	)
	if err != nil {
		return agentvoice.Confirmation{}, mapConversationError(err)
	}
	attachment, err := findAudioAttachmentInTransaction(
		ctx, tx, ownerID, draft.ID,
	)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	return agentvoice.Confirmation{
		Draft: draft, Message: message, Attachment: attachment,
		Run: pendingRun, Created: true,
	}, nil
}

func replayConfirmation(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	command agentvoice.ConfirmDraftCommand,
	draft agentvoice.Draft,
) (agentvoice.Confirmation, error) {
	message, err := conversationpostgres.FindMessageInTransaction(
		ctx, tx, ownerID, draft.ThreadID, draft.ConfirmedMessageID,
	)
	if err != nil {
		return agentvoice.Confirmation{}, mapConversationError(err)
	}
	if message.Role != conversation.MessageRoleUser ||
		message.Modality != conversation.MessageModalityVoice ||
		message.ClientMessageID != command.ClientMessageID ||
		message.Content != command.ConfirmedText {
		return agentvoice.Confirmation{}, agentvoice.ErrIdempotencyConflict
	}
	pendingRun, err := runpostgres.FindRunInTransaction(
		ctx, tx, ownerID, draft.ConfirmedRunID,
	)
	if err != nil {
		return agentvoice.Confirmation{}, mapRunError(err)
	}
	attachment, err := findAudioAttachmentInTransaction(
		ctx, tx, ownerID, draft.ID,
	)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	return agentvoice.Confirmation{
		Draft: draft, Message: message, Attachment: attachment,
		Run: pendingRun, Created: false,
	}, nil
}

func (repository *Repository) DiscardDraft(
	ctx context.Context,
	ownerID string,
	draftID string,
) error {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(draftID) {
		return agentvoice.ErrNotFound
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return agentvoice.ErrRepository
	}
	defer rollback(tx)
	if err := lockActiveOwner(ctx, tx, ownerID); err != nil {
		return err
	}
	threadID, err := draftThreadID(ctx, tx, ownerID, draftID)
	if err != nil {
		return err
	}
	if _, err := lockOwnedThread(ctx, tx, ownerID, threadID); err != nil {
		return err
	}
	asset, err := mediapostgres.LockOwnedInTransaction(
		ctx, tx, ownerID, draftID, sharedmedia.KindAudio,
	)
	if err != nil {
		return mapMediaError(err)
	}
	if asset.Status != sharedmedia.StatusReady {
		return agentvoice.ErrConflict
	}
	tag, err := tx.Exec(ctx, `
DELETE FROM agent_voice_drafts
WHERE id = $1 AND status <> 'confirmed'`, draftID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentvoice.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.ErrRepository
	}
	return nil
}

func (repository *Repository) DetachAudio(
	ctx context.Context,
	ownerID string,
	audioID string,
) error {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(audioID) {
		return agentvoice.ErrNotFound
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return agentvoice.ErrRepository
	}
	defer rollback(tx)
	if err := lockActiveOwner(ctx, tx, ownerID); err != nil {
		return err
	}
	var threadID string
	if err := tx.QueryRow(ctx, `
SELECT message.thread_id::text
FROM agent_message_attachments AS attachment
JOIN agent_messages AS message ON message.id = attachment.message_id
JOIN agent_threads AS thread ON thread.id = message.thread_id
WHERE attachment.asset_id = $1
  AND thread.user_id = $2
  AND thread.deleted_at IS NULL`, audioID, ownerID).Scan(&threadID); err != nil {
		return mapError(err)
	}
	if _, err := lockOwnedThread(ctx, tx, ownerID, threadID); err != nil {
		return err
	}
	asset, err := mediapostgres.LockOwnedInTransaction(
		ctx, tx, ownerID, audioID, sharedmedia.KindAudio,
	)
	if err != nil {
		return mapMediaError(err)
	}
	if asset.Status != sharedmedia.StatusReady {
		return agentvoice.ErrConflict
	}
	tag, err := tx.Exec(ctx, `
DELETE FROM agent_message_attachments AS attachment
USING agent_messages AS message, agent_threads AS thread
WHERE attachment.asset_id = $1
  AND message.id = attachment.message_id
  AND thread.id = message.thread_id
  AND thread.user_id = $2
  AND thread.id = $3`, audioID, ownerID, threadID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentvoice.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM agent_voice_drafts WHERE id = $1`, audioID); err != nil {
		return mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.ErrRepository
	}
	return nil
}

func advanceThread(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	content string,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    title = COALESCE(title, NULLIF($3, '')),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		threadID,
		ownerID,
		conversation.DeriveThreadTitle(content),
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentvoice.ErrNotFound
	}
	return nil
}

func mapRunError(err error) error {
	switch {
	case errors.Is(err, agentrun.ErrInvalidRequest):
		return agentvoice.ErrInvalidRequest
	case errors.Is(err, agentrun.ErrNotFound):
		return agentvoice.ErrNotFound
	case errors.Is(err, agentrun.ErrConflict):
		return agentvoice.ErrConflict
	case errors.Is(err, agentrun.ErrIdempotencyConflict):
		return agentvoice.ErrIdempotencyConflict
	default:
		return agentvoice.ErrRepository
	}
}
