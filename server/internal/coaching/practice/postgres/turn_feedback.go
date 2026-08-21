package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

// ScheduleTurnFeedbackInTransaction freezes the confirmed Practice-owned
// evidence and queues Evaluation before the caller commits the Turn.
func (r *Repository) ScheduleTurnFeedbackInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	turnID string,
) error {
	if r == nil || ctx == nil || tx == nil || !validUserID(userID) ||
		!validResourceID(turnID) {
		return practice.ErrInvalidArgument
	}
	var evidence practice.TurnFeedbackEvidence
	var snapshotJSON []byte
	err := tx.QueryRow(ctx, `
		SELECT t.session_id, t.turn_id, t.question_id, q.content, t.transcript,
		       t.respondent_participant_id, COALESCE(t.audio_asset_id::text, ''),
		       t.confirmed_at, s.evaluation_policy_ref, s.practice_experience,
		       s.scene_category, s.practice_mode, s.plan_snapshot
		FROM practice_turns AS t
		JOIN practice_questions AS q
		  ON q.session_id = t.session_id AND q.question_id = t.question_id
		JOIN practice_sessions AS s
		  ON s.session_id = t.session_id
		WHERE s.user_id = $1 AND t.turn_id = $2 AND t.status = 'confirmed'
	`, userID, turnID).Scan(
		&evidence.SessionID, &evidence.TurnID, &evidence.QuestionID,
		&evidence.QuestionText, &evidence.Transcript,
		&evidence.RespondentParticipantID, &evidence.AudioAssetID,
		&evidence.ConfirmedAt, &evidence.EvaluationPolicyRef,
		&evidence.PracticeExperience, &evidence.SceneCategory,
		&evidence.PracticeMode, &snapshotJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read confirmed Practice turn feedback evidence: %w", err)
	}
	var snapshot practice.SessionSnapshot
	if decodeStrictJSON(snapshotJSON, &snapshot) != nil {
		return practice.ErrConflict
	}
	if !snapshot.SessionPolicy.SpeechFeedbackAllowed {
		return nil
	}
	evidence.UserID = userID
	evidence.ConfirmedAt = evidence.ConfirmedAt.UTC()
	return r.turnFeedback.ScheduleConfirmedTurn(ctx, tx, evidence)
}
