package ielts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHTTPHandlerPublishesQuestionBankAtDedicatedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHTTPHandler(mustTestBank(t)).RegisterRoutes(router)

	first := serveRequest(router, "/v1/ielts-speaking/question-bank")
	second := serveRequest(router, "/v1/ielts-speaking/question-bank")
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
	}
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("repeated question-bank response changed")
	}
	var bank QuestionBank
	if err := json.Unmarshal(first.Body.Bytes(), &bank); err != nil {
		t.Fatalf("decode question bank: %v", err)
	}
	if len(bank.Part1Sets) != 38 || len(bank.TopicGroups) != 56 {
		t.Fatalf("published counts = %d/%d", len(bank.Part1Sets), len(bank.TopicGroups))
	}

	legacy := serveRequest(router, "/v1/scenes/ielts-speaking/question-bank")
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("legacy route status = %d", legacy.Code)
	}
}

func serveRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
