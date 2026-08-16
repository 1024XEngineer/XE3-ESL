package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"

	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
)

var _ practiceinteraction.RecordingConfirmationStore = (*Repository)(nil)

// ConfirmTurnWithRecording binds one ready shared audio asset and retains it in
// the same transaction that confirms the Practice Turn. Replays after explicit
// recording deletion return the preserved Turn with an empty AudioAssetID.
func (r *Repository) ConfirmTurnWithRecording(
	ctx context.Context,
	actor practiceinteraction.Actor,
	command practiceinteraction.ConfirmTurnCommand,
	uploadRequestID string,
) (practice.Turn, error) {
	uploadRequestID = strings.TrimSpace(uploadRequestID)
	if !validConfirmation(actor, command) ||
		!sharedmedia.ValidIdempotencyKey(uploadRequestID) {
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

	turn, found, err := findRecordingConfirmationReplay(
		ctx, tx, actor, command, sourceSessionID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return practice.Turn{}, safeDatabaseError(err)
		}
		return turn, nil
	}

	asset, err := mediapostgres.LockReadyByUploadRequestInTransaction(
		ctx,
		tx,
		actor.UserID,
		sharedmedia.KindAudio,
		uploadRequestID,
	)
	if err != nil {
		return practice.Turn{}, mapRecordingMediaError(err)
	}
	if err := verifyRecordingCandidate(
		ctx, tx, actor.UserID, sourceSessionID, command.CandidateID, uploadRequestID,
	); err != nil {
		return practice.Turn{}, err
	}
	if r.afterRecordingLock != nil {
		r.afterRecordingLock()
	}
	turn, err = r.confirmTurnInTransaction(
		ctx, tx, actor, command, sourceSessionID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE practice_turns AS turn
SET audio_asset_id = $3,
    updated_at = GREATEST(turn.updated_at, transaction_timestamp())
FROM practice_sessions AS session
WHERE session.session_id = turn.session_id
  AND session.user_id = $1
  AND turn.turn_id = $2
  AND turn.status = 'confirmed'
  AND (turn.audio_asset_id IS NULL OR turn.audio_asset_id = $3)`,
		actor.UserID,
		turn.ID,
		asset.ID,
	)
	if err != nil {
		return practice.Turn{}, safeDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return practice.Turn{}, practiceinteraction.ErrPersistenceConflict
	}
	if err := mediapostgres.RetainInTransaction(
		ctx, tx, actor.UserID, sharedmedia.KindAudio, []string{asset.ID},
	); err != nil {
		return practice.Turn{}, mapRecordingMediaError(err)
	}
	turn.AudioAssetID = asset.ID
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

func findRecordingConfirmationReplay(
	ctx context.Context,
	tx pgx.Tx,
	actor practiceinteraction.Actor,
	command practiceinteraction.ConfirmTurnCommand,
	sourceSessionID string,
) (practice.Turn, bool, error) {
	fingerprint := confirmationFingerprint(command)
	var existingFingerprint []byte
	var turnID string
	err := tx.QueryRow(ctx, `
SELECT turn.confirmation_fingerprint, turn.turn_id
FROM practice_turns AS turn
JOIN practice_sessions AS session ON session.session_id = turn.session_id
WHERE session.user_id = $1
  AND turn.session_id = $2
  AND turn.confirmation_client_request_id = $3`,
		actor.UserID,
		sourceSessionID,
		command.IdempotencyKey,
	).Scan(&existingFingerprint, &turnID)
	if err == nil {
		if !bytes.Equal(existingFingerprint, fingerprint) {
			return practice.Turn{}, false, practiceinteraction.ErrPersistenceConflict
		}
		turn, err := getTurn(ctx, tx, actor.UserID, turnID)
		return turn, err == nil, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, false, safeDatabaseError(err)
	}
	turn, err := scanTurn(tx.QueryRow(ctx, turnColumns+`
WHERE s.user_id = $1
  AND t.session_id = $2
  AND t.candidate_id = $3
  AND t.status = 'confirmed'
FOR UPDATE OF t`, actor.UserID, sourceSessionID, command.CandidateID))
	if errors.Is(err, practiceinteraction.ErrPersistenceNotFound) {
		return practice.Turn{}, false, nil
	}
	if err != nil {
		return practice.Turn{}, false, err
	}
	return turn, true, nil
}

func verifyRecordingCandidate(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	sessionID string,
	candidateID string,
	uploadRequestID string,
) error {
	var valid bool
	err := tx.QueryRow(ctx, `
SELECT true
FROM practice_turns AS turn
JOIN practice_sessions AS session ON session.session_id = turn.session_id
WHERE session.user_id = $1
  AND turn.session_id = $2
  AND turn.candidate_id = $3
  AND turn.transcription_request_id = $4`,
		userID,
		sessionID,
		candidateID,
		uploadRequestID,
	).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return practiceinteraction.ErrPersistenceConflict
	}
	if err != nil {
		return safeDatabaseError(err)
	}
	return nil
}

func mapRecordingMediaError(err error) error {
	switch {
	case errors.Is(err, sharedmedia.ErrInvalidRequest):
		return practiceinteraction.ErrPersistenceInvalid
	case errors.Is(err, sharedmedia.ErrNotFound),
		errors.Is(err, sharedmedia.ErrConflict):
		return practiceinteraction.ErrConflict
	default:
		return practiceinteraction.ErrPersistenceUnavailable
	}
}
