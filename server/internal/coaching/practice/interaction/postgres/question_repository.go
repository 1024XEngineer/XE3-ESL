package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
)

func (r *Repository) SaveQuestion(
	ctx context.Context,
	actor practiceinteraction.Actor,
	question practice.Question,
) (practice.Question, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validInputActor(actor) || !validQuestion(question) {
		return practice.Question{}, practiceinteraction.ErrPersistenceInvalid
	}
	createdAtProvided := !question.CreatedAt.IsZero()
	if question.CreatedAt.IsZero() {
		question.CreatedAt = databaseTime(r.now)
	} else {
		question.CreatedAt = question.CreatedAt.UTC().Truncate(time.Microsecond)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := ensureActorWritable(ctx, tx, actor.UserID); err != nil {
		return practice.Question{}, err
	}
	r.reachedWriteFence()
	if err := lockEvidenceSourceSession(ctx, tx, actor.UserID, question.SessionID); err != nil {
		return practice.Question{}, err
	}
	var sessionStatus practice.SessionStatus
	err = tx.QueryRow(ctx, `
		SELECT status FROM practice_sessions
		WHERE user_id = $1 AND session_id = $2 FOR UPDATE
	`, actor.UserID, question.SessionID).Scan(&sessionStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Question{}, practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	if sessionStatus != practice.SessionStarting &&
		sessionStatus != practice.SessionInProgress {
		return practice.Question{}, practiceinteraction.ErrPersistenceConflict
	}
	if question.Type == "FOLLOW_UP" {
		var parentType string
		err := tx.QueryRow(ctx, `
			SELECT q.question_type FROM practice_questions q
			JOIN practice_sessions s ON s.session_id=q.session_id
			WHERE s.user_id = $1 AND q.session_id = $2 AND q.question_id = $3
		`, actor.UserID, question.SessionID, question.ParentQuestionID).Scan(&parentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return practice.Question{}, practiceinteraction.ErrPersistenceInvalid
		}
		if err != nil {
			return practice.Question{}, safeDatabaseError(err)
		}
		if parentType != "PRIMARY" {
			return practice.Question{}, practiceinteraction.ErrPersistenceInvalid
		}
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO practice_questions (
			question_id, session_id, objective_id,
			question_type, parent_question_id, content,
			speaker_participant_id, addressee_participant_ids, sequence,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$10)
		ON CONFLICT (session_id, sequence) DO NOTHING
	`, question.ID, question.SessionID, question.ObjectiveID,
		question.Type, question.ParentQuestionID, question.Content,
		question.SpeakerParticipantID, question.AddresseeParticipantIDs,
		question.Sequence, question.CreatedAt)
	if err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	saved, err := getSessionQuestionBySequence(
		ctx, tx, actor.UserID, question.SessionID, question.Sequence,
	)
	if err != nil {
		return practice.Question{}, err
	}
	if tag.RowsAffected() == 0 {
		if saved.SessionID != question.SessionID || saved.Sequence != question.Sequence {
			return practice.Question{}, practiceinteraction.ErrPersistenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return practice.Question{}, safeDatabaseError(err)
		}
		return saved, nil
	}
	if !sameQuestion(saved, question, createdAtProvided) {
		return practice.Question{}, practiceinteraction.ErrPersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	return saved, nil
}

func (r *Repository) GetQuestion(
	ctx context.Context,
	actor practiceinteraction.Actor,
	questionID string,
) (practice.Question, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(questionID) == "" {
		return practice.Question{}, practiceinteraction.ErrPersistenceInvalid
	}
	tx, err := r.beginActorRead(ctx, actor)
	if err != nil {
		return practice.Question{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	question, err := getQuestion(ctx, tx, actor.UserID, questionID)
	if err != nil {
		return practice.Question{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	return question, nil
}

func (r *Repository) ListSessionQuestions(
	ctx context.Context,
	actor practiceinteraction.Actor,
	sessionID string,
) ([]practice.Question, error) {
	if r == nil || r.pool == nil || ctx == nil || !validInputActor(actor) ||
		strings.TrimSpace(sessionID) == "" {
		return nil, practiceinteraction.ErrPersistenceInvalid
	}
	return r.listSessionQuestions(ctx, actor.UserID, sessionID)
}

func (r *Repository) listSessionQuestions(
	ctx context.Context,
	ownerUserID string,
	sessionID string,
) ([]practice.Question, error) {
	tx, err := r.beginOwnerRead(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, questionColumns+`
		WHERE s.user_id = $1 AND q.session_id = $2
		ORDER BY q.sequence, q.question_id
	`, ownerUserID, sessionID)
	if err != nil {
		return nil, safeDatabaseError(err)
	}
	defer rows.Close()
	questions := make([]practice.Question, 0)
	for rows.Next() {
		question, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, safeDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, safeDatabaseError(err)
	}
	return questions, nil
}

const questionColumns = `
	SELECT q.question_id, q.session_id, q.speaker_participant_id,
	       q.addressee_participant_ids, q.objective_id, q.question_type,
	       COALESCE(q.parent_question_id::text, ''), q.content, q.sequence, q.created_at
	FROM practice_questions q
	JOIN practice_sessions s ON s.session_id=q.session_id
`

func getQuestion(
	ctx context.Context,
	database queryRow,
	ownerUserID string,
	questionID string,
) (practice.Question, error) {
	return scanQuestion(database.QueryRow(ctx, questionColumns+`
		WHERE s.user_id = $1 AND q.question_id = $2
	`, ownerUserID, questionID))
}

func getSessionQuestionBySequence(
	ctx context.Context,
	database queryRow,
	ownerUserID string,
	sessionID string,
	sequence int,
) (practice.Question, error) {
	return scanQuestion(database.QueryRow(ctx, questionColumns+`
		WHERE s.user_id = $1 AND q.session_id = $2 AND q.sequence = $3
	`, ownerUserID, sessionID, sequence))
}

func scanQuestion(row rowScanner) (practice.Question, error) {
	var question practice.Question
	err := row.Scan(
		&question.ID,
		&question.SessionID,
		&question.SpeakerParticipantID,
		&question.AddresseeParticipantIDs,
		&question.ObjectiveID,
		&question.Type,
		&question.ParentQuestionID,
		&question.Content,
		&question.Sequence,
		&question.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Question{}, practiceinteraction.ErrPersistenceNotFound
	}
	if err != nil {
		return practice.Question{}, safeDatabaseError(err)
	}
	question.CreatedAt = question.CreatedAt.UTC()
	return question, nil
}

func validQuestion(question practice.Question) bool {
	if strings.TrimSpace(question.ID) == "" ||
		strings.TrimSpace(question.SessionID) == "" ||
		strings.TrimSpace(question.SpeakerParticipantID) == "" ||
		len(question.AddresseeParticipantIDs) == 0 ||
		strings.TrimSpace(question.ObjectiveID) == "" ||
		strings.TrimSpace(question.Content) == "" || question.Sequence <= 0 {
		return false
	}
	seen := make(map[string]struct{}, len(question.AddresseeParticipantIDs))
	for _, addressee := range question.AddresseeParticipantIDs {
		if strings.TrimSpace(addressee) == "" {
			return false
		}
		if _, found := seen[addressee]; found {
			return false
		}
		seen[addressee] = struct{}{}
	}
	switch question.Type {
	case "PRIMARY":
		return question.ParentQuestionID == ""
	case "FOLLOW_UP":
		return strings.TrimSpace(question.ParentQuestionID) != ""
	default:
		return false
	}
}

func sameQuestion(left, right practice.Question, compareCreatedAt bool) bool {
	if left.ID != right.ID || left.SessionID != right.SessionID ||
		left.SpeakerParticipantID != right.SpeakerParticipantID ||
		left.ObjectiveID != right.ObjectiveID || left.Type != right.Type ||
		left.ParentQuestionID != right.ParentQuestionID ||
		left.Content != right.Content || left.Sequence != right.Sequence ||
		len(left.AddresseeParticipantIDs) != len(right.AddresseeParticipantIDs) {
		return false
	}
	if compareCreatedAt && !left.CreatedAt.Equal(right.CreatedAt) {
		return false
	}
	for index := range left.AddresseeParticipantIDs {
		if left.AddresseeParticipantIDs[index] != right.AddresseeParticipantIDs[index] {
			return false
		}
	}
	return true
}
