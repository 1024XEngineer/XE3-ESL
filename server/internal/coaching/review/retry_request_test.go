package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestRetryRequestServiceCreatesOneAuthorizedConversationDraft(
	t *testing.T,
) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	pending := retryRequestFixture(now)
	repository := &retryRequestRepositoryStub{request: pending}
	practice := &retryPracticeStub{}
	conversation := &retryConversationStub{
		newTurnID: "turn_retry_001",
	}
	service, err := NewRetryRequestService(
		repository,
		practice,
		conversation,
	)
	if err != nil {
		t.Fatal(err)
	}
	request, created, err := service.Request(
		context.Background(),
		retryRequestActor(),
		pending.FeedbackItemID,
		"retry-request-key",
	)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !created ||
		request.RetryStatus != RetryRequestTurnCreated ||
		request.NewTurnID != conversation.newTurnID ||
		practice.calls != 1 ||
		conversation.calls != 1 {
		t.Fatalf(
			"created=%v request=%#v practice=%d conversation=%d",
			created,
			request,
			practice.calls,
			conversation.calls,
		)
	}
	if practice.source != conversation.source ||
		practice.source.RetryRequestID != pending.RetryRequestID {
		t.Fatalf(
			"saga sources = %#v / %#v",
			practice.source,
			conversation.source,
		)
	}
}

func TestRetryRequestServicePersistsUnavailableSourceFailure(
	t *testing.T,
) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	pending := retryRequestFixture(now)
	repository := &retryRequestRepositoryStub{request: pending}
	practice := &retryPracticeStub{
		err: ErrRetryRequestSourceUnavailable,
	}
	conversation := &retryConversationStub{
		newTurnID: "turn_must_not_be_created",
	}
	service, err := NewRetryRequestService(
		repository,
		practice,
		conversation,
	)
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := service.Request(
		context.Background(),
		retryRequestActor(),
		pending.FeedbackItemID,
		"retry-request-key",
	)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if request.RetryStatus != RetryRequestFailed ||
		request.StableFailure == nil ||
		request.StableFailure.ReasonCode !=
			RetryRequestFailureSourceUnavailable ||
		request.StableFailure.Retryable ||
		conversation.calls != 0 {
		t.Fatalf("failed request = %#v", request)
	}
}

func retryRequestFixture(now time.Time) SpeechFeedbackRetryRequest {
	return SpeechFeedbackRetryRequest{
		RetryRequestID:    "92000000-0000-4000-8000-000000000001",
		FeedbackItemID:    "93000000-0000-4000-8000-000000000001",
		PracticeSessionID: "session_daily_001",
		OriginalTurnID:    "turn_daily_001",
		QuestionID:        "question_daily_001",
		RetryStatus:       RetryRequestPending,
		StatusURL: "/v1/retry-requests/" +
			"92000000-0000-4000-8000-000000000001",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func retryRequestActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "94000000-0000-4000-8000-000000000001",
		SessionID: "95000000-0000-4000-8000-000000000001",
	}
}

type retryRequestRepositoryStub struct {
	request       SpeechFeedbackRetryRequest
	reserveReplay bool
	reserveErr    error
	getErr        error
}

func (stub *retryRequestRepositoryStub) ReserveRetryRequest(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) (SpeechFeedbackRetryRequest, bool, error) {
	return stub.request, !stub.reserveReplay, stub.reserveErr
}

func (stub *retryRequestRepositoryStub) GetRetryRequest(
	_ context.Context,
	_ string,
	_ string,
) (SpeechFeedbackRetryRequest, error) {
	return stub.request, stub.getErr
}

func (stub *retryRequestRepositoryStub) CompleteRetryRequest(
	_ context.Context,
	_ string,
	_ string,
	newTurnID string,
) (SpeechFeedbackRetryRequest, error) {
	completed := stub.request
	completed.RetryStatus = RetryRequestTurnCreated
	completed.NewTurnID = newTurnID
	completed.NewTurnStatus = "ANSWERING"
	completed.AnswerPath = RetryTurnAnswerPath(newTurnID)
	completedAt := completed.UpdatedAt.Add(time.Second)
	completed.UpdatedAt = completedAt
	completed.CompletedAt = &completedAt
	stub.request = completed
	return completed, nil
}

func (stub *retryRequestRepositoryStub) FailRetryRequest(
	_ context.Context,
	_ string,
	_ string,
	failure RetryRequestStableFailure,
) (SpeechFeedbackRetryRequest, error) {
	failed := stub.request
	failed.RetryStatus = RetryRequestFailed
	failed.StableFailure = &failure
	completedAt := failed.UpdatedAt.Add(time.Second)
	failed.UpdatedAt = completedAt
	failed.CompletedAt = &completedAt
	stub.request = failed
	return failed, nil
}

type retryPracticeStub struct {
	source SameQuestionRetrySource
	calls  int
	err    error
}

func (stub *retryPracticeStub) AuthorizeSameQuestionRetry(
	_ context.Context,
	_ requestcontext.Actor,
	source SameQuestionRetrySource,
) error {
	stub.calls++
	stub.source = source
	return stub.err
}

type retryConversationStub struct {
	source    SameQuestionRetrySource
	newTurnID string
	calls     int
	err       error
}

func (stub *retryConversationStub) CreateSameQuestionRetryTurn(
	_ context.Context,
	_ requestcontext.Actor,
	source SameQuestionRetrySource,
) (string, error) {
	stub.calls++
	stub.source = source
	return stub.newTurnID, stub.err
}

var _ RetryRequestRepository = (*retryRequestRepositoryStub)(nil)
var _ SameQuestionRetryPracticePort = (*retryPracticeStub)(nil)
var _ SameQuestionRetryConversationPort = (*retryConversationStub)(nil)

func TestRetryRequestStableFailureRejectsMismatchedRetryability(
	t *testing.T,
) {
	if (RetryRequestStableFailure{
		ReasonCode: RetryRequestFailureSourceUnavailable,
		Retryable:  true,
	}).valid() {
		t.Fatal("source-unavailable failure accepted retryable=true")
	}
	if !errors.Is(
		ErrRetryRequestSourceUnavailable,
		ErrRetryRequestSourceUnavailable,
	) {
		t.Fatal("source unavailable sentinel lost errors.Is semantics")
	}
}
