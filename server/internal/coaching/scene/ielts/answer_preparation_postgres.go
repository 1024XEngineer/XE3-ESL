package ielts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const answerPreparationColumns = `answer_preparation_id, bank_id, part, source_id,
       question_position, question_prompt, personal_points, target_band, status,
       COALESCE(answer, ''), COALESCE(outline, '[]'::jsonb),
       COALESCE(useful_expressions, '[]'::jsonb), COALESCE(speech_text, ''),
       COALESCE(failure_code, ''), version, generation_revision, created_at, updated_at`

type SecureAnswerPreparationIDGenerator struct{}

func (SecureAnswerPreparationIDGenerator) NewAnswerPreparationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "ielts_answer_" + hex.EncodeToString(value[:]), nil
}

func (store *PostgresStore) ResolveAnswerQuestion(ctx context.Context, reference QuestionReference) (ResolvedQuestion, error) {
	if store == nil || store.database == nil || ctx == nil || !validQuestionReference(reference) {
		return ResolvedQuestion{}, ErrAnswerPreparationInvalid
	}
	var prompt string
	var err error
	switch reference.Part {
	case PracticeModePart1:
		err = store.database.QueryRow(ctx, `SELECT prompt FROM ielts_part1_questions WHERE bank_id=$1 AND topic_id=$2 AND question_position=$3`, reference.BankID, reference.SourceID, reference.QuestionPosition).Scan(&prompt)
	case PracticeModePart2:
		var pointsJSON []byte
		err = store.database.QueryRow(ctx, `SELECT cue_card_prompt, cue_card_points FROM ielts_part23_groups WHERE bank_id=$1 AND topic_group_id=$2`, reference.BankID, reference.SourceID).Scan(&prompt, &pointsJSON)
		if err == nil {
			var points []string
			if json.Unmarshal(pointsJSON, &points) != nil {
				return ResolvedQuestion{}, ErrAnswerPreparationRepository
			}
			prompt = formatCueCard(Part2CueCard{Prompt: prompt, Points: points})
		}
	case PracticeModePart3:
		err = store.database.QueryRow(ctx, `SELECT prompt FROM ielts_part3_questions WHERE bank_id=$1 AND topic_group_id=$2 AND question_position=$3`, reference.BankID, reference.SourceID, reference.QuestionPosition).Scan(&prompt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedQuestion{}, ErrQuestionSetNotFound
	}
	if err != nil {
		return ResolvedQuestion{}, fmt.Errorf("%w: resolve answer question", ErrAnswerPreparationRepository)
	}
	return ResolvedQuestion{Reference: reference, Prompt: prompt}, nil
}

func (store *PostgresStore) Create(ctx context.Context, actor requestcontext.Actor, command CreateAnswerPreparationCommand) (AnswerPreparation, bool, error) {
	if !validStoreActor(actor) || !validAnswerPreparationID(command.ID) {
		return AnswerPreparation{}, false, ErrAnswerPreparationInvalid
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveAnswerOwner(ctx, tx, actor.UserID); err != nil {
		return AnswerPreparation{}, false, err
	}
	if replay, found, err := replayAnswerMutation(ctx, tx, actor.UserID, command.Intent); err != nil || found {
		return replay, found, err
	}
	points, _ := json.Marshal(command.Request.PersonalPoints)
	tag, err := tx.Exec(ctx, `INSERT INTO ielts_answer_preparations (owner_user_id, answer_preparation_id, bank_id, part, source_id, question_position, question_prompt, personal_points, target_band) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (owner_user_id, bank_id, part, source_id, question_position) DO NOTHING`, actor.UserID, command.ID, command.Question.Reference.BankID, command.Question.Reference.Part, command.Question.Reference.SourceID, command.Question.Reference.QuestionPosition, command.Question.Prompt, points, command.Request.TargetBand)
	if err != nil {
		if isUniqueViolation(err) {
			return AnswerPreparation{}, false, ErrAnswerPreparationConflict
		}
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	if tag.RowsAffected() == 0 {
		existing, err := getAnswerPreparationByQuestion(ctx, tx, actor.UserID, command.Question.Reference)
		if err != nil {
			return AnswerPreparation{}, false, err
		}
		if err := persistAnswerMutation(ctx, tx, actor.UserID, command.Intent, existing.ID, existing, false); err != nil {
			return AnswerPreparation{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return AnswerPreparation{}, false, answerDatabaseError(err)
		}
		return existing, true, nil
	}
	created, err := getAnswerPreparation(ctx, tx, actor.UserID, command.ID)
	if err != nil {
		return AnswerPreparation{}, false, err
	}
	if err := persistAnswerMutation(ctx, tx, actor.UserID, command.Intent, command.ID, created, false); err != nil {
		return AnswerPreparation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	return created, false, nil
}

func (store *PostgresStore) Get(ctx context.Context, actor requestcontext.Actor, id string) (AnswerPreparation, error) {
	if !validStoreActor(actor) || !validAnswerPreparationID(id) {
		return AnswerPreparation{}, ErrAnswerPreparationInvalid
	}
	return getAnswerPreparation(ctx, store.database, actor.UserID, id)
}

func (store *PostgresStore) Update(ctx context.Context, actor requestcontext.Actor, command UpdateAnswerPreparationCommand) (AnswerPreparation, bool, error) {
	if !validStoreActor(actor) {
		return AnswerPreparation{}, false, ErrAnswerPreparationInvalid
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveAnswerOwner(ctx, tx, actor.UserID); err != nil {
		return AnswerPreparation{}, false, err
	}
	if replay, found, err := replayAnswerMutation(ctx, tx, actor.UserID, command.Intent); err != nil || found {
		return replay, found, err
	}
	points, _ := json.Marshal(command.Request.PersonalPoints)
	tag, err := tx.Exec(ctx, `UPDATE ielts_answer_preparations SET personal_points=$4, target_band=$5, status='draft', answer=NULL, outline=NULL, useful_expressions=NULL, speech_text=NULL, failure_code=NULL, version=version+1, updated_at=transaction_timestamp() WHERE owner_user_id=$1 AND answer_preparation_id=$2 AND version=$3 AND status <> 'generating'`, actor.UserID, command.ID, command.Request.ExpectedVersion, points, command.Request.TargetBand)
	if err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return AnswerPreparation{}, false, classifyAnswerMiss(ctx, tx, actor.UserID, command.ID)
	}
	updated, err := getAnswerPreparation(ctx, tx, actor.UserID, command.ID)
	if err != nil {
		return AnswerPreparation{}, false, err
	}
	if err := persistAnswerMutation(ctx, tx, actor.UserID, command.Intent, command.ID, updated, false); err != nil {
		return AnswerPreparation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	return updated, false, nil
}

func (store *PostgresStore) BeginGeneration(ctx context.Context, actor requestcontext.Actor, command BeginAnswerGenerationCommand) (AnswerPreparation, bool, error) {
	if !validStoreActor(actor) {
		return AnswerPreparation{}, false, ErrAnswerPreparationInvalid
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveAnswerOwner(ctx, tx, actor.UserID); err != nil {
		return AnswerPreparation{}, false, err
	}
	if replay, found, err := replayAnswerMutation(ctx, tx, actor.UserID, command.Intent); err != nil || found {
		return replay, found, err
	}
	tag, err := tx.Exec(ctx, `UPDATE ielts_answer_preparations SET status='generating', answer=NULL, outline=NULL, useful_expressions=NULL, speech_text=NULL, provider=NULL, model=NULL, provider_request_id=NULL, failure_code=NULL, version=version+1, generation_revision=generation_revision+1, updated_at=transaction_timestamp() WHERE owner_user_id=$1 AND answer_preparation_id=$2 AND version=$3 AND status <> 'generating'`, actor.UserID, command.ID, command.Request.ExpectedVersion)
	if err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return AnswerPreparation{}, false, classifyAnswerMiss(ctx, tx, actor.UserID, command.ID)
	}
	generating, err := getAnswerPreparation(ctx, tx, actor.UserID, command.ID)
	if err != nil {
		return AnswerPreparation{}, false, err
	}
	if err := persistAnswerMutation(ctx, tx, actor.UserID, command.Intent, command.ID, generating, true); err != nil {
		return AnswerPreparation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	return generating, false, nil
}

func (store *PostgresStore) CompleteGeneration(ctx context.Context, actor requestcontext.Actor, command CompleteAnswerGenerationCommand) (AnswerPreparation, error) {
	if !validStoreActor(actor) || !validGenerationResult(command.Result) {
		return AnswerPreparation{}, ErrAnswerPreparationInvalid
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return AnswerPreparation{}, answerDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	outline, _ := json.Marshal(command.Result.Outline)
	expressions, _ := json.Marshal(command.Result.UsefulExpressions)
	tag, err := tx.Exec(ctx, `UPDATE ielts_answer_preparations SET status='ready', answer=$5, outline=$6, useful_expressions=$7, speech_text=$8, provider=$9, model=$10, provider_request_id=$11, failure_code=NULL, version=version+1, updated_at=transaction_timestamp() WHERE owner_user_id=$1 AND answer_preparation_id=$2 AND status='generating' AND version=$3 AND generation_revision=$4`, actor.UserID, command.ID, command.GeneratingVersion, command.GenerationRevision, command.Result.Answer, outline, expressions, command.Result.SpeechText, command.Result.Provider, command.Result.Model, command.Result.RequestID)
	if err != nil {
		return AnswerPreparation{}, answerDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return AnswerPreparation{}, ErrAnswerPreparationConflict
	}
	ready, err := getAnswerPreparation(ctx, tx, actor.UserID, command.ID)
	if err != nil {
		return AnswerPreparation{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ielts_answer_preparation_idempotency SET pending=false, response=$3, updated_at=transaction_timestamp() WHERE owner_user_id=$1 AND resource_id=$2 AND pending`, actor.UserID, command.ID, mustJSON(ready)); err != nil {
		return AnswerPreparation{}, answerDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AnswerPreparation{}, answerDatabaseError(err)
	}
	return ready, nil
}

func (store *PostgresStore) FailGeneration(ctx context.Context, actor requestcontext.Actor, command FailAnswerGenerationCommand) error {
	if !validStoreActor(actor) || command.FailureCode == "" {
		return ErrAnswerPreparationInvalid
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return answerDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE ielts_answer_preparations SET status='failed', failure_code=$5, version=version+1, updated_at=transaction_timestamp() WHERE owner_user_id=$1 AND answer_preparation_id=$2 AND status='generating' AND version=$3 AND generation_revision=$4`, actor.UserID, command.ID, command.GeneratingVersion, command.GenerationRevision, command.FailureCode)
	if err != nil {
		return answerDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return ErrAnswerPreparationConflict
	}
	failed, err := getAnswerPreparation(ctx, tx, actor.UserID, command.ID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE ielts_answer_preparation_idempotency SET pending=false, response=$3, updated_at=transaction_timestamp() WHERE owner_user_id=$1 AND resource_id=$2 AND pending`, actor.UserID, command.ID, mustJSON(failed)); err != nil {
		return answerDatabaseError(err)
	}
	return tx.Commit(ctx)
}

func (store *PostgresStore) Delete(ctx context.Context, actor requestcontext.Actor, command DeleteAnswerPreparationCommand) (bool, error) {
	if !validStoreActor(actor) {
		return false, ErrAnswerPreparationInvalid
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return false, answerDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveAnswerOwner(ctx, tx, actor.UserID); err != nil {
		return false, err
	}
	if _, found, err := replayAnswerMutation(ctx, tx, actor.UserID, command.Intent); err != nil {
		return false, err
	} else if found {
		return true, nil
	}
	tag, err := tx.Exec(ctx, `DELETE FROM ielts_answer_preparations WHERE owner_user_id=$1 AND answer_preparation_id=$2 AND version=$3 AND status <> 'generating'`, actor.UserID, command.ID, command.ExpectedVersion)
	if err != nil {
		return false, answerDatabaseError(err)
	}
	if tag.RowsAffected() != 1 {
		return false, classifyAnswerMiss(ctx, tx, actor.UserID, command.ID)
	}
	// A tombstone keeps DELETE idempotency after the resource and its resource-bound mutation records are removed.
	if err := persistAnswerMutation(ctx, tx, actor.UserID, command.Intent, "", AnswerPreparation{}, false); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, answerDatabaseError(err)
	}
	return false, nil
}

// DeleteUserAnswerPreparations is the account-cleanup boundary. It serializes
// with writes through the identity row and accepts only an identity already in
// the deleting/deleted lifecycle.
func (store *PostgresStore) DeleteUserAnswerPreparations(ctx context.Context, userID string) error {
	if store == nil || store.database == nil || ctx == nil || strings.TrimSpace(userID) == "" {
		return ErrAnswerPreparationInvalid
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return answerDatabaseError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT account_status FROM identity_users WHERE id=$1 FOR UPDATE`, userID).Scan(&status); err != nil {
		return ErrAnswerPreparationNotFound
	}
	if status != "deleting" && status != "deleted" {
		return ErrAnswerPreparationConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ielts_answer_preparations WHERE owner_user_id=$1`, userID); err != nil {
		return answerDatabaseError(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ielts_answer_preparation_idempotency WHERE owner_user_id=$1`, userID); err != nil {
		return answerDatabaseError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return answerDatabaseError(err)
	}
	return nil
}

type answerQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getAnswerPreparation(ctx context.Context, query answerQueryer, owner, id string) (AnswerPreparation, error) {
	row := query.QueryRow(ctx, `SELECT `+answerPreparationColumns+` FROM ielts_answer_preparations WHERE owner_user_id=$1 AND answer_preparation_id=$2`, owner, id)
	return scanAnswerPreparation(row)
}

func getAnswerPreparationByQuestion(ctx context.Context, query answerQueryer, owner string, reference QuestionReference) (AnswerPreparation, error) {
	row := query.QueryRow(ctx, `SELECT `+answerPreparationColumns+` FROM ielts_answer_preparations WHERE owner_user_id=$1 AND bank_id=$2 AND part=$3 AND source_id=$4 AND question_position=$5`, owner, reference.BankID, reference.Part, reference.SourceID, reference.QuestionPosition)
	return scanAnswerPreparation(row)
}

func scanAnswerPreparation(row pgx.Row) (AnswerPreparation, error) {
	var value AnswerPreparation
	var points, outline, expressions []byte
	err := row.Scan(&value.ID, &value.Question.Reference.BankID, &value.Question.Reference.Part, &value.Question.Reference.SourceID, &value.Question.Reference.QuestionPosition, &value.Question.Prompt, &points, &value.TargetBand, &value.Status, &value.Answer, &outline, &expressions, &value.SpeechText, &value.FailureCode, &value.Version, &value.GenerationRevision, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnswerPreparation{}, ErrAnswerPreparationNotFound
	}
	if err != nil {
		return AnswerPreparation{}, answerDatabaseError(err)
	}
	if json.Unmarshal(points, &value.PersonalPoints) != nil || json.Unmarshal(outline, &value.Outline) != nil || json.Unmarshal(expressions, &value.UsefulExpressions) != nil {
		return AnswerPreparation{}, ErrAnswerPreparationRepository
	}
	return value, nil
}

func replayAnswerMutation(ctx context.Context, tx pgx.Tx, owner string, intent MutationIntent) (AnswerPreparation, bool, error) {
	var fingerprint []byte
	var response []byte
	var pending bool
	err := tx.QueryRow(ctx, `SELECT payload_fingerprint, response, pending FROM ielts_answer_preparation_idempotency WHERE owner_user_id=$1 AND method=$2 AND canonical_path=$3 AND idempotency_key=$4`, owner, intent.Method, intent.Path, intent.Key).Scan(&fingerprint, &response, &pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnswerPreparation{}, false, nil
	}
	if err != nil {
		return AnswerPreparation{}, false, answerDatabaseError(err)
	}
	if string(fingerprint) != string(intent.Fingerprint[:]) {
		return AnswerPreparation{}, false, ErrAnswerPreparationIdempotencyConflict
	}
	if pending {
		var resourceID string
		if err := tx.QueryRow(ctx, `SELECT resource_id FROM ielts_answer_preparation_idempotency WHERE owner_user_id=$1 AND method=$2 AND canonical_path=$3 AND idempotency_key=$4`, owner, intent.Method, intent.Path, intent.Key).Scan(&resourceID); err != nil {
			return AnswerPreparation{}, false, answerDatabaseError(err)
		}
		current, err := getAnswerPreparation(ctx, tx, owner, resourceID)
		return current, true, err
	}
	if len(response) == 0 {
		return AnswerPreparation{}, true, nil
	}
	var value AnswerPreparation
	if json.Unmarshal(response, &value) != nil {
		return AnswerPreparation{}, false, ErrAnswerPreparationRepository
	}
	return value, true, nil
}

func persistAnswerMutation(ctx context.Context, tx pgx.Tx, owner string, intent MutationIntent, resourceID string, value AnswerPreparation, pending bool) error {
	var response any
	if value.ID != "" {
		response = mustJSON(value)
	}
	_, err := tx.Exec(ctx, `INSERT INTO ielts_answer_preparation_idempotency (owner_user_id, method, canonical_path, idempotency_key, payload_fingerprint, resource_id, pending, response) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8)`, owner, intent.Method, intent.Path, intent.Key, intent.Fingerprint[:], resourceID, pending, response)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAnswerPreparationIdempotencyConflict
		}
		return answerDatabaseError(err)
	}
	return nil
}

func classifyAnswerMiss(ctx context.Context, tx pgx.Tx, owner, id string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ielts_answer_preparations WHERE owner_user_id=$1 AND answer_preparation_id=$2)`, owner, id).Scan(&exists); err != nil {
		return answerDatabaseError(err)
	}
	if exists {
		return ErrAnswerPreparationConflict
	}
	return ErrAnswerPreparationNotFound
}

func lockActiveAnswerOwner(ctx context.Context, tx pgx.Tx, owner string) error {
	var status string
	if err := tx.QueryRow(ctx, `SELECT account_status FROM identity_users WHERE id=$1 FOR SHARE`, owner).Scan(&status); err != nil {
		return ErrAnswerPreparationNotFound
	}
	if status != "active" {
		return ErrAnswerPreparationConflict
	}
	return nil
}

func validStoreActor(actor requestcontext.Actor) bool { return actor.Valid() }
func answerDatabaseError(error) error                 { return ErrAnswerPreparationRepository }
func isUniqueViolation(err error) bool                { return strings.Contains(err.Error(), "SQLSTATE 23505") }
func mustJSON(value any) []byte                       { encoded, _ := json.Marshal(value); return encoded }

var _ AnswerQuestionResolver = (*PostgresStore)(nil)
var _ AnswerPreparationRepository = (*PostgresStore)(nil)
