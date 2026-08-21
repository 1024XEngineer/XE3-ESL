package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func (r *Repository) CreateRetryTurn(
	ctx context.Context,
	tx pgx.Tx,
	command practice.CreateRetryTurnCommand,
) (practice.Turn, bool, error) {
	if r == nil || tx == nil || ctx == nil || !validUserID(command.UserID) ||
		!validResourceID(command.SessionID) ||
		!validResourceID(command.OriginalTurnID) ||
		!validResourceID(command.QuestionID) ||
		!validClientRequestID(command.ClientRequestID) {
		return practice.Turn{}, false, practice.ErrInvalidArgument
	}
	if err := lockActiveActor(ctx, tx, command.UserID); err != nil {
		return practice.Turn{}, false, err
	}

	existing, err := readRetryTurnByRequest(
		ctx, tx, command.UserID, command.SessionID, command.ClientRequestID,
		" FOR UPDATE OF t",
	)
	if err == nil {
		if existing.SessionID != command.SessionID ||
			existing.OriginalTurnID != command.OriginalTurnID ||
			existing.QuestionID != command.QuestionID {
			return practice.Turn{}, false, practice.ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, false, err
	}

	var status practice.SessionStatus
	var effectiveTurns int
	var snapshotJSON, participantsJSON []byte
	err = tx.QueryRow(ctx, `SELECT status,effective_turns,plan_snapshot,participants
FROM practice_sessions WHERE user_id=$1 AND session_id=$2 FOR UPDATE`,
		command.UserID, command.SessionID).
		Scan(&status, &effectiveTurns, &snapshotJSON, &participantsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, false, practice.ErrNotFound
	}
	if err != nil {
		return practice.Turn{}, false, err
	}
	if status != practice.SessionInProgress && status != practice.SessionCompleted {
		return practice.Turn{}, false, practice.ErrConflict
	}
	var snapshot practice.SessionSnapshot
	if decodeStrictJSON(snapshotJSON, &snapshot) != nil ||
		decodeStrictJSON(participantsJSON, &snapshot.Participants) != nil ||
		!snapshot.SessionPolicy.RetryAllowed {
		return practice.Turn{}, false, practice.ErrConflict
	}

	var respondent string
	var originalKind practice.TurnKind
	var originalStatus string
	err = tx.QueryRow(ctx, `SELECT t.respondent_participant_id,t.turn_kind,t.status
FROM practice_turns t JOIN practice_sessions s ON s.session_id=t.session_id
WHERE s.user_id=$1 AND t.turn_id=$2
AND t.session_id=$3 AND t.question_id=$4 FOR UPDATE OF t`, command.UserID,
		command.OriginalTurnID, command.SessionID, command.QuestionID).
		Scan(&respondent, &originalKind, &originalStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Turn{}, false, practice.ErrNotFound
	}
	if err != nil {
		return practice.Turn{}, false, err
	}
	if originalKind != practice.TurnKindEffective || originalStatus != "confirmed" ||
		!participantBelongsToActor(snapshot.Participants, respondent, command.UserID) {
		return practice.Turn{}, false, practice.ErrConflict
	}

	turnID, err := r.ids.NewID()
	if err != nil || !practice.ValidAggregateID(turnID) {
		return practice.Turn{}, false, practice.ErrConflict
	}
	var sequence int
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(t.sequence),0)+1
FROM practice_turns t JOIN practice_sessions s ON s.session_id=t.session_id
WHERE s.user_id=$1 AND t.session_id=$2`,
		command.UserID, command.SessionID).Scan(&sequence)
	if err != nil {
		return practice.Turn{}, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO practice_turns (
turn_id,session_id,question_id,respondent_participant_id,
sequence,turn_kind,status,original_turn_id,client_request_id,
counts_toward_turn_limit,effective_turns_after,session_version_after)
VALUES ($2,$3,$4,$5,$6,'RETRY','answering',$7,$8,false,$9,
       (SELECT version FROM practice_sessions
        WHERE user_id=$1 AND session_id=$3))`, command.UserID, turnID,
		command.SessionID, command.QuestionID, respondent, sequence,
		command.OriginalTurnID, command.ClientRequestID, effectiveTurns)
	if err != nil {
		return practice.Turn{}, false,
			classifyWriteError("create retry practice turn", err)
	}
	created, err := readRetryTurnByRequest(
		ctx, tx, command.UserID, command.SessionID, command.ClientRequestID, "",
	)
	return created, false, err
}

func participantBelongsToActor(
	participants []practice.Participant,
	participantID,
	userID string,
) bool {
	for _, participant := range participants {
		if participant.ID == participantID && participant.Role == "LEARNER" &&
			participant.SubjectRef.Namespace == "speakup.user" &&
			participant.SubjectRef.SubjectID == userID {
			return true
		}
	}
	return false
}

type retryTurnQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readRetryTurnByRequest(
	ctx context.Context,
	query retryTurnQuery,
	ownerID,
	sessionID,
	requestID,
	suffix string,
) (practice.Turn, error) {
	var turn practice.Turn
	var confirmedAt *time.Time
	err := query.QueryRow(ctx, `SELECT t.turn_id,t.session_id,t.question_id,
t.respondent_participant_id,t.sequence,t.turn_kind,t.client_request_id,
t.original_turn_id,t.counts_toward_turn_limit,
COALESCE(t.effective_turns_after,0),t.status,t.created_at,t.confirmed_at
FROM practice_turns t JOIN practice_sessions s ON s.session_id=t.session_id
WHERE s.user_id=$1 AND t.session_id=$2 AND t.client_request_id=$3`+suffix,
		ownerID, sessionID, requestID).Scan(&turn.ID, &turn.SessionID, &turn.QuestionID,
		&turn.RespondentParticipantID, &turn.Sequence, &turn.Kind,
		&turn.ClientRequestID, &turn.OriginalTurnID,
		&turn.CountsTowardTurnLimit, &turn.EffectiveTurns, &turn.Status,
		&turn.CreatedAt, &confirmedAt)
	if err != nil {
		return practice.Turn{}, err
	}
	turn.CreatedAt = turn.CreatedAt.UTC()
	if confirmedAt != nil {
		turn.ConfirmedAt = confirmedAt.UTC()
	}
	return turn, nil
}
