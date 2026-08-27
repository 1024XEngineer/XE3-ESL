package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
)

func (r *Repository) ConfirmTurn(
	ctx context.Context,
	actor practiceinteraction.Actor,
	command practiceinteraction.ConfirmTurnCommand,
) (practice.Turn, error) {
	if r == nil || r.pool == nil || ctx == nil || !validConfirmation(actor, command) {
		return practice.Turn{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureActorWritableForEvaluation(ctx, tx, actor.UserID); err != nil {
		return practice.Turn{}, err
	}
	r.reachedWriteFence()
	sourceSessionID, err := lockCandidateEvidenceSourceSession(
		ctx, tx, actor.UserID, command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	turn, err := r.confirmTurnInTransaction(ctx, tx, actor, command, sourceSessionID)
	if err != nil {
		return practice.Turn{}, err
	}
	turn, err = r.advanceConfirmedTurnInTransaction(ctx, tx, actor, turn)
	if err != nil {
		return practice.Turn{}, err
	}
	if err := r.Repository.ScheduleTurnFeedbackInTransaction(
		ctx, tx, actor.UserID, turn.ID,
	); err != nil {
		return practice.Turn{}, mapPracticeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	return turn, nil
}

func (r *Repository) confirmTurnInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	actor practiceinteraction.Actor,
	command practiceinteraction.ConfirmTurnCommand,
	sourceSessionID string,
) (practice.Turn, error) {
	fingerprint := confirmationFingerprint(command)
	if err := lockKey(ctx, tx, actor.UserID, "confirmation", command.IdempotencyKey); err != nil {
		return practice.Turn{}, err
	}
	var existingFingerprint []byte
	var existingTurnID string
	err := tx.QueryRow(ctx, `
		SELECT t.confirmation_fingerprint, t.turn_id FROM practice_turns t
		JOIN practice_sessions s ON s.session_id=t.session_id
		WHERE s.user_id = $1 AND t.session_id = $2
		  AND t.confirmation_client_request_id = $3
	`, actor.UserID, sourceSessionID, command.IdempotencyKey).
		Scan(&existingFingerprint, &existingTurnID)
	if err == nil {
		if !bytes.Equal(existingFingerprint, fingerprint) {
			return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
		}
		return getTurn(ctx, tx, actor.UserID, existingTurnID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, safeDatabaseError(err)
	}
	candidate, err := getCandidateInTransaction(
		ctx, tx, actor.UserID, command.CandidateID, true,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	if candidate.SessionID != sourceSessionID ||
		candidate.EvidenceVersion != command.EvidenceVersion ||
		candidate.Text != strings.TrimSpace(command.ConfirmedText) {
		return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
	}
	row, err := lockTranscriptionByCandidate(ctx, tx, actor.UserID, candidate.ID)
	if err != nil {
		return practice.Turn{}, err
	}
	if row.CandidateID != candidate.ID || row.SessionID != sourceSessionID {
		return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
	}
	switch row.TurnKind {
	case practice.TurnKindEffective:
		if command.RetryTurnID != "" {
			return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
		}
		var alreadyConfirmed bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM practice_turns
				WHERE session_id = $1
				  AND question_id = $2
				  AND turn_kind = 'EFFECTIVE'
				  AND status = 'confirmed'
				  AND turn_id <> $3
			)`, row.SessionID, row.QuestionID, row.TurnID,
		).Scan(&alreadyConfirmed); err != nil {
			return practice.Turn{}, safeDatabaseError(err)
		}
		if alreadyConfirmed {
			return practice.Turn{}, practiceinteraction.ErrConflict
		}
	case practice.TurnKindRetry:
		if command.RetryTurnID != row.TurnID {
			return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
		}
	default:
		return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
	}
	if row.Status == "confirmed" {
		return getTurn(ctx, tx, actor.UserID, row.TurnID)
	}
	if row.Status != "transcribed" {
		return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
	}
	interactionMode := "PUSH_TO_TALK"
	if candidate.Provider == "speakup" && candidate.Model == "direct_text" {
		interactionMode = "TEXT"
	}
	now := databaseTime(r.now)
	tag, err := tx.Exec(ctx, `
		UPDATE practice_turns t
		SET status = 'confirmed', interaction_mode = $3,
		    confirmation_client_request_id = $4,
		    confirmation_fingerprint = $5, confirmed_at = $6,
		    updated_at = $6
		FROM practice_sessions s
		WHERE s.session_id=t.session_id AND s.user_id = $1
		  AND t.turn_id = $2 AND t.status = 'transcribed'
	`, actor.UserID, row.TurnID, interactionMode, command.IdempotencyKey,
		fingerprint, now)
	if err != nil {
		return practice.Turn{}, mapConfirmedTurnWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
	}
	return getTurn(ctx, tx, actor.UserID, row.TurnID)
}

func mapConfirmedTurnWriteError(err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" &&
		pgError.ConstraintName ==
			"practice_turns_one_confirmed_effective_question_idx" {
		return practiceinteraction.ErrConflict
	}
	return safeDatabaseError(err)
}

func (r *Repository) advanceConfirmedTurnInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	actor practiceinteraction.Actor,
	turn practice.Turn,
) (practice.Turn, error) {
	if turn.Kind == practice.TurnKindRetry {
		var status practice.SessionStatus
		var effectiveTurns, version int
		err := tx.QueryRow(ctx, `
			SELECT status, effective_turns, version FROM practice_sessions
			WHERE user_id = $1 AND session_id = $2 FOR UPDATE
		`, actor.UserID, turn.SessionID).Scan(&status, &effectiveTurns, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return practice.Turn{}, practiceinteraction.ErrPersistenceNotFound
		}
		if err != nil {
			return practice.Turn{}, safeDatabaseError(err)
		}
		if effectiveTurns < 1 {
			return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
		}
		tag, err := tx.Exec(ctx, `
			UPDATE practice_turns t
			SET effective_turns_after = $3, session_version_after = $4,
			    progressed_at = COALESCE(progressed_at, transaction_timestamp())
			FROM practice_sessions s
			WHERE s.session_id=t.session_id AND s.user_id = $1 AND t.turn_id = $2
			  AND (t.progressed_at IS NULL OR
			       (t.effective_turns_after = $3 AND t.session_version_after = $4))
		`, actor.UserID, turn.ID, effectiveTurns, version)
		if err != nil {
			return practice.Turn{}, safeDatabaseError(err)
		}
		if tag.RowsAffected() != 1 {
			return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
		}
		turn.EffectiveTurns = effectiveTurns
		turn.SessionCompleted = status == practice.SessionCompleted
		return turn, nil
	}
	payload := []byte("practice/effective-turn/v1")
	if !turn.CountsTowardTurnLimit {
		payload = []byte("practice/follow-up-turn/v1")
	}
	result, err := r.Repository.AdvanceTurnInTransaction(
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
		return practice.Turn{}, mapPracticeError(err)
	}
	if result.SessionID != turn.SessionID || result.TurnID != turn.ID {
		return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
	}
	turn.EffectiveTurns = result.EffectiveTurns
	turn.SessionCompleted = result.Completed
	return turn, nil
}

func (r *Repository) GetTurn(
	ctx context.Context,
	actor practiceinteraction.Actor,
	turnID string,
) (practice.Turn, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(turnID) == "" {
		return practice.Turn{}, practiceinteraction.ErrPersistenceInvalid
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
	actor practiceinteraction.Actor,
	sessionID string,
) ([]practice.Turn, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(sessionID) == "" {
		return nil, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, turnColumns+`
		WHERE s.user_id = $1 AND t.session_id = $2
		  AND t.status = 'confirmed' AND t.turn_kind = 'EFFECTIVE'
		ORDER BY t.sequence, t.turn_id
	`, actor.UserID, sessionID)
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	defer rows.Close()
	turns := make([]practice.Turn, 0)
	for rows.Next() {
		turn, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, safeDatabaseError(err)
	}
	return turns, nil
}

func (r *Repository) LatestSessionProgress(
	ctx context.Context,
	actor practiceinteraction.Actor,
	sessionID string,
) (practiceinteraction.StoredTurnProgress, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(sessionID) == "" {
		return practiceinteraction.StoredTurnProgress{}, false,
			practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practiceinteraction.StoredTurnProgress{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var progress practiceinteraction.StoredTurnProgress
	err = tx.QueryRow(ctx, `
		SELECT t.turn_id,t.session_id,t.question_id,t.sequence,
		       t.effective_turns_after,t.counts_toward_turn_limit,
		       s.status = 'completed'
		FROM practice_turns t
		JOIN practice_sessions s ON s.session_id=t.session_id
		WHERE s.user_id=$1 AND t.session_id=$2
		  AND t.turn_kind='EFFECTIVE' AND t.progressed_at IS NOT NULL
		ORDER BY t.effective_turns_after DESC,t.sequence DESC,t.turn_id DESC
		LIMIT 1
	`, actor.UserID, sessionID).Scan(
		&progress.TurnID,
		&progress.SessionID,
		&progress.QuestionID,
		&progress.Sequence,
		&progress.EffectiveTurns,
		&progress.CountsTowardTurnLimit,
		&progress.SessionCompleted,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinteraction.StoredTurnProgress{}, false, nil
	}
	if err != nil {
		return practiceinteraction.StoredTurnProgress{}, false, safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return practiceinteraction.StoredTurnProgress{}, false, safeDatabaseError(err)
	}
	return progress, true, nil
}

const turnColumns = `
	SELECT t.turn_id, t.session_id, t.question_id,
	       q.speaker_participant_id, q.addressee_participant_ids,
	       t.respondent_participant_id, t.sequence,
	       COALESCE(t.interaction_mode, ''), t.transcript,
	       COALESCE(t.audio_asset_id::text, ''), t.candidate_id, t.transcript_id,
	       t.evidence_version, t.confirmed_at, t.turn_kind,
	       COALESCE(t.client_request_id, ''), COALESCE(t.original_turn_id::text, ''),
	       t.counts_toward_turn_limit,
	       COALESCE(t.effective_turns_after, s.effective_turns),
	       s.status = 'completed'
	       AND COALESCE(t.effective_turns_after, s.effective_turns) = s.effective_turns
	       AND t.turn_id = (
	           SELECT final_turn.turn_id
	           FROM practice_turns AS final_turn
	           WHERE final_turn.session_id = t.session_id
	             AND final_turn.status = 'confirmed'
	             AND final_turn.turn_kind = 'EFFECTIVE'
	           ORDER BY final_turn.sequence DESC, final_turn.turn_id DESC
	           LIMIT 1
	       ),
	       t.status, t.submitted_at,
	       t.created_at, t.confirmed_at
	FROM practice_turns AS t
	JOIN practice_questions AS q
	  ON q.session_id = t.session_id AND q.question_id = t.question_id
	JOIN practice_sessions AS s
	  ON s.session_id = t.session_id
`

func getTurn(
	ctx context.Context,
	database queryRow,
	ownerUserID, turnID string,
) (practice.Turn, error) {
	return scanTurn(database.QueryRow(ctx, turnColumns+`
		WHERE s.user_id = $1 AND t.turn_id = $2 AND t.status = 'confirmed'
	`, ownerUserID, turnID))
}

func scanTurn(row rowScanner) (practice.Turn, error) {
	var turn practice.Turn
	var submittedAt *time.Time
	err := row.Scan(
		&turn.ID, &turn.SessionID, &turn.QuestionID,
		&turn.SpeakerParticipantID, &turn.AddresseeParticipantIDs,
		&turn.RespondentParticipantID, &turn.Sequence, &turn.InteractionMode,
		&turn.AnswerText, &turn.AudioAssetID, &turn.CandidateID,
		&turn.TranscriptID, &turn.EvidenceVersion, &turn.ConfirmedAt,
		&turn.Kind, &turn.ClientRequestID, &turn.OriginalTurnID,
		&turn.CountsTowardTurnLimit, &turn.EffectiveTurns,
		&turn.SessionCompleted, &turn.Status, &submittedAt,
		&turn.CreatedAt, &turn.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	turn.ConfirmedAt = turn.ConfirmedAt.UTC()
	turn.CreatedAt = turn.CreatedAt.UTC()
	turn.CompletedAt = turn.CompletedAt.UTC()
	if submittedAt != nil {
		turn.SubmittedAt = submittedAt.UTC()
	}
	return turn, nil
}

func validConfirmation(
	actor practiceinteraction.Actor,
	command practiceinteraction.ConfirmTurnCommand,
) bool {
	return validInputActor(actor) && strings.TrimSpace(command.CandidateID) != "" &&
		command.EvidenceVersion > 0 && strings.TrimSpace(command.ConfirmedText) != "" &&
		strings.TrimSpace(command.IdempotencyKey) != "" && len(command.IdempotencyKey) <= 128 &&
		(command.RetryTurnID == "" || validRetryTurnIdentifier(command.RetryTurnID))
}

var _ practiceinteraction.PersistenceStore = (*Repository)(nil)
