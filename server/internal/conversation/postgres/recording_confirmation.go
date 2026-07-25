package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	domainconversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
)

var _ conversation.RecordingConfirmationStore = (*Repository)(nil)

// ConfirmTurnWithRecording keeps the cleanup claim and Turn confirmation on
// opposite sides of one AudioAsset row lock. Cleanup uses FOR UPDATE SKIP
// LOCKED, so it cannot claim an asset while this transaction creates the Turn
// and binds the recording. If cleanup already won, the transaction rolls back
// without leaving a confirmed Turn.
func (r *Repository) ConfirmTurnWithRecording(
	ctx context.Context,
	actor conversation.Actor,
	command conversation.ConfirmTurnCommand,
	uploadRequestID string,
) (conversation.RecordingConfirmation, error) {
	uploadRequestID = strings.TrimSpace(uploadRequestID)
	if !validConfirmation(actor, command) ||
		!validAudioAssetIdentifier(uploadRequestID) {
		return conversation.RecordingConfirmation{},
			conversation.ErrPersistenceInvalid
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return conversation.RecordingConfirmation{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return conversation.RecordingConfirmation{}, err
	}
	r.reachedWriteFence()

	record, err := lockRecordingByUploadRequest(
		ctx,
		tx,
		actor.UserID,
		uploadRequestID,
		command.CandidateID,
	)
	if err != nil {
		return conversation.RecordingConfirmation{}, err
	}
	if r.afterRecordingLock != nil {
		r.afterRecordingLock()
	}
	turn, err := r.confirmTurnInTransaction(ctx, tx, actor, command)
	if err != nil {
		return conversation.RecordingConfirmation{}, err
	}
	audioAssetID, recordingDeleted, err := bindRecordingInTransaction(
		ctx,
		tx,
		record,
		turn.CandidateID,
		turn.ID,
	)
	if err != nil {
		return conversation.RecordingConfirmation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return conversation.RecordingConfirmation{}, safeDatabaseError(err)
	}
	return conversation.RecordingConfirmation{
		Turn:             turn,
		AudioAssetID:     audioAssetID,
		RecordingDeleted: recordingDeleted,
	}, nil
}

func lockRecordingByUploadRequest(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	uploadRequestID string,
	candidateID string,
) (audioAssetRecord, error) {
	record, err := scanAudioAsset(tx.QueryRow(
		ctx,
		"SELECT "+audioAssetReturningColumns+
			` FROM conversation_audio_assets AS asset
			  JOIN conversation_transcript_candidates AS candidate
			    ON candidate.owner_user_id = asset.owner_user_id
			   AND candidate.reservation_id = asset.upload_request_id
			  WHERE asset.owner_user_id = $1
			    AND asset.upload_request_id = $2
			    AND candidate.candidate_id = $3
			  FOR UPDATE OF asset`,
		ownerID,
		uploadRequestID,
		candidateID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return audioAssetRecord{}, domainconversation.ErrAudioAssetNotFound
	}
	if err != nil {
		return audioAssetRecord{}, safeAudioAssetDatabaseError(err)
	}
	return record, nil
}

func bindRecordingInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	record audioAssetRecord,
	candidateID string,
	turnID string,
) (string, bool, error) {
	asset := record.asset
	switch asset.Status {
	case domainconversation.AudioAssetMetadataCommitted:
		if asset.CandidateID != "" || asset.TurnID != "" {
			return "", false, ErrAudioAssetDatabase
		}
		tag, err := tx.Exec(
			ctx,
			`UPDATE conversation_audio_assets
			 SET candidate_id = $3,
			     turn_id = $4,
			     status = 'readable',
			     updated_at = GREATEST(updated_at, transaction_timestamp()),
			     version = version + 1
			 WHERE owner_user_id = $1
			   AND audio_asset_id = $2
			   AND status = 'metadata_committed'
			   AND candidate_id IS NULL
			   AND turn_id IS NULL
			   AND version = $5`,
			asset.OwnerID,
			asset.ID,
			candidateID,
			turnID,
			int64(asset.Version),
		)
		if err != nil {
			return "", false, mapAudioAssetWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return "", false, domainconversation.ErrAudioAssetConcurrentUpdate
		}
		return asset.ID, false, nil
	case domainconversation.AudioAssetReadable:
		if asset.CandidateID == candidateID && asset.TurnID == turnID {
			return asset.ID, false, nil
		}
		return "", false, domainconversation.ErrAudioAssetAlreadyBound
	case domainconversation.AudioAssetDeleting,
		domainconversation.AudioAssetDeleted:
		if asset.CandidateID == candidateID && asset.TurnID == turnID {
			// The Turn was confirmed successfully before the user deleted its
			// recording. Replaying confirmation must preserve the Turn without
			// minting a new playback capability.
			return "", true, nil
		}
		return "", false, domainconversation.ErrAudioAssetInvalidTransition
	default:
		return "", false, domainconversation.ErrAudioAssetInvalidTransition
	}
}
