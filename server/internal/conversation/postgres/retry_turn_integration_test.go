package postgres

import (
	"context"
	"errors"
	"testing"

	domainconversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
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
		conversation.ConfirmTurnCommand{
			CandidateID:     originalCandidate.ID,
			EvidenceVersion: originalCandidate.EvidenceVersion,
			ConfirmedText:   originalCandidate.Text,
			IdempotencyKey:  "confirm-original-retry-turn",
		},
	)
	if err != nil {
		t.Fatalf("confirm original Turn: %v", err)
	}
	original, err = repository.SaveTurnProgress(
		context.Background(),
		actor,
		original.ID,
		conversation.TurnProgress{EffectiveTurns: 1},
	)
	if err != nil {
		t.Fatalf("save original Turn progress: %v", err)
	}
	if original.Kind != conversation.TurnKindEffective ||
		!original.CountsTowardTurnLimit {
		t.Fatalf("original Turn shape = %#v", original)
	}

	service, err := domainconversation.NewRetryTurnService(repository)
	if err != nil {
		t.Fatal(err)
	}
	command := domainconversation.CreateRetryTurnCommand{
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
		conversation.ConfirmTurnCommand{
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
		retry.Kind != conversation.TurnKindRetry ||
		retry.RetryRequestID != command.RetryRequestID ||
		retry.OriginalTurnID != original.ID ||
		retry.CountsTowardTurnLimit ||
		retry.Progress.EffectiveTurns != 0 ||
		retry.Progress.SessionCompleted {
		t.Fatalf("confirmed retry Turn = %#v", retry)
	}
	if _, err := repository.SaveTurnProgress(
		context.Background(),
		actor,
		retry.ID,
		conversation.TurnProgress{EffectiveTurns: 2},
	); !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("retry Turn progress error = %v", err)
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
	ALTER TABLE conversation_confirmed_turns
		DROP CONSTRAINT
			conversation_confirmed_turns_owner_user_id_practice_session_key;
	ALTER TABLE conversation_confirmed_turns
		ADD COLUMN turn_kind text NOT NULL DEFAULT 'EFFECTIVE',
		ADD COLUMN retry_request_id uuid,
		ADD COLUMN original_turn_id text,
		ADD COLUMN counts_toward_effective_turn_limit boolean
			NOT NULL DEFAULT true;
	CREATE UNIQUE INDEX conversation_effective_turn_question_unique
		ON conversation_confirmed_turns (
			owner_user_id,
			practice_session_id,
			question_id
		)
		WHERE turn_kind = 'EFFECTIVE';
	CREATE UNIQUE INDEX conversation_retry_turn_request_unique
		ON conversation_confirmed_turns (owner_user_id, retry_request_id)
		WHERE turn_kind = 'RETRY';
	CREATE TABLE conversation_retry_turn_drafts (
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
			REFERENCES conversation_confirmed_turns (
				owner_user_id,
				turn_id
			) ON DELETE CASCADE,
		FOREIGN KEY (owner_user_id, practice_session_id, question_id)
			REFERENCES conversation_questions (
				owner_user_id,
				practice_session_id,
				question_id
			) ON DELETE CASCADE,
		FOREIGN KEY (owner_user_id, candidate_id)
			REFERENCES conversation_transcript_candidates (
				owner_user_id,
				candidate_id
			) ON DELETE CASCADE,
		CHECK (status IN ('ANSWERING', 'CONFIRMED'))
	);
`
