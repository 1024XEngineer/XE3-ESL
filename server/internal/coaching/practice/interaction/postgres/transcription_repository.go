package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
)

type transcriptionRow struct {
	TurnID                  string
	TurnKind                practice.TurnKind
	Status                  string
	SessionID               string
	QuestionID              string
	RespondentParticipantID string
	ReservationID           string
	ClientRequestID         string
	InputFingerprint        string
	FencingToken            int64
	LeaseExpiresAt          *time.Time
	AttemptCount            int
	CandidateID             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (r *Repository) ReserveTranscription(
	ctx context.Context,
	actor practiceinteraction.Actor,
	command practiceinteraction.StoreReserveTranscriptionCommand,
) (practiceinteraction.StoredTranscriptionReservation, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		len(command.IdempotencyKey) > 128 ||
		strings.TrimSpace(command.InputFingerprint) == "" ||
		strings.TrimSpace(command.RespondentParticipantID) == "" ||
		command.LeaseDuration <= 0 {
		return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	r.reachedWriteFence()
	if err := lockEvidenceSourceSession(ctx, tx, actor.UserID, command.SessionID); err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	if err := lockKey(ctx, tx, actor.UserID, "transcription", command.IdempotencyKey); err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	question, err := getQuestion(ctx, tx, actor.UserID, command.QuestionID)
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	if question.SessionID != command.SessionID ||
		!containsParticipant(question.AddresseeParticipantIDs, command.RespondentParticipantID) {
		return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceNotFound
	}
	now := databaseTime(r.now)
	existing, found, err := findTranscriptionByClientRequest(
		ctx, tx, actor.UserID, command.SessionID, command.IdempotencyKey, true,
	)
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	if found {
		if existing.SessionID != command.SessionID ||
			existing.QuestionID != command.QuestionID ||
			existing.RespondentParticipantID != command.RespondentParticipantID ||
			existing.InputFingerprint != command.InputFingerprint ||
			(command.TurnID == "" && existing.TurnKind != practice.TurnKindEffective) ||
			(command.TurnID != "" && existing.TurnID != command.TurnID) {
			return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceConflict
		}
		reservation, err := r.replayOrReacquireTranscription(
			ctx, tx, actor.UserID, existing, command.LeaseDuration, now,
		)
		if err != nil {
			return practiceinteraction.StoredTranscriptionReservation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
		}
		return reservation, nil
	}

	var row transcriptionRow
	if command.TurnID != "" {
		row, err = lockTranscriptionTurn(ctx, tx, actor.UserID, command.TurnID)
		if err != nil {
			return practiceinteraction.StoredTranscriptionReservation{}, err
		}
		if row.TurnKind != practice.TurnKindRetry ||
			row.SessionID != command.SessionID || row.QuestionID != command.QuestionID ||
			row.RespondentParticipantID != command.RespondentParticipantID ||
			(row.Status != "answering" && row.Status != "failed") {
			return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceConflict
		}
	} else {
		var status practice.SessionStatus
		err := tx.QueryRow(ctx, `
			SELECT status FROM practice_sessions
			WHERE user_id = $1 AND session_id = $2 FOR UPDATE
		`, actor.UserID, command.SessionID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceNotFound
		}
		if err != nil {
			return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
		}
		if status != practice.SessionStarting && status != practice.SessionInProgress {
			return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceConflict
		}
		row.TurnID, err = r.ids.NewID()
		if err != nil || !practice.ValidAggregateID(row.TurnID) {
			return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceUnavailable
		}
		row.TurnKind = practice.TurnKindEffective
		row.SessionID = command.SessionID
		row.QuestionID = command.QuestionID
		row.RespondentParticipantID = command.RespondentParticipantID
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(t.sequence), 0) + 1 FROM practice_turns t
			JOIN practice_sessions s ON s.session_id=t.session_id
			WHERE s.user_id = $1 AND t.session_id = $2
		`, actor.UserID, command.SessionID).Scan(&row.AttemptCount); err != nil {
			return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
		}
	}
	reservationID, err := r.ids.NewID()
	if err != nil || !practice.ValidAggregateID(reservationID) {
		return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceUnavailable
	}
	leaseExpiresAt := now.Add(command.LeaseDuration)
	if command.TurnID == "" {
		sequence := row.AttemptCount
		_, err = tx.Exec(ctx, `
			INSERT INTO practice_turns (
				turn_id, session_id, question_id,
				respondent_participant_id, sequence, turn_kind, status,
				counts_toward_turn_limit, transcription_request_id,
				transcription_client_request_id, transcription_input_fingerprint,
				asr_fencing_token, asr_lease_expires_at, asr_attempt_count,
				created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,'EFFECTIVE','transcribing',$6,
			          $7,$8,$9,1,$10,1,$11,$11)
		`, row.TurnID, command.SessionID, command.QuestionID,
			command.RespondentParticipantID, sequence, question.Type == "PRIMARY",
			reservationID, command.IdempotencyKey, command.InputFingerprint,
			leaseExpiresAt, now)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE practice_turns t
			SET status = 'transcribing', transcription_request_id = $3,
			    transcription_client_request_id = $4,
			    transcription_input_fingerprint = $5,
			    asr_fencing_token = asr_fencing_token + 1,
			    asr_lease_expires_at = $6,
			    asr_attempt_count = asr_attempt_count + 1,
			    candidate_id = NULL, transcript_id = NULL,
			    evidence_version = NULL, transcript = NULL, provider = NULL,
			    model = NULL, provider_request_id = NULL, failure_code = NULL,
			    submitted_at = NULL, updated_at = $7
			FROM practice_sessions s
			WHERE s.session_id=t.session_id AND s.user_id = $1 AND t.turn_id = $2
		`, actor.UserID, row.TurnID, reservationID, command.IdempotencyKey,
			command.InputFingerprint, leaseExpiresAt, now)
	}
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	created, err := getTranscriptionByReservation(ctx, tx, actor.UserID, reservationID, false)
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	reservation := mapReservation(created, true)
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	return reservation, nil
}

func (r *Repository) replayOrReacquireTranscription(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	row transcriptionRow,
	leaseDuration time.Duration,
	now time.Time,
) (practiceinteraction.StoredTranscriptionReservation, error) {
	switch row.Status {
	case "transcribed", "confirmed":
		return mapReservation(row, false), nil
	case "transcribing":
		if row.LeaseExpiresAt != nil && row.LeaseExpiresAt.After(now) {
			return mapReservation(row, false), nil
		}
	case "failed":
	default:
		return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceConflict
	}
	leaseExpiresAt := now.Add(leaseDuration)
	tag, err := tx.Exec(ctx, `
		UPDATE practice_turns t
		SET status = 'transcribing', asr_fencing_token = asr_fencing_token + 1,
		    asr_lease_expires_at = $3, asr_attempt_count = asr_attempt_count + 1,
		    candidate_id = NULL, transcript_id = NULL, evidence_version = NULL,
		    transcript = NULL, provider = NULL, model = NULL,
		    provider_request_id = NULL, failure_code = NULL,
		    submitted_at = NULL, updated_at = $4
		FROM practice_sessions s
		WHERE s.session_id=t.session_id AND s.user_id = $1 AND t.turn_id = $2
		  AND t.status IN ('transcribing', 'failed')
	`, ownerUserID, row.TurnID, leaseExpiresAt, now)
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceConflict
	}
	updated, err := getTranscriptionByReservation(ctx, tx, ownerUserID, row.ReservationID, false)
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	return mapReservation(updated, true), nil
}

func (r *Repository) CompleteTranscription(
	ctx context.Context,
	job practiceinteraction.JobContext,
	command practiceinteraction.StoreCompleteTranscriptionCommand,
) (practiceinteraction.StoredTranscriptCandidate, error) {
	if r == nil || r.pool == nil || ctx == nil || !validJob(job) ||
		strings.TrimSpace(command.TranscriptID) == "" || command.EvidenceVersion <= 0 ||
		strings.TrimSpace(command.Provider) == "" || strings.TrimSpace(command.Model) == "" ||
		strings.TrimSpace(command.ProviderRequestID) == "" ||
		strings.TrimSpace(command.Text) == "" {
		return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureJobWritable(ctx, tx, job); err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, err
	}
	row, err := getTranscriptionByReservation(ctx, tx, job.OwnerUserID, job.ReservationID, true)
	if err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, err
	}
	if row.Status == "transcribed" || row.Status == "confirmed" {
		candidate, err := getCandidateInTransaction(ctx, tx, job.OwnerUserID, row.CandidateID, false)
		if err != nil {
			return practiceinteraction.StoredTranscriptCandidate{}, err
		}
		if !sameCandidateCompletion(candidate, command) {
			return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return practiceinteraction.StoredTranscriptCandidate{}, safeDatabaseError(err)
		}
		return candidate, nil
	}
	if row.Status != "transcribing" || row.FencingToken != job.FencingToken {
		return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceConflict
	}
	now := databaseTime(r.now)
	candidateID, err := r.ids.NewID()
	if err != nil || !practice.ValidAggregateID(candidateID) {
		return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceUnavailable
	}
	tag, err := tx.Exec(ctx, `
		UPDATE practice_turns t
		SET status = 'transcribed', candidate_id = $3, transcript_id = $4,
		    evidence_version = $5, transcript = $6, provider = $7, model = $8,
		    provider_request_id = $9, failure_code = NULL,
		    asr_lease_expires_at = NULL, submitted_at = $10, updated_at = $10
		FROM practice_sessions s
		WHERE s.session_id=t.session_id AND s.user_id = $1 AND t.turn_id = $2
		  AND t.status = 'transcribing' AND t.asr_fencing_token = $11
	`, job.OwnerUserID, row.TurnID, candidateID, command.TranscriptID,
		command.EvidenceVersion, strings.TrimSpace(command.Text), command.Provider,
		command.Model, command.ProviderRequestID, now, job.FencingToken)
	if err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceConflict
	}
	candidate, err := getCandidateInTransaction(ctx, tx, job.OwnerUserID, candidateID, false)
	if err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	return candidate, nil
}

func (r *Repository) FailTranscription(
	ctx context.Context,
	job practiceinteraction.JobContext,
	failure practiceinteraction.ProcessingFailure,
) error {
	if r == nil || r.pool == nil || ctx == nil || !validJob(job) ||
		!validProcessingFailureCode(failure.Code) {
		return practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureJobWritable(ctx, tx, job); err != nil {
		return err
	}
	row, err := getTranscriptionByReservation(ctx, tx, job.OwnerUserID, job.ReservationID, true)
	if err != nil {
		return err
	}
	if row.Status == "failed" && row.FencingToken == job.FencingToken {
		return tx.Commit(ctx)
	}
	if row.Status != "transcribing" || row.FencingToken != job.FencingToken {
		return practiceinteraction.ErrPersistenceConflict
	}
	now := databaseTime(r.now)
	tag, err := tx.Exec(ctx, `
		UPDATE practice_turns t
		SET status = 'failed', failure_code = $3,
		    provider_request_id = NULLIF($4, ''), asr_lease_expires_at = NULL,
		    updated_at = $5
		FROM practice_sessions s
		WHERE s.session_id=t.session_id AND s.user_id = $1 AND t.turn_id = $2
		  AND t.status = 'transcribing' AND t.asr_fencing_token = $6
	`, job.OwnerUserID, row.TurnID, failure.Code, failure.ProviderRequestID,
		now, job.FencingToken)
	if err != nil {
		return safeDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return practiceinteraction.ErrPersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

func (r *Repository) GetReservation(
	ctx context.Context,
	actor practiceinteraction.Actor,
	reservationID string,
) (practiceinteraction.StoredTranscriptionReservation, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(reservationID) == "" {
		return practiceinteraction.StoredTranscriptionReservation{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := getTranscriptionByReservation(ctx, tx, actor.UserID, reservationID, false)
	if err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, err
	}
	result := mapReservation(row, false)
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) GetCandidate(
	ctx context.Context,
	actor practiceinteraction.Actor,
	candidateID string,
) (practiceinteraction.StoredTranscriptCandidate, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(candidateID) == "" {
		return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	candidate, err := getCandidateInTransaction(ctx, tx, actor.UserID, candidateID, false)
	if err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	return candidate, nil
}

const transcriptionColumns = `
	SELECT t.turn_id, t.turn_kind, t.status, t.session_id, t.question_id,
	       t.respondent_participant_id, COALESCE(t.transcription_request_id, ''),
	       COALESCE(t.transcription_client_request_id, ''),
	       COALESCE(t.transcription_input_fingerprint, ''), t.asr_fencing_token,
	       t.asr_lease_expires_at, t.asr_attempt_count, COALESCE(t.candidate_id::text, ''),
	       t.created_at, t.updated_at
	FROM practice_turns t
	JOIN practice_sessions s ON s.session_id=t.session_id
`

func findTranscriptionByClientRequest(
	ctx context.Context,
	database queryRow,
	ownerUserID, sessionID, clientRequestID string,
	lock bool,
) (transcriptionRow, bool, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF t"
	}
	row, err := scanTranscription(database.QueryRow(ctx, transcriptionColumns+`
		WHERE s.user_id = $1 AND t.session_id = $2
		  AND t.transcription_client_request_id = $3
	`+suffix, ownerUserID, sessionID, clientRequestID))
	if errors.Is(err, practiceinteraction.ErrPersistenceNotFound) {
		return transcriptionRow{}, false, nil
	}
	return row, err == nil, err
}

func getTranscriptionByReservation(
	ctx context.Context,
	database queryRow,
	ownerUserID, reservationID string,
	lock bool,
) (transcriptionRow, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF t"
	}
	return scanTranscription(database.QueryRow(ctx, transcriptionColumns+`
		WHERE s.user_id = $1 AND t.transcription_request_id = $2
	`+suffix, ownerUserID, reservationID))
}

func lockTranscriptionTurn(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID, turnID string,
) (transcriptionRow, error) {
	return scanTranscription(tx.QueryRow(ctx, transcriptionColumns+`
		WHERE s.user_id = $1 AND t.turn_id = $2 FOR UPDATE OF t
	`, ownerUserID, turnID))
}

func lockTranscriptionByCandidate(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID, candidateID string,
) (transcriptionRow, error) {
	return scanTranscription(tx.QueryRow(ctx, transcriptionColumns+`
		WHERE s.user_id = $1 AND t.candidate_id = $2 FOR UPDATE OF t
	`, ownerUserID, candidateID))
}

func scanTranscription(row rowScanner) (transcriptionRow, error) {
	var value transcriptionRow
	var lease sql.NullTime
	err := row.Scan(
		&value.TurnID, &value.TurnKind, &value.Status, &value.SessionID,
		&value.QuestionID, &value.RespondentParticipantID,
		&value.ReservationID, &value.ClientRequestID, &value.InputFingerprint,
		&value.FencingToken, &lease, &value.AttemptCount, &value.CandidateID,
		&value.CreatedAt, &value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return transcriptionRow{}, practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return transcriptionRow{}, safeDatabaseError(err)
	}
	if lease.Valid {
		value.LeaseExpiresAt = &lease.Time
	}
	value.CreatedAt = value.CreatedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	return value, nil
}

func mapReservation(
	row transcriptionRow,
	leaseAcquired bool,
) practiceinteraction.StoredTranscriptionReservation {
	status := practiceinteraction.StoredTranscriptionProcessing
	switch row.Status {
	case "transcribed", "confirmed":
		status = practiceinteraction.StoredTranscriptionCompleted
	case "failed":
		status = practiceinteraction.StoredTranscriptionFailed
	}
	leaseExpiresAt := time.Time{}
	if row.LeaseExpiresAt != nil {
		leaseExpiresAt = row.LeaseExpiresAt.UTC()
	}
	return practiceinteraction.StoredTranscriptionReservation{
		ID:                      row.ReservationID,
		QuestionID:              row.QuestionID,
		SessionID:               row.SessionID,
		IdempotencyKey:          row.ClientRequestID,
		InputFingerprint:        row.InputFingerprint,
		RespondentParticipantID: row.RespondentParticipantID,
		Status:                  status,
		FencingToken:            row.FencingToken,
		DeletionGeneration:      0,
		LeaseAcquired:           leaseAcquired,
		LeaseExpiresAt:          leaseExpiresAt,
		CandidateID:             row.CandidateID,
		CurrentAttemptID:        fmt.Sprintf("%s:%d", row.TurnID, row.FencingToken),
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

func getCandidateInTransaction(
	ctx context.Context,
	database queryRow,
	ownerUserID, candidateID string,
	lock bool,
) (practiceinteraction.StoredTranscriptCandidate, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF t"
	}
	var candidate practiceinteraction.StoredTranscriptCandidate
	var rowStatus string
	err := database.QueryRow(ctx, `
		SELECT t.candidate_id, t.transcription_request_id, t.question_id, t.session_id,
		       t.respondent_participant_id, t.transcript_id, t.evidence_version,
		       t.provider, t.model, t.provider_request_id, t.transcript, t.status,
		       COALESCE(t.submitted_at, t.updated_at)
		FROM practice_turns t
		JOIN practice_sessions s ON s.session_id=t.session_id
		WHERE s.user_id = $1 AND t.candidate_id = $2
	`+suffix, ownerUserID, candidateID).Scan(
		&candidate.ID, &candidate.ReservationID, &candidate.QuestionID,
		&candidate.SessionID, &candidate.RespondentParticipantID,
		&candidate.TranscriptID, &candidate.EvidenceVersion, &candidate.Provider,
		&candidate.Model, &candidate.ProviderRequestID, &candidate.Text,
		&rowStatus, &candidate.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return practiceinteraction.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	if rowStatus == "confirmed" {
		candidate.Status = practiceinteraction.CandidateConfirmed
	} else if rowStatus == "transcribed" {
		candidate.Status = practiceinteraction.CandidateReady
	} else {
		return practiceinteraction.StoredTranscriptCandidate{}, practiceinteraction.ErrPersistenceConflict
	}
	candidate.CreatedAt = candidate.CreatedAt.UTC()
	return candidate, nil
}

func sameCandidateCompletion(
	candidate practiceinteraction.StoredTranscriptCandidate,
	command practiceinteraction.StoreCompleteTranscriptionCommand,
) bool {
	return candidate.TranscriptID == command.TranscriptID &&
		candidate.EvidenceVersion == command.EvidenceVersion &&
		candidate.Provider == command.Provider && candidate.Model == command.Model &&
		candidate.ProviderRequestID == command.ProviderRequestID &&
		candidate.Text == strings.TrimSpace(command.Text)
}
