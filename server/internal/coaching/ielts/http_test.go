package ielts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type questionBankReaderStub struct {
	bank QuestionBank
}

func (stub questionBankReaderStub) QuestionBank(context.Context) (QuestionBank, error) {
	return stub.bank, nil
}

func TestHTTPHandlerPublishesQuestionBankAtDedicatedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHTTPHandler(questionBankReaderStub{bank: QuestionBank{
		SchemaVersion: 4,
		BankID:        "ielts-test-bank",
		Season:        "2026-05-08",
		Part1Topics:   make([]Part1PracticeTopic, 38),
		TopicGroups:   make([]TopicGroup, 56),
	}}).RegisterRoutes(router)

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
	if len(bank.Part1Topics) != 38 || len(bank.TopicGroups) != 56 {
		t.Fatalf("published counts = %d/%d", len(bank.Part1Topics), len(bank.TopicGroups))
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
