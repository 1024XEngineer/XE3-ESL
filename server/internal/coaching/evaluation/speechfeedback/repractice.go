package speechfeedback

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SameQuestionRepracticeSource is the bounded Evaluation projection required
// to start another attempt for a feedback item. Review never reads Evaluation
// tables directly.
type SameQuestionRepracticeSource struct {
	FeedbackItemID    string
	SpeechFeedbackID  string
	PracticeSessionID string
	OriginalTurnID    string
	QuestionID        string
	SourceGeneration  int64
}

func (source SameQuestionRepracticeSource) valid() bool {
	return validUUID(source.FeedbackItemID) &&
		validUUID(source.SpeechFeedbackID) &&
		validSpeechFeedbackIdentifier(source.PracticeSessionID) &&
		validSpeechFeedbackIdentifier(source.OriginalTurnID) &&
		validSpeechFeedbackIdentifier(source.QuestionID) &&
		source.SourceGeneration >= 0
}

func (r *PostgresRepository) ReadSameQuestionRepracticeSource(
	ctx context.Context,
	ownerUserID string,
	feedbackItemID string,
) (SameQuestionRepracticeSource, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(feedbackItemID) {
		return SameQuestionRepracticeSource{}, ErrInvalidSpeechFeedback
	}
	var source SameQuestionRepracticeSource
	err := r.pool.QueryRow(ctx, `
		SELECT
			item.id::text,
			feedback.id::text,
			feedback.practice_session_id,
			feedback.turn_id,
			turn.question_id,
			feedback.deletion_generation
		FROM evaluation_speech_feedback_items AS item
		JOIN evaluation_speech_feedbacks AS feedback
		  ON feedback.id = item.speech_feedback_id
		 AND feedback.owner_user_id = item.owner_user_id
		JOIN practice_turns AS turn
		  ON turn.owner_user_id = feedback.owner_user_id
		 AND turn.turn_id = feedback.turn_id
		 AND turn.practice_session_id = feedback.practice_session_id
		JOIN identity_users AS owner
		  ON owner.id = feedback.owner_user_id
		LEFT JOIN evaluation_deletion_fences AS fence
		  ON fence.owner_user_id = feedback.owner_user_id
		WHERE item.owner_user_id = $1
		  AND item.id = $2
		  AND item.repractice_mode = 'SAME_QUESTION'
		  AND item.turn_id = feedback.turn_id
		  AND feedback.source_kind = 'CONVERSATION_TURN'
		  AND feedback.feedback_status = 'READY'
		  AND feedback.scoreability_status = 'PROVISIONAL'
		  AND feedback.gate_status = 'FEEDBACK_ONLY'
		  AND turn.turn_kind = 'EFFECTIVE'
		  AND turn.counts_toward_effective_turn_limit
		  AND owner.account_status = 'active'
		  AND coalesce(fence.deletion_generation, 0) <=
		      feedback.deletion_generation
	`, ownerUserID, feedbackItemID).Scan(
		&source.FeedbackItemID,
		&source.SpeechFeedbackID,
		&source.PracticeSessionID,
		&source.OriginalTurnID,
		&source.QuestionID,
		&source.SourceGeneration,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SameQuestionRepracticeSource{}, ErrSpeechFeedbackNotFound
	}
	if err != nil {
		return SameQuestionRepracticeSource{}, fmt.Errorf(
			"read same-question Repractice source: %w",
			err,
		)
	}
	if !source.valid() || source.FeedbackItemID != feedbackItemID {
		return SameQuestionRepracticeSource{}, ErrInvalidSpeechFeedback
	}
	return source, nil
}
