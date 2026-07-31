package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) SaveSpeechFeedbackAcousticEvidence(
	ctx context.Context,
	claim SpeechFeedbackClaim,
	evidence SpeechFeedbackAcousticEvidence,
) error {
	if r == nil || r.pool == nil || ctx == nil ||
		!claim.Valid() || !claim.SourceConsistent ||
		!claim.hasAcousticSource() || !evidence.valid() {
		return ErrInvalidSpeechFeedback
	}
	fields, err := json.Marshal(evidence.AvailableFields)
	if err != nil {
		return fmt.Errorf("encode SpeechFeedback acoustic fields: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin SpeechFeedback acoustic evidence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err := lockSpeechFeedbackClaim(ctx, tx, claim)
	if err != nil {
		return err
	}
	consistent, err := verifyStoredSpeechFeedbackSource(ctx, tx, stored)
	if err != nil {
		return err
	}
	if !consistent {
		return ErrSpeechFeedbackClaimLost
	}
	assessment := evidence.Assessment
	if _, err := tx.Exec(ctx, `
		INSERT INTO review_speech_feedback_acoustic_evidence (
			speech_feedback_id,
			owner_user_id,
			provider,
			provider_session_id,
			category,
			accuracy_score,
			fluency_score,
			integrity_score,
			raw_result,
			available_fields
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (speech_feedback_id) DO NOTHING
	`, claim.SpeechFeedbackID, claim.OwnerUserID,
		assessment.Provider, assessment.ProviderSession,
		assessment.Category, *assessment.AccuracyScore,
		*assessment.FluencyScore, *assessment.IntegrityScore,
		evidence.RawResult, fields); err != nil {
		return fmt.Errorf("insert SpeechFeedback acoustic evidence: %w", err)
	}
	persisted, found, err := getSpeechFeedbackAcousticAssessment(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.SpeechFeedbackID,
	)
	if err != nil {
		return err
	}
	if !found || !sameSpeechFeedbackAcousticAssessment(
		persisted,
		assessment,
	) {
		return ErrSpeechFeedbackConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit SpeechFeedback acoustic evidence: %w", err)
	}
	return nil
}

func getSpeechFeedbackAcousticAssessment(
	ctx context.Context,
	database queryer,
	ownerUserID string,
	speechFeedbackID string,
) (SpeechFeedbackAcousticAssessment, bool, error) {
	var assessment SpeechFeedbackAcousticAssessment
	err := database.QueryRow(ctx, `
		SELECT
			accuracy_score,
			fluency_score,
			integrity_score,
			provider,
			provider_session_id,
			category
		FROM review_speech_feedback_acoustic_evidence
		WHERE owner_user_id = $1
		  AND speech_feedback_id = $2
	`, ownerUserID, speechFeedbackID).Scan(
		&assessment.AccuracyScore,
		&assessment.FluencyScore,
		&assessment.IntegrityScore,
		&assessment.Provider,
		&assessment.ProviderSession,
		&assessment.Category,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SpeechFeedbackAcousticAssessment{}, false, nil
	}
	if err != nil {
		return SpeechFeedbackAcousticAssessment{}, false,
			fmt.Errorf("read SpeechFeedback acoustic evidence: %w", err)
	}
	assessment.Pronunciation = SpeechFeedbackAssessed
	assessment.AcousticFluency = SpeechFeedbackAssessed
	assessment.Integrity = SpeechFeedbackAssessed
	assessment.Notice = SpeechFeedbackAcousticNotice
	if !assessment.valid() {
		return SpeechFeedbackAcousticAssessment{}, false,
			ErrInvalidSpeechFeedback
	}
	return assessment, true, nil
}

func sameSpeechFeedbackAcousticAssessment(
	left SpeechFeedbackAcousticAssessment,
	right SpeechFeedbackAcousticAssessment,
) bool {
	return left.valid() && right.valid() &&
		left.Pronunciation == right.Pronunciation &&
		left.AcousticFluency == right.AcousticFluency &&
		left.Integrity == right.Integrity &&
		*left.AccuracyScore == *right.AccuracyScore &&
		*left.FluencyScore == *right.FluencyScore &&
		*left.IntegrityScore == *right.IntegrityScore &&
		left.Provider == right.Provider &&
		left.ProviderSession == right.ProviderSession &&
		left.Category == right.Category &&
		left.Notice == right.Notice
}
