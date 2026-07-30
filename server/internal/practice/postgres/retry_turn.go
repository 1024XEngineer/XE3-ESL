package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

var _ persistence.RetryTurnRepository = (*Repository)(nil)

func (r *Repository) AuthorizeRetryTurn(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.AuthorizeRetryTurnCommand,
) (persistence.RetryTurnAuthorization, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(command.RetryRequestID) ||
		!validContextResourceID(command.PracticeSessionID) ||
		!validContextResourceID(command.OriginalTurnID) ||
		!validContextResourceID(command.QuestionID) {
		return persistence.RetryTurnAuthorization{},
			persistence.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return persistence.RetryTurnAuthorization{},
			fmt.Errorf("begin Practice retry Turn authorization: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return persistence.RetryTurnAuthorization{}, err
	}

	existing, err := getRetryTurnAuthorization(
		ctx,
		tx,
		actor.UserID,
		command.RetryRequestID,
		" FOR UPDATE",
	)
	if err == nil {
		if existing.PracticeSessionID != command.PracticeSessionID ||
			existing.OriginalTurnID != command.OriginalTurnID ||
			existing.QuestionID != command.QuestionID {
			return persistence.RetryTurnAuthorization{},
				persistence.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return persistence.RetryTurnAuthorization{},
				fmt.Errorf(
					"commit replayed Practice retry authorization: %w",
					err,
				)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return persistence.RetryTurnAuthorization{}, err
	}

	var (
		scenarioType  persistence.ScenarioFamily
		scenarioModel persistence.ScenarioModel
		sessionStatus persistence.ContextSessionStatus
		snapshotID    string
		snapshotJSON  []byte
	)
	err = tx.QueryRow(ctx, `
		SELECT
			session.scenario_type,
			session.scenario_model,
			session.status,
			session.snapshot_id,
			snapshot.snapshot_document
		FROM practice_sessions AS session
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = session.owner_user_id
		 AND snapshot.session_id = session.session_id
		 AND snapshot.context_plan_id = session.context_plan_id
		 AND snapshot.snapshot_id = session.snapshot_id
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND session.context_plan_id IS NOT NULL
		FOR UPDATE OF session
	`, actor.UserID, command.PracticeSessionID).Scan(
		&scenarioType,
		&scenarioModel,
		&sessionStatus,
		&snapshotID,
		&snapshotJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.RetryTurnAuthorization{},
			persistence.ErrNotFound
	}
	if err != nil {
		return persistence.RetryTurnAuthorization{},
			fmt.Errorf("lock Practice retry Session: %w", err)
	}
	snapshot, decodeErr := decodeContextSnapshot(snapshotJSON)
	if decodeErr != nil ||
		snapshot.ID != snapshotID ||
		snapshot.SessionID != command.PracticeSessionID ||
		snapshot.ScenarioType != scenarioType ||
		snapshot.ScenarioModel != scenarioModel {
		return persistence.RetryTurnAuthorization{},
			persistence.ErrConflict
	}
	if !eligibleRetryScenario(scenarioType, scenarioModel) ||
		(sessionStatus != persistence.ContextSessionProgress &&
			sessionStatus != persistence.ContextSessionCompleted) {
		return persistence.RetryTurnAuthorization{},
			persistence.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_retry_turn_authorizations (
			owner_user_id,
			retry_request_id,
			practice_session_id,
			original_turn_id,
			question_id,
			scenario_type,
			scenario_model,
			session_status_at_authorization,
			counts_toward_effective_turn_limit
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, false)
	`, actor.UserID, command.RetryRequestID,
		command.PracticeSessionID, command.OriginalTurnID,
		command.QuestionID, scenarioType, scenarioModel,
		sessionStatus); err != nil {
		return persistence.RetryTurnAuthorization{},
			classifyRetryAuthorizationWrite(err)
	}
	authorization, err := getRetryTurnAuthorization(
		ctx,
		tx,
		actor.UserID,
		command.RetryRequestID,
		"",
	)
	if err != nil {
		return persistence.RetryTurnAuthorization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistence.RetryTurnAuthorization{},
			fmt.Errorf("commit Practice retry authorization: %w", err)
	}
	return authorization, nil
}

func (r *Repository) ResolveRetryParticipant(
	ctx context.Context,
	actor persistence.Actor,
	command persistence.ResolveRetryParticipantCommand,
) (string, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) ||
		!validContextResourceID(command.RetryRequestID) ||
		!validContextResourceID(command.ActorSubjectNamespace) {
		return "", persistence.ErrInvalidArgument
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin retry participant resolution: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return "", err
	}
	var (
		sessionID     string
		scenarioType  persistence.ScenarioFamily
		scenarioModel persistence.ScenarioModel
		sessionStatus persistence.ContextSessionStatus
		snapshotID    string
		snapshotJSON  []byte
		countsToward  bool
	)
	err = tx.QueryRow(ctx, `
		SELECT
			retry_auth.practice_session_id,
			retry_auth.scenario_type,
			retry_auth.scenario_model,
			practice_session.status,
			practice_session.snapshot_id,
			practice_snapshot.snapshot_document,
			retry_auth.counts_toward_effective_turn_limit
		FROM practice_retry_turn_authorizations AS retry_auth
		JOIN practice_sessions AS practice_session
		  ON practice_session.owner_user_id = retry_auth.owner_user_id
		 AND practice_session.session_id = retry_auth.practice_session_id
		JOIN practice_session_snapshots AS practice_snapshot
		  ON practice_snapshot.owner_user_id =
		     practice_session.owner_user_id
		 AND practice_snapshot.session_id = practice_session.session_id
		 AND practice_snapshot.context_plan_id =
		     practice_session.context_plan_id
		 AND practice_snapshot.snapshot_id = practice_session.snapshot_id
		WHERE retry_auth.owner_user_id = $1
		  AND retry_auth.retry_request_id = $2
		FOR SHARE OF retry_auth, practice_session, practice_snapshot
	`, actor.UserID, command.RetryRequestID).Scan(
		&sessionID,
		&scenarioType,
		&scenarioModel,
		&sessionStatus,
		&snapshotID,
		&snapshotJSON,
		&countsToward,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", persistence.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read retry participant source: %w", err)
	}
	snapshot, err := decodeContextSnapshot(snapshotJSON)
	if err != nil ||
		snapshot.ID != snapshotID ||
		snapshot.SessionID != sessionID ||
		snapshot.ScenarioType != scenarioType ||
		snapshot.ScenarioModel != scenarioModel ||
		countsToward ||
		!eligibleRetryScenario(scenarioType, scenarioModel) ||
		(sessionStatus != persistence.ContextSessionProgress &&
			sessionStatus != persistence.ContextSessionCompleted) {
		return "", persistence.ErrConflict
	}
	participantID, err := retryActorParticipant(
		snapshot,
		actor.UserID,
		command.ActorSubjectNamespace,
	)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit retry participant resolution: %w", err)
	}
	return participantID, nil
}

const retryAuthorizationSelect = `
	SELECT
		retry_request_id::text,
		practice_session_id,
		original_turn_id,
		question_id,
		scenario_type,
		scenario_model,
		session_status_at_authorization,
		counts_toward_effective_turn_limit,
		created_at
	FROM practice_retry_turn_authorizations
`

func getRetryTurnAuthorization(
	ctx context.Context,
	database interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	ownerUserID string,
	retryRequestID string,
	suffix string,
) (persistence.RetryTurnAuthorization, error) {
	var authorization persistence.RetryTurnAuthorization
	err := database.QueryRow(ctx, retryAuthorizationSelect+`
		WHERE owner_user_id = $1
		  AND retry_request_id = $2
	`+suffix, ownerUserID, retryRequestID).Scan(
		&authorization.RetryRequestID,
		&authorization.PracticeSessionID,
		&authorization.OriginalTurnID,
		&authorization.QuestionID,
		&authorization.ScenarioType,
		&authorization.ScenarioModel,
		&authorization.SessionStatusAtAuthorization,
		&authorization.CountsTowardEffectiveLimit,
		&authorization.CreatedAt,
	)
	if err != nil {
		return persistence.RetryTurnAuthorization{}, err
	}
	authorization.CreatedAt = authorization.CreatedAt.UTC()
	return authorization, nil
}

func eligibleRetryScenario(
	scenarioType persistence.ScenarioFamily,
	scenarioModel persistence.ScenarioModel,
) bool {
	switch scenarioType {
	case persistence.ScenarioFamilyWorkplace:
		return scenarioModel == persistence.ScenarioModelProgressAndRiskUpdate ||
			scenarioModel ==
				persistence.ScenarioModelWorkplaceBasicDialogue
	case persistence.ScenarioFamilyDaily:
		return scenarioModel ==
			persistence.ScenarioModelHotelCheckinAndIssueHandling ||
			scenarioModel == persistence.ScenarioModelDailyBasicDialogue
	default:
		return false
	}
}

func retryActorParticipant(
	snapshot persistence.ContextSessionSnapshot,
	actorUserID string,
	actorSubjectNamespace string,
) (string, error) {
	participantID := ""
	facilitators := 0
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	participantOrders := make(map[int]struct{}, len(snapshot.Participants))
	for _, participant := range snapshot.Participants {
		if participant.ID == "" ||
			participant.SessionID != snapshot.SessionID ||
			participant.Order < 1 {
			return "", persistence.ErrConflict
		}
		if _, duplicate := participantIDs[participant.ID]; duplicate {
			return "", persistence.ErrConflict
		}
		if _, duplicate := participantOrders[participant.Order]; duplicate {
			return "", persistence.ErrConflict
		}
		participantIDs[participant.ID] = struct{}{}
		participantOrders[participant.Order] = struct{}{}
		switch participant.Role {
		case "FACILITATOR", "INTERVIEWER":
			if participant.SubjectRef.Namespace != "speakup.role" ||
				participant.SubjectRef.SubjectID !=
					participant.RoleDefinitionID ||
				participant.RoleDefinitionID == "" ||
				participant.RoleSnapshot == nil ||
				participant.RoleSnapshot.ID !=
					participant.RoleDefinitionID {
				return "", persistence.ErrConflict
			}
			facilitators++
		case "LEARNER", "CANDIDATE":
			if participantID != "" {
				return "", persistence.ErrConflict
			}
			if participant.SubjectRef.Namespace !=
				actorSubjectNamespace ||
				participant.SubjectRef.SubjectID != actorUserID ||
				participant.RoleDefinitionID != "" ||
				participant.RoleSnapshot != nil {
				return "", persistence.ErrNotFound
			}
			participantID = participant.ID
		default:
			return "", persistence.ErrConflict
		}
	}
	if participantID == "" || facilitators == 0 {
		return "", persistence.ErrNotFound
	}
	return participantID, nil
}

func classifyRetryAuthorizationWrite(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return persistence.ErrNotFound
		case "23505", "23514":
			return persistence.ErrIdempotencyConflict
		}
	}
	return fmt.Errorf("persist Practice retry authorization: %w", err)
}
