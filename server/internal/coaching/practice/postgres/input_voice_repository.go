// Package postgres implements Practice's production persistence boundary.
package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
)

const evidenceSourceLockNamespace = "evidence-source"

func (r *Repository) SaveQuestion(
	ctx context.Context,
	actor practiceinput.Actor,
	question practice.Question,
) (practice.Question, error) {
	if !validInputActor(actor) || !validQuestion(question) {
		return practice.Question{}, practiceinput.ErrPersistenceInvalid
	}
	createdAtProvided := !question.CreatedAt.IsZero()
	if question.CreatedAt.IsZero() {
		question.CreatedAt = databaseTime(r.now)
	} else {
		question.CreatedAt = question.CreatedAt.UTC().Truncate(time.Microsecond)
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practice.Question{}, err
	}
	r.reachedWriteFence()
	if err := lockEvidenceSourceSession(
		ctx,
		tx,
		actor.UserID,
		question.SessionID,
	); err != nil {
		return practice.Question{}, err
	}
	if err := lockKey(ctx, tx, actor.UserID, "question", question.ID); err != nil {
		return practice.Question{}, err
	}
	if question.Type == "FOLLOW_UP" {
		var parentSession string
		var parentType string
		err := tx.QueryRow(
			ctx,
			`SELECT practice_session_id, question_type
			 FROM practice_questions
			 WHERE owner_user_id = $1 AND question_id = $2`,
			actor.UserID,
			question.ParentQuestionID,
		).Scan(&parentSession, &parentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return practice.Question{}, practiceinput.ErrPersistenceInvalid
		}
		if err != nil {
			return practice.Question{}, safeDatabaseError(err)
		}
		if parentSession != question.SessionID || parentType != "PRIMARY" {
			return practice.Question{}, practiceinput.ErrPersistenceInvalid
		}
	}

	tag, err := tx.Exec(
		ctx,
		`INSERT INTO practice_questions (
			owner_user_id, question_id, practice_session_id,
			speaker_participant_id, addressee_participant_ids,
			objective_id, question_type,
			parent_question_id, content, sequence, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10, $11
		) ON CONFLICT (owner_user_id, question_id) DO NOTHING`,
		actor.UserID,
		question.ID,
		question.SessionID,
		question.SpeakerParticipantID,
		question.AddresseeParticipantIDs,
		question.ObjectiveID,
		question.Type,
		question.ParentQuestionID,
		question.Content,
		question.Sequence,
		question.CreatedAt,
	)
	if err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}

	saved, err := getQuestion(ctx, tx, actor.UserID, question.ID)
	if err != nil {
		return practice.Question{}, err
	}
	if tag.RowsAffected() == 0 &&
		!sameQuestion(saved, question, createdAtProvided) {
		return practice.Question{}, practiceinput.ErrPersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	return saved, nil
}

func (r *Repository) GetQuestion(
	ctx context.Context,
	actor practiceinput.Actor,
	questionID string,
) (practice.Question, error) {
	if !validInputActor(actor) || strings.TrimSpace(questionID) == "" {
		return practice.Question{}, practiceinput.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practice.Question{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	question, err := getQuestion(ctx, tx, actor.UserID, questionID)
	if err != nil {
		return practice.Question{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	return question, nil
}

func (r *Repository) ListSessionQuestions(
	ctx context.Context,
	actor practiceinput.Actor,
	sessionID string,
) ([]practice.Question, error) {
	if !validInputActor(actor) || strings.TrimSpace(sessionID) == "" {
		return nil, practiceinput.ErrPersistenceInvalid
	}
	return r.listSessionQuestions(ctx, actor.UserID, sessionID)
}

func (r *Repository) ListCompletedSessionQuestions(
	ctx context.Context,
	ownerUserID string,
	sessionID string,
) ([]practice.Question, error) {
	if !validUUID(ownerUserID) || strings.TrimSpace(sessionID) == "" {
		return nil, practiceinput.ErrPersistenceInvalid
	}
	return r.listSessionQuestions(ctx, ownerUserID, sessionID)
}

func (r *Repository) listSessionQuestions(
	ctx context.Context,
	ownerUserID string,
	sessionID string,
) ([]practice.Question, error) {
	tx, err := r.beginOwnerRead(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(
		ctx,
		questionColumns+`
		 WHERE owner_user_id = $1 AND practice_session_id = $2
		 ORDER BY sequence, created_at, question_id`,
		ownerUserID,
		sessionID,
	)
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	defer rows.Close()
	questions := make([]practice.Question, 0)
	for rows.Next() {
		question, scanErr := scanQuestion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, safeDatabaseError(err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, safeDatabaseError(err)
	}
	return questions, nil
}

func (r *Repository) ReserveTranscription(
	ctx context.Context,
	actor practiceinput.Actor,
	command practiceinput.StoreReserveTranscriptionCommand,
) (practiceinput.StoredTranscriptionReservation, error) {
	if !validInputActor(actor) ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		strings.TrimSpace(command.InputFingerprint) == "" ||
		strings.TrimSpace(command.RespondentParticipantID) == "" ||
		command.LeaseDuration <= 0 {
		return practiceinput.StoredTranscriptionReservation{}, practiceinput.ErrPersistenceInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	deletionGeneration, err := ensureActorWritable(ctx, tx, actor.UserID)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	r.reachedWriteFence()
	if err := lockKey(
		ctx,
		tx,
		actor.UserID,
		"transcription",
		command.IdempotencyKey,
	); err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}

	question, err := getQuestion(ctx, tx, actor.UserID, command.QuestionID)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	if question.SessionID != command.SessionID {
		return practiceinput.StoredTranscriptionReservation{}, practiceinput.ErrPersistenceNotFound
	}
	// Proposal #47 keeps respondent off Question and resolves it from the
	// trusted Actor. Persistence still enforces that the resolved participant
	// was an addressee of this immutable Question snapshot.
	if !containsParticipant(
		question.AddresseeParticipantIDs,
		command.RespondentParticipantID,
	) {
		return practiceinput.StoredTranscriptionReservation{}, practiceinput.ErrPersistenceNotFound
	}

	reservation, found, err := findReservationByKey(
		ctx,
		tx,
		actor.UserID,
		command.IdempotencyKey,
	)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	now, err := transactionTime(ctx, tx)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	if found {
		if reservation.InputFingerprint != command.InputFingerprint ||
			reservation.QuestionID != command.QuestionID ||
			reservation.SessionID != command.SessionID ||
			reservation.RespondentParticipantID != command.RespondentParticipantID {
			return practiceinput.StoredTranscriptionReservation{}, practiceinput.ErrPersistenceConflict
		}
		if reservation.Status == practiceinput.StoredTranscriptionCompleted ||
			(reservation.Status == practiceinput.StoredTranscriptionProcessing &&
				reservation.LeaseExpiresAt.After(now)) {
			reservation.LeaseAcquired = false
			if err := tx.Commit(ctx); err != nil {
				return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
			}
			return reservation, nil
		}
		return r.takeOverReservation(
			ctx,
			tx,
			actor.UserID,
			reservation,
			now,
			command.LeaseDuration,
		)
	}

	reservation = practiceinput.StoredTranscriptionReservation{
		ID:                      newID("asr_res"),
		QuestionID:              command.QuestionID,
		SessionID:               command.SessionID,
		IdempotencyKey:          command.IdempotencyKey,
		InputFingerprint:        command.InputFingerprint,
		RespondentParticipantID: command.RespondentParticipantID,
		Status:                  practiceinput.StoredTranscriptionProcessing,
		FencingToken:            1,
		DeletionGeneration:      deletionGeneration,
		LeaseAcquired:           true,
		LeaseExpiresAt:          now.Add(command.LeaseDuration),
		CurrentAttemptID:        newID("asr_attempt"),
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO practice_transcription_reservations (
			owner_user_id, reservation_id, question_id, practice_session_id,
			idempotency_key, input_fingerprint, respondent_participant_id,
			status, fencing_token, deletion_generation, lease_expires_at, candidate_id,
			current_attempt_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULL, $12, $13, $13
		)`,
		actor.UserID,
		reservation.ID,
		reservation.QuestionID,
		reservation.SessionID,
		reservation.IdempotencyKey,
		reservation.InputFingerprint,
		reservation.RespondentParticipantID,
		reservation.Status,
		reservation.FencingToken,
		reservation.DeletionGeneration,
		reservation.LeaseExpiresAt,
		reservation.CurrentAttemptID,
		now,
	)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	if err := insertAttempt(ctx, tx, actor.UserID, reservation); err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	return reservation, nil
}

func (r *Repository) takeOverReservation(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	reservation practiceinput.StoredTranscriptionReservation,
	now time.Time,
	leaseDuration time.Duration,
) (practiceinput.StoredTranscriptionReservation, error) {
	_, err := tx.Exec(
		ctx,
		`UPDATE practice_processing_attempts
		 SET status = 'expired', finished_at = $1
		 WHERE owner_user_id = $2 AND attempt_id = $3 AND status = 'processing'`,
		now,
		ownerUserID,
		reservation.CurrentAttemptID,
	)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}

	reservation.Status = practiceinput.StoredTranscriptionProcessing
	reservation.FencingToken++
	reservation.LeaseAcquired = true
	reservation.LeaseExpiresAt = now.Add(leaseDuration)
	reservation.CurrentAttemptID = newID("asr_attempt")
	reservation.UpdatedAt = now
	_, err = tx.Exec(
		ctx,
		`UPDATE practice_transcription_reservations
		 SET status = $1, fencing_token = $2, lease_expires_at = $3,
		     current_attempt_id = $4, updated_at = $5
		 WHERE owner_user_id = $6 AND reservation_id = $7`,
		reservation.Status,
		reservation.FencingToken,
		reservation.LeaseExpiresAt,
		reservation.CurrentAttemptID,
		reservation.UpdatedAt,
		ownerUserID,
		reservation.ID,
	)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	if err := insertAttempt(ctx, tx, ownerUserID, reservation); err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	return reservation, nil
}

func (r *Repository) CompleteTranscription(
	ctx context.Context,
	job practiceinput.JobContext,
	command practiceinput.StoreCompleteTranscriptionCommand,
) (practiceinput.StoredTranscriptCandidate, error) {
	if !validJob(job) ||
		strings.TrimSpace(command.TranscriptID) == "" ||
		command.EvidenceVersion <= 0 ||
		strings.TrimSpace(command.Provider) == "" ||
		strings.TrimSpace(command.Model) == "" ||
		strings.TrimSpace(command.ProviderRequestID) == "" ||
		strings.TrimSpace(command.Text) == "" {
		return practiceinput.StoredTranscriptCandidate{}, practiceinput.ErrPersistenceInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureJobWritable(ctx, tx, job); err != nil {
		return practiceinput.StoredTranscriptCandidate{}, err
	}
	r.reachedWriteFence()
	reservation, err := lockReservation(
		ctx,
		tx,
		job.OwnerUserID,
		job.ReservationID,
	)
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, err
	}
	if reservation.Status == practiceinput.StoredTranscriptionCompleted {
		candidate, candidateErr := getCandidate(
			ctx,
			tx,
			job.OwnerUserID,
			reservation.CandidateID,
		)
		if candidateErr != nil {
			return practiceinput.StoredTranscriptCandidate{}, candidateErr
		}
		if reservation.FencingToken != job.FencingToken ||
			reservation.DeletionGeneration != job.DeletionGeneration ||
			!sameCandidateCompletion(candidate, command) {
			return practiceinput.StoredTranscriptCandidate{}, practiceinput.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
		}
		return candidate, nil
	}
	now, err := transactionTime(ctx, tx)
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, err
	}
	if reservation.Status != practiceinput.StoredTranscriptionProcessing ||
		reservation.FencingToken != job.FencingToken ||
		reservation.DeletionGeneration != job.DeletionGeneration ||
		!reservation.LeaseExpiresAt.After(now) {
		return practiceinput.StoredTranscriptCandidate{}, practiceinput.ErrPersistenceConflict
	}

	candidate := practiceinput.StoredTranscriptCandidate{
		ID:                      newID("transcript_candidate"),
		ReservationID:           reservation.ID,
		QuestionID:              reservation.QuestionID,
		SessionID:               reservation.SessionID,
		RespondentParticipantID: reservation.RespondentParticipantID,
		TranscriptID:            command.TranscriptID,
		EvidenceVersion:         command.EvidenceVersion,
		Provider:                command.Provider,
		Model:                   command.Model,
		ProviderRequestID:       command.ProviderRequestID,
		Text:                    command.Text,
		Status:                  practiceinput.CandidateReady,
		CreatedAt:               now,
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO practice_transcript_candidates (
			owner_user_id, candidate_id, reservation_id, question_id,
			practice_session_id, respondent_participant_id,
			transcript_id, evidence_version, provider,
			model, provider_request_id, transcript_text, status, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)`,
		job.OwnerUserID,
		candidate.ID,
		candidate.ReservationID,
		candidate.QuestionID,
		candidate.SessionID,
		candidate.RespondentParticipantID,
		candidate.TranscriptID,
		candidate.EvidenceVersion,
		candidate.Provider,
		candidate.Model,
		candidate.ProviderRequestID,
		candidate.Text,
		candidate.Status,
		candidate.CreatedAt,
	)
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE practice_processing_attempts
		 SET status = 'completed', provider_request_id = $1, finished_at = $2
		 WHERE owner_user_id = $3 AND attempt_id = $4`,
		command.ProviderRequestID,
		now,
		job.OwnerUserID,
		reservation.CurrentAttemptID,
	)
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE practice_transcription_reservations
		 SET status = 'completed', candidate_id = $1, updated_at = $2
		 WHERE owner_user_id = $3 AND reservation_id = $4`,
		candidate.ID,
		now,
		job.OwnerUserID,
		reservation.ID,
	)
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	return candidate, nil
}

func (r *Repository) FailTranscription(
	ctx context.Context,
	job practiceinput.JobContext,
	failure practiceinput.ProcessingFailure,
) error {
	if !validJob(job) ||
		!validProcessingFailureCode(failure.Code) ||
		failure.Duration < 0 {
		return practiceinput.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureJobWritable(ctx, tx, job); err != nil {
		return err
	}
	r.reachedWriteFence()
	reservation, err := lockReservation(
		ctx,
		tx,
		job.OwnerUserID,
		job.ReservationID,
	)
	if err != nil {
		return err
	}
	now, err := transactionTime(ctx, tx)
	if err != nil {
		return err
	}
	if reservation.Status != practiceinput.StoredTranscriptionProcessing ||
		reservation.FencingToken != job.FencingToken ||
		reservation.DeletionGeneration != job.DeletionGeneration ||
		!reservation.LeaseExpiresAt.After(now) {
		return practiceinput.ErrPersistenceConflict
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE practice_processing_attempts
		 SET status = 'failed', error_code = $1, retryable = $2,
		     provider_request_id = $3, duration_ms = $4, finished_at = $5
		 WHERE owner_user_id = $6 AND attempt_id = $7`,
		failure.Code,
		failure.Retryable,
		failure.ProviderRequestID,
		failure.Duration.Milliseconds(),
		now,
		job.OwnerUserID,
		reservation.CurrentAttemptID,
	)
	if err != nil {
		return safeDatabaseError(err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE practice_transcription_reservations
		 SET status = 'failed', updated_at = $1
		 WHERE owner_user_id = $2 AND reservation_id = $3`,
		now,
		job.OwnerUserID,
		reservation.ID,
	)
	if err != nil {
		return safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

func (r *Repository) GetReservation(
	ctx context.Context,
	actor practiceinput.Actor,
	reservationID string,
) (practiceinput.StoredTranscriptionReservation, error) {
	if !validInputActor(actor) || strings.TrimSpace(reservationID) == "" {
		return practiceinput.StoredTranscriptionReservation{}, practiceinput.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reservation, err := getReservation(ctx, tx, actor.UserID, reservationID, "")
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	return reservation, nil
}

func (r *Repository) GetCandidate(
	ctx context.Context,
	actor practiceinput.Actor,
	candidateID string,
) (practiceinput.StoredTranscriptCandidate, error) {
	if !validInputActor(actor) || strings.TrimSpace(candidateID) == "" {
		return practiceinput.StoredTranscriptCandidate{}, practiceinput.ErrPersistenceInvalid
	}
	return r.getCompletedCandidate(ctx, actor.UserID, candidateID)
}

func (r *Repository) GetCompletedCandidate(
	ctx context.Context,
	ownerUserID string,
	candidateID string,
) (practiceinput.StoredTranscriptCandidate, error) {
	if !validUUID(ownerUserID) || strings.TrimSpace(candidateID) == "" {
		return practiceinput.StoredTranscriptCandidate{},
			practiceinput.ErrPersistenceInvalid
	}
	return r.getCompletedCandidate(ctx, ownerUserID, candidateID)
}

func (r *Repository) getCompletedCandidate(
	ctx context.Context,
	ownerUserID string,
	candidateID string,
) (practiceinput.StoredTranscriptCandidate, error) {
	tx, err := r.beginOwnerRead(ctx, ownerUserID)
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	candidate, err := getCandidate(ctx, tx, ownerUserID, candidateID)
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	return candidate, nil
}

func (r *Repository) ListProcessingAttempts(
	ctx context.Context,
	actor practiceinput.Actor,
	reservationID string,
) ([]practiceinput.ProcessingAttempt, error) {
	if !validInputActor(actor) || strings.TrimSpace(reservationID) == "" {
		return nil, practiceinput.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := getReservation(ctx, tx, actor.UserID, reservationID, ""); err != nil {
		return nil, err
	}
	rows, err := tx.Query(
		ctx,
		`SELECT attempt_id, reservation_id, operation, fencing_token, status,
		        lease_expires_at, error_code, retryable, provider_request_id,
		        duration_ms, started_at, finished_at
		 FROM practice_processing_attempts
		 WHERE owner_user_id = $1 AND reservation_id = $2
		 ORDER BY fencing_token`,
		actor.UserID,
		reservationID,
	)
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	defer rows.Close()
	attempts := make([]practiceinput.ProcessingAttempt, 0)
	for rows.Next() {
		var attempt practiceinput.ProcessingAttempt
		var durationMS int64
		if err := rows.Scan(
			&attempt.ID,
			&attempt.ReservationID,
			&attempt.Operation,
			&attempt.FencingToken,
			&attempt.Status,
			&attempt.LeaseExpiresAt,
			&attempt.ErrorCode,
			&attempt.Retryable,
			&attempt.ProviderRequestID,
			&durationMS,
			&attempt.StartedAt,
			&attempt.FinishedAt,
		); err != nil {
			return nil, safeDatabaseError(err)
		}
		attempt.Duration = time.Duration(durationMS) * time.Millisecond
		attempt.LeaseExpiresAt = attempt.LeaseExpiresAt.UTC()
		attempt.StartedAt = attempt.StartedAt.UTC()
		if attempt.FinishedAt != nil {
			finishedAt := attempt.FinishedAt.UTC()
			attempt.FinishedAt = &finishedAt
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, safeDatabaseError(err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, safeDatabaseError(err)
	}
	return attempts, nil
}

func (r *Repository) ConfirmTurn(
	ctx context.Context,
	actor practiceinput.Actor,
	command practiceinput.ConfirmTurnCommand,
) (practice.Turn, error) {
	if !validConfirmation(actor, command) {
		return practice.Turn{}, practiceinput.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practice.Turn{}, err
	}
	r.reachedWriteFence()
	sourceSessionID, err := lockCandidateEvidenceSourceSession(
		ctx,
		tx,
		actor.UserID,
		command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	turn, err := r.confirmTurnInTransaction(
		ctx,
		tx,
		actor,
		command,
		sourceSessionID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	turn, err = r.advanceConfirmedTurnInTransaction(ctx, tx, actor, turn)
	if err != nil {
		return practice.Turn{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func (r *Repository) advanceConfirmedTurnInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	actor practiceinput.Actor,
	turn practice.Turn,
) (practice.Turn, error) {
	if turn.Kind == practice.TurnKindRetry {
		var status practice.SessionStatus
		err := tx.QueryRow(ctx, `
			SELECT status, effective_turns
			FROM practice_sessions
			WHERE owner_user_id = $1 AND session_id = $2
			FOR UPDATE
		`, actor.UserID, turn.SessionID).Scan(
			&status,
			&turn.EffectiveTurns,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return practice.Turn{}, practiceinput.ErrPersistenceNotFound
		}
		if err != nil {
			return practice.Turn{}, safeDatabaseError(err)
		}
		if turn.EffectiveTurns < 1 {
			return practice.Turn{}, practiceinput.ErrPersistenceConflict
		}
		turn.SessionCompleted = status == practice.SessionCompleted
		return r.recordTurnProgressInTransaction(ctx, tx, actor.UserID, turn)
	}
	payload := []byte("practice/effective-turn/v1")
	if !turn.CountsTowardTurnLimit {
		payload = []byte("practice/follow-up-turn/v1")
	}
	result, err := r.advanceTurnInTransaction(
		ctx,
		tx,
		practice.Actor{UserID: actor.UserID, SessionID: actor.SessionID},
		practice.ConsumeTurnCommand{
			SessionID:             turn.SessionID,
			TurnID:                turn.ID,
			CountsTowardTurnLimit: turn.CountsTowardTurnLimit,
			Payload:               payload,
		},
	)
	if err != nil {
		return practice.Turn{}, err
	}
	if result.SessionID != turn.SessionID || result.TurnID != turn.ID {
		return practice.Turn{}, practice.ErrConflict
	}
	turn.EffectiveTurns = result.EffectiveTurns
	turn.SessionCompleted = result.Completed
	return r.recordTurnProgressInTransaction(ctx, tx, actor.UserID, turn)
}

func (r *Repository) recordTurnProgressInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	turn practice.Turn,
) (practice.Turn, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE practice_turns
		SET effective_turns = $3,
		    session_completed = $4,
		    progress_recorded_at = COALESCE(
		        progress_recorded_at,
		        transaction_timestamp()
		    )
		WHERE owner_user_id = $1
		  AND turn_id = $2
		  AND (
		      effective_turns = 0 OR
		      (effective_turns = $3 AND session_completed = $4)
		  )
	`, ownerUserID, turn.ID, turn.EffectiveTurns, turn.SessionCompleted)
	if err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return practice.Turn{}, practiceinput.ErrPersistenceConflict
	}
	return turn, nil
}

func (r *Repository) confirmTurnInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	actor practiceinput.Actor,
	command practiceinput.ConfirmTurnCommand,
	sourceSessionID string,
) (practice.Turn, error) {
	payloadHash := confirmationHash(command)
	if err := lockKey(
		ctx,
		tx,
		actor.UserID,
		"confirmation",
		command.IdempotencyKey,
	); err != nil {
		return practice.Turn{}, err
	}

	var existingHash string
	var existingTurnID string
	err := tx.QueryRow(
		ctx,
		`SELECT payload_hash, turn_id
		 FROM practice_turn_confirmations
		 WHERE owner_user_id = $1 AND idempotency_key = $2`,
		actor.UserID,
		command.IdempotencyKey,
	).Scan(&existingHash, &existingTurnID)
	if err == nil {
		if existingHash != payloadHash {
			return practice.Turn{}, practiceinput.ErrPersistenceConflict
		}
		turn, turnErr := getTurn(ctx, tx, actor.UserID, existingTurnID)
		if turnErr != nil {
			return practice.Turn{}, turnErr
		}
		return turn, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, safeDatabaseError(err)
	}

	candidate, err := lockCandidate(ctx, tx, actor.UserID, command.CandidateID)
	if err != nil {
		return practice.Turn{}, err
	}
	if candidate.SessionID != sourceSessionID ||
		candidate.EvidenceVersion != command.EvidenceVersion {
		return practice.Turn{}, practiceinput.ErrPersistenceConflict
	}

	var retryDraft domainRetryTurnDraft
	if command.RetryTurnID != "" {
		persistedDraft, retryErr := lockRetryTurn(
			ctx,
			tx,
			actor.UserID,
			command.RetryTurnID,
		)
		if retryErr != nil {
			return practice.Turn{},
				mapRetryConfirmationError(retryErr)
		}
		if persistedDraft.PracticeSessionID != candidate.SessionID ||
			persistedDraft.QuestionID != candidate.QuestionID ||
			(persistedDraft.Status == "CONFIRMED" &&
				persistedDraft.CandidateID != candidate.ID) {
			return practice.Turn{},
				practiceinput.ErrPersistenceConflict
		}
		retryDraft = domainRetryTurnDraft{
			TurnID:         persistedDraft.TurnID,
			RetryRequestID: persistedDraft.RetryRequestID,
			OriginalTurnID: persistedDraft.OriginalTurnID,
			Status:         string(persistedDraft.Status),
		}
	}

	turn, found, err := findTurnByCandidate(
		ctx,
		tx,
		actor.UserID,
		candidate.ID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	now := databaseTime(r.now)
	if found {
		if turn.EvidenceVersion != command.EvidenceVersion ||
			turn.AnswerText != command.ConfirmedText ||
			(command.RetryTurnID == "" &&
				turn.Kind != practice.TurnKindEffective) ||
			(command.RetryTurnID != "" &&
				(turn.Kind != practice.TurnKindRetry ||
					turn.ID != command.RetryTurnID)) {
			return practice.Turn{}, practiceinput.ErrPersistenceConflict
		}
	} else {
		question, questionErr := getQuestion(
			ctx,
			tx,
			actor.UserID,
			candidate.QuestionID,
		)
		if questionErr != nil {
			return practice.Turn{}, questionErr
		}
		turnID := newID("turn")
		turnKind := practice.TurnKindEffective
		countsTowardTurnLimit := question.Type == "PRIMARY"
		retryRequestID := ""
		originalTurnID := ""
		if command.RetryTurnID != "" {
			if retryDraft.Status != "ANSWERING" {
				return practice.Turn{},
					practiceinput.ErrPersistenceConflict
			}
			turnID = retryDraft.TurnID
			turnKind = practice.TurnKindRetry
			countsTowardTurnLimit = false
			retryRequestID = retryDraft.RetryRequestID
			originalTurnID = retryDraft.OriginalTurnID
		}
		turn = practice.Turn{
			ID:                      turnID,
			SessionID:               candidate.SessionID,
			QuestionID:              candidate.QuestionID,
			SpeakerParticipantID:    question.SpeakerParticipantID,
			AddresseeParticipantIDs: question.AddresseeParticipantIDs,
			RespondentParticipantID: candidate.RespondentParticipantID,
			Sequence:                question.Sequence,
			InteractionMode:         "PUSH_TO_TALK",
			AnswerText:              command.ConfirmedText,
			CandidateID:             candidate.ID,
			EvidenceVersion:         candidate.EvidenceVersion,
			Kind:                    turnKind,
			RetryRequestID:          retryRequestID,
			OriginalTurnID:          originalTurnID,
			CountsTowardTurnLimit:   countsTowardTurnLimit,
			ConfirmedAt:             now,
			CreatedAt:               now,
		}
		_, err = tx.Exec(
			ctx,
			`INSERT INTO practice_turns (
				owner_user_id, turn_id, candidate_id, question_id,
				practice_session_id, speaker_participant_id,
				addressee_participant_ids, respondent_participant_id,
				sequence, interaction_mode, answer_text, evidence_version,
				confirmed_at, created_at, turn_kind, retry_request_id,
				original_turn_id, counts_toward_effective_turn_limit
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
				$13, $13, $14, NULLIF($15, '')::uuid,
				NULLIF($16, ''), $17
			)`,
			actor.UserID,
			turn.ID,
			turn.CandidateID,
			turn.QuestionID,
			turn.SessionID,
			turn.SpeakerParticipantID,
			turn.AddresseeParticipantIDs,
			turn.RespondentParticipantID,
			turn.Sequence,
			turn.InteractionMode,
			turn.AnswerText,
			turn.EvidenceVersion,
			now,
			turn.Kind,
			turn.RetryRequestID,
			turn.OriginalTurnID,
			turn.CountsTowardTurnLimit,
		)
		if err != nil {
			return practice.Turn{}, safeDatabaseError(err)
		}
		_, err = tx.Exec(
			ctx,
			`UPDATE practice_transcript_candidates
			 SET status = 'confirmed'
			 WHERE owner_user_id = $1 AND candidate_id = $2`,
			actor.UserID,
			candidate.ID,
		)
		if err != nil {
			return practice.Turn{}, safeDatabaseError(err)
		}
		if command.RetryTurnID != "" {
			tag, updateErr := tx.Exec(ctx, `
				UPDATE practice_retry_turn_drafts
				SET status = 'CONFIRMED',
				    candidate_id = $3,
				    confirmed_at = $4,
				    updated_at = $4
				WHERE owner_user_id = $1
				  AND turn_id = $2
				  AND status = 'ANSWERING'
				  AND candidate_id IS NULL
			`, actor.UserID, command.RetryTurnID, candidate.ID, now)
			if updateErr != nil {
				return practice.Turn{},
					safeDatabaseError(updateErr)
			}
			if tag.RowsAffected() != 1 {
				return practice.Turn{},
					practiceinput.ErrPersistenceConflict
			}
		}
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO practice_turn_confirmations (
			owner_user_id, idempotency_key, payload_hash, turn_id, created_at
		) VALUES ($1, $2, $3, $4, $5)`,
		actor.UserID,
		command.IdempotencyKey,
		payloadHash,
		turn.ID,
		now,
	)
	if err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func validConfirmation(
	actor practiceinput.Actor,
	command practiceinput.ConfirmTurnCommand,
) bool {
	return validInputActor(actor) &&
		strings.TrimSpace(command.CandidateID) != "" &&
		command.EvidenceVersion > 0 &&
		strings.TrimSpace(command.ConfirmedText) != "" &&
		strings.TrimSpace(command.IdempotencyKey) != "" &&
		(command.RetryTurnID == "" ||
			validRetryTurnIdentifier(command.RetryTurnID))
}

func (r *Repository) GetTurn(
	ctx context.Context,
	actor practiceinput.Actor,
	turnID string,
) (practice.Turn, error) {
	if !validInputActor(actor) || strings.TrimSpace(turnID) == "" {
		return practice.Turn{}, practiceinput.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practice.Turn{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	turn, err := getTurn(ctx, tx, actor.UserID, turnID)
	if err != nil {
		return practice.Turn{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func (r *Repository) ListSessionTurns(
	ctx context.Context,
	actor practiceinput.Actor,
	sessionID string,
) ([]practice.Turn, error) {
	if !validInputActor(actor) || strings.TrimSpace(sessionID) == "" {
		return nil, practiceinput.ErrPersistenceInvalid
	}
	return r.listCompletedSessionTurns(ctx, actor.UserID, sessionID)
}

func (r *Repository) ListCompletedSessionTurns(
	ctx context.Context,
	ownerUserID string,
	sessionID string,
) ([]practice.Turn, error) {
	if !validUUID(ownerUserID) || strings.TrimSpace(sessionID) == "" {
		return nil, practiceinput.ErrPersistenceInvalid
	}
	return r.listCompletedSessionTurns(ctx, ownerUserID, sessionID)
}

func (r *Repository) listCompletedSessionTurns(
	ctx context.Context,
	ownerUserID string,
	sessionID string,
) ([]practice.Turn, error) {
	tx, err := r.beginOwnerRead(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(
		ctx,
		turnColumns+`
		 WHERE owner_user_id = $1
		   AND practice_session_id = $2
		   AND turn_kind = 'EFFECTIVE'
		 ORDER BY sequence, created_at, turn_id`,
		ownerUserID,
		sessionID,
	)
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	defer rows.Close()
	turns := make([]practice.Turn, 0)
	for rows.Next() {
		turn, scanErr := scanTurn(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, safeDatabaseError(err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, safeDatabaseError(err)
	}
	return turns, nil
}

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const questionColumns = `SELECT question_id, practice_session_id,
                                speaker_participant_id,
                                addressee_participant_ids, objective_id,
                                question_type,
                                COALESCE(parent_question_id, ''),
                                content, sequence, created_at
                         FROM practice_questions`

func getQuestion(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	questionID string,
) (practice.Question, error) {
	return scanQuestion(db.QueryRow(
		ctx,
		questionColumns+`
		 WHERE owner_user_id = $1 AND question_id = $2`,
		ownerUserID,
		questionID,
	))
}

func scanQuestion(row rowScanner) (practice.Question, error) {
	var question practice.Question
	err := row.Scan(
		&question.ID,
		&question.SessionID,
		&question.SpeakerParticipantID,
		&question.AddresseeParticipantIDs,
		&question.ObjectiveID,
		&question.Type,
		&question.ParentQuestionID,
		&question.Content,
		&question.Sequence,
		&question.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Question{}, practiceinput.ErrPersistenceNotFound
	}
	if err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	question.CreatedAt = question.CreatedAt.UTC()
	return question, nil
}

func findReservationByKey(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	key string,
) (practiceinput.StoredTranscriptionReservation, bool, error) {
	reservation, err := scanReservation(tx.QueryRow(
		ctx,
		reservationColumns+`
		 WHERE owner_user_id = $1 AND idempotency_key = $2
		 FOR UPDATE`,
		ownerUserID,
		key,
	))
	if errors.Is(err, practiceinput.ErrPersistenceNotFound) {
		return practiceinput.StoredTranscriptionReservation{}, false, nil
	}
	return reservation, err == nil, err
}

func lockReservation(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	reservationID string,
) (practiceinput.StoredTranscriptionReservation, error) {
	return getReservation(ctx, tx, ownerUserID, reservationID, " FOR UPDATE")
}

const reservationColumns = `SELECT reservation_id, question_id, practice_session_id,
                                    idempotency_key, input_fingerprint,
                                    respondent_participant_id, status, fencing_token,
                                    deletion_generation,
                                    lease_expires_at, COALESCE(candidate_id, ''),
                                    current_attempt_id, created_at, updated_at
                             FROM practice_transcription_reservations`

func getReservation(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	reservationID string,
	suffix string,
) (practiceinput.StoredTranscriptionReservation, error) {
	return scanReservation(db.QueryRow(
		ctx,
		reservationColumns+`
		 WHERE owner_user_id = $1 AND reservation_id = $2`+suffix,
		ownerUserID,
		reservationID,
	))
}

func scanReservation(
	row rowScanner,
) (practiceinput.StoredTranscriptionReservation, error) {
	var reservation practiceinput.StoredTranscriptionReservation
	err := row.Scan(
		&reservation.ID,
		&reservation.QuestionID,
		&reservation.SessionID,
		&reservation.IdempotencyKey,
		&reservation.InputFingerprint,
		&reservation.RespondentParticipantID,
		&reservation.Status,
		&reservation.FencingToken,
		&reservation.DeletionGeneration,
		&reservation.LeaseExpiresAt,
		&reservation.CandidateID,
		&reservation.CurrentAttemptID,
		&reservation.CreatedAt,
		&reservation.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinput.StoredTranscriptionReservation{}, practiceinput.ErrPersistenceNotFound
	}
	if err != nil {
		return practiceinput.StoredTranscriptionReservation{}, safeDatabaseError(err)
	}
	reservation.LeaseExpiresAt = reservation.LeaseExpiresAt.UTC()
	reservation.CreatedAt = reservation.CreatedAt.UTC()
	reservation.UpdatedAt = reservation.UpdatedAt.UTC()
	return reservation, nil
}

func getCandidate(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	candidateID string,
) (practiceinput.StoredTranscriptCandidate, error) {
	return candidateQuery(
		ctx,
		db,
		ownerUserID,
		candidateID,
		"",
	)
}

func lockCandidate(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	candidateID string,
) (practiceinput.StoredTranscriptCandidate, error) {
	return candidateQuery(
		ctx,
		tx,
		ownerUserID,
		candidateID,
		" FOR UPDATE",
	)
}

func candidateQuery(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	candidateID string,
	suffix string,
) (practiceinput.StoredTranscriptCandidate, error) {
	var candidate practiceinput.StoredTranscriptCandidate
	err := db.QueryRow(
		ctx,
		`SELECT candidate_id, reservation_id, question_id, practice_session_id,
		        respondent_participant_id, transcript_id, evidence_version, provider, model,
		        provider_request_id, transcript_text, status, created_at
		 FROM practice_transcript_candidates
		 WHERE owner_user_id = $1 AND candidate_id = $2`+suffix,
		ownerUserID,
		candidateID,
	).Scan(
		&candidate.ID,
		&candidate.ReservationID,
		&candidate.QuestionID,
		&candidate.SessionID,
		&candidate.RespondentParticipantID,
		&candidate.TranscriptID,
		&candidate.EvidenceVersion,
		&candidate.Provider,
		&candidate.Model,
		&candidate.ProviderRequestID,
		&candidate.Text,
		&candidate.Status,
		&candidate.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinput.StoredTranscriptCandidate{}, practiceinput.ErrPersistenceNotFound
	}
	if err != nil {
		return practiceinput.StoredTranscriptCandidate{}, safeDatabaseError(err)
	}
	candidate.CreatedAt = candidate.CreatedAt.UTC()
	return candidate, nil
}

const turnColumns = `SELECT turn_id, practice_session_id, question_id,
                            speaker_participant_id, addressee_participant_ids,
                            respondent_participant_id, sequence, interaction_mode,
                            answer_text, candidate_id, evidence_version,
                            turn_kind,
                            COALESCE(retry_request_id::text, ''),
                            COALESCE(original_turn_id, ''),
                            counts_toward_effective_turn_limit,
							effective_turns, session_completed,
							confirmed_at, created_at
                     FROM practice_turns`

type rowScanner interface {
	Scan(...any) error
}

func scanTurn(row rowScanner) (practice.Turn, error) {
	var turn practice.Turn
	err := row.Scan(
		&turn.ID,
		&turn.SessionID,
		&turn.QuestionID,
		&turn.SpeakerParticipantID,
		&turn.AddresseeParticipantIDs,
		&turn.RespondentParticipantID,
		&turn.Sequence,
		&turn.InteractionMode,
		&turn.AnswerText,
		&turn.CandidateID,
		&turn.EvidenceVersion,
		&turn.Kind,
		&turn.RetryRequestID,
		&turn.OriginalTurnID,
		&turn.CountsTowardTurnLimit,
		&turn.EffectiveTurns,
		&turn.SessionCompleted,
		&turn.ConfirmedAt,
		&turn.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, practiceinput.ErrPersistenceNotFound
	}
	if err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	turn.ConfirmedAt = turn.ConfirmedAt.UTC()
	turn.CreatedAt = turn.CreatedAt.UTC()
	return turn, nil
}

func getTurn(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	turnID string,
) (practice.Turn, error) {
	return scanTurn(db.QueryRow(
		ctx,
		turnColumns+` WHERE owner_user_id = $1 AND turn_id = $2`,
		ownerUserID,
		turnID,
	))
}

func lockTurn(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	turnID string,
) (practice.Turn, error) {
	return scanTurn(tx.QueryRow(
		ctx,
		turnColumns+`
		 WHERE owner_user_id = $1 AND turn_id = $2
		 FOR UPDATE`,
		ownerUserID,
		turnID,
	))
}

func findTurnByCandidate(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	candidateID string,
) (practice.Turn, bool, error) {
	turn, err := scanTurn(db.QueryRow(
		ctx,
		turnColumns+` WHERE owner_user_id = $1 AND candidate_id = $2`,
		ownerUserID,
		candidateID,
	))
	if errors.Is(err, practiceinput.ErrPersistenceNotFound) {
		return practice.Turn{}, false, nil
	}
	return turn, err == nil, err
}

func insertAttempt(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	reservation practiceinput.StoredTranscriptionReservation,
) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO practice_processing_attempts (
			owner_user_id, attempt_id, reservation_id, operation,
			fencing_token, status, lease_expires_at, started_at
		) VALUES ($1, $2, $3, 'transcription', $4, 'processing', $5, $6)`,
		ownerUserID,
		reservation.CurrentAttemptID,
		reservation.ID,
		reservation.FencingToken,
		reservation.LeaseExpiresAt,
		reservation.UpdatedAt,
	)
	if err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

func (r *Repository) beginActorRead(
	ctx context.Context,
	actor practiceinput.Actor,
) (pgx.Tx, error) {
	return r.beginOwnerRead(ctx, actor.UserID)
}

func (r *Repository) beginOwnerRead(
	ctx context.Context,
	ownerUserID string,
) (pgx.Tx, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	if _, err := ensureActorWritable(ctx, tx, ownerUserID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func ensureActorWritable(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
) (int64, error) {
	var accountStatus string
	var deletionGeneration int64
	err := tx.QueryRow(
		ctx,
		`SELECT users.account_status, COALESCE(fences.deletion_generation, 0)
		 FROM identity_users AS users
		 LEFT JOIN practice_deletion_fences AS fences
		   ON fences.owner_user_id = users.id
		 WHERE users.id = $1
		 FOR SHARE OF users`,
		ownerUserID,
	).Scan(&accountStatus, &deletionGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, practiceinput.ErrPersistenceNotFound
	}
	if err != nil {
		return 0, safeDatabaseError(err)
	}
	if accountStatus != "active" || deletionGeneration != 0 {
		return 0, practiceinput.ErrActorDeleted
	}
	return deletionGeneration, nil
}

func ensureJobWritable(
	ctx context.Context,
	tx pgx.Tx,
	job practiceinput.JobContext,
) error {
	var accountStatus string
	var deletionGeneration int64
	err := tx.QueryRow(
		ctx,
		`SELECT users.account_status, COALESCE(fences.deletion_generation, 0)
		 FROM identity_users AS users
		 LEFT JOIN practice_deletion_fences AS fences
		   ON fences.owner_user_id = users.id
		 WHERE users.id = $1
		 FOR SHARE OF users`,
		job.OwnerUserID,
	).Scan(&accountStatus, &deletionGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinput.ErrPersistenceNotFound
	}
	if err != nil {
		return safeDatabaseError(err)
	}
	if accountStatus != "active" ||
		deletionGeneration != job.DeletionGeneration {
		return practiceinput.ErrPersistenceConflict
	}
	return nil
}

func lockKey(
	ctx context.Context,
	tx pgx.Tx,
	parts ...string,
) error {
	key := strings.Join(parts, "\x1f")
	if _, err := tx.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		key,
	); err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

func lockEvidenceSourceSession(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	sessionID string,
) error {
	return lockKey(
		ctx,
		tx,
		ownerUserID,
		evidenceSourceLockNamespace,
		sessionID,
	)
}

func lockCandidateEvidenceSourceSession(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	candidateID string,
) (string, error) {
	var sessionID string
	err := tx.QueryRow(
		ctx,
		`SELECT practice_session_id
		 FROM practice_transcript_candidates
		 WHERE owner_user_id = $1 AND candidate_id = $2`,
		ownerUserID,
		candidateID,
	).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", practiceinput.ErrPersistenceNotFound
	}
	if err != nil {
		return "", safeDatabaseError(err)
	}
	if err := lockEvidenceSourceSession(
		ctx,
		tx,
		ownerUserID,
		sessionID,
	); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (r *Repository) reachedWriteFence() {
	if r.afterWriteFence != nil {
		r.afterWriteFence()
	}
}

func transactionTime(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&now); err != nil {
		return time.Time{}, safeDatabaseError(err)
	}
	return now.UTC(), nil
}

func validInputActor(actor practiceinput.Actor) bool {
	return actor.Valid() && validUUID(actor.UserID)
}

func validJob(job practiceinput.JobContext) bool {
	return job.Valid() && validUUID(job.OwnerUserID)
}

func validProcessingFailureCode(code string) bool {
	switch code {
	case "invalid_request",
		"configuration",
		"authentication",
		"authorization",
		"quota_exhausted",
		"rate_limited",
		"timeout",
		"provider_timeout",
		"provider_unavailable",
		"invalid_response",
		"cancelled",
		"legacy_provider_failure":
		return true
	default:
		return false
	}
}

func validUUID(value string) bool {
	var identifier pgtype.UUID
	return identifier.Scan(value) == nil && identifier.Valid
}

func containsParticipant(participants []string, participantID string) bool {
	for _, candidate := range participants {
		if candidate == participantID {
			return true
		}
	}
	return false
}

func validQuestion(question practice.Question) bool {
	if strings.TrimSpace(question.ID) == "" ||
		strings.TrimSpace(question.SessionID) == "" ||
		strings.TrimSpace(question.SpeakerParticipantID) == "" ||
		len(question.AddresseeParticipantIDs) == 0 ||
		strings.TrimSpace(question.ObjectiveID) == "" ||
		strings.TrimSpace(question.Content) == "" ||
		question.Sequence <= 0 {
		return false
	}
	seenAddressees := make(map[string]struct{}, len(question.AddresseeParticipantIDs))
	for _, addressee := range question.AddresseeParticipantIDs {
		if strings.TrimSpace(addressee) == "" {
			return false
		}
		if _, exists := seenAddressees[addressee]; exists {
			return false
		}
		seenAddressees[addressee] = struct{}{}
	}
	switch question.Type {
	case "PRIMARY":
		return question.ParentQuestionID == ""
	case "FOLLOW_UP":
		return strings.TrimSpace(question.ParentQuestionID) != ""
	default:
		return false
	}
}

func sameQuestion(
	left practice.Question,
	right practice.Question,
	compareCreatedAt bool,
) bool {
	if left.ID != right.ID ||
		left.SessionID != right.SessionID ||
		left.SpeakerParticipantID != right.SpeakerParticipantID ||
		left.ObjectiveID != right.ObjectiveID ||
		left.Type != right.Type ||
		left.ParentQuestionID != right.ParentQuestionID ||
		left.Content != right.Content ||
		left.Sequence != right.Sequence ||
		len(left.AddresseeParticipantIDs) != len(right.AddresseeParticipantIDs) {
		return false
	}
	if compareCreatedAt && !left.CreatedAt.Equal(right.CreatedAt) {
		return false
	}
	for index := range left.AddresseeParticipantIDs {
		if left.AddresseeParticipantIDs[index] != right.AddresseeParticipantIDs[index] {
			return false
		}
	}
	return true
}

func sameCandidateCompletion(
	candidate practiceinput.StoredTranscriptCandidate,
	command practiceinput.StoreCompleteTranscriptionCommand,
) bool {
	return candidate.TranscriptID == command.TranscriptID &&
		candidate.EvidenceVersion == command.EvidenceVersion &&
		candidate.Provider == command.Provider &&
		candidate.Model == command.Model &&
		candidate.ProviderRequestID == command.ProviderRequestID &&
		candidate.Text == command.Text
}

func confirmationHash(command practiceinput.ConfirmTurnCommand) string {
	payload := fmt.Sprintf(
		"%s\x00%d\x00%s",
		command.CandidateID,
		command.EvidenceVersion,
		command.ConfirmedText,
	)
	if command.RetryTurnID != "" {
		payload += "\x00" + command.RetryTurnID
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func newID(prefix string) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("cryptographic random source unavailable")
	}
	return prefix + "_" + hex.EncodeToString(bytes)
}

func databaseTime(now func() time.Time) time.Time {
	return now().UTC().Truncate(time.Microsecond)
}

func safeDatabaseError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23505", "23514":
			return practiceinput.ErrPersistenceConflict
		}
	}
	return practiceinput.ErrPersistenceUnavailable
}

var _ practiceinput.PersistenceStore = (*Repository)(nil)
