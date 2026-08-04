package reviewhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

var reviewHTTPTestCursorKey = []byte(
	"0123456789abcdef0123456789abcdef",
)

func TestHandlerListsAuthenticatedHistoryWithOpaqueCursor(t *testing.T) {
	newerCreatedAt := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	newer := completedReview(
		"20000000-0000-4000-8000-000000000002", newerCreatedAt, 91,
	)
	older := completedReview(
		"20000000-0000-4000-8000-000000000001",
		newerCreatedAt.Add(-time.Hour),
		78,
	)
	router := reviewHTTPTestRouter(t, historyStub{items: []review.FormalReview{
		newer, older,
	}})

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(
		unauthenticated,
		httptest.NewRequest(http.MethodGet, "/v1/formal-reviews", nil),
	)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated history status = %d", unauthenticated.Code)
	}

	firstResponse := reviewHTTPRequest(
		t, router, http.MethodGet, "/v1/formal-reviews?limit=1",
	)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf(
			"first history status = %d, body = %s",
			firstResponse.Code,
			firstResponse.Body,
		)
	}
	first := decodeReviewJSONObject(t, firstResponse)
	firstItems := first["items"].([]any)
	if len(firstItems) != 1 {
		t.Fatalf("first history items = %#v", firstItems)
	}
	firstItem := firstItems[0].(map[string]any)
	if firstItem["review_id"] != newer.ID ||
		firstItem["practice_session_id"] != newer.PracticeSessionID ||
		firstItem["status"] != string(review.FormalReviewCompleted) {
		t.Fatalf("first history item = %#v", firstItem)
	}
	if _, leaked := firstItem["owner_user_id"]; leaked {
		t.Fatalf("history DTO leaked owner: %#v", firstItem)
	}
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" || strings.Contains(cursor, newer.ID) {
		t.Fatalf("history cursor is not opaque: %#v", first["next_cursor"])
	}

	secondResponse := reviewHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/formal-reviews?limit=1&cursor="+cursor,
	)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf(
			"second history status = %d, body = %s",
			secondResponse.Code,
			secondResponse.Body,
		)
	}
	secondItems := decodeReviewJSONObject(t, secondResponse)["items"].([]any)
	if len(secondItems) != 1 ||
		secondItems[0].(map[string]any)["review_id"] != older.ID {
		t.Fatalf("second history items = %#v", secondItems)
	}

	for _, path := range []string{
		"/v1/formal-reviews?limit=51",
		"/v1/formal-reviews?limit=1&limit=2",
		"/v1/formal-reviews?cursor=not-base64",
		"/v1/formal-reviews?offset=1",
		"/v1/formal-reviews?limit=1;offset=1",
	} {
		response := reviewHTTPRequest(t, router, http.MethodGet, path)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid history %q status = %d", path, response.Code)
		}
	}
}

func TestHistoryCursorIsSignedCanonicalAndActorBound(t *testing.T) {
	handler := &Handler{cursorKey: reviewHTTPTestCursorKey}
	cursor := review.HistoryCursor{
		CreatedAt: time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC),
		ReviewID:  "20000000-0000-4000-8000-000000000002",
	}
	encoded, ok := handler.encodeCursor("actor-a", cursor)
	if !ok || strings.Count(encoded, ".") != 1 {
		t.Fatalf("encodeCursor() = %q, %t", encoded, ok)
	}
	decoded, ok := handler.decodeCursor("actor-a", encoded)
	if !ok || decoded != cursor {
		t.Fatalf("decodeCursor() = %+v, %t", decoded, ok)
	}
	if _, ok := handler.decodeCursor("actor-b", encoded); ok {
		t.Fatal("cursor was accepted for another actor")
	}
	otherHandler := &Handler{
		cursorKey: []byte("fedcba9876543210fedcba9876543210"),
	}
	if _, ok := otherHandler.decodeCursor("actor-a", encoded); ok {
		t.Fatal("cursor was accepted with another signing key")
	}
	if _, ok := handler.decodeCursor("actor-a", "A"+encoded[1:]); ok {
		t.Fatal("tampered cursor was accepted")
	}
	if _, ok := handler.decodeCursor("actor-a", encoded+"="); ok {
		t.Fatal("non-canonical padded cursor was accepted")
	}
}

func TestHistoryResponseBudget(t *testing.T) {
	t.Run("maximum page remains below the response cap", func(t *testing.T) {
		router := reviewHTTPTestRouter(
			t, historyStub{items: maximumHistoryPage()},
		)
		response := reviewHTTPRequest(
			t, router, http.MethodGet, "/v1/formal-reviews?limit=50",
		)
		if response.Code != http.StatusOK {
			t.Fatalf(
				"maximum history status = %d, body = %s",
				response.Code,
				response.Body,
			)
		}
		if response.Body.Len() >= maxHistoryBody {
			t.Fatalf(
				"maximum history bytes = %d, limit = %d",
				response.Body.Len(),
				maxHistoryBody,
			)
		}
		root := decodeReviewJSONObject(t, response)
		items := root["items"].([]any)
		if len(items) != 50 {
			t.Fatalf("maximum history item count = %d", len(items))
		}
		cursor, ok := root["next_cursor"].(string)
		if !ok || cursor == "" {
			t.Fatalf("maximum history omitted next_cursor: %#v", root)
		}
		continuation := reviewHTTPRequest(
			t,
			router,
			http.MethodGet,
			"/v1/formal-reviews?limit=50&cursor="+cursor,
		)
		if continuation.Code != http.StatusOK {
			t.Fatalf(
				"continuation status = %d, body = %s",
				continuation.Code,
				continuation.Body,
			)
		}
		continuationRoot := decodeReviewJSONObject(t, continuation)
		if len(continuationRoot["items"].([]any)) != 1 {
			t.Fatalf("continuation = %#v", continuationRoot)
		}
		if _, present := continuationRoot["next_cursor"]; present {
			t.Fatalf("continuation exposed cursor: %#v", continuationRoot)
		}
	})

	t.Run("oversized history returns only a safe error", func(t *testing.T) {
		item := completedReview(
			"20000000-0000-4000-8000-000000000001",
			time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC),
			80,
		)
		item.Result.Summary = strings.Repeat("<", maxHistoryBody)
		router := reviewHTTPTestRouter(t, fixedHistoryStub{
			page: review.HistoryPage{Items: []review.FormalReview{item}},
		})
		response := reviewHTTPRequest(
			t, router, http.MethodGet, "/v1/formal-reviews?limit=1",
		)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf(
				"oversized history status = %d, body bytes = %d",
				response.Code,
				response.Body.Len(),
			)
		}
		failure := decodeReviewJSONObject(t, response)["error"].(map[string]any)
		if failure["code"] != "internal_error" ||
			strings.Contains(response.Body.String(), `"items"`) ||
			strings.Contains(response.Body.String(), `"review_id"`) {
			t.Fatalf("unsafe oversized response = %s", response.Body)
		}
	})

	t.Run("hard cap rejects encoded product bytes", func(t *testing.T) {
		gin.SetMode(gin.ReleaseMode)
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		handler := &Handler{errors: httpresponse.NewRenderer(
			func() string { return "corr_review_cap" },
		)}
		handler.writeBoundedJSON(context, map[string]any{
			"items": []string{strings.Repeat("x", maxHistoryBody)},
		})
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf(
				"hard-cap status = %d, body bytes = %d",
				recorder.Code,
				recorder.Body.Len(),
			)
		}
		if strings.Contains(recorder.Body.String(), strings.Repeat("x", 64)) ||
			strings.Contains(recorder.Body.String(), `"items"`) {
			t.Fatalf("hard-cap response leaked product bytes: %s", recorder.Body)
		}
	})
}

func completedReview(
	id string,
	createdAt time.Time,
	score int,
) review.FormalReview {
	completedAt := createdAt.Add(time.Minute)
	return review.FormalReview{
		ID:                    id,
		OwnerUserID:           "private-owner",
		PracticeSessionID:     "session-" + id,
		Status:                review.FormalReviewCompleted,
		ImplementationVersion: "review-v1",
		SourceTurnID:          "turn-" + id,
		SourceTurnVersion:     "conversation-turn:evidence-v1",
		Result: &review.ReviewResult{
			SummaryEligibility:  review.SummaryEligible,
			OverallScore:        score,
			OverallScorePresent: true,
			Summary:             "Server-owned review history.",
			Conclusions: []review.ReviewConclusion{{
				Key:          "clarity",
				Category:     "clarity",
				Score:        score,
				ScorePresent: true,
				Message:      "Clear response.",
				Suggestion:   "Add one concrete outcome.",
			}},
		},
		CreatedAt:   createdAt,
		UpdatedAt:   completedAt,
		CompletedAt: &completedAt,
	}
}

func maximumHistoryPage() []review.FormalReview {
	items := make([]review.FormalReview, 51)
	baseCreatedAt := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	for index := range items {
		item := completedReview(
			fmt.Sprintf("20000000-0000-4000-8000-%012d", 51-index),
			baseCreatedAt.Add(-time.Duration(index)*time.Second),
			100,
		)
		item.Result.Summary = strings.Repeat("s", 2048)
		item.Result.Conclusions = make([]review.ReviewConclusion, 4)
		for conclusionIndex := range item.Result.Conclusions {
			item.Result.Conclusions[conclusionIndex] = review.ReviewConclusion{
				Key:          fmt.Sprintf("clarity-%d", conclusionIndex),
				Category:     "clarity",
				Score:        100,
				ScorePresent: true,
				Message:      strings.Repeat("m", 1800),
				Suggestion:   strings.Repeat("s", 300),
			}
		}
		items[index] = item
	}
	return items
}

type historyStub struct {
	items []review.FormalReview
}

func (stub historyStub) Get(
	_ context.Context,
	actor review.Actor,
	reviewID string,
) (review.FormalReview, error) {
	if actor.UserID != "user-a" {
		return review.FormalReview{}, review.ErrReviewNotFound
	}
	for _, item := range stub.items {
		if item.ID == reviewID {
			return item, nil
		}
	}
	return review.FormalReview{}, review.ErrReviewNotFound
}

func (stub historyStub) ListCompleted(
	_ context.Context,
	actor review.Actor,
	query review.HistoryQuery,
) (review.HistoryPage, error) {
	if actor.UserID != "user-a" {
		return review.HistoryPage{}, review.ErrReviewNotFound
	}
	eligible := make([]review.FormalReview, 0, len(stub.items))
	for _, item := range stub.items {
		if query.Before != nil &&
			!historyKeyBefore(
				item.CreatedAt,
				item.ID,
				query.Before.CreatedAt,
				query.Before.ReviewID,
			) {
			continue
		}
		eligible = append(eligible, item)
	}
	count := min(query.Limit, len(eligible))
	page := review.HistoryPage{Items: eligible[:count]}
	if len(eligible) > count && count > 0 {
		last := eligible[count-1]
		page.Next = &review.HistoryCursor{
			CreatedAt: last.CreatedAt,
			ReviewID:  last.ID,
		}
	}
	return page, nil
}

type fixedHistoryStub struct {
	page review.HistoryPage
}

func (stub fixedHistoryStub) Get(
	context.Context,
	review.Actor,
	string,
) (review.FormalReview, error) {
	return review.FormalReview{}, review.ErrReviewNotFound
}

func (stub fixedHistoryStub) ListCompleted(
	context.Context,
	review.Actor,
	review.HistoryQuery,
) (review.HistoryPage, error) {
	return stub.page, nil
}

func historyKeyBefore(
	createdAt time.Time,
	reviewID string,
	boundaryCreatedAt time.Time,
	boundaryReviewID string,
) bool {
	return createdAt.Before(boundaryCreatedAt) ||
		(createdAt.Equal(boundaryCreatedAt) && reviewID < boundaryReviewID)
}

func reviewHTTPTestRouter(t *testing.T, history History) *gin.Engine {
	t.Helper()
	handler, err := NewHandler(
		history,
		reviewHTTPTestCursorKey,
		httpresponse.NewRenderer(func() string { return "corr_review" }),
	)
	if err != nil {
		t.Fatalf("new review HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "Bearer review-token-a" {
			c.Request = c.Request.WithContext(requestcontext.WithActor(
				c.Request.Context(),
				requestcontext.Actor{UserID: "user-a", SessionID: "session-a"},
			))
		}
		c.Next()
	})
	handler.RegisterRoutes(router)
	return router
}

func reviewHTTPRequest(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(nil))
	request.Header.Set("Authorization", "Bearer review-token-a")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeReviewJSONObject(
	t *testing.T,
	response *httptest.ResponseRecorder,
) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	return result
}
