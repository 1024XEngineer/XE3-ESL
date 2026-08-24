package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) ReadIELTSPartProfileEvidence(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	sessionID string,
	stage practice.IELTSProfileStage,
	part1Boundary int,
	part2Boundary int,
) (practice.IELTSPartProfileEvidence, error) {
	if r == nil || tx == nil || ctx == nil || !validUserID(ownerID) ||
		!validResourceID(sessionID) || part1Boundary < 1 ||
		part2Boundary <= part1Boundary ||
		(stage != practice.IELTSProfileStagePart1 &&
			stage != practice.IELTSProfileStagePart2) {
		return practice.IELTSPartProfileEvidence{}, practice.ErrInvalidArgument
	}
	boundary := part1Boundary
	if stage == practice.IELTSProfileStagePart2 {
		boundary = part2Boundary
	}
	value := practice.IELTSPartProfileEvidence{
		Stage: stage, Part1Boundary: part1Boundary, Part2Boundary: part2Boundary,
	}
	var status practice.SessionStatus
	var effectiveTurns int
	err := tx.QueryRow(ctx, `SELECT user_id::text, session_id, version, status,
			effective_turns
		FROM practice_sessions
		WHERE user_id=$1 AND session_id=$2`, ownerID, sessionID).Scan(
		&value.UserID, &value.SessionID, &value.SessionVersion,
		&status, &effectiveTurns,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.IELTSPartProfileEvidence{}, practice.ErrNotFound
	}
	if err != nil {
		return practice.IELTSPartProfileEvidence{}, err
	}
	if (status != practice.SessionInProgress && status != practice.SessionCompleted) ||
		effectiveTurns < boundary {
		return practice.IELTSPartProfileEvidence{}, practice.ErrConflict
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT q.question_id, q.sequence,
			COALESCE(q.parent_question_id::text,''), q.content,
			q.speaker_participant_id, q.addressee_participant_ids
		FROM practice_questions q
		JOIN practice_turns t
		  ON t.session_id=q.session_id AND t.question_id=q.question_id
		WHERE q.session_id=$1 AND t.status='confirmed'
		  AND t.turn_kind='EFFECTIVE' AND t.effective_turns_after <= $2
		ORDER BY q.sequence, q.question_id`, sessionID, boundary)
	if err != nil {
		return practice.IELTSPartProfileEvidence{}, err
	}
	for rows.Next() {
		var question practice.EvidenceQuestion
		if err := rows.Scan(
			&question.ID, &question.Position, &question.ParentQuestionID,
			&question.Text, &question.SpeakerParticipantID,
			&question.AddresseeParticipantIDs,
		); err != nil {
			rows.Close()
			return practice.IELTSPartProfileEvidence{}, err
		}
		value.Questions = append(value.Questions, question)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return practice.IELTSPartProfileEvidence{}, err
	}
	rows.Close()
	turnRows, err := tx.Query(ctx, `SELECT t.turn_id, t.sequence,
			t.question_id, t.respondent_participant_id, t.transcript,
			t.confirmed_at, COALESCE(t.audio_asset_id::text,''), t.progressed_at
		FROM practice_turns t
		WHERE t.session_id=$1 AND t.status='confirmed'
		  AND t.turn_kind='EFFECTIVE' AND t.effective_turns_after <= $2
		ORDER BY t.effective_turns_after, t.sequence`, sessionID, boundary)
	if err != nil {
		return practice.IELTSPartProfileEvidence{}, err
	}
	defer turnRows.Close()
	for turnRows.Next() {
		var turn practice.EvidenceTurn
		var progressedAt *time.Time
		if err := turnRows.Scan(
			&turn.ID, &turn.Position, &turn.QuestionID,
			&turn.RespondentParticipantID, &turn.Transcript,
			&turn.ConfirmedAt, &turn.AudioAssetID, &progressedAt,
		); err != nil {
			return practice.IELTSPartProfileEvidence{}, err
		}
		if progressedAt == nil {
			return practice.IELTSPartProfileEvidence{}, practice.ErrConflict
		}
		turn.Effective = true
		value.Turns = append(value.Turns, turn)
		value.CompletedAt = progressedAt.UTC()
	}
	if err := turnRows.Err(); err != nil {
		return practice.IELTSPartProfileEvidence{}, err
	}
	if len(value.Turns) != boundary || len(value.Questions) == 0 ||
		value.CompletedAt.IsZero() {
		return practice.IELTSPartProfileEvidence{}, practice.ErrConflict
	}
	return value, nil
}
