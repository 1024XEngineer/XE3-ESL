package review

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) ClaimSpeechFeedback(
	ctx context.Context,
	configuration SpeechFeedbackWorkerConfiguration,
) (SpeechFeedbackClaim, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!configuration.Valid() {
		return SpeechFeedbackClaim{}, false,
			ErrInvalidSpeechFeedback
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedbackClaim{}, false, fmt.Errorf(
			"begin SpeechFeedback claim: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var speechFeedbackID string
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT feedback.id
			FROM review_speech_feedbacks AS feedback
			JOIN identity_users AS owner
			  ON owner.id = feedback.owner_user_id
			LEFT JOIN review_deletion_fences AS fence
			  ON fence.owner_user_id = feedback.owner_user_id
			WHERE owner.account_status = 'active'
			  AND fence.owner_user_id IS NULL
			  AND feedback.strategy_ref = $1
			  AND feedback.pipeline_version = $2
			  AND feedback.attempt_count < $3
			  AND (
			      (
			          feedback.feedback_status = 'QUEUED'
			          AND feedback.available_at <=
			              transaction_timestamp()
			      )
			      OR
			      (
			          feedback.feedback_status = 'RUNNING'
			          AND feedback.lease_expires_at <=
			              clock_timestamp()
			      )
			  )
			ORDER BY
				feedback.available_at,
				feedback.created_at,
				feedback.id
			FOR UPDATE OF feedback SKIP LOCKED
			LIMIT 1
		)
		UPDATE review_speech_feedbacks AS feedback
		SET feedback_status = 'RUNNING',
		    attempt_count = feedback.attempt_count + 1,
		    fencing_token = feedback.fencing_token + 1,
		    lease_expires_at =
		        clock_timestamp() + make_interval(secs => $4),
		    updated_at = transaction_timestamp()
		FROM candidate
		WHERE feedback.id = candidate.id
		RETURNING feedback.id::text
	`, configuration.StrategyRef, configuration.PipelineVersion,
		configuration.MaxAttempts,
		configuration.LeaseDuration.Seconds()).Scan(&speechFeedbackID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SpeechFeedbackClaim{}, false, nil
	}
	if err != nil {
		return SpeechFeedbackClaim{}, false, fmt.Errorf(
			"claim SpeechFeedback: %w",
			err,
		)
	}
	stored, err := selectSpeechFeedbackByClaimID(
		ctx,
		tx,
		speechFeedbackID,
	)
	if err != nil {
		return SpeechFeedbackClaim{}, false, err
	}
	stored.PromptText, err = selectSpeechFeedbackAcousticPrompt(
		ctx,
		tx,
		stored,
	)
	if err != nil {
		return SpeechFeedbackClaim{}, false, err
	}
	sourceConsistent, err := verifyStoredSpeechFeedbackSource(
		ctx,
		tx,
		stored,
	)
	if err != nil {
		return SpeechFeedbackClaim{}, false, err
	}
	if stored.LeaseExpiresAt == nil {
		return SpeechFeedbackClaim{}, false,
			ErrInvalidSpeechFeedback
	}
	claim := SpeechFeedbackClaim{
		SpeechFeedbackID:   stored.Feedback.SpeechFeedbackID,
		OwnerUserID:        stored.OwnerUserID,
		Source:             stored.Feedback.Source,
		CanonicalText:      stored.CanonicalText,
		PromptText:         stored.PromptText,
		EvidenceRefID:      stored.EvidenceRefID,
		AudioAssetID:       stored.AudioAssetID,
		AudioAssetVersion:  stored.AudioAssetVersion,
		AudioChecksum:      stored.AudioChecksum,
		AudioObjectKey:     stored.AudioObjectKey,
		SourceDigest:       stored.SourceDigest,
		DeletionGeneration: stored.DeletionGeneration,
		AttemptCount:       stored.AttemptCount,
		FencingToken:       stored.FencingToken,
		LeaseExpiresAt:     *stored.LeaseExpiresAt,
		StrategyRef:        stored.Feedback.StrategyRef,
		PipelineVersion:    stored.Feedback.PipelineVersion,
		SourceConsistent:   sourceConsistent,
	}
	if !claim.Valid() {
		return SpeechFeedbackClaim{}, false,
			ErrInvalidSpeechFeedback
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedbackClaim{}, false, fmt.Errorf(
			"commit SpeechFeedback claim: %w",
			err,
		)
	}
	return claim, true, nil
}

func (r *PostgresRepository) CompleteSpeechFeedback(
	ctx context.Context,
	claim SpeechFeedbackClaim,
	drafts []SpeechFeedbackDraftItem,
) (SpeechFeedback, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!claim.Valid() || !claim.SourceConsistent ||
		len(drafts) == 0 ||
		len(drafts) > maxSpeechFeedbackProviderItems {
		return SpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	for _, item := range drafts {
		if !item.validFor(
			claim.Source,
			claim.EvidenceRefID,
			claim.CanonicalText,
		) {
			return SpeechFeedback{}, ErrInvalidSpeechFeedback
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"begin SpeechFeedback completion: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err := lockSpeechFeedbackClaim(ctx, tx, claim)
	if err != nil {
		return SpeechFeedback{}, err
	}
	sourceConsistent, err := verifyStoredSpeechFeedbackSource(
		ctx,
		tx,
		stored,
	)
	if err != nil {
		return SpeechFeedback{}, err
	}
	if !sourceConsistent {
		return SpeechFeedback{}, ErrSpeechFeedbackClaimLost
	}

	for _, draft := range drafts {
		var suggestedText any
		if draft.SuggestedText != nil {
			suggestedText = *draft.SuggestedText
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO review_speech_feedback_items (
				speech_feedback_id,
				owner_user_id,
				kind,
				anchor_kind,
				evidence_ref_id,
				turn_id,
				transcript_evidence_id,
				message_id,
				start_utf8_byte,
				end_utf8_byte,
				original_excerpt,
				explanation,
				suggested_text,
				repractice_mode
			)
			VALUES (
				$1, $2, $3, $4,
				NULLIF($5, ''),
				NULLIF($6, ''),
				NULLIF($7, '')::uuid,
				NULLIF($8, '')::uuid,
				$9, $10, $11, $12, $13, $14
			)
		`, claim.SpeechFeedbackID, claim.OwnerUserID,
			draft.Kind, draft.Anchor.AnchorKind,
			draft.Anchor.EvidenceRefID, draft.Anchor.TurnID,
			draft.Anchor.TranscriptEvidenceID,
			draft.Anchor.MessageID,
			draft.Anchor.StartUTF8Byte,
			draft.Anchor.EndUTF8Byte,
			draft.Anchor.OriginalExcerpt,
			draft.Explanation,
			suggestedText,
			draft.RepracticeMode); err != nil {
			return SpeechFeedback{}, fmt.Errorf(
				"insert SpeechFeedback item: %w",
				err,
			)
		}
	}
	tag, err := tx.Exec(ctx, `
		UPDATE review_speech_feedbacks
		SET feedback_status = 'READY',
		    scoreability_status = 'PROVISIONAL',
		    gate_status = 'FEEDBACK_ONLY',
		    reason_codes = ARRAY[]::text[],
		    lease_expires_at = NULL,
		    completed_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE id = $1
		  AND owner_user_id = $2
		  AND feedback_status = 'RUNNING'
		  AND fencing_token = $3
		  AND deletion_generation = $4
		  AND source_digest = $5
		  AND lease_expires_at > clock_timestamp()
	`, claim.SpeechFeedbackID, claim.OwnerUserID,
		claim.FencingToken, claim.DeletionGeneration,
		claim.SourceDigest[:])
	if err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"complete SpeechFeedback: %w",
			err,
		)
	}
	if tag.RowsAffected() != 1 {
		return SpeechFeedback{}, ErrSpeechFeedbackClaimLost
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"commit SpeechFeedback completion: %w",
			err,
		)
	}
	return r.GetSpeechFeedback(
		ctx,
		claim.OwnerUserID,
		claim.SpeechFeedbackID,
	)
}

func (r *PostgresRepository) CompleteSpeechFeedbackInsufficient(
	ctx context.Context,
	claim SpeechFeedbackClaim,
	reasonCodes []SpeechFeedbackReasonCode,
) (SpeechFeedback, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!claim.Valid() || len(reasonCodes) == 0 ||
		len(reasonCodes) > 3 {
		return SpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	reasons := make([]string, 0, len(reasonCodes))
	seen := make(map[SpeechFeedbackReasonCode]struct{}, len(reasonCodes))
	for _, code := range reasonCodes {
		if !code.valid() {
			return SpeechFeedback{}, ErrInvalidSpeechFeedback
		}
		if _, duplicate := seen[code]; duplicate {
			return SpeechFeedback{}, ErrInvalidSpeechFeedback
		}
		seen[code] = struct{}{}
		reasons = append(reasons, string(code))
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"begin insufficient SpeechFeedback completion: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err := lockSpeechFeedbackClaim(ctx, tx, claim)
	if err != nil {
		return SpeechFeedback{}, err
	}
	sourceConsistent, err := verifyStoredSpeechFeedbackSource(
		ctx,
		tx,
		stored,
	)
	if err != nil {
		return SpeechFeedback{}, err
	}
	if claim.SourceConsistent && !sourceConsistent {
		return SpeechFeedback{}, ErrSpeechFeedbackClaimLost
	}
	if claim.SourceConsistent &&
		reasonCodes[0] == SpeechFeedbackReasonEvidenceInconsistent {
		return SpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	if !claim.SourceConsistent &&
		(len(reasonCodes) != 1 ||
			reasonCodes[0] !=
				SpeechFeedbackReasonEvidenceInconsistent) {
		return SpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	tag, err := tx.Exec(ctx, `
		UPDATE review_speech_feedbacks
		SET feedback_status = 'READY',
		    scoreability_status = 'INSUFFICIENT',
		    gate_status = 'BLOCKED',
		    reason_codes = $5,
		    lease_expires_at = NULL,
		    completed_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE id = $1
		  AND owner_user_id = $2
		  AND feedback_status = 'RUNNING'
		  AND fencing_token = $3
		  AND deletion_generation = $4
		  AND lease_expires_at > clock_timestamp()
	`, claim.SpeechFeedbackID, claim.OwnerUserID,
		claim.FencingToken, claim.DeletionGeneration, reasons)
	if err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"complete insufficient SpeechFeedback: %w",
			err,
		)
	}
	if tag.RowsAffected() != 1 {
		return SpeechFeedback{}, ErrSpeechFeedbackClaimLost
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"commit insufficient SpeechFeedback: %w",
			err,
		)
	}
	return r.GetSpeechFeedback(
		ctx,
		claim.OwnerUserID,
		claim.SpeechFeedbackID,
	)
}

func (r *PostgresRepository) FailSpeechFeedback(
	ctx context.Context,
	claim SpeechFeedbackClaim,
	failure SpeechFeedbackStableFailure,
	configuration SpeechFeedbackWorkerConfiguration,
) (SpeechFeedbackStatus, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!claim.Valid() || !failure.valid() ||
		!configuration.Valid() ||
		claim.StrategyRef != configuration.StrategyRef ||
		claim.PipelineVersion != configuration.PipelineVersion {
		return "", ErrInvalidSpeechFeedback
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin SpeechFeedback failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockSpeechFeedbackClaim(ctx, tx, claim); err != nil {
		return "", err
	}
	retry := failure.Retryable &&
		claim.AttemptCount < configuration.MaxAttempts
	status := SpeechFeedbackFailed
	if retry {
		status = SpeechFeedbackQueued
	}
	persistedRetryable := failure.Retryable
	if !retry && claim.AttemptCount >= configuration.MaxAttempts {
		persistedRetryable = false
	}
	var returnedStatus string
	err = tx.QueryRow(ctx, `
		UPDATE review_speech_feedbacks
		SET feedback_status =
		        CASE
		            WHEN $6::boolean THEN 'QUEUED'
		            ELSE 'FAILED'
		        END,
		    stable_failure_code =
		        CASE
		            WHEN $6::boolean THEN NULL
		            ELSE $7::text
		        END,
		    stable_failure_retryable =
		        CASE
		            WHEN $6::boolean THEN NULL
		            ELSE $8::boolean
		        END,
		    lease_expires_at = NULL,
		    available_at =
		        CASE
		            WHEN $6::boolean THEN transaction_timestamp() +
		                make_interval(secs => $9::double precision)
		            ELSE available_at
		        END,
		    completed_at =
		        CASE
		            WHEN $6::boolean THEN NULL
		            ELSE transaction_timestamp()
		        END,
		    updated_at = transaction_timestamp()
		WHERE id = $1
		  AND owner_user_id = $2
		  AND feedback_status = 'RUNNING'
		  AND fencing_token = $3
		  AND deletion_generation = $4
		  AND source_digest = $5
		  AND lease_expires_at > clock_timestamp()
		RETURNING feedback_status
	`, claim.SpeechFeedbackID, claim.OwnerUserID,
		claim.FencingToken, claim.DeletionGeneration,
		claim.SourceDigest[:], retry, failure.ReasonCode,
		persistedRetryable, configuration.RetryDelay.Seconds()).Scan(
		&returnedStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSpeechFeedbackClaimLost
	}
	if err != nil {
		return "", fmt.Errorf("record SpeechFeedback failure: %w", err)
	}
	if SpeechFeedbackStatus(returnedStatus) != status {
		return "", ErrInvalidSpeechFeedback
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit SpeechFeedback failure: %w", err)
	}
	return status, nil
}

func selectSpeechFeedbackByClaimID(
	ctx context.Context,
	tx pgx.Tx,
	speechFeedbackID string,
) (storedSpeechFeedback, error) {
	stored, err := scanStoredSpeechFeedback(tx.QueryRow(
		ctx,
		speechFeedbackSelect+`
		WHERE feedback.id = $1
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		FOR UPDATE OF feedback
	`,
		speechFeedbackID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedSpeechFeedback{}, ErrSpeechFeedbackClaimLost
	}
	if err != nil {
		return storedSpeechFeedback{}, fmt.Errorf(
			"read claimed SpeechFeedback: %w",
			err,
		)
	}
	return stored, nil
}

func lockSpeechFeedbackClaim(
	ctx context.Context,
	tx pgx.Tx,
	claim SpeechFeedbackClaim,
) (storedSpeechFeedback, error) {
	stored, err := selectSpeechFeedbackByClaimID(
		ctx,
		tx,
		claim.SpeechFeedbackID,
	)
	if err != nil {
		return storedSpeechFeedback{}, err
	}
	stored.PromptText, err = selectSpeechFeedbackAcousticPrompt(
		ctx,
		tx,
		stored,
	)
	if err != nil {
		return storedSpeechFeedback{}, err
	}
	if stored.OwnerUserID != claim.OwnerUserID ||
		stored.Feedback.FeedbackStatus != SpeechFeedbackRunning ||
		stored.DeletionGeneration != claim.DeletionGeneration ||
		stored.SourceDigest != claim.SourceDigest ||
		stored.PromptText != claim.PromptText ||
		stored.FencingToken != claim.FencingToken ||
		stored.AttemptCount != claim.AttemptCount ||
		stored.LeaseExpiresAt == nil ||
		!stored.LeaseExpiresAt.Equal(claim.LeaseExpiresAt) {
		return storedSpeechFeedback{}, ErrSpeechFeedbackClaimLost
	}
	return stored, nil
}

func selectSpeechFeedbackAcousticPrompt(
	ctx context.Context,
	database queryer,
	stored storedSpeechFeedback,
) (string, error) {
	switch stored.Feedback.Source.SourceKind {
	case SpeechFeedbackSourceAgentVoiceMessage:
		return "", nil
	case SpeechFeedbackSourceConversationTurn:
		var promptText string
		err := database.QueryRow(ctx, `
			SELECT question.content
			FROM practice_turns AS turn
			JOIN practice_questions AS question
			  ON question.owner_user_id = turn.owner_user_id
			 AND question.practice_session_id =
			     turn.practice_session_id
			 AND question.question_id = turn.question_id
			WHERE turn.owner_user_id = $1
			  AND turn.practice_session_id = $2
			  AND turn.turn_id = $3
		`, stored.OwnerUserID,
			stored.Feedback.Source.PracticeSessionID,
			stored.Feedback.Source.TurnID).Scan(&promptText)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrSpeechFeedbackClaimLost
		}
		if err != nil {
			return "", fmt.Errorf(
				"read Conversation SpeechFeedback prompt: %w",
				err,
			)
		}
		if !validSpeechFeedbackText(promptText, 10_000) {
			return "", ErrInvalidSpeechFeedback
		}
		return promptText, nil
	default:
		return "", ErrInvalidSpeechFeedback
	}
}

func verifyStoredSpeechFeedbackSource(
	ctx context.Context,
	tx pgx.Tx,
	stored storedSpeechFeedback,
) (bool, error) {
	switch stored.Feedback.Source.SourceKind {
	case SpeechFeedbackSourceConversationTurn:
		var (
			digest        []byte
			canonicalText string
			evidenceRefID string
			promptText    string
		)
		err := tx.QueryRow(ctx, `
			SELECT
				snapshot.source_digest,
				snapshot.transcript_text,
				snapshot.evidence_ref_id,
				question.content
			FROM review_speech_feedback_turn_snapshots AS snapshot
			JOIN practice_turns AS turn
			  ON turn.owner_user_id = snapshot.owner_user_id
			 AND turn.practice_session_id =
			     snapshot.practice_session_id
			 AND turn.turn_id = snapshot.turn_id
			JOIN practice_questions AS question
			  ON question.owner_user_id = turn.owner_user_id
			 AND question.practice_session_id =
			     turn.practice_session_id
			 AND question.question_id = turn.question_id
			WHERE snapshot.id = $1
			  AND snapshot.owner_user_id = $2
			  AND snapshot.practice_session_id = $3
			  AND snapshot.turn_id = $4
			  AND snapshot.input_revision = $5
			FOR SHARE OF snapshot, turn, question
		`, stored.Feedback.Source.EvidenceSnapshotID,
			stored.OwnerUserID,
			stored.Feedback.Source.PracticeSessionID,
			stored.Feedback.Source.TurnID,
			stored.Feedback.Source.InputRevision).Scan(
			&digest,
			&canonicalText,
			&evidenceRefID,
			&promptText,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf(
				"verify Conversation SpeechFeedback source: %w",
				err,
			)
		}
		if len(digest) != sha256.Size {
			return false, ErrInvalidSpeechFeedback
		}
		var sourceDigest [sha256.Size]byte
		copy(sourceDigest[:], digest)
		return sourceDigest == stored.SourceDigest &&
			canonicalText == stored.CanonicalText &&
			evidenceRefID == stored.EvidenceRefID &&
			promptText == stored.PromptText, nil
	case SpeechFeedbackSourceAgentVoiceMessage:
		source, canonicalText, digest, err :=
			selectConfirmedAgentSpeechFeedbackSource(
				ctx,
				tx,
				stored.OwnerUserID,
				stored.Feedback.Source.ThreadID,
				stored.Feedback.Source.MessageID,
			)
		if errors.Is(err, ErrSpeechFeedbackNotApplicable) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return source == stored.Feedback.Source &&
			canonicalText == stored.CanonicalText &&
			digest == stored.SourceDigest, nil
	default:
		return false, ErrInvalidSpeechFeedback
	}
}
