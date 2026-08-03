package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const voiceCandidateSelectColumns = `
    candidate_id::text,
    owner_user_id::text,
    thread_id::text,
    upload_request_id,
    object_key,
    content_type,
    size_bytes,
    checksum_sha256,
    duration_ns,
    sample_rate,
    etag,
    upload_lease_until,
    upload_fencing_token,
    status,
    asr_attempt,
    candidate_version,
    asr_lease_until,
    asr_fencing_token,
    COALESCE(asr_request_id, ''),
    COALESCE(asr_provider, ''),
    COALESCE(asr_model, ''),
    COALESCE(asr_candidate_text, ''),
    COALESCE(asr_language, ''),
    COALESCE(asr_emotion, ''),
    COALESCE(asr_finish_reason, ''),
    COALESCE(failure_kind, ''),
    failure_retryable,
    expires_at,
    COALESCE(confirmed_message_id::text, ''),
    COALESCE(confirmed_run_id::text, ''),
    COALESCE(message_audio_id::text, ''),
    created_at,
    updated_at,
    confirmed_at,
    deleted_at`

const messageAudioSelectColumns = `
    audio_id::text,
    owner_user_id::text,
    thread_id::text,
    message_id::text,
    candidate_id::text,
    object_key,
    content_type,
    size_bytes,
    checksum_sha256,
    duration_ns,
    sample_rate,
    etag,
    status,
    created_at,
    updated_at,
    deleted_at`

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

func (r *PostgresStore) StageCandidate(
	ctx context.Context,
	command agentvoice.StageCandidateCommand,
) (agentvoice.CandidateStage, error) {
	candidate := command.Candidate
	if !candidate.ValidNew() {
		return agentvoice.CandidateStage{}, agentvoice.ErrInvalidRequest
	}
	inserted, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
INSERT INTO agent_voice_candidates (
    candidate_id,
    owner_user_id,
    thread_id,
    upload_request_id,
    object_key,
    content_type,
    size_bytes,
    checksum_sha256,
    duration_ns,
    sample_rate,
    etag,
    status,
    expires_at,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '', 'staged',
    CURRENT_TIMESTAMP + ($11::bigint * interval '1 microsecond'),
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
)
ON CONFLICT (owner_user_id, thread_id, upload_request_id) DO NOTHING
RETURNING `+voiceCandidateSelectColumns,
		candidate.ID,
		candidate.OwnerID,
		candidate.ThreadID,
		candidate.UploadRequestID,
		candidate.ObjectKey,
		candidate.ContentType,
		candidate.Size,
		candidate.ChecksumSHA256,
		int64(candidate.Duration),
		candidate.SampleRate,
		candidate.ExpiresAt.Sub(candidate.CreatedAt).Microseconds(),
	))
	if err == nil {
		return agentvoice.CandidateStage{Candidate: inserted, Created: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.CandidateStage{}, mapVoicePostgresError(err)
	}
	existing, err := r.findCandidateByUpload(
		ctx,
		candidate.OwnerID,
		candidate.ThreadID,
		candidate.UploadRequestID,
	)
	if err != nil {
		return agentvoice.CandidateStage{}, err
	}
	return agentvoice.CandidateStage{Candidate: existing, Created: false}, nil
}

func (r *PostgresStore) ClaimUpload(
	ctx context.Context,
	ownerID string,
	candidateID string,
	leaseDuration time.Duration,
) (agentvoice.UploadClaim, bool, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) ||
		leaseDuration <= 0 || leaseDuration > 10*time.Minute {
		return agentvoice.UploadClaim{}, false, agentvoice.ErrInvalidRequest
	}
	candidate, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    upload_lease_until =
        transaction_timestamp() + ($3::bigint * interval '1 microsecond'),
    upload_fencing_token = upload_fencing_token + 1,
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'staged'
  AND etag = ''
  AND (
      upload_lease_until IS NULL
      OR upload_lease_until <= transaction_timestamp()
  )
  AND EXISTS (
      SELECT 1
      FROM identity_users
      WHERE id = $2
        AND account_status = 'active'
  )
RETURNING `+voiceCandidateSelectColumns,
		candidateID,
		ownerID,
		leaseDuration.Microseconds(),
	))
	if err == nil {
		return agentvoice.UploadClaim{
			Candidate:      candidate,
			FencingToken:   candidate.UploadFencingToken,
			LeaseExpiresAt: candidate.UploadLeaseUntil,
		}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.UploadClaim{}, false, mapVoicePostgresError(err)
	}
	current, findErr := r.FindCandidate(ctx, ownerID, candidateID)
	if findErr != nil {
		return agentvoice.UploadClaim{}, false, findErr
	}
	return agentvoice.UploadClaim{
		Candidate:      current,
		FencingToken:   current.UploadFencingToken,
		LeaseExpiresAt: current.UploadLeaseUntil,
	}, false, nil
}

func (r *PostgresStore) CommitUpload(
	ctx context.Context,
	ownerID string,
	candidateID string,
	fencingToken uint64,
	etag string,
) (agentvoice.Candidate, error) {
	etag = strings.TrimSpace(etag)
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) ||
		fencingToken == 0 || fencingToken > uint64(1<<63-1) ||
		etag == "" || len(etag) > 512 {
		return agentvoice.Candidate{}, agentvoice.ErrInvalidRequest
	}
	candidate, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    etag = $4,
    upload_lease_until = NULL,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'staged'
  AND etag = ''
  AND upload_fencing_token = $3
  AND upload_lease_until > transaction_timestamp()
RETURNING `+voiceCandidateSelectColumns,
		candidateID,
		ownerID,
		int64(fencingToken),
		etag,
	))
	if err == nil {
		return candidate, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	current, findErr := r.FindCandidate(ctx, ownerID, candidateID)
	if findErr != nil {
		return agentvoice.Candidate{}, findErr
	}
	if current.ETag == etag &&
		current.ETag != "" &&
		current.UploadFencingToken == fencingToken {
		return current, nil
	}
	return agentvoice.Candidate{}, agentvoice.ErrConflict
}

func (r *PostgresStore) FindCandidate(
	ctx context.Context,
	ownerID string,
	candidateID string,
) (agentvoice.Candidate, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) {
		return agentvoice.Candidate{}, agentvoice.ErrNotFound
	}
	candidate, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
SELECT `+voiceCandidateSelectColumns+`
FROM agent_voice_candidates
WHERE candidate_id = $1
  AND owner_user_id = $2`,
		candidateID,
		ownerID,
	))
	if err != nil {
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	return candidate, nil
}

func (r *PostgresStore) findCandidateByUpload(
	ctx context.Context,
	ownerID string,
	threadID string,
	uploadRequestID string,
) (agentvoice.Candidate, error) {
	candidate, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
SELECT `+voiceCandidateSelectColumns+`
FROM agent_voice_candidates
WHERE owner_user_id = $1
  AND thread_id = $2
  AND upload_request_id = $3`,
		ownerID,
		threadID,
		uploadRequestID,
	))
	if err != nil {
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	return candidate, nil
}

func (r *PostgresStore) ClaimTranscription(
	ctx context.Context,
	ownerID string,
	candidateID string,
	leaseDuration time.Duration,
) (agentvoice.TranscriptionClaim, bool, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) ||
		leaseDuration <= 0 || leaseDuration > 10*time.Minute {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrInvalidRequest
	}
	candidate, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'transcribing',
    asr_attempt = asr_attempt + 1,
    candidate_version = candidate_version + 1,
    asr_lease_until =
        transaction_timestamp() + ($3::bigint * interval '1 microsecond'),
    asr_fencing_token = asr_fencing_token + 1,
    asr_request_id = NULL,
    asr_provider = NULL,
    asr_model = NULL,
    asr_candidate_text = NULL,
    asr_language = NULL,
    asr_emotion = NULL,
    asr_finish_reason = NULL,
    failure_kind = NULL,
    failure_retryable = NULL,
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND etag <> ''
  AND (
      status = 'staged'
      OR (status = 'failed' AND failure_retryable)
      OR (
          status = 'transcribing'
          AND asr_lease_until <= transaction_timestamp()
      )
  )
RETURNING `+voiceCandidateSelectColumns,
		candidateID,
		ownerID,
		leaseDuration.Microseconds(),
	))
	if err == nil {
		return agentvoice.TranscriptionClaim{
			Candidate:      candidate,
			FencingToken:   candidate.ASRFencingToken,
			LeaseExpiresAt: candidate.ASRLeaseUntil,
		}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.TranscriptionClaim{}, false, mapVoicePostgresError(err)
	}
	current, findErr := r.FindCandidate(ctx, ownerID, candidateID)
	if findErr != nil {
		return agentvoice.TranscriptionClaim{}, false, findErr
	}
	return agentvoice.TranscriptionClaim{
		Candidate:      current,
		FencingToken:   current.ASRFencingToken,
		LeaseExpiresAt: current.ASRLeaseUntil,
	}, false, nil
}

func (r *PostgresStore) CompleteTranscription(
	ctx context.Context,
	ownerID string,
	candidateID string,
	fencingToken uint64,
	result ai.TranscriptionResult,
) (agentvoice.Candidate, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) ||
		fencingToken == 0 || !agentvoice.ValidTranscription(result) {
		return agentvoice.Candidate{}, agentvoice.ErrInvalidRequest
	}
	candidate, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'candidate_ready',
    asr_lease_until = NULL,
    asr_request_id = $4,
    asr_provider = $5,
    asr_model = $6,
    asr_candidate_text = $7,
    asr_language = NULLIF($8, ''),
    asr_emotion = NULLIF($9, ''),
    asr_finish_reason = NULLIF($10, ''),
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'transcribing'
  AND asr_fencing_token = $3
  AND asr_lease_until > transaction_timestamp()
RETURNING `+voiceCandidateSelectColumns,
		candidateID,
		ownerID,
		int64(fencingToken),
		result.ID,
		result.Provider,
		result.Model,
		strings.TrimSpace(result.Transcript),
		result.Language,
		result.Emotion,
		result.FinishReason,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agentvoice.Candidate{}, agentvoice.ErrConflict
		}
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	return candidate, nil
}

func (r *PostgresStore) FailTranscription(
	ctx context.Context,
	ownerID string,
	candidateID string,
	fencingToken uint64,
	failureKind string,
	retryable bool,
) (agentvoice.Candidate, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) ||
		fencingToken == 0 ||
		!agentvoice.ValidProviderID(failureKind) {
		return agentvoice.Candidate{}, agentvoice.ErrInvalidRequest
	}
	candidate, err := scanVoiceCandidate(r.database.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'failed',
    asr_lease_until = NULL,
    failure_kind = $4,
    failure_retryable = $5,
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'transcribing'
  AND asr_fencing_token = $3
  AND asr_lease_until > transaction_timestamp()
RETURNING `+voiceCandidateSelectColumns,
		candidateID,
		ownerID,
		int64(fencingToken),
		failureKind,
		retryable,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agentvoice.Candidate{}, agentvoice.ErrConflict
		}
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	return candidate, nil
}

func (r *PostgresStore) ConfirmCandidate(
	ctx context.Context,
	ownerID string,
	command agentvoice.ConfirmCandidateCommand,
) (agentvoice.Confirmation, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(command.CandidateID) ||
		command.CandidateVersion < 1 ||
		!agentvoice.ValidClientMessageID(command.ClientMessageID) ||
		!agentvoice.ValidMessageContent(command.ConfirmedText) ||
		!agentrun.ValidConfiguration(command.Configuration) {
		return agentvoice.Confirmation{}, agentvoice.ErrInvalidRequest
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	defer rollback(tx)

	candidate, err := findVoiceCandidateForUpdate(
		ctx,
		tx,
		ownerID,
		command.CandidateID,
	)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	if candidate.Status == agentvoice.StatusConfirmed {
		confirmation, loadErr := loadVoiceConfirmation(
			ctx,
			tx,
			candidate,
		)
		if loadErr != nil {
			return agentvoice.Confirmation{}, loadErr
		}
		if candidate.CandidateVersion != command.CandidateVersion {
			return agentvoice.Confirmation{}, agentvoice.ErrCandidateStale
		}
		if confirmation.Message.ClientMessageID != command.ClientMessageID ||
			confirmation.Message.Content != command.ConfirmedText {
			return agentvoice.Confirmation{}, agentvoice.ErrIdempotencyConflict
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return agentvoice.Confirmation{}, agentvoice.ErrRepository
		}
		confirmation.Created = false
		return confirmation, nil
	}
	if candidate.Status != agentvoice.StatusReady {
		if candidate.Status == agentvoice.StatusTranscribing ||
			candidate.Status == agentvoice.StatusConfirming {
			return agentvoice.Confirmation{}, agentvoice.ErrCandidateProcessing
		}
		return agentvoice.Confirmation{}, agentvoice.ErrConflict
	}
	if candidate.CandidateVersion != command.CandidateVersion {
		return agentvoice.Confirmation{}, agentvoice.ErrCandidateStale
	}

	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		candidate.ThreadID,
		ownerID,
	).Scan(&nextSequence); err != nil {
		return agentvoice.Confirmation{}, mapVoicePostgresError(err)
	}
	if existing, found, findErr := findMessageByClientID(
		ctx,
		tx,
		ownerID,
		candidate.ThreadID,
		command.ClientMessageID,
	); findErr != nil {
		return agentvoice.Confirmation{}, findErr
	} else if found {
		if existing.Content == command.ConfirmedText &&
			existing.Modality == conversation.MessageModalityVoice {
			return agentvoice.Confirmation{}, agentvoice.ErrConflict
		}
		return agentvoice.Confirmation{}, agentvoice.ErrIdempotencyConflict
	}

	messageID, audioID, evidenceID, runID, err := r.newConfirmationIDs()
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	message, err := insertConfirmedVoiceInputMessage(
		ctx,
		tx,
		messageID,
		candidate,
		nextSequence,
		command.ClientMessageID,
		command.ConfirmedText,
	)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	audio, err := insertMessageAudio(ctx, tx, audioID, messageID, candidate)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	evidence, err := insertTranscriptEvidence(
		ctx,
		tx,
		evidenceID,
		messageID,
		candidate,
		command.ConfirmedText,
	)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	run, err := insertPendingRun(
		ctx,
		tx,
		runID,
		ownerID,
		candidate.ThreadID,
		messageID,
		1,
		"",
		"",
		command.Configuration,
	)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2`,
		candidate.ThreadID,
		ownerID,
	); err != nil {
		return agentvoice.Confirmation{}, mapVoicePostgresError(err)
	}
	candidate, err = scanVoiceCandidate(tx.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'confirmed',
    confirmed_message_id = $3,
    confirmed_run_id = $4,
    message_audio_id = $5,
    confirmed_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'candidate_ready'
  AND candidate_version = $6
RETURNING `+voiceCandidateSelectColumns,
		candidate.ID,
		ownerID,
		messageID,
		runID,
		audioID,
		command.CandidateVersion,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agentvoice.Confirmation{}, agentvoice.ErrConflict
		}
		return agentvoice.Confirmation{}, mapVoicePostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	message.Audio = &audio
	return agentvoice.Confirmation{
		Candidate: candidate,
		Evidence:  evidence,
		Message:   message,
		Audio:     audio,
		Run:       run,
		Created:   true,
	}, nil
}

func (r *PostgresStore) newConfirmationIDs() (
	string,
	string,
	string,
	string,
	error,
) {
	values := make([]string, 4)
	for index := range values {
		value, err := r.ids.NewID()
		if err != nil || !agentvoice.ValidUUID(value) {
			return "", "", "", "", agentvoice.ErrRepository
		}
		values[index] = value
	}
	return values[0], values[1], values[2], values[3], nil
}

func insertConfirmedVoiceInputMessage(
	ctx context.Context,
	tx pgx.Tx,
	messageID string,
	candidate agentvoice.Candidate,
	sequence int64,
	clientMessageID string,
	content string,
) (conversation.Message, error) {
	var result conversation.Message
	var role string
	var modality string
	err := tx.QueryRow(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at
) VALUES ($1, $2, $3, $4, 'user', $5, 'voice', $6, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at`,
		messageID,
		candidate.OwnerID,
		candidate.ThreadID,
		sequence,
		clientMessageID,
		content,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.Sequence,
		&role,
		&result.ClientMessageID,
		&modality,
		&result.Content,
		&result.CreatedAt,
	)
	if err != nil {
		return conversation.Message{}, mapVoicePostgresError(err)
	}
	result.Role = conversation.MessageRole(role)
	result.Modality = conversation.MessageModality(modality)
	return result, nil
}

func insertMessageAudio(
	ctx context.Context,
	tx pgx.Tx,
	audioID string,
	messageID string,
	candidate agentvoice.Candidate,
) (conversation.MessageAudio, error) {
	audio, err := scanMessageAudio(tx.QueryRow(ctx, `
INSERT INTO agent_message_audios (
    audio_id,
    owner_user_id,
    thread_id,
    message_id,
    candidate_id,
    object_key,
    content_type,
    size_bytes,
    checksum_sha256,
    duration_ns,
    sample_rate,
    etag,
    status,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
    'readable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
RETURNING `+messageAudioSelectColumns,
		audioID,
		candidate.OwnerID,
		candidate.ThreadID,
		messageID,
		candidate.ID,
		candidate.ObjectKey,
		candidate.ContentType,
		candidate.Size,
		candidate.ChecksumSHA256,
		int64(candidate.Duration),
		candidate.SampleRate,
		candidate.ETag,
	))
	if err != nil {
		return conversation.MessageAudio{}, mapVoicePostgresError(err)
	}
	return audio, nil
}

func insertTranscriptEvidence(
	ctx context.Context,
	tx pgx.Tx,
	evidenceID string,
	messageID string,
	candidate agentvoice.Candidate,
	confirmedText string,
) (agentvoice.TranscriptEvidence, error) {
	var result agentvoice.TranscriptEvidence
	err := tx.QueryRow(ctx, `
INSERT INTO agent_voice_transcript_evidence (
    evidence_id,
    owner_user_id,
    thread_id,
    candidate_id,
    candidate_version,
    message_id,
    asr_request_id,
    asr_provider,
    asr_model,
    asr_candidate_text,
    confirmed_text,
    language,
    emotion,
    finish_reason,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
    NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''),
    CURRENT_TIMESTAMP
)
RETURNING
    evidence_id::text,
    owner_user_id::text,
    thread_id::text,
    candidate_id::text,
    candidate_version,
    message_id::text,
    asr_request_id,
    asr_provider,
    asr_model,
    asr_candidate_text,
    confirmed_text,
    COALESCE(language, ''),
    COALESCE(emotion, ''),
    COALESCE(finish_reason, ''),
    created_at`,
		evidenceID,
		candidate.OwnerID,
		candidate.ThreadID,
		candidate.ID,
		candidate.CandidateVersion,
		messageID,
		candidate.ASRRequestID,
		candidate.ASRProvider,
		candidate.ASRModel,
		candidate.ASRCandidateText,
		confirmedText,
		candidate.ASRLanguage,
		candidate.ASREmotion,
		candidate.ASRFinishReason,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.CandidateID,
		&result.CandidateVersion,
		&result.MessageID,
		&result.ASRRequestID,
		&result.ASRProvider,
		&result.ASRModel,
		&result.ASRCandidateText,
		&result.ConfirmedText,
		&result.Language,
		&result.Emotion,
		&result.FinishReason,
		&result.CreatedAt,
	)
	if err != nil {
		return agentvoice.TranscriptEvidence{}, mapVoicePostgresError(err)
	}
	return result, nil
}

func (r *PostgresStore) FindMessageAudio(
	ctx context.Context,
	ownerID string,
	audioID string,
) (conversation.MessageAudio, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(audioID) {
		return conversation.MessageAudio{}, agentvoice.ErrNotFound
	}
	audio, err := scanMessageAudio(r.database.QueryRow(ctx, `
SELECT `+messageAudioSelectColumns+`
FROM agent_message_audios
WHERE audio_id = $1
  AND owner_user_id = $2`,
		audioID,
		ownerID,
	))
	if err != nil {
		return conversation.MessageAudio{}, mapVoicePostgresError(err)
	}
	return audio, nil
}

func (r *PostgresStore) FindMessageByID(
	ctx context.Context,
	ownerID string,
	messageID string,
) (conversation.Message, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(messageID) {
		return conversation.Message{}, agentvoice.ErrNotFound
	}
	result, err := scanMessageWithAudio(r.database.QueryRow(ctx, `
SELECT `+agentMessageWithAudioSelectColumns+`
FROM agent_messages AS message
LEFT JOIN agent_message_audios AS audio
  ON audio.message_id = message.id
 AND audio.owner_user_id = message.owner_user_id
 AND audio.thread_id = message.thread_id
WHERE message.id = $1
  AND message.owner_user_id = $2`,
		messageID,
		ownerID,
	))
	if err != nil {
		return conversation.Message{}, mapVoicePostgresError(err)
	}
	return result, nil
}

func (r *PostgresStore) BeginCandidateDeletion(
	ctx context.Context,
	ownerID string,
	candidateID string,
) (agentvoice.Candidate, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) {
		return agentvoice.Candidate{}, agentvoice.ErrNotFound
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentvoice.Candidate{}, agentvoice.ErrRepository
	}
	defer rollback(tx)
	candidate, err := findVoiceCandidateForUpdate(
		ctx,
		tx,
		ownerID,
		candidateID,
	)
	if err != nil {
		return agentvoice.Candidate{}, err
	}
	if candidate.Status == agentvoice.StatusDeleted {
		if err := tx.Commit(ctx); err != nil {
			return agentvoice.Candidate{}, agentvoice.ErrRepository
		}
		return candidate, nil
	}
	if candidate.Status == agentvoice.StatusConfirming {
		return agentvoice.Candidate{}, agentvoice.ErrCandidateProcessing
	}
	if candidate.MessageAudioID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE agent_message_audios
SET
    status = CASE WHEN status = 'deleted' THEN status ELSE 'deleting' END,
    cleanup_lease_until = NULL,
    cleanup_fencing_token = cleanup_fencing_token + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE audio_id = $1
  AND owner_user_id = $2`,
			candidate.MessageAudioID,
			ownerID,
		); err != nil {
			return agentvoice.Candidate{}, mapVoicePostgresError(err)
		}
	}
	candidate, err = scanVoiceCandidate(tx.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'deleting',
    upload_lease_until = NULL,
    upload_fencing_token = upload_fencing_token + 1,
    asr_lease_until = NULL,
    asr_fencing_token = asr_fencing_token + 1,
    cleanup_lease_until = NULL,
    cleanup_fencing_token = cleanup_fencing_token + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND (
      upload_lease_until IS NULL
      OR upload_lease_until <= transaction_timestamp()
  )
RETURNING `+voiceCandidateSelectColumns,
		candidateID,
		ownerID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.Candidate{}, agentvoice.ErrCandidateProcessing
	}
	if err != nil {
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.Candidate{}, agentvoice.ErrRepository
	}
	return candidate, nil
}

func (r *PostgresStore) FinishCandidateDeletion(
	ctx context.Context,
	ownerID string,
	candidateID string,
) (agentvoice.Candidate, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(candidateID) {
		return agentvoice.Candidate{}, agentvoice.ErrNotFound
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentvoice.Candidate{}, agentvoice.ErrRepository
	}
	defer rollback(tx)
	candidate, err := findVoiceCandidateForUpdate(
		ctx,
		tx,
		ownerID,
		candidateID,
	)
	if err != nil {
		return agentvoice.Candidate{}, err
	}
	if candidate.Status == agentvoice.StatusDeleted {
		if err := tx.Commit(ctx); err != nil {
			return agentvoice.Candidate{}, agentvoice.ErrRepository
		}
		return candidate, nil
	}
	if candidate.Status != agentvoice.StatusDeleting {
		return agentvoice.Candidate{}, agentvoice.ErrConflict
	}
	if candidate.MessageAudioID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE agent_message_audios
SET
    status = 'deleted',
    cleanup_lease_until = NULL,
    deleted_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE audio_id = $1
  AND owner_user_id = $2
  AND status <> 'deleted'`,
			candidate.MessageAudioID,
			ownerID,
		); err != nil {
			return agentvoice.Candidate{}, mapVoicePostgresError(err)
		}
	}
	candidate, err = scanVoiceCandidate(tx.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'deleted',
    cleanup_lease_until = NULL,
    deleted_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'deleting'
RETURNING `+voiceCandidateSelectColumns,
		candidateID,
		ownerID,
	))
	if err != nil {
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.Candidate{}, agentvoice.ErrRepository
	}
	return candidate, nil
}

func (r *PostgresStore) BeginMessageAudioDeletion(
	ctx context.Context,
	ownerID string,
	audioID string,
) (conversation.MessageAudio, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(audioID) {
		return conversation.MessageAudio{}, agentvoice.ErrNotFound
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return conversation.MessageAudio{}, agentvoice.ErrRepository
	}
	defer rollback(tx)
	candidateID, err := findMessageAudioCandidateID(
		ctx,
		tx,
		ownerID,
		audioID,
	)
	if err != nil {
		return conversation.MessageAudio{}, err
	}
	if _, err := findVoiceCandidateForUpdate(
		ctx,
		tx,
		ownerID,
		candidateID,
	); err != nil {
		return conversation.MessageAudio{}, err
	}
	audio, err := findMessageAudioForUpdate(ctx, tx, ownerID, audioID)
	if err != nil {
		return conversation.MessageAudio{}, err
	}
	if audio.CandidateID != candidateID {
		return conversation.MessageAudio{}, agentvoice.ErrRepository
	}
	if audio.Status == conversation.MessageAudioDeleted {
		if err := tx.Commit(ctx); err != nil {
			return conversation.MessageAudio{}, agentvoice.ErrRepository
		}
		return audio, nil
	}
	audio, err = scanMessageAudio(tx.QueryRow(ctx, `
UPDATE agent_message_audios
SET
    status = 'deleting',
    cleanup_lease_until = NULL,
    cleanup_fencing_token = cleanup_fencing_token + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE audio_id = $1
  AND owner_user_id = $2
RETURNING `+messageAudioSelectColumns,
		audioID,
		ownerID,
	))
	if err != nil {
		return conversation.MessageAudio{}, mapVoicePostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'deleting',
    upload_lease_until = NULL,
    upload_fencing_token = upload_fencing_token + 1,
    cleanup_lease_until = NULL,
    cleanup_fencing_token = cleanup_fencing_token + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status <> 'deleted'`,
		audio.CandidateID,
		ownerID,
	); err != nil {
		return conversation.MessageAudio{}, mapVoicePostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.MessageAudio{}, agentvoice.ErrRepository
	}
	return audio, nil
}

func (r *PostgresStore) FinishMessageAudioDeletion(
	ctx context.Context,
	ownerID string,
	audioID string,
) (conversation.MessageAudio, error) {
	if !agentvoice.ValidUUID(ownerID) || !agentvoice.ValidUUID(audioID) {
		return conversation.MessageAudio{}, agentvoice.ErrNotFound
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return conversation.MessageAudio{}, agentvoice.ErrRepository
	}
	defer rollback(tx)
	candidateID, err := findMessageAudioCandidateID(
		ctx,
		tx,
		ownerID,
		audioID,
	)
	if err != nil {
		return conversation.MessageAudio{}, err
	}
	if _, err := findVoiceCandidateForUpdate(
		ctx,
		tx,
		ownerID,
		candidateID,
	); err != nil {
		return conversation.MessageAudio{}, err
	}
	audio, err := findMessageAudioForUpdate(ctx, tx, ownerID, audioID)
	if err != nil {
		return conversation.MessageAudio{}, err
	}
	if audio.CandidateID != candidateID {
		return conversation.MessageAudio{}, agentvoice.ErrRepository
	}
	if audio.Status == conversation.MessageAudioDeleted {
		if err := tx.Commit(ctx); err != nil {
			return conversation.MessageAudio{}, agentvoice.ErrRepository
		}
		return audio, nil
	}
	if audio.Status != conversation.MessageAudioDeleting {
		return conversation.MessageAudio{}, agentvoice.ErrConflict
	}
	audio, err = scanMessageAudio(tx.QueryRow(ctx, `
UPDATE agent_message_audios
SET
    status = 'deleted',
    cleanup_lease_until = NULL,
    deleted_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE audio_id = $1
  AND owner_user_id = $2
  AND status = 'deleting'
RETURNING `+messageAudioSelectColumns,
		audioID,
		ownerID,
	))
	if err != nil {
		return conversation.MessageAudio{}, mapVoicePostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'deleted',
    cleanup_lease_until = NULL,
    deleted_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'deleting'`,
		audio.CandidateID,
		ownerID,
	); err != nil {
		return conversation.MessageAudio{}, mapVoicePostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.MessageAudio{}, agentvoice.ErrRepository
	}
	return audio, nil
}

func (r *PostgresStore) ClaimCleanup(
	ctx context.Context,
	leaseDuration time.Duration,
	limit int,
) ([]agentvoice.CleanupClaim, error) {
	if leaseDuration <= 0 || leaseDuration > 10*time.Minute ||
		limit < 1 || limit > 100 {
		return nil, agentvoice.ErrInvalidRequest
	}
	rows, err := r.database.Query(ctx, `
WITH eligible AS (
    SELECT candidate.candidate_id
    FROM agent_voice_candidates AS candidate
    JOIN identity_users AS owner
      ON owner.id = candidate.owner_user_id
    WHERE (
        (
            candidate.status IN ('staged', 'candidate_ready', 'failed')
            AND candidate.expires_at <= transaction_timestamp()
        )
        OR (
            candidate.status = 'transcribing'
            AND candidate.expires_at <= transaction_timestamp()
            AND candidate.asr_lease_until <= transaction_timestamp()
        )
        OR candidate.status = 'deleting'
        OR (
            owner.account_status IN ('deleting', 'deleted')
            AND candidate.status <> 'deleted'
        )
    )
      AND (
          candidate.cleanup_lease_until IS NULL
          OR candidate.cleanup_lease_until <= transaction_timestamp()
      )
      AND (
          candidate.upload_lease_until IS NULL
          OR candidate.upload_lease_until <= transaction_timestamp()
      )
    ORDER BY
        CASE
            WHEN owner.account_status IN ('deleting', 'deleted') THEN 0
            WHEN candidate.status = 'deleting' THEN 1
            ELSE 2
        END,
        candidate.updated_at,
        candidate.candidate_id
    FOR UPDATE OF candidate SKIP LOCKED
    LIMIT $1
)
UPDATE agent_voice_candidates AS candidate
SET
    status = 'deleting',
    upload_lease_until = NULL,
    upload_fencing_token = upload_fencing_token + 1,
    asr_lease_until = NULL,
    asr_fencing_token = asr_fencing_token + 1,
    cleanup_lease_until =
        transaction_timestamp() + ($2::bigint * interval '1 microsecond'),
    cleanup_fencing_token = cleanup_fencing_token + 1,
    updated_at = GREATEST(
        transaction_timestamp(),
        candidate.updated_at + interval '1 microsecond'
    )
FROM eligible
WHERE candidate.candidate_id = eligible.candidate_id
RETURNING
    candidate.owner_user_id::text,
    candidate.candidate_id::text,
    COALESCE(candidate.message_audio_id::text, ''),
    candidate.object_key,
    candidate.cleanup_fencing_token,
    candidate.cleanup_lease_until`,
		limit,
		leaseDuration.Microseconds(),
	)
	if err != nil {
		return nil, agentvoice.ErrRepository
	}
	defer rows.Close()
	claims := make([]agentvoice.CleanupClaim, 0, limit)
	for rows.Next() {
		var claim agentvoice.CleanupClaim
		var token int64
		if err := rows.Scan(
			&claim.OwnerID,
			&claim.CandidateID,
			&claim.AudioID,
			&claim.ObjectKey,
			&token,
			&claim.LeaseExpiresAt,
		); err != nil || token <= 0 {
			return nil, agentvoice.ErrRepository
		}
		claim.Kind = agentvoice.CleanupCandidate
		claim.FencingToken = uint64(token)
		claims = append(claims, claim)
	}
	if rows.Err() != nil {
		return nil, agentvoice.ErrRepository
	}
	return claims, nil
}

func (r *PostgresStore) FinishCleanup(
	ctx context.Context,
	claim agentvoice.CleanupClaim,
) error {
	if !claim.Valid() || claim.Kind != agentvoice.CleanupCandidate {
		return agentvoice.ErrInvalidRequest
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentvoice.ErrRepository
	}
	defer rollback(tx)
	var audioID string
	err = tx.QueryRow(ctx, `
UPDATE agent_voice_candidates
SET
    status = 'deleted',
    cleanup_lease_until = NULL,
    deleted_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'deleting'
  AND cleanup_fencing_token = $3
  AND cleanup_lease_until > transaction_timestamp()
RETURNING COALESCE(message_audio_id::text, '')`,
		claim.CandidateID,
		claim.OwnerID,
		int64(claim.FencingToken),
	).Scan(&audioID)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.ErrConflict
	}
	if err != nil {
		return mapVoicePostgresError(err)
	}
	if audioID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE agent_message_audios
SET
    status = 'deleted',
    cleanup_lease_until = NULL,
    deleted_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE audio_id = $1
  AND owner_user_id = $2
  AND status <> 'deleted'`,
			audioID,
			claim.OwnerID,
		); err != nil {
			return mapVoicePostgresError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.ErrRepository
	}
	return nil
}

func (r *PostgresStore) ReleaseCleanup(
	ctx context.Context,
	claim agentvoice.CleanupClaim,
) error {
	if !claim.Valid() || claim.Kind != agentvoice.CleanupCandidate {
		return agentvoice.ErrInvalidRequest
	}
	command, err := r.database.Exec(ctx, `
UPDATE agent_voice_candidates
SET
    cleanup_lease_until = NULL,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + interval '1 microsecond'
    )
WHERE candidate_id = $1
  AND owner_user_id = $2
  AND status = 'deleting'
  AND cleanup_fencing_token = $3`,
		claim.CandidateID,
		claim.OwnerID,
		int64(claim.FencingToken),
	)
	if err != nil {
		return mapVoicePostgresError(err)
	}
	if command.RowsAffected() == 0 {
		current, findErr := r.FindCandidate(
			ctx,
			claim.OwnerID,
			claim.CandidateID,
		)
		if findErr == nil && current.Status == agentvoice.StatusDeleted {
			return nil
		}
		return agentvoice.ErrConflict
	}
	return nil
}

func findVoiceCandidateForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	candidateID string,
) (agentvoice.Candidate, error) {
	candidate, err := scanVoiceCandidate(tx.QueryRow(ctx, `
SELECT `+voiceCandidateSelectColumns+`
FROM agent_voice_candidates
WHERE candidate_id = $1
  AND owner_user_id = $2
FOR UPDATE`,
		candidateID,
		ownerID,
	))
	if err != nil {
		return agentvoice.Candidate{}, mapVoicePostgresError(err)
	}
	return candidate, nil
}

func findMessageAudioForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	audioID string,
) (conversation.MessageAudio, error) {
	audio, err := scanMessageAudio(tx.QueryRow(ctx, `
SELECT `+messageAudioSelectColumns+`
FROM agent_message_audios
WHERE audio_id = $1
  AND owner_user_id = $2
FOR UPDATE`,
		audioID,
		ownerID,
	))
	if err != nil {
		return conversation.MessageAudio{}, mapVoicePostgresError(err)
	}
	return audio, nil
}

func findMessageAudioCandidateID(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	audioID string,
) (string, error) {
	var candidateID string
	if err := tx.QueryRow(ctx, `
SELECT candidate_id::text
FROM agent_message_audios
WHERE audio_id = $1
  AND owner_user_id = $2`,
		audioID,
		ownerID,
	).Scan(&candidateID); err != nil {
		return "", mapVoicePostgresError(err)
	}
	return candidateID, nil
}

func loadVoiceConfirmation(
	ctx context.Context,
	tx pgx.Tx,
	candidate agentvoice.Candidate,
) (agentvoice.Confirmation, error) {
	if candidate.ConfirmedMessageID == "" ||
		candidate.ConfirmedRunID == "" ||
		candidate.MessageAudioID == "" {
		return agentvoice.Confirmation{}, agentvoice.ErrRepository
	}
	var message conversation.Message
	var role string
	var modality string
	err := tx.QueryRow(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    COALESCE(client_message_id, ''),
    COALESCE(produced_by_run_id::text, ''),
    modality,
    content,
    created_at
FROM agent_messages
WHERE id = $1
  AND owner_user_id = $2
  AND thread_id = $3`,
		candidate.ConfirmedMessageID,
		candidate.OwnerID,
		candidate.ThreadID,
	).Scan(
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
	)
	if err != nil {
		return agentvoice.Confirmation{}, mapVoicePostgresError(err)
	}
	message.Role = conversation.MessageRole(role)
	message.Modality = conversation.MessageModality(modality)
	audio, err := scanMessageAudio(tx.QueryRow(ctx, `
SELECT `+messageAudioSelectColumns+`
FROM agent_message_audios
WHERE audio_id = $1
  AND owner_user_id = $2`,
		candidate.MessageAudioID,
		candidate.OwnerID,
	))
	if err != nil {
		return agentvoice.Confirmation{}, mapVoicePostgresError(err)
	}
	evidence, err := findTranscriptEvidence(
		ctx,
		tx,
		candidate.OwnerID,
		candidate.ID,
		candidate.CandidateVersion,
	)
	if err != nil {
		return agentvoice.Confirmation{}, err
	}
	run, err := scanRun(tx.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs
WHERE id = $1
  AND owner_user_id = $2`,
		candidate.ConfirmedRunID,
		candidate.OwnerID,
	))
	if err != nil {
		return agentvoice.Confirmation{}, mapVoicePostgresError(err)
	}
	message.Audio = &audio
	return agentvoice.Confirmation{
		Candidate: candidate,
		Evidence:  evidence,
		Message:   message,
		Audio:     audio,
		Run:       run,
	}, nil
}

func findTranscriptEvidence(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	candidateID string,
	candidateVersion int64,
) (agentvoice.TranscriptEvidence, error) {
	var result agentvoice.TranscriptEvidence
	err := tx.QueryRow(ctx, `
SELECT
    evidence_id::text,
    owner_user_id::text,
    thread_id::text,
    candidate_id::text,
    candidate_version,
    message_id::text,
    asr_request_id,
    asr_provider,
    asr_model,
    asr_candidate_text,
    confirmed_text,
    COALESCE(language, ''),
    COALESCE(emotion, ''),
    COALESCE(finish_reason, ''),
    created_at
FROM agent_voice_transcript_evidence
WHERE owner_user_id = $1
  AND candidate_id = $2
  AND candidate_version = $3`,
		ownerID,
		candidateID,
		candidateVersion,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.CandidateID,
		&result.CandidateVersion,
		&result.MessageID,
		&result.ASRRequestID,
		&result.ASRProvider,
		&result.ASRModel,
		&result.ASRCandidateText,
		&result.ConfirmedText,
		&result.Language,
		&result.Emotion,
		&result.FinishReason,
		&result.CreatedAt,
	)
	if err != nil {
		return agentvoice.TranscriptEvidence{}, mapVoicePostgresError(err)
	}
	return result, nil
}

func scanVoiceCandidate(row rowScanner) (agentvoice.Candidate, error) {
	var candidate agentvoice.Candidate
	var status string
	var duration int64
	var uploadLease pgtype.Timestamptz
	var asrLease pgtype.Timestamptz
	var failureRetryable pgtype.Bool
	var confirmedAt pgtype.Timestamptz
	var deletedAt pgtype.Timestamptz
	var uploadFencingToken int64
	var asrFencingToken int64
	err := row.Scan(
		&candidate.ID,
		&candidate.OwnerID,
		&candidate.ThreadID,
		&candidate.UploadRequestID,
		&candidate.ObjectKey,
		&candidate.ContentType,
		&candidate.Size,
		&candidate.ChecksumSHA256,
		&duration,
		&candidate.SampleRate,
		&candidate.ETag,
		&uploadLease,
		&uploadFencingToken,
		&status,
		&candidate.ASRAttempt,
		&candidate.CandidateVersion,
		&asrLease,
		&asrFencingToken,
		&candidate.ASRRequestID,
		&candidate.ASRProvider,
		&candidate.ASRModel,
		&candidate.ASRCandidateText,
		&candidate.ASRLanguage,
		&candidate.ASREmotion,
		&candidate.ASRFinishReason,
		&candidate.FailureKind,
		&failureRetryable,
		&candidate.ExpiresAt,
		&candidate.ConfirmedMessageID,
		&candidate.ConfirmedRunID,
		&candidate.MessageAudioID,
		&candidate.CreatedAt,
		&candidate.UpdatedAt,
		&confirmedAt,
		&deletedAt,
	)
	if err != nil {
		return agentvoice.Candidate{}, err
	}
	if duration <= 0 ||
		uploadFencingToken < 0 ||
		asrFencingToken < 0 {
		return agentvoice.Candidate{}, agentvoice.ErrRepository
	}
	candidate.Duration = time.Duration(duration)
	candidate.Status = agentvoice.CandidateStatus(status)
	candidate.UploadFencingToken = uint64(uploadFencingToken)
	candidate.ASRFencingToken = uint64(asrFencingToken)
	if uploadLease.Valid {
		candidate.UploadLeaseUntil = uploadLease.Time
	}
	if asrLease.Valid {
		candidate.ASRLeaseUntil = asrLease.Time
	}
	if failureRetryable.Valid {
		candidate.FailureRetryable = failureRetryable.Bool
	}
	if confirmedAt.Valid {
		candidate.ConfirmedAt = confirmedAt.Time
	}
	if deletedAt.Valid {
		candidate.DeletedAt = deletedAt.Time
	}
	return candidate, nil
}

func scanMessageAudio(row rowScanner) (conversation.MessageAudio, error) {
	var audio conversation.MessageAudio
	var status string
	var duration int64
	var deletedAt pgtype.Timestamptz
	err := row.Scan(
		&audio.ID,
		&audio.OwnerID,
		&audio.ThreadID,
		&audio.MessageID,
		&audio.CandidateID,
		&audio.ObjectKey,
		&audio.ContentType,
		&audio.Size,
		&audio.ChecksumSHA256,
		&duration,
		&audio.SampleRate,
		&audio.ETag,
		&status,
		&audio.CreatedAt,
		&audio.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return conversation.MessageAudio{}, err
	}
	if duration <= 0 {
		return conversation.MessageAudio{}, agentvoice.ErrRepository
	}
	audio.Duration = time.Duration(duration)
	audio.Status = conversation.MessageAudioStatus(status)
	if deletedAt.Valid {
		audio.DeletedAt = deletedAt.Time
	}
	return audio, nil
}

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
	if !candidateID.Valid ||
		!objectKey.Valid ||
		!contentType.Valid ||
		!size.Valid ||
		!checksum.Valid ||
		!duration.Valid ||
		!sampleRate.Valid ||
		!etag.Valid ||
		!status.Valid ||
		!audioCreatedAt.Valid ||
		!audioUpdatedAt.Valid {
		return conversation.Message{}, agentvoice.ErrRepository
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

func mapVoicePostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentvoice.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return agentvoice.ErrNotFound
		case "23505":
			switch postgresError.ConstraintName {
			case "agent_messages_client_idempotency_key":
				return agentvoice.ErrIdempotencyConflict
			default:
				return agentvoice.ErrConflict
			}
		case "23514", "22003":
			return agentvoice.ErrInvalidRequest
		}
	}
	return agentvoice.ErrRepository
}

var _ agentvoice.Repository = (*PostgresStore)(nil)
