package review

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestRetryResponseProjectsOnlyPublicTurnFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	createdAt := time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC)
	service := retryCreatorStub{turn: practice.Turn{
		ID:                      "11111111-1111-4111-8111-111111111111",
		SessionID:               "22222222-2222-4222-8222-222222222222",
		QuestionID:              "33333333-3333-4333-8333-333333333333",
		OriginalTurnID:          "44444444-4444-4444-8444-444444444444",
		RespondentParticipantID: "55555555-5555-4555-8555-555555555555",
		ClientRequestID:         "internal-request-id",
		Sequence:                4,
		Status:                  "answering",
		CreatedAt:               createdAt,
	}}
	handler, err := NewRetryHTTPHandler(service)
	if err != nil {
		t.Fatalf("NewRetryHTTPHandler: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := requestcontext.WithActor(c.Request.Context(), requestcontext.Actor{
			UserID:    "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			SessionID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	handler.RegisterRoutes(router)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/evaluation-feedback-items/66666666-6666-4666-8666-666666666666/retry-turns",
		nil,
	)
	request.Header.Set("Idempotency-Key", "client-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	turn := payload["turn"].(map[string]any)
	for _, internal := range []string{
		"respondent_participant_id",
		"client_request_id",
		"candidate_id",
		"transcript_id",
	} {
		if _, exists := turn[internal]; exists {
			t.Fatalf("response leaked %s: %s", internal, response.Body.String())
		}
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want absent", location)
	}
	if strings.TrimSpace(turn["turn_id"].(string)) == "" {
		t.Fatal("turn_id missing")
	}
}

type retryCreatorStub struct {
	turn practice.Turn
}

func (stub retryCreatorStub) CreateTurn(
	context.Context,
	string,
	string,
	string,
) (practice.Turn, bool, error) {
	return stub.turn, false, nil
}
