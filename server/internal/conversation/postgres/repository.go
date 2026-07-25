// Package postgres implements Conversation's production persistence boundary.
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
	"github.com/jackc/pgx/v5/pgxpool"

	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
)

type Repository struct {
	pool            *pgxpool.Pool
	now             func() time.Time
	afterWriteFence func()
}

func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("conversation postgres pool is required")
	}
	return &Repository{pool: pool, now: time.Now}, nil
}

func (r *Repository) SaveQuestion(
	ctx context.Context,
	actor conversation.Actor,
	question conversation.PersistentQuestion,
) (conversation.PersistentQuestion, error) {
	if !validActor(actor) || !validQuestion(question) {
		return conversation.PersistentQuestion{}, conversation.ErrPersistenceInvalid
	}
	createdAtProvided := !question.CreatedAt.IsZero()
	if question.CreatedAt.IsZero() {
		question.CreatedAt = databaseTime(r.now)
	} else {
		question.CreatedAt = question.CreatedAt.UTC().Truncate(time.Microsecond)
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return conversation.PersistentQuestion{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return conversation.PersistentQuestion{}, err
	}
	r.reachedWriteFence()
	if err := lockKey(ctx, tx, actor.UserID, "question", question.ID); err != nil {
		return conversation.PersistentQuestion{}, err
	}
	if question.Type == "FOLLOW_UP" {
		var parentSession string
		var parentType string
		err := tx.QueryRow(
			ctx,
			`SELECT practice_session_id, question_type
			 FROM conversation_questions
			 WHERE owner_user_id = $1 AND question_id = $2`,
			actor.UserID,
			question.ParentQuestionID,
		).Scan(&parentSession, &parentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return conversation.PersistentQuestion{}, conversation.ErrPersistenceInvalid
		}
		if err != nil {
			return conversation.PersistentQuestion{}, safeDatabaseError(err)
		}
		if parentSession != question.SessionID || parentType != "PRIMARY" {
			return conversation.PersistentQuestion{}, conversation.ErrPersistenceInvalid
		}
	}

	tag, err := tx.Exec(
		ctx,
		`INSERT INTO conversation_questions (
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
		return conversation.PersistentQuestion{}, safeDatabaseError(err)
	}

	saved, err := getQuestion(ctx, tx, actor.UserID, question.ID)
	if err != nil {
		return conversation.PersistentQuestion{}, err
	}
	if tag.RowsAffected() == 0 &&
		!sameQuestion(saved, question, createdAtProvided) {
		return conversation.PersistentQuestion{}, conversation.ErrPersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.PersistentQuestion{}, safeDatabaseError(err)
	}
	return saved, nil
}

func (r *Repository) GetQuestion(
	ctx context.Context,
	actor conversation.Actor,
	questionID string,
) (conversation.PersistentQuestion, error) {
	if !validActor(actor) || strings.TrimSpace(questionID) == "" {
		return conversation.PersistentQuestion{}, conversation.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return conversation.PersistentQuestion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	question, err := getQuestion(ctx, tx, actor.UserID, questionID)
	if err != nil {
		return conversation.PersistentQuestion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.PersistentQuestion{}, safeDatabaseError(err)
	}
	return question, nil
}

func (r *Repository) ReserveTranscription(
	ctx context.Context,
	actor conversation.Actor,
	command conversation.ReserveTranscriptionCommand,
) (conversation.TranscriptionReservation, error) {
	if !validActor(actor) ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		strings.TrimSpace(command.InputFingerprint) == "" ||
		strings.TrimSpace(command.RespondentParticipantID) == "" ||
		command.LeaseDuration <= 0 {
		return conversation.TranscriptionReservation{}, conversation.ErrPersistenceInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	deletionGeneration, err := ensureActorWritable(ctx, tx, actor.UserID)
	if err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	r.reachedWriteFence()
	if err := lockKey(
		ctx,
		tx,
		actor.UserID,
		"transcription",
		command.IdempotencyKey,
	); err != nil {
		return conversation.TranscriptionReservation{}, err
	}

	question, err := getQuestion(ctx, tx, actor.UserID, command.QuestionID)
	if err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	if question.SessionID != command.SessionID {
		return conversation.TranscriptionReservation{}, conversation.ErrPersistenceNotFound
	}

	reservation, found, err := findReservationByKey(
		ctx,
		tx,
		actor.UserID,
		command.IdempotencyKey,
	)
	if err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	now, err := transactionTime(ctx, tx)
	if err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	if found {
		if reservation.InputFingerprint != command.InputFingerprint ||
			reservation.QuestionID != command.QuestionID ||
			reservation.SessionID != command.SessionID ||
			reservation.RespondentParticipantID != command.RespondentParticipantID {
			return conversation.TranscriptionReservation{}, conversation.ErrPersistenceConflict
		}
		if reservation.Status == conversation.TranscriptionCompleted ||
			(reservation.Status == conversation.TranscriptionProcessing &&
				reservation.LeaseExpiresAt.After(now)) {
			reservation.LeaseAcquired = false
			if err := tx.Commit(ctx); err != nil {
				return conversation.TranscriptionReservation{}, safeDatabaseError(err)
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

	reservation = conversation.TranscriptionReservation{
		ID:                      newID("asr_res"),
		QuestionID:              command.QuestionID,
		SessionID:               command.SessionID,
		IdempotencyKey:          command.IdempotencyKey,
		InputFingerprint:        command.InputFingerprint,
		RespondentParticipantID: command.RespondentParticipantID,
		Status:                  conversation.TranscriptionProcessing,
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
		`INSERT INTO conversation_transcription_reservations (
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
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
	}
	if err := insertAttempt(ctx, tx, actor.UserID, reservation); err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
	}
	return reservation, nil
}

func (r *Repository) takeOverReservation(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	reservation conversation.TranscriptionReservation,
	now time.Time,
	leaseDuration time.Duration,
) (conversation.TranscriptionReservation, error) {
	_, err := tx.Exec(
		ctx,
		`UPDATE conversation_processing_attempts
		 SET status = 'expired', finished_at = $1
		 WHERE owner_user_id = $2 AND attempt_id = $3 AND status = 'processing'`,
		now,
		ownerUserID,
		reservation.CurrentAttemptID,
	)
	if err != nil {
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
	}

	reservation.Status = conversation.TranscriptionProcessing
	reservation.FencingToken++
	reservation.LeaseAcquired = true
	reservation.LeaseExpiresAt = now.Add(leaseDuration)
	reservation.CurrentAttemptID = newID("asr_attempt")
	reservation.UpdatedAt = now
	_, err = tx.Exec(
		ctx,
		`UPDATE conversation_transcription_reservations
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
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
	}
	if err := insertAttempt(ctx, tx, ownerUserID, reservation); err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
	}
	return reservation, nil
}

func (r *Repository) CompleteTranscription(
	ctx context.Context,
	job conversation.JobContext,
	command conversation.CompleteTranscriptionCommand,
) (conversation.TranscriptCandidate, error) {
	if !validJob(job) ||
		strings.TrimSpace(command.TranscriptID) == "" ||
		command.EvidenceVersion <= 0 ||
		strings.TrimSpace(command.Provider) == "" ||
		strings.TrimSpace(command.Model) == "" ||
		strings.TrimSpace(command.Text) == "" {
		return conversation.TranscriptCandidate{}, conversation.ErrPersistenceInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return conversation.TranscriptCandidate{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureJobWritable(ctx, tx, job); err != nil {
		return conversation.TranscriptCandidate{}, err
	}
	r.reachedWriteFence()
	reservation, err := lockReservation(
		ctx,
		tx,
		job.OwnerUserID,
		job.ReservationID,
	)
	if err != nil {
		return conversation.TranscriptCandidate{}, err
	}
	if reservation.Status == conversation.TranscriptionCompleted {
		candidate, candidateErr := getCandidate(
			ctx,
			tx,
			job.OwnerUserID,
			reservation.CandidateID,
		)
		if candidateErr != nil {
			return conversation.TranscriptCandidate{}, candidateErr
		}
		if reservation.FencingToken != job.FencingToken ||
			reservation.DeletionGeneration != job.DeletionGeneration ||
			!sameCandidateCompletion(candidate, command) {
			return conversation.TranscriptCandidate{}, conversation.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return conversation.TranscriptCandidate{}, safeDatabaseError(err)
		}
		return candidate, nil
	}
	now, err := transactionTime(ctx, tx)
	if err != nil {
		return conversation.TranscriptCandidate{}, err
	}
	if reservation.Status != conversation.TranscriptionProcessing ||
		reservation.FencingToken != job.FencingToken ||
		reservation.DeletionGeneration != job.DeletionGeneration ||
		!reservation.LeaseExpiresAt.After(now) {
		return conversation.TranscriptCandidate{}, conversation.ErrPersistenceConflict
	}

	candidate := conversation.TranscriptCandidate{
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
		Status:                  conversation.CandidateReady,
		CreatedAt:               now,
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO conversation_transcript_candidates (
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
		return conversation.TranscriptCandidate{}, safeDatabaseError(err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE conversation_processing_attempts
		 SET status = 'completed', provider_request_id = $1, finished_at = $2
		 WHERE owner_user_id = $3 AND attempt_id = $4`,
		command.ProviderRequestID,
		now,
		job.OwnerUserID,
		reservation.CurrentAttemptID,
	)
	if err != nil {
		return conversation.TranscriptCandidate{}, safeDatabaseError(err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE conversation_transcription_reservations
		 SET status = 'completed', candidate_id = $1, updated_at = $2
		 WHERE owner_user_id = $3 AND reservation_id = $4`,
		candidate.ID,
		now,
		job.OwnerUserID,
		reservation.ID,
	)
	if err != nil {
		return conversation.TranscriptCandidate{}, safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.TranscriptCandidate{}, safeDatabaseError(err)
	}
	return candidate, nil
}

func (r *Repository) FailTranscription(
	ctx context.Context,
	job conversation.JobContext,
	failure conversation.ProcessingFailure,
) error {
	if !validJob(job) ||
		strings.TrimSpace(failure.Code) == "" ||
		failure.Duration < 0 {
		return conversation.ErrPersistenceInvalid
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
	if reservation.Status != conversation.TranscriptionProcessing ||
		reservation.FencingToken != job.FencingToken ||
		reservation.DeletionGeneration != job.DeletionGeneration ||
		!reservation.LeaseExpiresAt.After(now) {
		return conversation.ErrPersistenceConflict
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE conversation_processing_attempts
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
		`UPDATE conversation_transcription_reservations
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
	actor conversation.Actor,
	reservationID string,
) (conversation.TranscriptionReservation, error) {
	if !validActor(actor) || strings.TrimSpace(reservationID) == "" {
		return conversation.TranscriptionReservation{}, conversation.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reservation, err := getReservation(ctx, tx, actor.UserID, reservationID, "")
	if err != nil {
		return conversation.TranscriptionReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
	}
	return reservation, nil
}

func (r *Repository) GetCandidate(
	ctx context.Context,
	actor conversation.Actor,
	candidateID string,
) (conversation.TranscriptCandidate, error) {
	if !validActor(actor) || strings.TrimSpace(candidateID) == "" {
		return conversation.TranscriptCandidate{}, conversation.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return conversation.TranscriptCandidate{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	candidate, err := getCandidate(ctx, tx, actor.UserID, candidateID)
	if err != nil {
		return conversation.TranscriptCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.TranscriptCandidate{}, safeDatabaseError(err)
	}
	return candidate, nil
}

func (r *Repository) ListProcessingAttempts(
	ctx context.Context,
	actor conversation.Actor,
	reservationID string,
) ([]conversation.ProcessingAttempt, error) {
	if !validActor(actor) || strings.TrimSpace(reservationID) == "" {
		return nil, conversation.ErrPersistenceInvalid
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
		 FROM conversation_processing_attempts
		 WHERE owner_user_id = $1 AND reservation_id = $2
		 ORDER BY fencing_token`,
		actor.UserID,
		reservationID,
	)
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	defer rows.Close()
	attempts := make([]conversation.ProcessingAttempt, 0)
	for rows.Next() {
		var attempt conversation.ProcessingAttempt
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
	actor conversation.Actor,
	command conversation.ConfirmTurnCommand,
) (conversation.ConfirmedTurn, error) {
	if !validActor(actor) ||
		strings.TrimSpace(command.CandidateID) == "" ||
		command.EvidenceVersion <= 0 ||
		strings.TrimSpace(command.ConfirmedText) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" {
		return conversation.ConfirmedTurn{}, conversation.ErrPersistenceInvalid
	}
	payloadHash := confirmationHash(command)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	r.reachedWriteFence()
	if err := lockKey(
		ctx,
		tx,
		actor.UserID,
		"confirmation",
		command.IdempotencyKey,
	); err != nil {
		return conversation.ConfirmedTurn{}, err
	}

	var existingHash string
	var existingTurnID string
	err = tx.QueryRow(
		ctx,
		`SELECT payload_hash, turn_id
		 FROM conversation_turn_confirmations
		 WHERE owner_user_id = $1 AND idempotency_key = $2`,
		actor.UserID,
		command.IdempotencyKey,
	).Scan(&existingHash, &existingTurnID)
	if err == nil {
		if existingHash != payloadHash {
			return conversation.ConfirmedTurn{}, conversation.ErrPersistenceConflict
		}
		turn, turnErr := getTurn(ctx, tx, actor.UserID, existingTurnID)
		if turnErr != nil {
			return conversation.ConfirmedTurn{}, turnErr
		}
		if err := tx.Commit(ctx); err != nil {
			return conversation.ConfirmedTurn{}, safeDatabaseError(err)
		}
		return turn, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}

	candidate, err := lockCandidate(ctx, tx, actor.UserID, command.CandidateID)
	if err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	if candidate.EvidenceVersion != command.EvidenceVersion {
		return conversation.ConfirmedTurn{}, conversation.ErrPersistenceConflict
	}

	turn, found, err := findTurnByCandidate(
		ctx,
		tx,
		actor.UserID,
		candidate.ID,
	)
	if err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	now := databaseTime(r.now)
	if found {
		if turn.EvidenceVersion != command.EvidenceVersion ||
			turn.AnswerText != command.ConfirmedText {
			return conversation.ConfirmedTurn{}, conversation.ErrPersistenceConflict
		}
	} else {
		question, questionErr := getQuestion(
			ctx,
			tx,
			actor.UserID,
			candidate.QuestionID,
		)
		if questionErr != nil {
			return conversation.ConfirmedTurn{}, questionErr
		}
		turn = conversation.ConfirmedTurn{
			ID:                      newID("turn"),
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
			ConfirmedAt:             now,
			CreatedAt:               now,
		}
		_, err = tx.Exec(
			ctx,
			`INSERT INTO conversation_confirmed_turns (
				owner_user_id, turn_id, candidate_id, question_id,
				practice_session_id, speaker_participant_id,
				addressee_participant_ids, respondent_participant_id,
				sequence, interaction_mode, answer_text, evidence_version,
				confirmed_at, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13
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
		)
		if err != nil {
			return conversation.ConfirmedTurn{}, safeDatabaseError(err)
		}
		_, err = tx.Exec(
			ctx,
			`UPDATE conversation_transcript_candidates
			 SET status = 'confirmed'
			 WHERE owner_user_id = $1 AND candidate_id = $2`,
			actor.UserID,
			candidate.ID,
		)
		if err != nil {
			return conversation.ConfirmedTurn{}, safeDatabaseError(err)
		}
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO conversation_turn_confirmations (
			owner_user_id, idempotency_key, payload_hash, turn_id, created_at
		) VALUES ($1, $2, $3, $4, $5)`,
		actor.UserID,
		command.IdempotencyKey,
		payloadHash,
		turn.ID,
		now,
	)
	if err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func (r *Repository) GetTurn(
	ctx context.Context,
	actor conversation.Actor,
	turnID string,
) (conversation.ConfirmedTurn, error) {
	if !validActor(actor) || strings.TrimSpace(turnID) == "" {
		return conversation.ConfirmedTurn{}, conversation.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	turn, err := getTurn(ctx, tx, actor.UserID, turnID)
	if err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func (r *Repository) SaveTurnProgress(
	ctx context.Context,
	actor conversation.Actor,
	turnID string,
	progress conversation.TurnProgress,
) (conversation.ConfirmedTurn, error) {
	if !validActor(actor) ||
		strings.TrimSpace(turnID) == "" ||
		progress.EffectiveTurns <= 0 {
		return conversation.ConfirmedTurn{}, conversation.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	r.reachedWriteFence()
	turn, err := lockTurn(ctx, tx, actor.UserID, turnID)
	if err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	if turn.Progress.EffectiveTurns != 0 {
		if progress != turn.Progress {
			return conversation.ConfirmedTurn{}, conversation.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return conversation.ConfirmedTurn{}, safeDatabaseError(err)
		}
		return turn, nil
	}
	now := databaseTime(r.now)
	_, err = tx.Exec(
		ctx,
		`UPDATE conversation_confirmed_turns
		 SET effective_turns = $1, session_completed = $2,
		     progress_recorded_at = $3
		 WHERE owner_user_id = $4 AND turn_id = $5`,
		progress.EffectiveTurns,
		progress.SessionCompleted,
		now,
		actor.UserID,
		turnID,
	)
	if err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	turn.Progress = progress
	if err := tx.Commit(ctx); err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func (r *Repository) SaveTurnReview(
	ctx context.Context,
	actor conversation.Actor,
	turnID string,
	checkpoint conversation.TurnReviewCheckpoint,
) (conversation.ConfirmedTurn, error) {
	if !validActor(actor) ||
		strings.TrimSpace(turnID) == "" ||
		strings.TrimSpace(checkpoint.ReviewID) == "" ||
		checkpoint.SourceTurnID != turnID {
		return conversation.ConfirmedTurn{}, conversation.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	r.reachedWriteFence()
	turn, err := lockTurn(ctx, tx, actor.UserID, turnID)
	if err != nil {
		return conversation.ConfirmedTurn{}, err
	}
	if !turn.Progress.SessionCompleted {
		return conversation.ConfirmedTurn{}, conversation.ErrPersistenceConflict
	}
	if turn.Review.ReviewID != "" {
		if turn.Review != checkpoint {
			return conversation.ConfirmedTurn{}, conversation.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return conversation.ConfirmedTurn{}, safeDatabaseError(err)
		}
		return turn, nil
	}
	now := databaseTime(r.now)
	_, err = tx.Exec(
		ctx,
		`UPDATE conversation_confirmed_turns
		 SET review_id = $1, review_source_turn_id = $2,
		     review_recorded_at = $3
		 WHERE owner_user_id = $4 AND turn_id = $5`,
		checkpoint.ReviewID,
		checkpoint.SourceTurnID,
		now,
		actor.UserID,
		turnID,
	)
	if err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	turn.Review = checkpoint
	if err := tx.Commit(ctx); err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func (r *Repository) ListSessionTurns(
	ctx context.Context,
	actor conversation.Actor,
	sessionID string,
) ([]conversation.ConfirmedTurn, error) {
	if !validActor(actor) || strings.TrimSpace(sessionID) == "" {
		return nil, conversation.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(
		ctx,
		turnColumns+`
		 WHERE owner_user_id = $1 AND practice_session_id = $2
		 ORDER BY sequence, created_at, turn_id`,
		actor.UserID,
		sessionID,
	)
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	defer rows.Close()
	turns := make([]conversation.ConfirmedTurn, 0)
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

func (r *Repository) DeleteUserData(
	ctx context.Context,
	deletion conversation.DeletionContext,
) error {
	if !validDeletion(deletion) {
		return conversation.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockKey(ctx, tx, deletion.OwnerUserID, "deletion"); err != nil {
		return err
	}
	var accountStatus string
	err = tx.QueryRow(
		ctx,
		`SELECT account_status FROM identity_users WHERE id = $1 FOR UPDATE`,
		deletion.OwnerUserID,
	).Scan(&accountStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		// A repeated coordinator call after final Identity removal is complete.
		// The RESTRICT root foreign key guarantees no Question aggregate remains.
		return nil
	}
	if err != nil {
		return safeDatabaseError(err)
	}
	if accountStatus != "deleting" && accountStatus != "deleted" {
		return conversation.ErrPersistenceConflict
	}
	now := databaseTime(r.now)
	var appliedGeneration int64
	err = tx.QueryRow(
		ctx,
		`SELECT deletion_generation
		 FROM conversation_deletion_fences
		 WHERE owner_user_id = $1
		 FOR UPDATE`,
		deletion.OwnerUserID,
	).Scan(&appliedGeneration)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return safeDatabaseError(err)
	}
	if err == nil && appliedGeneration > deletion.DeletionGeneration {
		return conversation.ErrPersistenceConflict
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO conversation_deletion_fences (
			owner_user_id, deletion_generation, applied_at
		 ) VALUES ($1, $2, $3)
		 ON CONFLICT (owner_user_id) DO UPDATE
		 SET deletion_generation = EXCLUDED.deletion_generation,
		     applied_at = EXCLUDED.applied_at
		 WHERE conversation_deletion_fences.deletion_generation
		       <= EXCLUDED.deletion_generation`,
		deletion.OwnerUserID,
		deletion.DeletionGeneration,
		now,
	)
	if err != nil {
		return safeDatabaseError(err)
	}
	// Question is the root of every durable Conversation aggregate. Conversation
	// deletes it explicitly; Identity's RESTRICT foreign key cannot substitute
	// for this module-owned deletion port.
	_, err = tx.Exec(
		ctx,
		`DELETE FROM conversation_questions WHERE owner_user_id = $1`,
		deletion.OwnerUserID,
	)
	if err != nil {
		return safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getQuestion(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	questionID string,
) (conversation.PersistentQuestion, error) {
	var question conversation.PersistentQuestion
	err := db.QueryRow(
		ctx,
		`SELECT question_id, practice_session_id, speaker_participant_id,
		        addressee_participant_ids, objective_id, question_type,
		        COALESCE(parent_question_id, ''),
		        content, sequence, created_at
		 FROM conversation_questions
		 WHERE owner_user_id = $1 AND question_id = $2`,
		ownerUserID,
		questionID,
	).Scan(
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
		return conversation.PersistentQuestion{}, conversation.ErrPersistenceNotFound
	}
	if err != nil {
		return conversation.PersistentQuestion{}, safeDatabaseError(err)
	}
	question.CreatedAt = question.CreatedAt.UTC()
	return question, nil
}

func findReservationByKey(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	key string,
) (conversation.TranscriptionReservation, bool, error) {
	reservation, err := scanReservation(tx.QueryRow(
		ctx,
		reservationColumns+`
		 WHERE owner_user_id = $1 AND idempotency_key = $2
		 FOR UPDATE`,
		ownerUserID,
		key,
	))
	if errors.Is(err, conversation.ErrPersistenceNotFound) {
		return conversation.TranscriptionReservation{}, false, nil
	}
	return reservation, err == nil, err
}

func lockReservation(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	reservationID string,
) (conversation.TranscriptionReservation, error) {
	return getReservation(ctx, tx, ownerUserID, reservationID, " FOR UPDATE")
}

const reservationColumns = `SELECT reservation_id, question_id, practice_session_id,
                                    idempotency_key, input_fingerprint,
                                    respondent_participant_id, status, fencing_token,
                                    deletion_generation,
                                    lease_expires_at, COALESCE(candidate_id, ''),
                                    current_attempt_id, created_at, updated_at
                             FROM conversation_transcription_reservations`

func getReservation(
	ctx context.Context,
	db queryRow,
	ownerUserID string,
	reservationID string,
	suffix string,
) (conversation.TranscriptionReservation, error) {
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
) (conversation.TranscriptionReservation, error) {
	var reservation conversation.TranscriptionReservation
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
		return conversation.TranscriptionReservation{}, conversation.ErrPersistenceNotFound
	}
	if err != nil {
		return conversation.TranscriptionReservation{}, safeDatabaseError(err)
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
) (conversation.TranscriptCandidate, error) {
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
) (conversation.TranscriptCandidate, error) {
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
) (conversation.TranscriptCandidate, error) {
	var candidate conversation.TranscriptCandidate
	err := db.QueryRow(
		ctx,
		`SELECT candidate_id, reservation_id, question_id, practice_session_id,
		        respondent_participant_id, transcript_id, evidence_version, provider, model,
		        provider_request_id, transcript_text, status, created_at
		 FROM conversation_transcript_candidates
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
		return conversation.TranscriptCandidate{}, conversation.ErrPersistenceNotFound
	}
	if err != nil {
		return conversation.TranscriptCandidate{}, safeDatabaseError(err)
	}
	candidate.CreatedAt = candidate.CreatedAt.UTC()
	return candidate, nil
}

const turnColumns = `SELECT turn_id, practice_session_id, question_id,
                            speaker_participant_id, addressee_participant_ids,
                            respondent_participant_id, sequence, interaction_mode,
                            answer_text, candidate_id, evidence_version,
                            effective_turns, session_completed,
                            COALESCE(review_id, ''),
                            COALESCE(review_source_turn_id, ''),
                            confirmed_at, created_at
                     FROM conversation_confirmed_turns`

type rowScanner interface {
	Scan(...any) error
}

func scanTurn(row rowScanner) (conversation.ConfirmedTurn, error) {
	var turn conversation.ConfirmedTurn
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
		&turn.Progress.EffectiveTurns,
		&turn.Progress.SessionCompleted,
		&turn.Review.ReviewID,
		&turn.Review.SourceTurnID,
		&turn.ConfirmedAt,
		&turn.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.ConfirmedTurn{}, conversation.ErrPersistenceNotFound
	}
	if err != nil {
		return conversation.ConfirmedTurn{}, safeDatabaseError(err)
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
) (conversation.ConfirmedTurn, error) {
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
) (conversation.ConfirmedTurn, error) {
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
) (conversation.ConfirmedTurn, bool, error) {
	turn, err := scanTurn(db.QueryRow(
		ctx,
		turnColumns+` WHERE owner_user_id = $1 AND candidate_id = $2`,
		ownerUserID,
		candidateID,
	))
	if errors.Is(err, conversation.ErrPersistenceNotFound) {
		return conversation.ConfirmedTurn{}, false, nil
	}
	return turn, err == nil, err
}

func insertAttempt(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	reservation conversation.TranscriptionReservation,
) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO conversation_processing_attempts (
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
	actor conversation.Actor,
) (pgx.Tx, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
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
		 LEFT JOIN conversation_deletion_fences AS fences
		   ON fences.owner_user_id = users.id
		 WHERE users.id = $1
		 FOR SHARE OF users`,
		ownerUserID,
	).Scan(&accountStatus, &deletionGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, conversation.ErrPersistenceNotFound
	}
	if err != nil {
		return 0, safeDatabaseError(err)
	}
	if accountStatus != "active" || deletionGeneration != 0 {
		return 0, conversation.ErrActorDeleted
	}
	return deletionGeneration, nil
}

func ensureJobWritable(
	ctx context.Context,
	tx pgx.Tx,
	job conversation.JobContext,
) error {
	var accountStatus string
	var deletionGeneration int64
	err := tx.QueryRow(
		ctx,
		`SELECT users.account_status, COALESCE(fences.deletion_generation, 0)
		 FROM identity_users AS users
		 LEFT JOIN conversation_deletion_fences AS fences
		   ON fences.owner_user_id = users.id
		 WHERE users.id = $1
		 FOR SHARE OF users`,
		job.OwnerUserID,
	).Scan(&accountStatus, &deletionGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.ErrPersistenceNotFound
	}
	if err != nil {
		return safeDatabaseError(err)
	}
	if accountStatus != "active" ||
		deletionGeneration != job.DeletionGeneration {
		return conversation.ErrPersistenceConflict
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

func validActor(actor conversation.Actor) bool {
	return actor.Valid() && validUUID(actor.UserID)
}

func validJob(job conversation.JobContext) bool {
	return job.Valid() && validUUID(job.OwnerUserID)
}

func validDeletion(deletion conversation.DeletionContext) bool {
	return deletion.Valid() && validUUID(deletion.OwnerUserID)
}

func validUUID(value string) bool {
	var identifier pgtype.UUID
	return identifier.Scan(value) == nil && identifier.Valid
}

func validQuestion(question conversation.PersistentQuestion) bool {
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
	left conversation.PersistentQuestion,
	right conversation.PersistentQuestion,
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
	candidate conversation.TranscriptCandidate,
	command conversation.CompleteTranscriptionCommand,
) bool {
	return candidate.TranscriptID == command.TranscriptID &&
		candidate.EvidenceVersion == command.EvidenceVersion &&
		candidate.Provider == command.Provider &&
		candidate.Model == command.Model &&
		candidate.ProviderRequestID == command.ProviderRequestID &&
		candidate.Text == command.Text
}

func confirmationHash(command conversation.ConfirmTurnCommand) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%d\x00%s",
		command.CandidateID,
		command.EvidenceVersion,
		command.ConfirmedText,
	)))
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
			return conversation.ErrPersistenceConflict
		}
	}
	return conversation.ErrPersistenceUnavailable
}

var _ conversation.PersistenceStore = (*Repository)(nil)
