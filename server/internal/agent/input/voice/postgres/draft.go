package postgres

import (
	"context"
	"errors"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) StageDraft(
	ctx context.Context,
	ownerID string,
	threadID string,
	assetID string,
	lease time.Duration,
) (agentvoice.TranscriptionClaim, bool, error) {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(threadID) || !agentvoice.ValidUUID(assetID) ||
		lease <= 0 {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrInvalidRequest
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrRepository
	}
	defer rollback(tx)
	if err := lockActiveOwner(ctx, tx, ownerID); err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	if _, err := lockOwnedThread(ctx, tx, ownerID, threadID); err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	draft, err := findDraftForUpdate(ctx, tx, ownerID, assetID)
	if errors.Is(err, agentvoice.ErrNotFound) {
		if err := mediapostgres.LockAttachableInTransaction(
			ctx, tx, ownerID, sharedmedia.KindAudio, []string{assetID},
		); err != nil {
			return agentvoice.TranscriptionClaim{}, false, mapMediaError(err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO agent_voice_drafts (
    id,
    thread_id,
    status,
    asr_attempt,
    version,
    asr_fencing_token,
    asr_lease_until,
    created_at,
    updated_at
) VALUES (
    $1, $2, 'transcribing', 1, 1, 1,
    CURRENT_TIMESTAMP + $3::interval,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
)`, assetID, threadID, lease); err != nil {
			return agentvoice.TranscriptionClaim{}, false, mapError(err)
		}
		draft, err = findDraftForUpdate(ctx, tx, ownerID, assetID)
		if err != nil {
			return agentvoice.TranscriptionClaim{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrRepository
		}
		return claimFromDraft(draft), true, nil
	}
	if err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	if draft.ThreadID != threadID {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrIdempotencyConflict
	}
	if draft.Status == agentvoice.StatusTranscribing {
		var leaseExpired bool
		if err := tx.QueryRow(ctx, `
SELECT asr_lease_until <= CURRENT_TIMESTAMP
FROM agent_voice_drafts
WHERE id = $1`, assetID).Scan(&leaseExpired); err != nil {
			return agentvoice.TranscriptionClaim{}, false, mapError(err)
		}
		if leaseExpired {
			draft, err = claimDraft(ctx, tx, ownerID, assetID, lease)
			if err != nil {
				return agentvoice.TranscriptionClaim{}, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrRepository
			}
			return claimFromDraft(draft), true, nil
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrRepository
	}
	return claimFromDraft(draft), false, nil
}

func (repository *Repository) ClaimTranscription(
	ctx context.Context,
	ownerID string,
	draftID string,
	lease time.Duration,
) (agentvoice.TranscriptionClaim, bool, error) {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(draftID) || lease <= 0 {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrInvalidRequest
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrRepository
	}
	defer rollback(tx)
	if err := lockActiveOwner(ctx, tx, ownerID); err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	threadID, err := draftThreadID(ctx, tx, ownerID, draftID)
	if err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	if _, err := lockOwnedThread(ctx, tx, ownerID, threadID); err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	draft, err := findDraftForUpdate(ctx, tx, ownerID, draftID)
	if err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	if draft.Status == agentvoice.StatusTranscribing {
		var leaseActive bool
		if err := tx.QueryRow(ctx, `
SELECT asr_lease_until > CURRENT_TIMESTAMP
FROM agent_voice_drafts
WHERE id = $1`, draftID).Scan(&leaseActive); err != nil {
			return agentvoice.TranscriptionClaim{}, false, mapError(err)
		}
		if leaseActive {
			if err := tx.Commit(ctx); err != nil {
				return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrRepository
			}
			return claimFromDraft(draft), false, nil
		}
	} else if draft.Status != agentvoice.StatusFailed || !draft.FailureRetryable {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrConflict
	}
	draft, err = claimDraft(ctx, tx, ownerID, draftID, lease)
	if err != nil {
		return agentvoice.TranscriptionClaim{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentvoice.TranscriptionClaim{}, false, agentvoice.ErrRepository
	}
	return claimFromDraft(draft), true, nil
}

func claimDraft(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	draftID string,
	lease time.Duration,
) (agentvoice.Draft, error) {
	tag, err := tx.Exec(ctx, `
UPDATE agent_voice_drafts AS draft
SET
    status = 'transcribing',
    asr_attempt = asr_attempt + 1,
    version = version + 1,
    asr_fencing_token = asr_fencing_token + 1,
    asr_lease_until = CURRENT_TIMESTAMP + $3::interval,
    asr_request_id = NULL,
    asr_provider = NULL,
    asr_model = NULL,
    transcript = NULL,
    language = NULL,
    emotion = NULL,
    finish_reason = NULL,
    failure_kind = NULL,
    failure_retryable = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM media_assets AS asset, agent_threads AS thread
WHERE draft.id = $1
  AND asset.id = draft.id
  AND thread.id = draft.thread_id
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id
  AND (
      (draft.status = 'transcribing' AND draft.asr_lease_until <= CURRENT_TIMESTAMP)
      OR (draft.status = 'failed' AND draft.failure_retryable)
  )`, draftID, ownerID, lease)
	if err != nil {
		return agentvoice.Draft{}, mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentvoice.Draft{}, agentvoice.ErrConflict
	}
	return findDraftForUpdate(ctx, tx, ownerID, draftID)
}

func (repository *Repository) CompleteTranscription(
	ctx context.Context,
	ownerID string,
	draftID string,
	fencingToken uint64,
	result agentvoice.TranscriptionResult,
) (agentvoice.Draft, error) {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(draftID) || fencingToken == 0 ||
		!agentvoice.ValidTranscription(result) {
		return agentvoice.Draft{}, agentvoice.ErrInvalidRequest
	}
	tag, err := repository.database.Exec(ctx, `
UPDATE agent_voice_drafts AS draft
SET
    status = 'ready',
    asr_lease_until = NULL,
    asr_request_id = $4,
    asr_provider = $5,
    asr_model = $6,
    transcript = $7,
    language = NULLIF($8, ''),
    emotion = NULLIF($9, ''),
    finish_reason = NULLIF($10, ''),
    failure_kind = NULL,
    failure_retryable = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM media_assets AS asset, agent_threads AS thread
WHERE draft.id = $1
  AND asset.id = draft.id
  AND thread.id = draft.thread_id
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id
  AND draft.status = 'transcribing'
  AND draft.asr_fencing_token = $3
  AND draft.asr_lease_until > CURRENT_TIMESTAMP`,
		draftID,
		ownerID,
		int64(fencingToken),
		result.ID,
		result.Provider,
		result.Model,
		result.Transcript,
		result.Language,
		result.Emotion,
		result.FinishReason,
	)
	if err != nil {
		return agentvoice.Draft{}, mapError(err)
	}
	if tag.RowsAffected() != 1 {
		if _, findErr := repository.FindDraft(ctx, ownerID, draftID); findErr != nil {
			return agentvoice.Draft{}, findErr
		}
		return agentvoice.Draft{}, agentvoice.ErrConflict
	}
	return repository.FindDraft(ctx, ownerID, draftID)
}

func (repository *Repository) FailTranscription(
	ctx context.Context,
	ownerID string,
	draftID string,
	fencingToken uint64,
	failureKind string,
	retryable bool,
) (agentvoice.Draft, error) {
	if ctx == nil || !agentvoice.ValidUUID(ownerID) ||
		!agentvoice.ValidUUID(draftID) || fencingToken == 0 ||
		!validFailure(failureKind) {
		return agentvoice.Draft{}, agentvoice.ErrInvalidRequest
	}
	tag, err := repository.database.Exec(ctx, `
UPDATE agent_voice_drafts AS draft
SET
    status = 'failed',
    asr_lease_until = NULL,
    asr_request_id = NULL,
    asr_provider = NULL,
    asr_model = NULL,
    transcript = NULL,
    language = NULL,
    emotion = NULL,
    finish_reason = NULL,
    failure_kind = $4,
    failure_retryable = $5,
    updated_at = CURRENT_TIMESTAMP
FROM media_assets AS asset, agent_threads AS thread
WHERE draft.id = $1
  AND asset.id = draft.id
  AND thread.id = draft.thread_id
  AND thread.user_id = $2
  AND asset.user_id = thread.user_id
  AND draft.status = 'transcribing'
  AND draft.asr_fencing_token = $3
  AND draft.asr_lease_until > CURRENT_TIMESTAMP`,
		draftID,
		ownerID,
		int64(fencingToken),
		failureKind,
		retryable,
	)
	if err != nil {
		return agentvoice.Draft{}, mapError(err)
	}
	if tag.RowsAffected() != 1 {
		if _, findErr := repository.FindDraft(ctx, ownerID, draftID); findErr != nil {
			return agentvoice.Draft{}, findErr
		}
		return agentvoice.Draft{}, agentvoice.ErrConflict
	}
	return repository.FindDraft(ctx, ownerID, draftID)
}

func claimFromDraft(draft agentvoice.Draft) agentvoice.TranscriptionClaim {
	return agentvoice.TranscriptionClaim{
		Draft: draft, FencingToken: draft.ASRFencingToken,
		LeaseExpiresAt: draft.ASRLeaseUntil,
	}
}

func mapMediaError(err error) error {
	switch {
	case errors.Is(err, sharedmedia.ErrInvalidRequest):
		return agentvoice.ErrInvalidRequest
	case errors.Is(err, sharedmedia.ErrNotFound):
		return agentvoice.ErrNotFound
	case errors.Is(err, sharedmedia.ErrConflict):
		return agentvoice.ErrConflict
	default:
		return agentvoice.ErrRepository
	}
}
