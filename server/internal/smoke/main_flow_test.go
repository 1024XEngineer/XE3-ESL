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
	baseURL string
	client  *http.Client
}

type flowSummary struct {
	SessionID          string
	EffectiveTurns     int
	RetryTurnID        string
	HistoryCount       int
	EventTypes         []string
	RecoverableFailure bool
}

func TestDeterministicMainFlow(t *testing.T) {
	first := runMainFlow(t)
	second := runMainFlow(t)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same input produced different results:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func runMainFlow(t *testing.T) flowSummary {
	t.Helper()
	server := NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	client := flowClient{baseURL: httpServer.URL, client: httpServer.Client()}

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
	events := startEventCollector(t, WebSocketURL(httpServer.URL)+"/v1/practice-sessions/"+demoPracticeSession+"/events?after_sequence=0")

	var invalidError map[string]any
	client.expect(t, http.MethodPost, "/v1/questions/question_demo_001/turns", map[string]any{
		"interaction_mode": "PUSH_TO_TALK",
		"answer_text":      " ",
	}, map[string]string{"Idempotency-Key": "invalid-turn"}, http.StatusBadRequest, &invalidError)

	answers := []string{
		"I led a Go API reliability project and reduced incident recovery time.",
		"I introduced request tracing, defined an error budget, and staged the rollout.",
		"I chose sampling by endpoint risk to control cost while preserving failure evidence.",
		"I published the error budget, ran a canary review, and agreed rollback signals.",
	}
	feedbackID := ""
	recoverableFailure := false
	for index, answer := range answers {
		questionID := fmt.Sprintf("question_demo_%03d", index+1)
		headers := map[string]string{"Idempotency-Key": fmt.Sprintf("turn-key-%d", index+1)}
		if index == 1 {
			headers["X-Mock-Fail-Once"] = "true"
			client.expect(t, http.MethodPost, "/v1/questions/"+questionID+"/turns", map[string]any{
				"interaction_mode": "PUSH_TO_TALK",
				"answer_text":      answer,
			}, headers, http.StatusUnprocessableEntity, nil)
			recoverableFailure = true
		}
		var turn Turn
		client.expect(t, http.MethodPost, "/v1/questions/"+questionID+"/turns", map[string]any{
			"interaction_mode": "PUSH_TO_TALK",
			"answer_text":      answer,
		}, headers, http.StatusAccepted, &turn)
		var analyses struct {
			Analyses []Analysis `json:"analyses"`
		}
		client.expect(t, http.MethodGet, "/v1/turns/"+turn.ID+"/turn-analyses", nil, nil, http.StatusOK, &analyses)
		if len(analyses.Analyses) != 1 {
			t.Fatalf("turn %s did not produce one analysis", turn.ID)
		}
		var feedback struct {
			Items []Feedback `json:"feedback_items"`
		}
		client.expect(t, http.MethodGet, "/v1/turn-analyses/"+analyses.Analyses[0].ID+"/feedback-items", nil, nil, http.StatusOK, &feedback)
		if len(feedback.Items) != 1 {
			t.Fatalf("analysis %s did not produce feedback", analyses.Analyses[0].ID)
		}
		if index == 0 {
			feedbackID = feedback.Items[0].ID
			var replay Turn
			client.expect(t, http.MethodPost, "/v1/questions/"+questionID+"/turns", map[string]any{
				"interaction_mode": "PUSH_TO_TALK",
				"answer_text":      answer,
			}, headers, http.StatusAccepted, &replay)
			if replay.ID != turn.ID {
				t.Fatalf("idempotent replay created a different turn: %s != %s", replay.ID, turn.ID)
			}
		}
	}

	var retryResponse RetryRequest
	retryHeaders := map[string]string{"Idempotency-Key": "retry-key-1"}
	client.expect(t, http.MethodPost, "/v1/feedback-items/"+feedbackID+"/retry-requests", map[string]any{}, retryHeaders, http.StatusCreated, &retryResponse)
	var replayRetry RetryRequest
	client.expect(t, http.MethodPost, "/v1/feedback-items/"+feedbackID+"/retry-requests", map[string]any{}, retryHeaders, http.StatusCreated, &replayRetry)
	if replayRetry.NewTurnID != retryResponse.NewTurnID {
		t.Fatalf("idempotent retry created a different turn: %s != %s", replayRetry.NewTurnID, retryResponse.NewTurnID)
	}
	client.expect(t, http.MethodPost, "/v1/questions/question_demo_001/turns", map[string]any{
		"interaction_mode": "PUSH_TO_TALK",
		"answer_text":      "I selected tracing after comparing cost and coverage, then reduced recovery time.",
		"retry_request_id": retryResponse.ID,
	}, map[string]string{"Idempotency-Key": "retry-turn-key-1"}, http.StatusAccepted, nil)

	var finalSession map[string]any
	client.expect(t, http.MethodGet, "/v1/practice-sessions/"+demoPracticeSession, nil, nil, http.StatusOK, &finalSession)
	if finalSession["practice_session_status"] != "completed" {
		t.Fatalf("session did not complete: %#v", finalSession)
	}

	var history struct {
		Items []HistoryRecord `json:"history_records"`
	}
	client.expect(t, http.MethodGet, "/v1/history-records?practice_session_id="+demoPracticeSession, nil, nil, http.StatusOK, &history)
	if len(history.Items) != 5 {
		t.Fatalf("history does not contain four original turns and one retry: %d", len(history.Items))
	}

	requiredEvents := []string{
		"practice_session.started",
		"answer.processing_failed",
		"turn.completed",
		"turn_analysis.completed",
		"practice_session.completed",
	}
	eventTypes := events.awaitAndStop(t, requiredEvents)
	for _, required := range requiredEvents {
		if !contains(eventTypes, required) {
			t.Fatalf("event stream does not contain %q: %#v", required, eventTypes)
		}
	}
	var bootstrap struct {
		LastEventSequence int `json:"last_event_sequence"`
	}
	client.expect(t, http.MethodGet, "/v1/practice-sessions/"+demoPracticeSession+"/bootstrap", nil, nil, http.StatusOK, &bootstrap)
	replayed := readOneEvent(t, fmt.Sprintf(
		"%s/v1/practice-sessions/%s/events?after_sequence=%d",
		WebSocketURL(httpServer.URL),
		demoPracticeSession,
		bootstrap.LastEventSequence-1,
	))
	if !replayed.Replayable || replayed.Sequence != bootstrap.LastEventSequence {
		t.Fatalf("event cursor replay is inconsistent: %#v", replayed)
	}

	return flowSummary{
		SessionID:          sessionBootstrap.Session["practice_session_id"].(string),
		EffectiveTurns:     len(answers),
		RetryTurnID:        retryResponse.NewTurnID,
		HistoryCount:       len(history.Items),
		EventTypes:         eventTypes,
		RecoverableFailure: recoverableFailure,
	}
}

func (c flowClient) expect(t *testing.T, method, path string, body any, headers map[string]string, status int, target any) {
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
	if target != nil {
		if err := json.Unmarshal(payload, target); err != nil {
			t.Fatalf("decode %s %s: %v\n%s", method, path, err, payload)
		}
	}
}

type eventCollector struct {
	connection *websocket.Conn
	types      chan string
	done       chan struct{}
}

func startEventCollector(t *testing.T, url string) *eventCollector {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	collector := &eventCollector{
		connection: connection,
		types:      make(chan string, 64),
		done:       make(chan struct{}),
	}
	go func() {
		defer close(collector.done)
		defer close(collector.types)
		for {
			var event Event
			if err := connection.ReadJSON(&event); err != nil {
				return
			}
			collector.types <- event.Type
		}
	}()
	return collector
}

func readOneEvent(t *testing.T, url string) Event {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event Event
	if err := connection.ReadJSON(&event); err != nil {
		t.Fatalf("read replayed event: %v", err)
	}
	return event
}

func (c *eventCollector) awaitAndStop(t *testing.T, required []string) []string {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	var eventTypes []string
	for {
		complete := true
		for _, requiredType := range required {
			if !contains(eventTypes, requiredType) {
				complete = false
				break
			}
		}
		if complete {
			break
		}
		select {
		case eventType := <-c.types:
			eventTypes = append(eventTypes, eventType)
		case <-timer.C:
			t.Fatalf("timed out waiting for events; received %#v", eventTypes)
		}
	}
	_ = c.connection.Close()
	<-c.done
	for eventType := range c.types {
		eventTypes = append(eventTypes, eventType)
	}
	return eventTypes
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
