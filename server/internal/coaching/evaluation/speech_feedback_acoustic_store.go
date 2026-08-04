package evaluation

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
	providerAccuracy := assessment.AccuracyScore
	if assessment.Category == "topic" {
		providerAccuracy = assessment.SemanticScore
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO evaluation_speech_feedback_acoustic_evidence (
			speech_feedback_id,
			owner_user_id,
			provider,
			provider_session_id,
			category,
			accuracy_score,
			fluency_score,
			integrity_score,
			phone_score,
			speaking_speed_wpm,
			raw_result,
			available_fields
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		ON CONFLICT (speech_feedback_id) DO NOTHING
	`, claim.SpeechFeedbackID, claim.OwnerUserID,
		assessment.Provider, assessment.ProviderSession,
		assessment.Category, providerAccuracy,
		assessment.FluencyScore, assessment.IntegrityScore,
		assessment.PronunciationScore, assessment.SpeakingSpeedWPM,
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
	var providerAccuracy *float64
	err := database.QueryRow(ctx, `
		SELECT
			accuracy_score,
			fluency_score,
			integrity_score,
			phone_score,
			speaking_speed_wpm,
			provider,
			provider_session_id,
			category
		FROM evaluation_speech_feedback_acoustic_evidence
		WHERE owner_user_id = $1
		  AND speech_feedback_id = $2
	`, ownerUserID, speechFeedbackID).Scan(
		&providerAccuracy,
		&assessment.FluencyScore,
		&assessment.IntegrityScore,
		&assessment.PronunciationScore,
		&assessment.SpeakingSpeedWPM,
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
	if assessment.Category == "topic" {
		assessment.SemanticScore = providerAccuracy
	} else {
		assessment.Integrity = SpeechFeedbackAssessed
		assessment.AccuracyScore = providerAccuracy
	}
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
		sameSpeechFeedbackScore(
			left.AccuracyScore,
			right.AccuracyScore,
		) &&
		sameSpeechFeedbackScore(left.FluencyScore, right.FluencyScore) &&
		sameSpeechFeedbackScore(
			left.IntegrityScore,
			right.IntegrityScore,
		) &&
		sameSpeechFeedbackScore(
			left.PronunciationScore,
			right.PronunciationScore,
		) &&
		sameSpeechFeedbackScore(
			left.SpeakingSpeedWPM,
			right.SpeakingSpeedWPM,
		) &&
		sameSpeechFeedbackScore(
			left.SemanticScore,
			right.SemanticScore,
		) &&
		left.Provider == right.Provider &&
		left.ProviderSession == right.ProviderSession &&
		left.Category == right.Category &&
		left.Notice == right.Notice
}

func sameSpeechFeedbackScore(left *float64, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
