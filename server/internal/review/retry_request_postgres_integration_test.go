package review_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

func TestPostgresRetryRequestAcceptsOnlyCompletedSameQuestionItem(
	t *testing.T,
) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000001"
		otherOwner  = "10000000-0000-4000-8000-000000000002"
		sessionID   = "practice-daily-retry"
		turnID      = "turn-daily-retry"
		candidateID = "candidate-daily-retry"
	)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, retryRequestIntegrationSchema); err != nil {
		t.Fatalf("create RetryRequest integration schema: %v", err)
	}
	insertConversationSpeechFeedbackFixture(
		t,
		pool,
		ownerID,
		sessionID,
		turnID,
		candidateID,
		true,
	)
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO identity_users (id) VALUES ($1)`,
		otherOwner,
	); err != nil {
		t.Fatal(err)
	}
	repository := review.NewPostgresRepository(pool)
	if _, err := repository.EnsureConfirmedConversationTurn(
		ctx,
		ownerID,
		sessionID,
		turnID,
	); err != nil {
		t.Fatal(err)
	}
	configuration := review.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Second,
		RetryDelay:      time.Second,
		StrategyRef:     review.SpeechFeedbackStrategyRef,
		PipelineVersion: review.SpeechFeedbackPipelineVersion,
		PromptVersion:   review.SpeechFeedbackPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
	claim, acquired, err := repository.ClaimSpeechFeedback(
		ctx,
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim SpeechFeedback = %#v, %t, %v", claim, acquired, err)
	}
	suggestion := "Could I have a quieter room, please?"
	completed, err := repository.CompleteSpeechFeedback(
		ctx,
		claim,
		[]review.SpeechFeedbackDraftItem{{
			Kind: review.SpeechFeedbackItemImprovement,
			Anchor: review.SpeechFeedbackAnchor{
				AnchorKind: review.
					SpeechFeedbackAnchorConversationTranscript,
				EvidenceRefID:   claim.EvidenceRefID,
				TurnID:          turnID,
				StartUTF8Byte:   0,
				EndUTF8Byte:     len(claim.CanonicalText),
				OriginalExcerpt: claim.CanonicalText,
			},
			Explanation:   "Add a polite closing.",
			SuggestedText: &suggestion,
			RepracticeMode: review.
				SpeechFeedbackRepracticeSameQuestion,
		}},
	)
	if err != nil {
		t.Fatalf("complete SpeechFeedback: %v", err)
	}
	itemID := completed.Items[0].FeedbackItemID
	request, created, err := repository.ReserveRetryRequest(
		ctx,
		ownerID,
		itemID,
		"retry-request-integration-key",
	)
	if err != nil || !created {
		t.Fatalf("reserve RetryRequest = %#v, %t, %v", request, created, err)
	}
	if request.RetryStatus != review.RetryRequestPending ||
		request.OriginalTurnID != turnID ||
		request.QuestionID != "question-1" ||
		request.PracticeSessionID != sessionID {
		t.Fatalf("reserved RetryRequest = %#v", request)
	}
	replayed, created, err := repository.ReserveRetryRequest(
		ctx,
		ownerID,
		itemID,
		"retry-request-integration-key",
	)
	if err != nil || created ||
		replayed.RetryRequestID != request.RetryRequestID {
		t.Fatalf("replayed RetryRequest = %#v, %t, %v", replayed, created, err)
	}
	completedRequest, err := repository.CompleteRetryRequest(
		ctx,
		ownerID,
		request.RetryRequestID,
		"turn_daily_retry_new",
	)
	if err != nil {
		t.Fatalf("complete RetryRequest: %v", err)
	}
	if completedRequest.RetryStatus != review.RetryRequestTurnCreated ||
		completedRequest.NewTurnStatus != "ANSWERING" ||
		completedRequest.AnswerPath !=
			"/v1/retry-turns/turn_daily_retry_new/transcription-candidates" {
		t.Fatalf("completed RetryRequest = %#v", completedRequest)
	}
	if _, err := repository.GetRetryRequest(
		ctx,
		otherOwner,
		request.RetryRequestID,
	); !errors.Is(err, review.ErrRetryRequestNotFound) {
		t.Fatalf("cross-owner RetryRequest read error = %v", err)
	}
}

const retryRequestIntegrationSchema = `
	ALTER TABLE conversation_confirmed_turns
		ADD COLUMN turn_kind text NOT NULL DEFAULT 'EFFECTIVE',
		ADD COLUMN counts_toward_effective_turn_limit boolean
			NOT NULL DEFAULT true;
	CREATE TABLE review_speech_feedback_retry_requests (
		retry_request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		owner_user_id uuid NOT NULL,
		feedback_item_id uuid NOT NULL,
		speech_feedback_id uuid NOT NULL,
		idempotency_key text NOT NULL,
		request_fingerprint bytea NOT NULL,
		deletion_generation bigint NOT NULL,
		practice_session_id text NOT NULL,
		original_turn_id text NOT NULL,
		question_id text NOT NULL,
		retry_status text NOT NULL DEFAULT 'PENDING',
		new_turn_id text,
		stable_failure_reason text,
		stable_failure_retryable boolean,
		created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
		updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
		completed_at timestamptz,
		UNIQUE (owner_user_id, idempotency_key)
	);
`
