package postgres

import (
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5/pgtype"
)

const agentMessageWithAudioSelectColumns = `
    message.id::text,
    message.owner_user_id::text,
    message.thread_id::text,
    message.sequence_no,
    message.role,
    COALESCE(message.client_message_id, ''),
    COALESCE(message.produced_by_run_id::text, ''),
    message.modality,
    message.content,
    message.created_at,
    audio.audio_id::text,
    audio.candidate_id::text,
    audio.object_key,
    audio.content_type,
    audio.size_bytes,
    audio.checksum_sha256,
    audio.duration_ns,
    audio.sample_rate,
    audio.etag,
    audio.status,
    audio.created_at,
    audio.updated_at,
    audio.deleted_at`

func scanMessageWithAudio(row rowScanner) (conversation.Message, error) {
	var message conversation.Message
	var role string
	var modality string
	var audioID pgtype.Text
	var candidateID pgtype.Text
	var objectKey pgtype.Text
	var contentType pgtype.Text
	var size pgtype.Int8
	var checksum pgtype.Text
	var duration pgtype.Int8
	var sampleRate pgtype.Int4
	var etag pgtype.Text
	var status pgtype.Text
	var audioCreatedAt pgtype.Timestamptz
	var audioUpdatedAt pgtype.Timestamptz
	var audioDeletedAt pgtype.Timestamptz
	err := row.Scan(
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
		&candidateID,
		&objectKey,
		&contentType,
		&size,
		&checksum,
		&duration,
		&sampleRate,
		&etag,
		&status,
		&audioCreatedAt,
		&audioUpdatedAt,
		&audioDeletedAt,
	)
	if err != nil {
		return conversation.Message{}, err
	}
	message.Role = conversation.MessageRole(role)
	message.Modality = conversation.MessageModality(modality)
	if !audioID.Valid {
		return message, nil
	}
	if !candidateID.Valid || !objectKey.Valid || !contentType.Valid ||
		!size.Valid || !checksum.Valid || !duration.Valid ||
		!sampleRate.Valid || !etag.Valid || !status.Valid ||
		!audioCreatedAt.Valid || !audioUpdatedAt.Valid {
		return conversation.Message{}, conversation.ErrRepository
	}
	message.Audio = &conversation.MessageAudio{
		ID:             audioID.String,
		OwnerID:        message.OwnerID,
		ThreadID:       message.ThreadID,
		MessageID:      message.ID,
		CandidateID:    candidateID.String,
		ObjectKey:      objectKey.String,
		ContentType:    contentType.String,
		Size:           size.Int64,
		ChecksumSHA256: checksum.String,
		Duration:       time.Duration(duration.Int64),
		SampleRate:     int(sampleRate.Int32),
		ETag:           etag.String,
		Status:         conversation.MessageAudioStatus(status.String),
		CreatedAt:      audioCreatedAt.Time,
		UpdatedAt:      audioUpdatedAt.Time,
	}
	if audioDeletedAt.Valid {
		message.Audio.DeletedAt = audioDeletedAt.Time
	}
	return message, nil
}
