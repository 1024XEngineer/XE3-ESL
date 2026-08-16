package postgres

import (
	"context"
	"errors"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) VerifyOwnedRecording(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) error {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!sharedmedia.ValidUUID(actor.UserID) || !sharedmedia.ValidUUID(assetID) {
		return sharedmedia.ErrNotFound
	}
	var found bool
	err := r.pool.QueryRow(ctx, `
SELECT true
FROM practice_turns AS turn
JOIN practice_sessions AS session ON session.session_id = turn.session_id
JOIN users AS owner ON owner.id = session.user_id
JOIN media_assets AS asset ON asset.id = turn.audio_asset_id
WHERE session.user_id = $1
  AND turn.audio_asset_id = $2
  AND turn.status = 'confirmed'
  AND owner.status = 'active'
  AND asset.user_id = session.user_id
  AND asset.kind = 'audio'
  AND asset.status = 'ready'
  AND asset.etag <> ''`, actor.UserID, assetID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return sharedmedia.ErrNotFound
	}
	if err != nil {
		return sharedmedia.ErrRepository
	}
	return nil
}

func (r *Repository) DetachRecording(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) error {
	if r == nil || r.pool == nil || ctx == nil || !actor.Valid() ||
		!sharedmedia.ValidUUID(actor.UserID) || !sharedmedia.ValidUUID(assetID) {
		return sharedmedia.ErrNotFound
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return sharedmedia.ErrRepository
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureActorWritableForEvaluation(ctx, tx, actor.UserID); err != nil {
		return mapRecordingReferenceError(err)
	}
	var sessionID string
	err = tx.QueryRow(ctx, `
SELECT turn.session_id
FROM practice_turns AS turn
JOIN practice_sessions AS session ON session.session_id = turn.session_id
WHERE session.user_id = $1
  AND turn.audio_asset_id = $2
  AND turn.status = 'confirmed'`, actor.UserID, assetID).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sharedmedia.ErrNotFound
	}
	if err != nil {
		return sharedmedia.ErrRepository
	}
	if err := lockEvidenceSourceSession(ctx, tx, actor.UserID, sessionID); err != nil {
		return mapRecordingReferenceError(err)
	}
	var turnID string
	err = tx.QueryRow(ctx, `
SELECT turn.turn_id
FROM practice_turns AS turn
JOIN practice_sessions AS session ON session.session_id = turn.session_id
WHERE session.user_id = $1
  AND turn.session_id = $2
  AND turn.audio_asset_id = $3
  AND turn.status = 'confirmed'
FOR UPDATE OF turn`, actor.UserID, sessionID, assetID).Scan(&turnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sharedmedia.ErrNotFound
	}
	if err != nil {
		return sharedmedia.ErrRepository
	}
	if _, err := mediapostgres.LockOwnedInTransaction(
		ctx, tx, actor.UserID, assetID, sharedmedia.KindAudio,
	); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE practice_turns AS turn
SET audio_asset_id = NULL,
    updated_at = GREATEST(turn.updated_at, transaction_timestamp())
FROM practice_sessions AS session
WHERE session.session_id = turn.session_id
  AND session.user_id = $1
  AND turn.turn_id = $2
  AND turn.audio_asset_id = $3`, actor.UserID, turnID, assetID)
	if err != nil {
		return sharedmedia.ErrRepository
	}
	if tag.RowsAffected() != 1 {
		return sharedmedia.ErrConflict
	}
	if err := mediapostgres.ScheduleDeletionInTransaction(
		ctx, tx, actor.UserID, []string{assetID},
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return sharedmedia.ErrRepository
	}
	return nil
}

func mapRecordingReferenceError(err error) error {
	switch {
	case errors.Is(err, practiceinteraction.ErrPersistenceNotFound):
		return sharedmedia.ErrNotFound
	case errors.Is(err, practiceinteraction.ErrPersistenceConflict):
		return sharedmedia.ErrConflict
	default:
		return sharedmedia.ErrRepository
	}
}

var _ practiceinteraction.RecordingReferenceStore = (*Repository)(nil)
