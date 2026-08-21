package postgres

import (
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5/pgtype"
)

const agentMessageWithAttachmentSelectColumns = `
    message.id::text,
    thread.user_id::text,
    message.thread_id::text,
    message.sequence_no,
    message.role,
    COALESCE(message.client_message_id, ''),
    COALESCE(message.produced_by_run_id::text, ''),
    message.modality,
    message.content,
    message.created_at,
    audio.id::text,
    audio.content_type,
    audio.size_bytes,
    audio.duration_ns,
    audio_attachment.created_at`

const audioAttachmentJoin = `
LEFT JOIN (
    agent_message_attachments AS audio_attachment
    JOIN media_assets AS audio
      ON audio.id = audio_attachment.asset_id
     AND audio.kind = 'audio'
     AND audio.status = 'ready'
) ON audio_attachment.message_id = message.id`

func scanMessageWithAttachment(row rowScanner) (conversation.Message, error) {
	var message conversation.Message
	var role string
	var modality string
	var audioID pgtype.Text
	var contentType pgtype.Text
	var size pgtype.Int8
	var duration pgtype.Int8
	var attachedAt pgtype.Timestamptz
	if err := row.Scan(
		&message.ID,
		&message.OwnerID,
		&message.ThreadID,
		&message.Sequence,
		&role,
		&message.ClientMessageID,
		&message.ProducedByRunID,
		&modality,
		&message.Content,
		&message.CreatedAt,
		&audioID,
		&contentType,
		&size,
		&duration,
		&attachedAt,
	); err != nil {
		return conversation.Message{}, err
	}
	message.Role = conversation.MessageRole(role)
	message.Modality = conversation.MessageModality(modality)
	if !audioID.Valid {
		return message, nil
	}
	if !contentType.Valid || !size.Valid || !duration.Valid || !attachedAt.Valid {
		return conversation.Message{}, conversation.ErrRepository
	}
	message.Audio = &conversation.AudioAttachment{
		ID: audioID.String, MessageID: message.ID,
		ContentType: contentType.String, Size: size.Int64,
		Duration: time.Duration(duration.Int64), CreatedAt: attachedAt.Time,
	}
	return message, nil
}
