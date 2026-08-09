package ielts

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestAnswerPreparationHTTPRequiresActorAndIdempotencyKey(t *testing.T) {
	router := answerPreparationTestRouter(t, false)
	request := httptest.NewRequest(http.MethodPost, "/v1/ielts-speaking/answer-preparations", bytes.NewBufferString(`{"question":{"bank_id":"bank","part":"PART_1","source_id":"topic","question_position":1},"personal_points":[],"target_band":6.5}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", response.Code, response.Body.String())
	}

	router = answerPreparationTestRouter(t, true)
	request = httptest.NewRequest(http.MethodPost, "/v1/ielts-speaking/answer-preparations", bytes.NewBufferString(`{"question":{"bank_id":"bank","part":"PART_1","source_id":"topic","question_position":1},"personal_points":[],"target_band":6.5}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing key status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAnswerPreparationHTTPRejectsUnknownFields(t *testing.T) {
	router := answerPreparationTestRouter(t, true)
	request := httptest.NewRequest(http.MethodPost, "/v1/ielts-speaking/answer-preparations", bytes.NewBufferString(`{"question":{"bank_id":"bank","part":"PART_1","source_id":"topic","question_position":1},"personal_points":[],"target_band":6.5,"owner_user_id":"attacker"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-key-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown owner status=%d body=%s", response.Code, response.Body.String())
	}
}

func answerPreparationTestRouter(t *testing.T, authenticated bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
			c.Request = c.Request.WithContext(requestcontext.WithActor(c.Request.Context(), actor))
			c.Next()
		})
	}
	repository := &answerRepositoryStub{}
	service, err := NewAnswerPreparationService(repository, answerQuestionStub{}, &answerGeneratorStub{}, answerIDStub{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAnswerPreparationHTTPHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	handler.RegisterRoutes(router)
	return router
}
