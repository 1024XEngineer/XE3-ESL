package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type flowClient struct {
	baseURL   string
	client    *http.Client
	exchanges *[]httpExchange
}

type httpExchange struct {
	Method string
	Path   string
	Status int
	Body   string
}

type flowTrace struct {
	Exchanges    []httpExchange
	Events       []Event
	ReplayEvents []Event
	Turns        []Turn
	Analyses     []Analysis
	Feedback     []Feedback
	Retry        RetryRequest
	History      []HistoryRecord
	FinalSession map[string]any
}

func TestDeterministicMainFlow(t *testing.T) {
	first := runMainFlow(t)
	second := runMainFlow(t)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same input produced a different complete trace:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func runMainFlow(t *testing.T) flowTrace {
	t.Helper()
	server := NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	trace := flowTrace{}
	client := flowClient{
		baseURL:   httpServer.URL,
		client:    httpServer.Client(),
		exchanges: &trace.Exchanges,
	}

	client.expect(t, http.MethodGet, "/v1/scenario-definitions/"+DemoScenarioDefinition, nil, nil, http.StatusOK, nil)
	client.expect(t, http.MethodGet, "/v1/scenario-definitions/"+DemoScenarioDefinition+"/role-definitions", nil, nil, http.StatusOK, nil)
	client.expect(t, http.MethodPost, "/v1/preparation-profiles", map[string]any{
		"background_summary": "Backend engineer preparing for an English technical interview.",
	}, map[string]string{"Idempotency-Key": "profile-key-1"}, http.StatusCreated, nil)
	client.expect(t, http.MethodPost, "/v1/preparation-profiles/"+demoPreparationProfile+"/snapshots", map[string]any{
		"source_version": 1,
	}, map[string]string{"Idempotency-Key": "snapshot-key-1"}, http.StatusCreated, nil)
	client.expect(t, http.MethodPost, "/v1/practice-plans", map[string]any{
		"scenario_definition_id":      DemoScenarioDefinition,
		"scenario_definition_version": 1,
		"scenario_config_id":          "scenario_config_backend",
		"scenario_config_version":     1,
		"preparation_profile_id":      demoPreparationProfile,
		"selected_role_ids":           []string{DemoRoleDefinition},
	}, map[string]string{"Idempotency-Key": "plan-key-1"}, http.StatusCreated, nil)
	var sessionBootstrap struct {
		Session map[string]any `json:"practice_session"`
	}
	client.expect(t, http.MethodPost, "/v1/practice-plans/"+demoPracticePlan+"/practice-sessions", map[string]any{
		"expected_plan_revision":  1,
		"preparation_snapshot_id": demoPreparationSnapshot,
		"practice_option_id":      DemoPracticeOption,
		"role_definition_ids":     []string{DemoRoleDefinition},
	}, map[string]string{"Idempotency-Key": "session-key-1"}, http.StatusCreated, &sessionBootstrap)
	client.expect(t, http.MethodGet, "/v1/practice-sessions/"+demoPracticeSession+"/snapshot", nil, nil, http.StatusOK, nil)
	client.expect(t, http.MethodPost, "/v1/practice-sessions/"+demoPracticeSession+"/questions", nil, map[string]string{
		"Idempotency-Key": "question-key-1",
	}, http.StatusOK, nil)
	client.expect(t, http.MethodGet, "/v1/practice-sessions/"+demoPracticeSession+"/bootstrap", nil, nil, http.StatusOK, nil)
	events := startEventCollector(
		t,
		WebSocketURL(httpServer.URL)+"/v1/practice-sessions/"+demoPracticeSession+"/events?after_sequence=0",
	)

	var invalidError errorEnvelope
	client.expect(t, http.MethodPost, "/v1/questions/question_demo_001/turns", map[string]any{
		"interaction_mode": "PUSH_TO_TALK",
		"answer_text":      " ",
	}, map[string]string{"Idempotency-Key": "invalid-turn-key"}, http.StatusBadRequest, &invalidError)
	if invalidError.Error.Code != "answer_invalid" {
		t.Fatalf("invalid answer returned %q", invalidError.Error.Code)
	}

	answers := []string{
		"I led a Go API reliability project and reduced incident recovery time.",
		"I introduced request tracing, defined an error budget, and staged the rollout.",
		"I chose sampling by endpoint risk to control cost while preserving failure evidence.",
		"I published the error budget, ran a canary review, and agreed rollback signals.",
	}
	feedbackID := ""
	for index, answer := range answers {
		questionID := fmt.Sprintf("question_demo_%03d", index+1)
		if index == 1 {
			var failure errorEnvelope
			failureBody := client.expect(t, http.MethodPost, "/v1/questions/"+questionID+"/turns", map[string]any{
				"interaction_mode": "PUSH_TO_TALK",
				"answer_text":      answer,
			}, map[string]string{
				"Idempotency-Key":  "turn-failure-key-2",
				"X-Mock-Fail-Once": "true",
			}, http.StatusUnprocessableEntity, &failure)
			if failure.Error.Code != "transcript_unavailable" || !failure.Error.Retryable {
				t.Fatalf("recoverable failure is not contract-shaped: %#v", failure)
			}
			replayedFailure := client.expect(t, http.MethodPost, "/v1/questions/"+questionID+"/turns", map[string]any{
				"interaction_mode": "PUSH_TO_TALK",
				"answer_text":      answer,
			}, map[string]string{"Idempotency-Key": "turn-failure-key-2"}, http.StatusUnprocessableEntity, nil)
			if !bytes.Equal(failureBody, replayedFailure) {
				t.Fatal("the first 422 response was not replayed byte-for-byte")
			}
		}

		var submitted Turn
		key := fmt.Sprintf("turn-success-key-%d", index+1)
		firstBody := client.expect(t, http.MethodPost, "/v1/questions/"+questionID+"/turns", map[string]any{
			"interaction_mode": "PUSH_TO_TALK",
			"answer_text":      answer,
		}, map[string]string{"Idempotency-Key": key}, http.StatusAccepted, &submitted)
		if submitted.Status != "submitted" || submitted.QuestionID != questionID {
			t.Fatalf("unexpected submitted Turn: %#v", submitted)
		}
		if index == 0 {
			replayedBody := client.expect(t, http.MethodPost, "/v1/questions/"+questionID+"/turns", map[string]any{
				"answer_text":      answer,
				"interaction_mode": "PUSH_TO_TALK",
			}, map[string]string{"Idempotency-Key": key}, http.StatusAccepted, nil)
			if !bytes.Equal(firstBody, replayedBody) {
				t.Fatal("successful Turn response was not replayed byte-for-byte")
			}
		}
		trace.Turns = append(trace.Turns, submitted)

		var pending Analysis
		client.expect(t, http.MethodPost, "/v1/turns/"+submitted.ID+"/turn-analyses", nil, map[string]string{
			"Idempotency-Key": fmt.Sprintf("analysis-key-%d", index+1),
		}, http.StatusAccepted, &pending)
		if pending.Status != "pending" || pending.TurnID != submitted.ID {
			t.Fatalf("unexpected pending analysis: %#v", pending)
		}
		var analyses struct {
			Analyses []Analysis `json:"analyses"`
		}
		client.expect(t, http.MethodGet, "/v1/turns/"+submitted.ID+"/turn-analyses", nil, nil, http.StatusOK, &analyses)
		if len(analyses.Analyses) != 1 || analyses.Analyses[0].Status != "completed" {
			t.Fatalf("turn %s did not produce one completed analysis: %#v", submitted.ID, analyses)
		}
		trace.Analyses = append(trace.Analyses, analyses.Analyses[0])
		var feedback struct {
			Items []Feedback `json:"feedback_items"`
		}
		client.expect(
			t,
			http.MethodGet,
			"/v1/turn-analyses/"+analyses.Analyses[0].ID+"/feedback-items",
			nil,
			nil,
			http.StatusOK,
			&feedback,
		)
		if len(feedback.Items) != 1 {
			t.Fatalf("analysis %s did not produce one feedback item", analyses.Analyses[0].ID)
		}
		trace.Feedback = append(trace.Feedback, feedback.Items[0])
		if index == 0 {
			feedbackID = feedback.Items[0].ID
		}
	}

	retryHeaders := map[string]string{"Idempotency-Key": "retry-key-1"}
	client.expect(
		t,
		http.MethodPost,
		"/v1/feedback-items/"+feedbackID+"/retry-requests",
		nil,
		retryHeaders,
		http.StatusCreated,
		&trace.Retry,
	)
	var replayRetry RetryRequest
	client.expect(
		t,
		http.MethodPost,
		"/v1/feedback-items/"+feedbackID+"/retry-requests",
		nil,
		retryHeaders,
		http.StatusCreated,
		&replayRetry,
	)
	if replayRetry != trace.Retry {
		t.Fatalf("retry replay changed the resource: %#v != %#v", replayRetry, trace.Retry)
	}

	var retryTurn Turn
	client.expect(t, http.MethodPost, "/v1/questions/question_demo_001/turns", map[string]any{
		"interaction_mode": "PUSH_TO_TALK",
		"answer_text":      "I selected tracing after comparing cost and coverage, then reduced recovery time.",
		"retry_request_id": trace.Retry.ID,
	}, map[string]string{"Idempotency-Key": "retry-turn-key-1"}, http.StatusAccepted, &retryTurn)
	trace.Turns = append(trace.Turns, retryTurn)
	var retryPending Analysis
	client.expect(t, http.MethodPost, "/v1/turns/"+retryTurn.ID+"/turn-analyses", nil, map[string]string{
		"Idempotency-Key": "retry-analysis-key-1",
	}, http.StatusAccepted, &retryPending)
	var retryAnalyses struct {
		Analyses []Analysis `json:"analyses"`
	}
	client.expect(t, http.MethodGet, "/v1/turns/"+retryTurn.ID+"/turn-analyses", nil, nil, http.StatusOK, &retryAnalyses)
	if len(retryAnalyses.Analyses) != 1 {
		t.Fatalf("retry Turn did not produce one analysis: %#v", retryAnalyses)
	}
	trace.Analyses = append(trace.Analyses, retryAnalyses.Analyses[0])

	client.expect(
		t,
		http.MethodGet,
		"/v1/practice-sessions/"+demoPracticeSession,
		nil,
		nil,
		http.StatusOK,
		&trace.FinalSession,
	)
	if trace.FinalSession["practice_session_status"] != "completed" {
		t.Fatalf("session did not complete: %#v", trace.FinalSession)
	}
	var history struct {
		Items []HistoryRecord `json:"history_records"`
	}
	client.expect(
		t,
		http.MethodGet,
		"/v1/history-records?practice_session_id="+demoPracticeSession,
		nil,
		nil,
		http.StatusOK,
		&history,
	)
	if len(history.Items) != 5 || len(trace.Turns) != 5 {
		t.Fatalf("expected four effective Turns and one retry: turns=%d history=%d", len(trace.Turns), len(history.Items))
	}
	trace.History = history.Items

	expectedTypes := []string{
		"stream.ready",
		"practice_session.started",
		"question.created",
		"turn.submitted", "turn.processing", "turn.completed", "question.created", "turn_analysis.completed",
		"answer.processing_failed",
		"turn.submitted", "turn.processing", "turn.completed", "question.created", "turn_analysis.completed",
		"turn.submitted", "turn.processing", "turn.completed", "question.created", "turn_analysis.completed",
		"turn.submitted", "turn.processing", "turn.completed", "practice_session.completed", "turn_analysis.completed",
		"turn.submitted", "turn.processing", "turn.completed", "turn_analysis.completed",
	}
	trace.Events = events.awaitAndStop(t, expectedTypes)
	assertEventTrace(t, trace.Events, expectedTypes)

	var bootstrap struct {
		LastEventSequence int `json:"last_event_sequence"`
	}
	client.expect(
		t,
		http.MethodGet,
		"/v1/practice-sessions/"+demoPracticeSession+"/bootstrap",
		nil,
		nil,
		http.StatusOK,
		&bootstrap,
	)
	trace.ReplayEvents = readEvents(
		t,
		fmt.Sprintf(
			"%s/v1/practice-sessions/%s/events?after_sequence=%d",
			WebSocketURL(httpServer.URL),
			demoPracticeSession,
			bootstrap.LastEventSequence-1,
		),
		2,
	)
	if trace.ReplayEvents[0].Type != "stream.ready" ||
		trace.ReplayEvents[1].Sequence != bootstrap.LastEventSequence {
		t.Fatalf("cursor replay is inconsistent: %#v", trace.ReplayEvents)
	}
	return trace
}

type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func (c *flowClient) expect(
	t *testing.T,
	method string,
	path string,
	body any,
	headers map[string]string,
	status int,
	target any,
) []byte {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.StatusCode, status, payload)
	}
	*c.exchanges = append(*c.exchanges, httpExchange{
		Method: method,
		Path:   path,
		Status: response.StatusCode,
		Body:   string(payload),
	})
	if target != nil {
		if err := json.Unmarshal(payload, target); err != nil {
			t.Fatalf("decode %s %s: %v\n%s", method, path, err, payload)
		}
	}
	return payload
}

type eventCollector struct {
	connection *websocket.Conn
	events     chan Event
	done       chan struct{}
}

func startEventCollector(t *testing.T, url string) *eventCollector {
	t.Helper()
	connection := dialEvents(t, url)
	collector := &eventCollector{
		connection: connection,
		events:     make(chan Event, 64),
		done:       make(chan struct{}),
	}
	go func() {
		defer close(collector.done)
		defer close(collector.events)
		for {
			var event Event
			if err := connection.ReadJSON(&event); err != nil {
				return
			}
			collector.events <- event
		}
	}()
	return collector
}

func dialEvents(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{Subprotocols: []string{eventProtocol}}
	connection, response, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial event stream: %v", err)
	}
	if response == nil || response.Header.Get("Sec-WebSocket-Protocol") != eventProtocol ||
		connection.Subprotocol() != eventProtocol {
		_ = connection.Close()
		t.Fatalf("event protocol was not negotiated: %#v", response)
	}
	return connection
}

func readEvents(t *testing.T, url string, count int) []Event {
	t.Helper()
	connection := dialEvents(t, url)
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	events := make([]Event, 0, count)
	for len(events) < count {
		var event Event
		if err := connection.ReadJSON(&event); err != nil {
			t.Fatalf("read replayed event: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func (c *eventCollector) awaitAndStop(t *testing.T, expectedTypes []string) []Event {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	events := make([]Event, 0, len(expectedTypes))
	for len(events) < len(expectedTypes) {
		select {
		case event, open := <-c.events:
			if !open {
				t.Fatalf("event stream closed after %d events", len(events))
			}
			events = append(events, event)
		case <-timer.C:
			t.Fatalf("timed out waiting for events; received %#v", events)
		}
	}
	_ = c.connection.Close()
	<-c.done
	for event := range c.events {
		events = append(events, event)
	}
	if len(events) != len(expectedTypes) {
		t.Fatalf("event stream produced %d events, want %d: %#v", len(events), len(expectedTypes), events)
	}
	return events
}

func assertEventTrace(t *testing.T, events []Event, expectedTypes []string) {
	t.Helper()
	nextSequence := 1
	for index, event := range events {
		if event.Type != expectedTypes[index] {
			t.Fatalf("event %d type=%q, want %q", index, event.Type, expectedTypes[index])
		}
		if event.Version != 1 || event.SessionID != demoPracticeSession ||
			event.ID == "" || event.OccurredAt == "" || event.CorrelationID == "" ||
			event.Payload == nil {
			t.Fatalf("event %d has an invalid envelope: %#v", index, event)
		}
		if event.Replayable {
			if event.Sequence != nextSequence {
				t.Fatalf("event %d sequence=%d, want %d", index, event.Sequence, nextSequence)
			}
			nextSequence++
		} else if event.Sequence != 0 {
			t.Fatalf("non-replayable event %d exposed sequence %d", index, event.Sequence)
		}
	}
}
