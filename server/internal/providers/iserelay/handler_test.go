package iserelay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

const testRequestID = "f7023b34-0000-4000-8000-000000000001"

type evaluatorStub struct {
	mu            sync.Mutex
	calls         int
	result        speechfeedback.AcousticAssessmentResult
	err           error
	wait          <-chan struct{}
	ignoreContext bool
}

func (stub *evaluatorStub) Evaluate(
	ctx context.Context,
	_ speechfeedback.AcousticAssessmentRequest,
) (speechfeedback.AcousticAssessmentResult, error) {
	stub.mu.Lock()
	stub.calls++
	stub.mu.Unlock()
	if stub.wait != nil {
		if stub.ignoreContext {
			<-stub.wait
			return stub.result, stub.err
		}
		select {
		case <-ctx.Done():
			return speechfeedback.AcousticAssessmentResult{}, ctx.Err()
		case <-stub.wait:
		}
	}
	return stub.result, stub.err
}

func (stub *evaluatorStub) callCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.calls
}

func TestHandlerCreatesPollsAndDeduplicatesEvaluation(t *testing.T) {
	accuracy := 91.5
	stub := &evaluatorStub{result: speechfeedback.AcousticAssessmentResult{
		Provider:  "xfyun_ise",
		SessionID: "session-1",
		Summary: speechfeedback.AcousticAssessmentSummary{
			AccuracyScore: &accuracy,
		},
	}}
	handler := newTestHandler(t, stub, time.Second, 8, 2)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	first := postAssessment(t, server.URL, validAssessment())
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("create status = %d, want 202", first.StatusCode)
	}
	first.Body.Close()

	var completed StatusResponse
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		response, err := http.Get(server.URL + "/v1/evaluations/" + testRequestID)
		if err != nil {
			t.Fatalf("poll evaluation: %v", err)
		}
		if err := json.NewDecoder(response.Body).Decode(&completed); err != nil {
			response.Body.Close()
			t.Fatalf("decode poll response: %v", err)
		}
		response.Body.Close()
		if completed.Status == StatusSucceeded {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if completed.Status != StatusSucceeded || completed.Result == nil ||
		completed.Result.Summary.AccuracyScore == nil ||
		*completed.Result.Summary.AccuracyScore != accuracy {
		t.Fatalf("unexpected completed response: %#v", completed)
	}

	duplicate := postAssessment(t, server.URL, validAssessment())
	defer duplicate.Body.Close()
	if duplicate.StatusCode != http.StatusOK || stub.callCount() != 1 {
		t.Fatalf("duplicate status/calls = %d/%d, want 200/1", duplicate.StatusCode, stub.callCount())
	}
}

func TestHandlerRejectsIdempotencyConflictAndCapacityOverflow(t *testing.T) {
	wait := make(chan struct{})
	stub := &evaluatorStub{wait: wait, ignoreContext: true}
	handler := newTestHandler(t, stub, time.Second, 1, 1)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	created := postAssessment(t, server.URL, validAssessment())
	created.Body.Close()
	conflictAssessment := validAssessment()
	conflictAssessment.ReferenceText = "Different reference"
	conflict := postAssessment(t, server.URL, conflictAssessment)
	conflict.Body.Close()
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", conflict.StatusCode)
	}

	overflowAssessment := validAssessment()
	overflowAssessment.RequestID = "f7023b34-0000-4000-8000-000000000002"
	overflow := postAssessment(t, server.URL, overflowAssessment)
	defer overflow.Body.Close()
	if overflow.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("overflow status = %d, want 503", overflow.StatusCode)
	}
	close(wait)
}

func TestHandlerRejectsInvalidCategoryAndOversizedAudio(t *testing.T) {
	stub := &evaluatorStub{}
	handler := newTestHandler(t, stub, time.Second, 2, 1)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	invalidCategory := validAssessment()
	invalidCategory.Category = "unsupported"
	response := postAssessment(t, server.URL, invalidCategory)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid category status = %d, want 400", response.StatusCode)
	}

	oversized := validAssessment()
	oversized.Audio = make([]byte, maxAudioBytes+2)
	response = postAssessment(t, server.URL, oversized)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || stub.callCount() != 0 {
		t.Fatalf("oversized status/calls = %d/%d, want 400/0", response.StatusCode, stub.callCount())
	}
}

func TestHandlerTreatsNewAttemptIDAsNewProviderCall(t *testing.T) {
	stub := &evaluatorStub{result: testResult()}
	handler := newTestHandler(t, stub, time.Second, 2, 1)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	for index, requestID := range []string{
		testRequestID,
		"f7023b34-0000-4000-8000-000000000002",
	} {
		assessment := validAssessment()
		assessment.RequestID = requestID
		response := postAssessment(t, server.URL, assessment)
		response.Body.Close()
		for deadline := time.Now().Add(time.Second); stub.callCount() != index+1; {
			if time.Now().After(deadline) {
				t.Fatalf("provider calls = %d, want %d", stub.callCount(), index+1)
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func TestHandlerMapsProviderDeadlineToStableFailure(t *testing.T) {
	wait := make(chan struct{})
	stub := &evaluatorStub{wait: wait}
	handler := newTestHandler(t, stub, 10*time.Millisecond, 2, 1)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()
	response := postAssessment(t, server.URL, validAssessment())
	response.Body.Close()

	var completed StatusResponse
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		polled, err := http.Get(server.URL + "/v1/evaluations/" + testRequestID)
		if err != nil {
			t.Fatalf("poll evaluation: %v", err)
		}
		_ = json.NewDecoder(polled.Body).Decode(&completed)
		polled.Body.Close()
		if completed.Status == StatusFailed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if completed.Failure == nil || completed.Failure.Code != FailureProcessingTimeout {
		t.Fatalf("unexpected timeout response: %#v", completed)
	}
}

func TestHandlerMapsInvalidProviderResultToFailure(t *testing.T) {
	stub := &evaluatorStub{result: speechfeedback.AcousticAssessmentResult{
		Provider:  "xfyun_ise",
		SessionID: "",
	}}
	handler := newTestHandler(t, stub, time.Second, 2, 1)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()
	response := postAssessment(t, server.URL, validAssessment())
	response.Body.Close()

	var completed StatusResponse
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		polled, err := http.Get(server.URL + "/v1/evaluations/" + testRequestID)
		if err != nil {
			t.Fatalf("poll evaluation: %v", err)
		}
		_ = json.NewDecoder(polled.Body).Decode(&completed)
		polled.Body.Close()
		if completed.Status == StatusFailed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if completed.Failure == nil || completed.Failure.Code != FailureProviderUnavailable {
		t.Fatalf("unexpected invalid-result response: %#v", completed)
	}
}

func TestHandlerBoundsQueueWaitAndDoesNotInvokeProviderAfterDeadline(t *testing.T) {
	wait := make(chan struct{})
	stub := &evaluatorStub{wait: wait, ignoreContext: true}
	handler := newTestHandler(t, stub, 10*time.Millisecond, 2, 1)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	first := postAssessment(t, server.URL, validAssessment())
	first.Body.Close()
	for deadline := time.Now().Add(time.Second); stub.callCount() != 1; {
		if time.Now().After(deadline) {
			t.Fatal("first evaluation did not acquire the provider slot")
		}
		time.Sleep(time.Millisecond)
	}
	secondAssessment := validAssessment()
	secondAssessment.RequestID = "f7023b34-0000-4000-8000-000000000002"
	second := postAssessment(t, server.URL, secondAssessment)
	second.Body.Close()
	time.Sleep(30 * time.Millisecond)

	response, err := http.Get(server.URL + "/v1/evaluations/" + secondAssessment.RequestID)
	if err != nil {
		t.Fatalf("poll queued evaluation: %v", err)
	}
	defer response.Body.Close()
	var completed StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&completed); err != nil {
		t.Fatalf("decode queued evaluation: %v", err)
	}
	if completed.Failure == nil || completed.Failure.Code != FailureProcessingTimeout ||
		stub.callCount() != 1 {
		t.Fatalf("unexpected queued timeout: %#v, calls=%d", completed, stub.callCount())
	}
	close(wait)
}

func newTestHandler(
	t *testing.T,
	evaluator acousticEvaluator,
	timeout time.Duration,
	maxJobs int,
	maxInFlight int,
) *Handler {
	t.Helper()
	handler, err := NewHandler(evaluator, HandlerConfig{
		ProviderTimeout: timeout,
		Retention:       time.Minute,
		MaxJobs:         maxJobs,
		MaxInFlight:     maxInFlight,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return handler
}

func validAssessment() speechfeedback.AcousticAssessmentRequest {
	return speechfeedback.AcousticAssessmentRequest{
		RequestID:     testRequestID,
		Audio:         []byte{0, 1, 2, 3},
		ReferenceText: "Hello world",
		Category:      speechfeedback.AcousticCategoryReadSentence,
	}
}

func postAssessment(
	t *testing.T,
	baseURL string,
	assessment speechfeedback.AcousticAssessmentRequest,
) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"request_id":     assessment.RequestID,
		"reference_text": assessment.ReferenceText,
		"topic_title":    assessment.TopicTitle,
		"category":       string(assessment.Category),
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	audio, err := writer.CreateFormFile("audio", "audio.pcm")
	if err != nil {
		t.Fatalf("create audio field: %v", err)
	}
	if _, err := audio.Write(assessment.Audio); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/v1/evaluations", &body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("content-type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post evaluation: %v", err)
	}
	return response
}
