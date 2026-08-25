package iserelay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

func TestClientCreatesAndPollsEvaluation(t *testing.T) {
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			if err := request.ParseMultipartForm(1024); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			writeJSON(writer, http.StatusAccepted, processingResponse(testRequestID))
			return
		}
		polls.Add(1)
		result := resultResponse(testRequestID, testResult())
		writeJSON(writer, http.StatusOK, result)
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	result, err := client.Evaluate(t.Context(), validAssessment())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if polls.Load() != 1 || result.Provider != "xfyun_ise" || result.RawResult != "" {
		t.Fatalf("unexpected relay result: %#v, polls=%d", result, polls.Load())
	}
}

func testResult() speechfeedback.AcousticAssessmentResult {
	return speechfeedback.AcousticAssessmentResult{
		Provider:  "xfyun_ise",
		SessionID: "session-1",
	}
}

func TestClientReturnsStableRelayFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response := failureResponse(testRequestID, FailureProcessingTimeout, true)
		writeJSON(writer, http.StatusOK, response)
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Evaluate(t.Context(), validAssessment())
	failure, ok := err.(RelayError)
	if !ok || failure.StableCategory() != FailureProcessingTimeout || !failure.Retryable() {
		t.Fatalf("unexpected relay error: %#v", err)
	}
}

func TestClientRejectsMalformedSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]string{"status": StatusSucceeded})
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Evaluate(t.Context(), validAssessment()); err == nil {
		t.Fatal("malformed response must fail closed")
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("content-type", "application/json")
		_, _ = writer.Write(append(
			[]byte(`{"schema_version":"ise-relay/v1","request_id":"`+testRequestID+`","status":"PROCESSING"}`),
			bytes.Repeat([]byte(" "), 64*1024)...,
		))
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Evaluate(t.Context(), validAssessment()); err == nil {
		t.Fatal("oversized response must fail closed")
	}
}

func TestClientRejectsMismatchedResponseRequestID(t *testing.T) {
	otherRequestID := "f7023b34-0000-4000-8000-000000000099"
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "create response",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writeJSON(writer, http.StatusAccepted, processingResponse(otherRequestID))
			},
		},
		{
			name: "poll response",
			handler: func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost {
					writeJSON(writer, http.StatusAccepted, processingResponse(testRequestID))
					return
				}
				writeJSON(writer, http.StatusOK, resultResponse(otherRequestID, testResult()))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			if _, err := client.Evaluate(t.Context(), validAssessment()); err == nil {
				t.Fatal("mismatched request ID must fail closed")
			}
		})
	}
}

func TestClientTreatsMissingPolledJobAsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			writeJSON(writer, http.StatusAccepted, processingResponse(testRequestID))
			return
		}
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "NOT_FOUND"})
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Evaluate(t.Context(), validAssessment())
	failure, ok := err.(RelayError)
	if !ok || !failure.Retryable() {
		t.Fatalf("missing polled job must be retryable: %#v", err)
	}
}
