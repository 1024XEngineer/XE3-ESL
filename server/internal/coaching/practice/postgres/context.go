package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	presentationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/presentation/postgres"
)

const sessionColumns = `session_id, plan_id, plan_version, practice_experience,
scene_category, practice_mode, evaluation_policy_ref, status, version,
effective_turns, started_at, ended_at, COALESCE(end_reason,''), created_at`

func (r *Repository) CreateSession(ctx context.Context, actor practice.Actor, command practice.CreateSessionCommand) (practice.SessionBootstrap, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) || !validCreateSessionCommand(command) {
		return practice.SessionBootstrap{}, false, practice.ErrInvalidArgument
	}
	planSnapshot := command.Snapshot
	participants := planSnapshot.Participants
	planSnapshot.Participants = nil
	planJSON, err := json.Marshal(planSnapshot)
	if err != nil {
		return practice.SessionBootstrap{}, false, practice.ErrInvalidArgument
	}
	participantsJSON, err := json.Marshal(participants)
	if err != nil {
		return practice.SessionBootstrap{}, false, practice.ErrInvalidArgument
	}
	evaluationPolicy := selectedEvaluationPolicyRef(command.Snapshot)
	if evaluationPolicy == "" {
		return practice.SessionBootstrap{}, false, practice.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return practice.SessionBootstrap{}, false, fmt.Errorf("begin practice session creation: %w", err)
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return practice.SessionBootstrap{}, false, err
	}
	var existingSessionID, storedPlanID string
	var storedPlanVersion int
	var storedFingerprint []byte
	err = tx.QueryRow(ctx, `SELECT session_id,plan_id,plan_version,initial_request_fingerprint
FROM practice_sessions WHERE user_id=$1 AND initial_client_request_id=$2`,
		actor.UserID, command.ClientRequestID,
	).Scan(&existingSessionID, &storedPlanID, &storedPlanVersion, &storedFingerprint)
	if err == nil {
		if !bytes.Equal(storedFingerprint, command.RequestFingerprint[:]) ||
			storedPlanID != command.PlanID || storedPlanVersion != command.PlanVersion {
			return practice.SessionBootstrap{}, false, practice.ErrIdempotencyConflict
		}
		value, readErr := readBootstrap(ctx, tx, actor.UserID, existingSessionID)
		if readErr != nil {
			return practice.SessionBootstrap{}, false, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return practice.SessionBootstrap{}, false, err
		}
		return value, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return practice.SessionBootstrap{}, false, err
	}
	presentationRepository, err := presentationpostgres.New(tx)
	if err != nil {
		return practice.SessionBootstrap{}, false, practice.ErrConflict
	}
	selection, err := presentationRepository.ResolveSelection(ctx, actor.UserID)
	if err != nil {
		return practice.SessionBootstrap{}, false, practice.ErrConflict
	}
	presentationSnapshot := practice.PresentationSnapshot{
		SchemaVersion: practice.PresentationSnapshotSchemaVersion,
		Avatar: practice.AvatarPresentationSnapshot{
			OptionID: selection.Avatar.ID, Provider: selection.Avatar.Provider,
			ProviderProfile:  selection.Avatar.ProviderProfile,
			ProviderAvatarID: selection.Avatar.ProviderAvatarID,
			BindingVersion:   selection.Avatar.BindingVersion,
		},
		Voice: practice.VoicePresentationSnapshot{
			OptionID: selection.Voice.ID, Provider: selection.Voice.Provider,
			ProviderProfile: selection.Voice.ProviderProfile,
			ProviderModel:   selection.Voice.ProviderModel,
			ProviderVoiceID: selection.Voice.ProviderVoiceID,
			Locale:          selection.Voice.Locale,
			BindingVersion:  selection.Voice.BindingVersion,
		},
	}
	if !presentationSnapshot.Valid() {
		return practice.SessionBootstrap{}, false, practice.ErrConflict
	}
	presentationJSON, err := json.Marshal(presentationSnapshot)
	if err != nil {
		return practice.SessionBootstrap{}, false, practice.ErrInvalidArgument
	}
	_, err = tx.Exec(ctx, `INSERT INTO practice_sessions (
user_id,session_id,plan_id,plan_version,practice_experience,scene_category,
practice_mode,evaluation_policy_ref,status,version,effective_turns,plan_snapshot,
participants,presentation_snapshot,initial_client_request_id,initial_request_fingerprint)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'starting',1,0,$9,$10,$11,$12,$13)`, actor.UserID,
		command.SessionID, command.PlanID, command.PlanVersion,
		string(command.Snapshot.Experience), string(command.Snapshot.Category),
		string(command.Snapshot.PracticeMode), evaluationPolicy, planJSON,
		participantsJSON, presentationJSON, command.ClientRequestID,
		command.RequestFingerprint[:])
	if err != nil {
		return practice.SessionBootstrap{}, false, classifyWriteError("create practice session", err)
	}
	value, err := readBootstrap(ctx, tx, actor.UserID, command.SessionID)
	if err != nil {
		return practice.SessionBootstrap{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.SessionBootstrap{}, false, err
	}
	return value, false, nil
}

func (r *Repository) GetSession(ctx context.Context, actor practice.Actor, id string) (practice.Session, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) || !validResourceID(id) {
		return practice.Session{}, practice.ErrNotFound
	}
	value, err := readBootstrap(ctx, r.pool, actor.UserID, id)
	return value.Session, err
}

func (r *Repository) GetSessionSnapshot(ctx context.Context, actor practice.Actor, id string) (practice.SessionSnapshot, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) || !validResourceID(id) {
		return practice.SessionSnapshot{}, practice.ErrNotFound
	}
	value, err := readBootstrap(ctx, r.pool, actor.UserID, id)
	return value.Snapshot, err
}

func (r *Repository) ActivateSession(ctx context.Context, actor practice.Actor, sessionID, clientRequestID string, fingerprint [32]byte) (practice.SessionBootstrap, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) || !validResourceID(sessionID) || !validClientRequestID(clientRequestID) {
		return practice.SessionBootstrap{}, practice.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return practice.SessionBootstrap{}, err
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return practice.SessionBootstrap{}, err
	}
	var status practice.SessionStatus
	var lastID string
	var stored []byte
	err = tx.QueryRow(ctx, `SELECT status,COALESCE(last_client_request_id,''),COALESCE(last_request_fingerprint,'') FROM practice_sessions WHERE user_id=$1 AND session_id=$2 FOR UPDATE`, actor.UserID, sessionID).Scan(&status, &lastID, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.SessionBootstrap{}, practice.ErrNotFound
	}
	if err != nil {
		return practice.SessionBootstrap{}, err
	}
	if lastID == clientRequestID {
		if !bytes.Equal(stored, fingerprint[:]) {
			return practice.SessionBootstrap{}, practice.ErrIdempotencyConflict
		}
		value, err := readBootstrap(ctx, tx, actor.UserID, sessionID)
		if err != nil {
			return practice.SessionBootstrap{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return practice.SessionBootstrap{}, err
		}
		return value, nil
	}
	if status == practice.SessionStarting {
		_, err = tx.Exec(ctx, `UPDATE practice_sessions SET status='in_progress',version=version+1,started_at=transaction_timestamp(),last_client_request_id=$3,last_request_fingerprint=$4,updated_at=transaction_timestamp() WHERE user_id=$1 AND session_id=$2`, actor.UserID, sessionID, clientRequestID, fingerprint[:])
		if err != nil {
			return practice.SessionBootstrap{}, classifyWriteError("activate practice session", err)
		}
	} else if status != practice.SessionInProgress {
		return practice.SessionBootstrap{}, practice.ErrConflict
	}
	value, err := readBootstrap(ctx, tx, actor.UserID, sessionID)
	if err != nil {
		return practice.SessionBootstrap{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.SessionBootstrap{}, err
	}
	return value, nil
}

func (r *Repository) TransitionSession(ctx context.Context, actor practice.Actor, command practice.TransitionSessionCommand) (practice.Session, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validActor(actor) || !validTransitionCommand(command) {
		return practice.Session{}, false, practice.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return practice.Session{}, false, err
	}
	defer rollback(ctx, tx)
	if err := lockActiveActor(ctx, tx, actor.UserID); err != nil {
		return practice.Session{}, false, err
	}
	var current practice.SessionStatus
	var version, effectiveTurns int
	var lastID string
	var stored, snapshotJSON []byte
	err = tx.QueryRow(ctx, `SELECT status,version,effective_turns,COALESCE(last_client_request_id,''),COALESCE(last_request_fingerprint,''),plan_snapshot FROM practice_sessions WHERE user_id=$1 AND session_id=$2 FOR UPDATE`, actor.UserID, command.SessionID).Scan(&current, &version, &effectiveTurns, &lastID, &stored, &snapshotJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.Session{}, false, practice.ErrNotFound
	}
	if err != nil {
		return practice.Session{}, false, err
	}
	if lastID == command.ClientRequestID {
		if !bytes.Equal(stored, command.RequestFingerprint[:]) {
			return practice.Session{}, false, practice.ErrIdempotencyConflict
		}
		value, err := readBootstrap(ctx, tx, actor.UserID, command.SessionID)
		if err != nil {
			return practice.Session{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return practice.Session{}, false, err
		}
		return value.Session, true, nil
	}
	if version != command.ExpectedSessionVersion {
		return practice.Session{}, false, practice.ErrConflict
	}
	var snapshot practice.SessionSnapshot
	if decodeStrictJSON(snapshotJSON, &snapshot) != nil {
		return practice.Session{}, false, practice.ErrConflict
	}
	next, endReason, err := transitionStatus(current, command.Transition, effectiveTurns, snapshot.SessionPolicy.MinEffectiveTurns)
	if err != nil {
		return practice.Session{}, false, err
	}
	endExpression := "ended_at"
	startExpression := "started_at"
	if next == practice.SessionCompleted || next == practice.SessionEndedEarly {
		endExpression = "transaction_timestamp()"
		startExpression = "COALESCE(started_at, transaction_timestamp())"
	}
	_, err = tx.Exec(ctx, `UPDATE practice_sessions SET status=$3,version=version+1,started_at=`+startExpression+`,ended_at=`+endExpression+`,end_reason=NULLIF($4,''),last_client_request_id=$5,last_request_fingerprint=$6,updated_at=transaction_timestamp() WHERE user_id=$1 AND session_id=$2`, actor.UserID, command.SessionID, string(next), endReason, command.ClientRequestID, command.RequestFingerprint[:])
	if err != nil {
		return practice.Session{}, false, classifyWriteError("transition practice session", err)
	}
	if next == practice.SessionCompleted {
		evidence, err := r.ReadSessionEvidence(ctx, tx, actor.UserID, command.SessionID)
		if err != nil {
			return practice.Session{}, false, err
		}
		if err := r.completion.ScheduleCompletedSession(ctx, tx, evidence); err != nil {
			return practice.Session{}, false, err
		}
	}
	value, err := readBootstrap(ctx, tx, actor.UserID, command.SessionID)
	if err != nil {
		return practice.Session{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Session{}, false, err
	}
	return value.Session, false, nil
}

func (r *Repository) ReadSessionEvidence(ctx context.Context, tx pgx.Tx, ownerID, sessionID string) (practice.SessionEvidence, error) {
	if r == nil || tx == nil || ctx == nil || !validUserID(ownerID) || !validResourceID(sessionID) {
		return practice.SessionEvidence{}, practice.ErrInvalidArgument
	}
	var value practice.SessionEvidence
	var participants []byte
	var completedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT user_id::text,session_id,version,ended_at,evaluation_policy_ref,practice_experience,scene_category,practice_mode,plan_snapshot,participants FROM practice_sessions WHERE user_id=$1 AND session_id=$2 AND status='completed'`, ownerID, sessionID).Scan(&value.UserID, &value.SessionID, &value.Version, &completedAt, &value.EvaluationPolicyRef, &value.PracticeExperience, &value.SceneCategory, &value.PracticeMode, &value.PlanSnapshot, &participants)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.SessionEvidence{}, practice.ErrNotFound
	}
	if err != nil || completedAt == nil {
		return practice.SessionEvidence{}, practice.ErrConflict
	}
	value.CompletedAt = completedAt.UTC()
	value.Participants = append(json.RawMessage(nil), participants...)
	rows, err := tx.Query(ctx, `SELECT q.question_id,q.sequence,COALESCE(q.parent_question_id::text,''),q.content,q.speaker_participant_id,q.addressee_participant_ids FROM practice_questions q JOIN practice_sessions s ON s.session_id=q.session_id WHERE s.user_id=$1 AND q.session_id=$2 ORDER BY q.sequence,q.question_id`, ownerID, sessionID)
	if err != nil {
		return practice.SessionEvidence{}, err
	}
	for rows.Next() {
		var q practice.EvidenceQuestion
		if err := rows.Scan(&q.ID, &q.Position, &q.ParentQuestionID, &q.Text, &q.SpeakerParticipantID, &q.AddresseeParticipantIDs); err != nil {
			rows.Close()
			return practice.SessionEvidence{}, err
		}
		value.Questions = append(value.Questions, q)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return practice.SessionEvidence{}, err
	}
	rows.Close()
	turnRows, err := tx.Query(ctx, `SELECT t.turn_id,t.sequence,t.question_id,t.respondent_participant_id,t.transcript,(t.turn_kind='EFFECTIVE'),t.confirmed_at,COALESCE(t.audio_asset_id::text,'') FROM practice_turns t JOIN practice_sessions s ON s.session_id=t.session_id WHERE s.user_id=$1 AND t.session_id=$2 AND t.status='confirmed' ORDER BY t.sequence,t.turn_id`, ownerID, sessionID)
	if err != nil {
		return practice.SessionEvidence{}, err
	}
	defer turnRows.Close()
	for turnRows.Next() {
		var turn practice.EvidenceTurn
		if err := turnRows.Scan(&turn.ID, &turn.Position, &turn.QuestionID, &turn.RespondentParticipantID, &turn.Transcript, &turn.Effective, &turn.ConfirmedAt, &turn.AudioAssetID); err != nil {
			return practice.SessionEvidence{}, err
		}
		value.Turns = append(value.Turns, turn)
	}
	if err := turnRows.Err(); err != nil {
		return practice.SessionEvidence{}, err
	}
	return value, nil
}

type sessionQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readBootstrap(ctx context.Context, query sessionQuery, ownerID, sessionID string) (practice.SessionBootstrap, error) {
	var value practice.SessionBootstrap
	var snapshotJSON, participantsJSON, presentationJSON []byte
	err := query.QueryRow(ctx, `SELECT `+sessionColumns+`,plan_snapshot,participants,presentation_snapshot FROM practice_sessions WHERE user_id=$1 AND session_id=$2`, ownerID, sessionID).Scan(&value.Session.ID, &value.Session.PlanID, &value.Session.PlanVersion, &value.Session.Experience, &value.Session.Category, &value.Session.PracticeMode, &value.Session.EvaluationPolicyRef, &value.Session.Status, &value.Session.Version, &value.Session.EffectiveTurns, &value.Session.StartedAt, &value.Session.EndedAt, &value.Session.EndReason, &value.Session.CreatedAt, &snapshotJSON, &participantsJSON, &presentationJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return practice.SessionBootstrap{}, practice.ErrNotFound
	}
	if err != nil {
		return practice.SessionBootstrap{}, err
	}
	if decodeStrictJSON(snapshotJSON, &value.Snapshot) != nil || decodeStrictJSON(participantsJSON, &value.Snapshot.Participants) != nil || decodeStrictJSON(presentationJSON, &value.Snapshot.Presentation) != nil || !value.Snapshot.Presentation.Valid() {
		return practice.SessionBootstrap{}, practice.ErrConflict
	}
	return value, nil
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func validCreateSessionCommand(command practice.CreateSessionCommand) bool {
	return validResourceID(command.SessionID) && validResourceID(command.PlanID) && command.PlanVersion > 0 && validClientRequestID(command.ClientRequestID) && command.Snapshot.SessionID == command.SessionID && command.Snapshot.PlanVersion == command.PlanVersion && len(command.Snapshot.Participants) > 0
}
func validTransitionCommand(command practice.TransitionSessionCommand) bool {
	return validResourceID(command.SessionID) && command.ExpectedSessionVersion > 0 && validClientRequestID(command.ClientRequestID)
}
func validResourceID(value string) bool {
	return practice.ValidAggregateID(value)
}
func validClientRequestID(value string) bool {
	return len(value) >= 8 && len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}
func selectedEvaluationPolicyRef(snapshot practice.SessionSnapshot) string {
	option, err := snapshot.SceneSelection.PracticeOption()
	if err != nil {
		return ""
	}
	return option.EvaluationPolicyRef
}

func transitionStatus(current practice.SessionStatus, transition practice.SessionTransition, effectiveTurns, minimum int) (practice.SessionStatus, string, error) {
	switch transition {
	case practice.SessionPause:
		if current == practice.SessionInProgress {
			return practice.SessionPaused, "", nil
		}
	case practice.SessionResume:
		if current == practice.SessionPaused {
			return practice.SessionInProgress, "", nil
		}
	case practice.SessionComplete:
		if (current == practice.SessionInProgress || current == practice.SessionPaused) && effectiveTurns >= minimum {
			return practice.SessionCompleted, "completed", nil
		}
	case practice.SessionEndEarly:
		if current == practice.SessionStarting || current == practice.SessionInProgress || current == practice.SessionPaused {
			return practice.SessionEndedEarly, "user_ended", nil
		}
	}
	return "", "", practice.ErrConflict
}

var _ practice.SessionRepository = (*Repository)(nil)
