package reviewhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

var reviewHTTPTestCursorKey = []byte(
	"review-history-test-signing-key-32-bytes-minimum",
)

const reviewHTTPTestOwner = "10000000-0000-4000-8000-000000000001"

func TestHandlerListsCanonicalEvaluationReportsWithOpaqueCursor(t *testing.T) {
	t.Parallel()
	newest := testReviewReport(
		"20000000-0000-4000-8000-000000000002",
		time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
	)
	older := testReviewReport(
		"30000000-0000-4000-8000-000000000003",
		newest.CreatedAt.Add(-time.Hour),
	)
	router := reviewHTTPTestRouter(t, historyStub{items: []review.Report{
		newest,
		older,
	}})

	first := reviewHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/evaluation-reports?limit=1",
		true,
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	payload := decodeReviewJSONObject(t, first)
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v", payload["items"])
	}
	item := items[0].(map[string]any)
	if item["report_id"] != newest.ID ||
		item["scene_type"] != newest.SceneType ||
		item["scoreability_status"] != newest.ScoreabilityStatus ||
		item["result"] != nil || item["implementation_version"] != nil {
		t.Fatalf("canonical report = %#v", item)
	}
	cursor, ok := payload["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("next cursor = %#v", payload["next_cursor"])
	}

	second := reviewHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/evaluation-reports?limit=1&cursor="+cursor,
		true,
	)
	secondPayload := decodeReviewJSONObject(t, second)
	secondItems := secondPayload["items"].([]any)
	if secondItems[0].(map[string]any)["report_id"] != older.ID {
		t.Fatalf("second page = %#v", secondPayload)
	}
}

func TestHandlerGetsCanonicalEvaluationReport(t *testing.T) {
	t.Parallel()
	report := testReviewReport(
		"20000000-0000-4000-8000-000000000002",
		time.Now().UTC(),
	)
	router := reviewHTTPTestRouter(t, historyStub{items: []review.Report{report}})
	response := reviewHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/evaluation-reports/"+report.ID,
		true,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	payload := decodeReviewJSONObject(t, response)
	if payload["report_id"] != report.ID || payload["detail_schema"] == nil ||
		payload["dimensions"] == nil {
		t.Fatalf("report response = %#v", payload)
	}
}

func TestHandlerRejectsMissingAuthenticationAndInvalidCursor(t *testing.T) {
	t.Parallel()
	router := reviewHTTPTestRouter(t, historyStub{})
	unauthenticated := reviewHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/evaluation-reports",
		false,
	)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	invalidCursor := reviewHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/evaluation-reports?cursor=invalid",
		true,
	)
	if invalidCursor.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", invalidCursor.Code)
	}
}

type historyStub struct{ items []review.Report }

func (stub historyStub) Get(
	_ context.Context,
	actor review.Actor,
	reportID string,
) (review.Report, error) {
	if actor.UserID != reviewHTTPTestOwner {
		return review.Report{}, review.ErrReviewNotFound
	}
	for _, item := range stub.items {
		if item.ID == reportID {
			return item, nil
		}
	}
	return review.Report{}, review.ErrReviewNotFound
}

func (stub historyStub) ListCompleted(
	_ context.Context,
	actor review.Actor,
	query review.HistoryQuery,
) (review.HistoryPage, error) {
	if actor.UserID != reviewHTTPTestOwner {
		return review.HistoryPage{}, review.ErrReviewNotFound
	}
	eligible := make([]review.Report, 0, len(stub.items))
	for _, item := range stub.items {
		if query.Before != nil &&
			!(item.CreatedAt.Before(query.Before.CreatedAt) ||
				(item.CreatedAt.Equal(query.Before.CreatedAt) &&
					item.ID < query.Before.ReportID)) {
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
			ReportID:  last.ID,
		}
	}
	return page, nil
}

func testReviewReport(id string, createdAt time.Time) review.Report {
	score := 78.0
	return review.Report{
		ID:                   id,
		EvaluationID:         "40000000-0000-4000-8000-000000000004",
		EvaluationRevisionID: "50000000-0000-4000-8000-000000000005",
		OwnerUserID:          reviewHTTPTestOwner,
		PracticeSessionID:    "practice-session-1",
		Revision:             1,
		SchemaVersion:        "evaluation-report/v1",
		SceneType:            "INTERVIEW",
		SceneModel:           "project_interview",
		ScoreabilityStatus:   "PROVISIONAL",
		Summary:              "练习报告摘要",
		Dimensions: []review.ReportDimension{{
			Key:          "relevance",
			Score:        &score,
			Scale:        "PERCENTAGE_100",
			Coverage:     1,
			Confidence:   0.8,
			ReasonCodes:  []string{},
			EvidenceRefs: []string{"evidence:1"},
			Strengths:    []review.ReportFinding{},
			Improvements: []review.ReportFinding{},
			Examples:     []review.ReportFinding{},
		}},
		PriorityActions: []review.ReportPriorityAction{},
		DetailSchema:    "interview-report/v1",
		Detail:          json.RawMessage(`{"schema_version":"interview-report/v1"}`),
		CreatedAt:       createdAt.UTC(),
	}
}

func reviewHTTPTestRouter(t *testing.T, history History) *gin.Engine {
	t.Helper()
	handler, err := NewHandler(
		history,
		reviewHTTPTestCursorKey,
		httpresponse.NewRenderer(func() string { return "corr_review" }),
	)
	if err != nil {
		t.Fatalf("new Review HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "Bearer review-token-a" {
			c.Request = c.Request.WithContext(requestcontext.WithActor(
				c.Request.Context(),
				requestcontext.Actor{
					UserID:    reviewHTTPTestOwner,
					SessionID: "session-a",
				},
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
	authenticated bool,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(nil))
	if authenticated {
		request.Header.Set("Authorization", "Bearer review-token-a")
	}
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
