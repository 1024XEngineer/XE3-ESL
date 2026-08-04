package review

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestRetryRequestHTTPCreateReplayAndGet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	pending := retryRequestFixture(now)
	repository := &retryRequestRepositoryStub{request: pending}
	handler := newRetryRequestHTTPHandlerForTest(t, repository)

	created := performRetryRequestHTTP(
		t,
		handler,
		http.MethodPost,
		"/v1/feedback-items/"+pending.FeedbackItemID+"/retry-requests",
		nil,
		"retry-http-key",
		true,
	)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body)
	}
	if created.Header().Get("Location") != repository.request.StatusURL ||
		created.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("create headers = %#v", created.Header())
	}
	assertRetryRequestHTTPPayload(t, created, RetryRequestTurnCreated)

	repository.reserveReplay = true
	replayed := performRetryRequestHTTP(
		t,
		handler,
		http.MethodPost,
		"/v1/feedback-items/"+pending.FeedbackItemID+"/retry-requests",
		nil,
		"retry-http-key",
		true,
	)
	if replayed.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body = %s", replayed.Code, replayed.Body)
	}
	if replayed.Header().Get("Location") != "" {
		t.Fatalf("replay Location = %q", replayed.Header().Get("Location"))
	}
	assertRetryRequestHTTPPayload(t, replayed, RetryRequestTurnCreated)

	get := performRetryRequestHTTP(
		t,
		handler,
		http.MethodGet,
		repository.request.StatusURL,
		nil,
		"",
		true,
	)
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", get.Code, get.Body)
	}
	assertRetryRequestHTTPPayload(t, get, RetryRequestTurnCreated)
}

func TestRetryRequestHTTPRejectsInvalidAndUnauthenticatedRequests(
	t *testing.T,
) {
	t.Parallel()

	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	pending := retryRequestFixture(now)
	handler := newRetryRequestHTTPHandlerForTest(
		t,
		&retryRequestRepositoryStub{request: pending},
	)
	invalidBody := performRetryRequestHTTP(
		t,
		handler,
		http.MethodPost,
		"/v1/feedback-items/"+pending.FeedbackItemID+"/retry-requests",
		[]byte(`{}`),
		"retry-http-key",
		true,
	)
	if invalidBody.Code != http.StatusBadRequest {
		t.Fatalf(
			"non-empty body status = %d, body = %s",
			invalidBody.Code,
			invalidBody.Body,
		)
	}
	unauthenticated := performRetryRequestHTTP(
		t,
		handler,
		http.MethodGet,
		pending.StatusURL,
		nil,
		"",
		false,
	)
	if unauthenticated.Code != http.StatusUnauthorized ||
		unauthenticated.Header().Get("WWW-Authenticate") != "Bearer" ||
		unauthenticated.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf(
			"unauthenticated response = %d %#v %s",
			unauthenticated.Code,
			unauthenticated.Header(),
			unauthenticated.Body,
		)
	}
}

func newRetryRequestHTTPHandlerForTest(
	t *testing.T,
	repository RetryRequestRepository,
) *RetryRequestHTTPHandler {
	t.Helper()
	service, err := NewRetryRequestService(
		repository,
		&repracticeSourceReaderStub{source: repracticeSourceFixture()},
		&retryPracticeStub{},
		&retryConversationStub{newTurnID: "turn_retry_001"},
	)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewRetryRequestHTTPHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func performRetryRequestHTTP(
	t *testing.T,
	handler *RetryRequestHTTPHandler,
	method string,
	path string,
	body []byte,
	idempotencyKey string,
	authenticated bool,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.RegisterRoutes(router)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if authenticated {
		request = request.WithContext(
			requestcontext.WithActor(
				request.Context(),
				retryRequestActor(),
			),
		)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertRetryRequestHTTPPayload(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status RetryRequestStatus,
) {
	t.Helper()
	var payload RepracticeRequest
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RetryStatus != status ||
		payload.NewTurnStatus != "ANSWERING" ||
		payload.AnswerPath != RetryTurnAnswerPath(payload.NewTurnID) ||
		!payload.valid() {
		t.Fatalf("invalid retry response: %#v", payload)
	}
}
