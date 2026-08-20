package evaluationapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestEvaluationRoutesShareCanonicalResourceParameterNames(t *testing.T) {
	router := gin.New()
	noop := func(*gin.Context) {}
	router.GET("/v1/practice-sessions/:practice_session_id", noop)
	router.GET("/v1/agent-messages/:message_id/translation", noop)

	(&HTTPHandler{}).RegisterRoutes(router)
}

func TestRetrySessionEvaluationReturnsAcceptedThenReplaysCurrentResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		replayed bool
		status   int
	}{
		{name: "reset failed report", status: http.StatusAccepted},
		{name: "replay current report", replayed: true, status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &apiStoreStub{replayed: test.replayed}
			application, err := NewApplication(store)
			if err != nil {
				t.Fatal(err)
			}
			handler, err := NewHTTPHandler(application)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(requestcontext.WithActor(
					c.Request.Context(),
					requestcontext.Actor{UserID: testAPIUserID, SessionID: "session-token"},
				))
				c.Next()
			})
			handler.RegisterRoutes(router)
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/practice-sessions/"+testAPISessionID+"/evaluation/retry",
				nil,
			)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || store.retryCalls != 1 ||
				store.userID != testAPIUserID || store.sessionID != testAPISessionID {
				t.Fatalf(
					"status=%d body=%s store=%#v", response.Code,
					response.Body.String(), store,
				)
			}
			var payload map[string]any
			if json.Unmarshal(response.Body.Bytes(), &payload) != nil ||
				payload["status"] != string(evaluation.JobQueued) {
				t.Fatalf("payload=%s", response.Body.String())
			}
		})
	}
}

func TestRetrySessionEvaluationRejectsNonRetryableFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application, err := NewApplication(&apiStoreStub{err: evaluation.ErrRetryNotAllowed})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHTTPHandler(application)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(requestcontext.WithActor(
			c.Request.Context(),
			requestcontext.Actor{UserID: testAPIUserID, SessionID: "session-token"},
		))
		c.Next()
	})
	handler.RegisterRoutes(router)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/practice-sessions/"+testAPISessionID+"/evaluation/retry",
		nil,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), `"code":"resource_conflict"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPublicSpeechResultDoesNotExposeProviderLineage(t *testing.T) {
	t.Parallel()
	pronunciation := 91.0
	fluency := 82.0
	integrity := 88.0
	speed := 126.0
	result := evaluation.SpeechResult{
		SchemaVersion:      "speech-feedback/v1",
		ScoreabilityStatus: "PROVISIONAL",
		Summary:            "Feedback is ready.",
		ReasonCodes:        []string{},
		Acoustic: evaluation.AcousticCheckpoint{
			Status:           evaluation.AcousticAssessed,
			Pronunciation:    &pronunciation,
			Fluency:          &fluency,
			Integrity:        &integrity,
			SpeakingSpeedWPM: &speed,
			Provider:         "iflytek",
			ProviderSession:  "provider-session-1",
		},
	}
	encoded, _, err := evaluation.EncodeStrict(result)
	if err != nil {
		t.Fatalf("EncodeStrict: %v", err)
	}
	public, err := publicResult(evaluation.Record{
		Kind:   evaluation.KindPracticeTurnFeedback,
		Result: encoded,
	})
	if err != nil {
		t.Fatalf("publicResult: %v", err)
	}
	payload, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(payload), "provider") ||
		strings.Contains(string(payload), "provider-session-1") {
		t.Fatalf("public payload leaked provider lineage: %s", payload)
	}
	if !strings.Contains(string(payload), `"pronunciation":91`) {
		t.Fatalf("public payload lost product acoustic score: %s", payload)
	}
}

func TestDecodeCursorRejectsTrailingJSON(t *testing.T) {
	t.Parallel()
	boundary := report.HistoryBoundary{
		CreatedAt: time.Date(2026, 8, 15, 1, 2, 3, 4, time.UTC),
		ReportID:  "7311adb4-1ea0-41c7-8c6d-f336f854f1c6",
	}
	valid := encodeCursor(boundary)
	decoded, err := decodeCursor(valid)
	if err != nil || decoded != boundary {
		t.Fatalf("decodeCursor(valid) = %#v, %v", decoded, err)
	}
	trailing := base64.RawURLEncoding.EncodeToString([]byte(
		`{"created_at":"2026-08-15T01:02:03.000000004Z","report_id":"7311adb4-1ea0-41c7-8c6d-f336f854f1c6"}{}`,
	))
	if _, err := decodeCursor(trailing); err == nil {
		t.Fatal("decodeCursor accepted trailing JSON")
	}
}

type apiStoreStub struct {
	replayed   bool
	err        error
	retryCalls int
	userID     string
	sessionID  string
}

func (store *apiStoreStub) GetRecordBySource(
	context.Context, string, evaluation.Kind, string,
) (evaluation.Record, error) {
	return evaluation.Record{}, evaluation.ErrNotFound
}

func (store *apiStoreStub) RetryFailedSessionReport(
	_ context.Context, userID string, sessionID string,
) (evaluation.Record, bool, error) {
	store.retryCalls++
	store.userID = userID
	store.sessionID = sessionID
	if store.err != nil {
		return evaluation.Record{}, false, store.err
	}
	now := time.Date(2026, 8, 21, 1, 2, 3, 0, time.UTC)
	return evaluation.Record{
		ID: testAPIEvaluationID, UserID: userID,
		Kind: evaluation.KindSessionReport, SourceID: sessionID, ContextID: sessionID,
		Status: evaluation.JobQueued, CreatedAt: now, UpdatedAt: now,
	}, store.replayed, nil
}

func (*apiStoreStub) ListFeedbackItems(
	context.Context, string, string,
) ([]evaluation.FeedbackItem, error) {
	return []evaluation.FeedbackItem{}, nil
}

func (*apiStoreStub) GetFormalReport(
	context.Context, string, string,
) (report.StoredFormalReport, error) {
	return report.StoredFormalReport{}, evaluation.ErrNotFound
}

func (*apiStoreStub) ListFormalReports(
	context.Context, string, report.HistoryQuery,
) (report.HistoryPage, error) {
	return report.HistoryPage{}, evaluation.ErrNotFound
}

const (
	testAPIUserID       = "10000000-0000-4000-8000-000000000001"
	testAPISessionID    = "20000000-0000-4000-8000-000000000001"
	testAPIEvaluationID = "70000000-0000-4000-8000-000000000001"
)
