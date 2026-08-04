package postgres

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestRetryTurnKeepsDraftIdentityAndDoesNotEnterEffectiveHistory(
	t *testing.T,
) {
	repository, _ := newIntegrationRepository(t)
	actor := testActor(testUserA)
	requestActor := requestcontext.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
	question := saveTestQuestion(
		t,
		repository,
		actor,
		"question-retry-turn",
		"session-retry-turn",
	)
	originalReservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-original-retry-turn",
	)
	originalCandidate := completeTestTranscription(
		t,
		repository,
		originalReservation,
		"transcript-original-retry-turn",
	)
	original, err := repository.ConfirmTurn(
		context.Background(),
		actor,
		practiceinput.ConfirmTurnCommand{
			CandidateID:     originalCandidate.ID,
			EvidenceVersion: originalCandidate.EvidenceVersion,
			ConfirmedText:   originalCandidate.Text,
			IdempotencyKey:  "confirm-original-retry-turn",
		},
	)
	if err != nil {
		t.Fatalf("confirm original Turn: %v", err)
	}
	if original.Kind != practice.TurnKindEffective ||
		!original.CountsTowardTurnLimit {
		t.Fatalf("original Turn shape = %#v", original)
	}

	service, err := practiceinput.NewRetryTurnService(repository)
	if err != nil {
		t.Fatal(err)
	}
	command := practiceinput.CreateRetryTurnCommand{
		RetryRequestID:    "90000000-0000-4000-8000-000000000001",
		PracticeSessionID: question.SessionID,
		OriginalTurnID:    original.ID,
		QuestionID:        question.ID,
	}
	draft, err := service.Create(
		context.Background(),
		requestActor,
		command,
	)
	if err != nil {
		t.Fatalf("create retry Turn: %v", err)
	}
	replayedDraft, err := service.Create(
		context.Background(),
		requestActor,
		command,
	)
	if err != nil || replayedDraft.TurnID != draft.TurnID {
		t.Fatalf("replay retry Turn = %#v, %v", replayedDraft, err)
	}

	retryReservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-retry-turn",
	)
	retryCandidate := completeTestTranscription(
		t,
		repository,
		retryReservation,
		"transcript-retry-turn",
	)
	retry, err := repository.ConfirmTurn(
		context.Background(),
		actor,
		practiceinput.ConfirmTurnCommand{
			CandidateID:     retryCandidate.ID,
			EvidenceVersion: retryCandidate.EvidenceVersion,
			ConfirmedText:   retryCandidate.Text,
			IdempotencyKey:  "confirm-retry-turn",
			RetryTurnID:     draft.TurnID,
		},
	)
	if err != nil {
		t.Fatalf("confirm retry Turn: %v", err)
	}
	if retry.ID != draft.TurnID ||
		retry.Kind != practice.TurnKindRetry ||
		retry.RetryRequestID != command.RetryRequestID ||
		retry.OriginalTurnID != original.ID ||
		retry.CountsTowardTurnLimit ||
		retry.EffectiveTurns != original.EffectiveTurns ||
		retry.SessionCompleted {
		t.Fatalf("confirmed retry Turn = %#v", retry)
	}
	history, err := repository.ListSessionTurns(
		context.Background(),
		actor,
		question.SessionID,
	)
	if err != nil {
		t.Fatalf("list ordinary Turn history: %v", err)
	}
	if len(history) != 1 || history[0].ID != original.ID {
		t.Fatalf("ordinary history = %#v", history)
	}
}

const conversationRetryIntegrationSchema = `
	DROP TABLE conversation_deletion_fences;
	CREATE TABLE practice_deletion_fences (
		owner_user_id uuid PRIMARY KEY,
		deletion_generation bigint NOT NULL CHECK (deletion_generation > 0),
		created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
		updated_at timestamptz NOT NULL DEFAULT transaction_timestamp()
	);
	CREATE TABLE practice_idempotency_records (
		owner_user_id uuid NOT NULL
	);
	ALTER TABLE conversation_questions
		RENAME TO practice_questions;
	ALTER TABLE conversation_transcription_reservations
		RENAME TO practice_transcription_reservations;
	ALTER TABLE conversation_processing_attempts
		RENAME TO practice_processing_attempts;
	ALTER TABLE conversation_transcript_candidates
		RENAME TO practice_transcript_candidates;
	ALTER TABLE conversation_confirmed_turns
		RENAME TO practice_turns;
	ALTER TABLE conversation_turn_confirmations
		RENAME TO practice_turn_confirmations;
	DROP INDEX conversation_turns_review_owner_idx;
	ALTER TABLE practice_turns
		DROP COLUMN review_id,
		DROP COLUMN review_source_turn_id,
		DROP COLUMN review_recorded_at;

	CREATE TABLE practice_sessions (
		owner_user_id uuid NOT NULL,
		session_id text NOT NULL,
		status text NOT NULL,
		version integer NOT NULL,
		effective_turns integer NOT NULL DEFAULT 0,
		snapshot_id text NOT NULL,
		started_at timestamptz,
		completed_at timestamptz,
		end_reason text,
		created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
		updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
		PRIMARY KEY (owner_user_id, session_id)
	);
	CREATE TABLE practice_session_snapshots (
		owner_user_id uuid NOT NULL,
		session_id text NOT NULL,
		snapshot_id text NOT NULL,
		turn_limit integer NOT NULL,
		snapshot_document jsonb NOT NULL,
		PRIMARY KEY (owner_user_id, session_id)
	);
	CREATE TABLE practice_turn_results (
		owner_user_id uuid NOT NULL,
		session_id text NOT NULL,
		turn_id text NOT NULL,
		payload_fingerprint bytea NOT NULL,
		round_number integer NOT NULL,
		effective_turns integer NOT NULL,
		session_version integer NOT NULL,
		completed boolean NOT NULL,
		completion_token text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
		PRIMARY KEY (owner_user_id, session_id, turn_id),
		CONSTRAINT practice_turn_results_owner_turn_key
			UNIQUE (owner_user_id, turn_id),
		UNIQUE (owner_user_id, session_id, round_number)
	);
	CREATE TABLE practice_completed (
		owner_user_id uuid NOT NULL,
		session_id text NOT NULL,
		final_turn_id text NOT NULL,
		session_version integer NOT NULL,
		completion_token text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
		PRIMARY KEY (owner_user_id, session_id),
		UNIQUE (owner_user_id, final_turn_id),
		UNIQUE (completion_token)
	);

	ALTER TABLE practice_turns
		DROP CONSTRAINT
			conversation_confirmed_turns_owner_user_id_practice_session_key;
	ALTER TABLE practice_turns
		ADD COLUMN turn_kind text NOT NULL DEFAULT 'EFFECTIVE',
		ADD COLUMN retry_request_id uuid,
		ADD COLUMN original_turn_id text,
		ADD COLUMN counts_toward_effective_turn_limit boolean
			NOT NULL DEFAULT true;
	CREATE UNIQUE INDEX conversation_effective_turn_question_unique
		ON practice_turns (
			owner_user_id,
			practice_session_id,
			question_id
		)
		WHERE turn_kind = 'EFFECTIVE';
	CREATE UNIQUE INDEX conversation_retry_turn_request_unique
		ON practice_turns (owner_user_id, retry_request_id)
		WHERE turn_kind = 'RETRY';
	CREATE TABLE practice_retry_turn_drafts (
		owner_user_id uuid NOT NULL,
		retry_request_id uuid NOT NULL,
		turn_id text NOT NULL,
		practice_session_id text NOT NULL,
		original_turn_id text NOT NULL,
		question_id text NOT NULL,
		status text NOT NULL DEFAULT 'ANSWERING',
		candidate_id text,
		created_at timestamptz NOT NULL,
		updated_at timestamptz NOT NULL,
		confirmed_at timestamptz,
		PRIMARY KEY (owner_user_id, turn_id),
		UNIQUE (owner_user_id, retry_request_id),
		FOREIGN KEY (owner_user_id, original_turn_id)
			REFERENCES practice_turns (
				owner_user_id,
				turn_id
			) ON DELETE CASCADE,
		FOREIGN KEY (owner_user_id, practice_session_id, question_id)
			REFERENCES practice_questions (
				owner_user_id,
				practice_session_id,
				question_id
			) ON DELETE CASCADE,
		FOREIGN KEY (owner_user_id, candidate_id)
			REFERENCES practice_transcript_candidates (
				owner_user_id,
				candidate_id
			) ON DELETE CASCADE,
		CHECK (status IN ('ANSWERING', 'CONFIRMED'))
	);
`
