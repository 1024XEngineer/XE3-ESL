package smoke

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type contractClient struct {
	baseURL string
	client  *http.Client
}

type concurrentResponse struct {
	status int
	body   []byte
	err    error
}

func newContractClient(t *testing.T) contractClient {
	t.Helper()
	server := NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return contractClient{baseURL: httpServer.URL, client: httpServer.Client()}
}

func TestStrictRequestAndIdempotencyKeyValidation(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		code string
	}{
		{"profile missing required field", "/v1/preparation-profiles", `{}`, "invalid_request"},
		{"profile rejects unknown field", "/v1/preparation-profiles", `{"background_summary":"x","extra":true}`, "invalid_request"},
		{"profile rejects null", "/v1/preparation-profiles", `{"background_summary":null}`, "invalid_request"},
		{"profile rejects trailing JSON", "/v1/preparation-profiles", `{"background_summary":"x"} {}`, "invalid_request"},
		{"snapshot requires body", "/v1/preparation-profiles/" + demoPreparationProfile + "/snapshots", ``, "invalid_request"},
		{"snapshot rejects zero version", "/v1/preparation-profiles/" + demoPreparationProfile + "/snapshots", `{"source_version":0}`, "invalid_request"},
		{"plan rejects duplicate roles", "/v1/practice-plans", `{
			"scenario_definition_id":"scenario_programmer_interview",
			"scenario_definition_version":1,
			"scenario_config_id":"scenario_config_backend",
			"scenario_config_version":1,
			"preparation_profile_id":"profile_demo_001",
			"selected_role_ids":["role_technical_interviewer","role_technical_interviewer"]
		}`, "invalid_request"},
		{"session requires fields", "/v1/practice-plans/" + demoPracticePlan + "/practice-sessions", `{}`, "invalid_request"},
		{"question rejects body", "/v1/practice-sessions/" + demoPracticeSession + "/questions", `{}`, "invalid_request"},
		{"turn requires mode", "/v1/questions/question_demo_001/turns", `{"answer_text":"hello"}`, "invalid_request"},
		{"turn rejects unknown mode", "/v1/questions/question_demo_001/turns", `{"interaction_mode":"TEXT","answer_text":"hello"}`, "invalid_request"},
		{"turn rejects unknown field", "/v1/questions/question_demo_001/turns", `{"interaction_mode":"PUSH_TO_TALK","answer_text":"hello","actor_id":"forged"}`, "invalid_request"},
		{"analysis rejects body", "/v1/turns/turn_demo_001/turn-analyses", `{}`, "invalid_request"},
		{"retry rejects body", "/v1/feedback-items/feedback_demo_001/retry-requests", `{}`, "invalid_request"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newContractClient(t)
			status, body := client.request(
				t,
				http.MethodPost,
				test.path,
				test.body,
				map[string]string{"Idempotency-Key": "validation-key-1"},
			)
			assertError(t, status, body, http.StatusBadRequest, test.code)
		})
	}

	for _, test := range []struct {
		name string
		key  string
	}{
		{"missing", ""},
		{"short", "short"},
		{"too long", strings.Repeat("x", 129)},
	} {
		t.Run("idempotency key "+test.name, func(t *testing.T) {
			client := newContractClient(t)
			headers := map[string]string{}
			if test.key != "" {
				headers["Idempotency-Key"] = test.key
			}
			status, body := client.request(
				t,
				http.MethodPost,
				"/v1/preparation-profiles",
				`{"background_summary":"valid"}`,
				headers,
			)
			assertError(t, status, body, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestIdempotencyReplayScopeAndNoRepeatedSideEffects(t *testing.T) {
	client := newContractClient(t)

	profileBody := `{"background_summary":"Backend engineer"}`
	firstProfile := client.must(
		t,
		http.MethodPost,
		"/v1/preparation-profiles",
		profileBody,
		map[string]string{"Idempotency-Key": "profile-replay-key"},
		http.StatusCreated,
	)
	replayedProfile := client.must(
		t,
		http.MethodPost,
		"/v1/preparation-profiles",
		"{ \n \"background_summary\" : \"Backend engineer\" }",
		map[string]string{"Idempotency-Key": "profile-replay-key"},
		http.StatusCreated,
	)
	if !bytes.Equal(firstProfile, replayedProfile) {
		t.Fatal("profile replay did not preserve the original response body")
	}
	status, body := client.request(
		t,
		http.MethodPost,
		"/v1/preparation-profiles",
		`{"background_summary":"different"}`,
		map[string]string{"Idempotency-Key": "profile-replay-key"},
	)
	assertError(t, status, body, http.StatusConflict, "idempotency_key_conflict")

	snapshotBody := `{"source_version":1}`
	assertReplay(
		t,
		client,
		"/v1/preparation-profiles/"+demoPreparationProfile+"/snapshots",
		snapshotBody,
		"snapshot-replay-key",
		http.StatusCreated,
	)
	planBody := `{
		"scenario_definition_id":"scenario_programmer_interview",
		"scenario_definition_version":1,
		"scenario_config_id":"scenario_config_backend",
		"scenario_config_version":1,
		"preparation_profile_id":"profile_demo_001",
		"selected_role_ids":["role_technical_interviewer"]
	}`
	assertReplay(t, client, "/v1/practice-plans", planBody, "plan-replay-key", http.StatusCreated)
	sessionBody := `{
		"expected_plan_revision":1,
		"preparation_snapshot_id":"preparation_snapshot_demo_001",
		"practice_option_id":"option_full_interview",
		"role_definition_ids":["role_technical_interviewer"]
	}`
	sessionPath := "/v1/practice-plans/" + demoPracticePlan + "/practice-sessions"
	firstSession := client.must(
		t,
		http.MethodPost,
		sessionPath,
		sessionBody,
		map[string]string{"Idempotency-Key": "session-replay-key"},
		http.StatusCreated,
	)

	questionPath := "/v1/practice-sessions/" + demoPracticeSession + "/questions"
	firstQuestion := client.must(
		t,
		http.MethodPost,
		questionPath,
		"",
		map[string]string{"Idempotency-Key": "question-replay-key"},
		http.StatusOK,
	)
	replayedQuestion := client.must(
		t,
		http.MethodPost,
		questionPath,
		"",
		map[string]string{"Idempotency-Key": "question-replay-key"},
		http.StatusOK,
	)
	if !bytes.Equal(firstQuestion, replayedQuestion) {
		t.Fatal("question replay changed the response")
	}
	status, body = client.request(
		t,
		http.MethodPost,
		questionPath,
		`{}`,
		map[string]string{"Idempotency-Key": "question-replay-key"},
	)
	assertError(t, status, body, http.StatusConflict, "idempotency_key_conflict")

	replayedSession := client.must(
		t,
		http.MethodPost,
		sessionPath,
		sessionBody,
		map[string]string{"Idempotency-Key": "session-replay-key"},
		http.StatusCreated,
	)
	if !bytes.Equal(firstSession, replayedSession) {
		t.Fatal("session replay did not preserve the original response")
	}
	currentSession := client.must(
		t,
		http.MethodGet,
		"/v1/practice-sessions/"+demoPracticeSession,
		"",
		nil,
		http.StatusOK,
	)
	var session map[string]any
	if err := json.Unmarshal(currentSession, &session); err != nil {
		t.Fatal(err)
	}
	if session["practice_session_status"] != "in_progress" {
		t.Fatalf("session replay reset lifecycle state: %#v", session)
	}

	sharedKey := "canonical-path-shared-key"
	firstTurnBody := client.must(
		t,
		http.MethodPost,
		"/v1/questions/question_demo_001/turns",
		`{"interaction_mode":"PUSH_TO_TALK","answer_text":"first answer"}`,
		map[string]string{"Idempotency-Key": sharedKey},
		http.StatusAccepted,
	)
	var firstTurn Turn
	if err := json.Unmarshal(firstTurnBody, &firstTurn); err != nil {
		t.Fatal(err)
	}
	status, body = client.request(
		t,
		http.MethodPost,
		"/v1/questions/question_demo_001/turns",
		`{"interaction_mode":"REALTIME","answer_text":"first answer"}`,
		map[string]string{"Idempotency-Key": sharedKey},
	)
	assertError(t, status, body, http.StatusConflict, "idempotency_key_conflict")

	secondTurnBody := client.must(
		t,
		http.MethodPost,
		"/v1/questions/question_demo_002/turns",
		`{"interaction_mode":"REALTIME","answer_text":"second answer"}`,
		map[string]string{"Idempotency-Key": sharedKey},
		http.StatusAccepted,
	)
	var secondTurn Turn
	if err := json.Unmarshal(secondTurnBody, &secondTurn); err != nil {
		t.Fatal(err)
	}
	if firstTurn.ID == secondTurn.ID || secondTurn.QuestionID != "question_demo_002" ||
		secondTurn.InteractionMode != "REALTIME" {
		t.Fatalf("same key on different canonical paths was incorrectly aliased: %#v %#v", firstTurn, secondTurn)
	}

	analysisPath := "/v1/turns/" + firstTurn.ID + "/turn-analyses"
	assertReplay(t, client, analysisPath, "", "analysis-replay-key", http.StatusAccepted)
	analysesBody := client.must(t, http.MethodGet, analysisPath, "", nil, http.StatusOK)
	var analyses struct {
		Items []Analysis `json:"analyses"`
	}
	if err := json.Unmarshal(analysesBody, &analyses); err != nil || len(analyses.Items) != 1 {
		t.Fatalf("analysis replay repeated a side effect: %v %#v", err, analyses)
	}
	feedbackBody := client.must(
		t,
		http.MethodGet,
		"/v1/turn-analyses/"+analyses.Items[0].ID+"/feedback-items",
		"",
		nil,
		http.StatusOK,
	)
	var feedback struct {
		Items []Feedback `json:"feedback_items"`
	}
	if err := json.Unmarshal(feedbackBody, &feedback); err != nil || len(feedback.Items) != 1 {
		t.Fatalf("feedback missing: %v %#v", err, feedback)
	}
	retryPath := "/v1/feedback-items/" + feedback.Items[0].ID + "/retry-requests"
	assertReplay(t, client, retryPath, "", "retry-replay-key", http.StatusCreated)
	historyBody := client.must(
		t,
		http.MethodGet,
		"/v1/history-records?practice_session_id="+demoPracticeSession,
		"",
		nil,
		http.StatusOK,
	)
	var history struct {
		Items []HistoryRecord `json:"history_records"`
	}
	if err := json.Unmarshal(historyBody, &history); err != nil || len(history.Items) != 1 {
		t.Fatalf("replayed analysis/retry duplicated history: %v %#v", err, history)
	}
}

func TestConcurrentIdempotencyCoalescesAndRejectsConflicts(t *testing.T) {
	t.Run("same fingerprint", func(t *testing.T) {
		client := newContractClient(t)
		client.must(
			t,
			http.MethodPost,
			"/v1/preparation-profiles",
			`{"background_summary":"Backend engineer"}`,
			map[string]string{"Idempotency-Key": "concurrent-profile-key"},
			http.StatusCreated,
		)
		client.must(
			t,
			http.MethodPost,
			"/v1/preparation-profiles/"+demoPreparationProfile+"/snapshots",
			`{"source_version":1}`,
			map[string]string{"Idempotency-Key": "concurrent-snapshot-key"},
			http.StatusCreated,
		)
		client.must(
			t,
			http.MethodPost,
			"/v1/practice-plans",
			validPlanBody(),
			map[string]string{"Idempotency-Key": "concurrent-plan-key"},
			http.StatusCreated,
		)

		results := concurrentRequests(
			client,
			"/v1/practice-plans/"+demoPracticePlan+"/practice-sessions",
			[]string{validSessionBody(), validSessionBody()},
			"concurrent-session-key",
		)
		for _, result := range results {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.status != http.StatusCreated {
				t.Fatalf("concurrent replay status=%d, want 201: %s", result.status, result.body)
			}
		}
		if !bytes.Equal(results[0].body, results[1].body) {
			t.Fatalf("concurrent replay returned different bodies:\n%s\n%s", results[0].body, results[1].body)
		}
	})

	t.Run("different fingerprints", func(t *testing.T) {
		client := newContractClient(t)
		results := concurrentRequests(
			client,
			"/v1/preparation-profiles",
			[]string{
				`{"background_summary":"first"}`,
				`{"background_summary":"second"}`,
			},
			"concurrent-conflict-key",
		)
		created := 0
		conflicts := 0
		for _, result := range results {
			if result.err != nil {
				t.Fatal(result.err)
			}
			switch result.status {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
				conflicts++
				assertError(
					t,
					result.status,
					result.body,
					http.StatusConflict,
					"idempotency_key_conflict",
				)
			default:
				t.Fatalf("unexpected concurrent conflict status=%d: %s", result.status, result.body)
			}
		}
		if created != 1 || conflicts != 1 {
			t.Fatalf("created=%d conflicts=%d, want one of each", created, conflicts)
		}
	})
}

func TestPreparationReferencesGatePracticeResources(t *testing.T) {
	client := newContractClient(t)

	status, body := client.request(
		t,
		http.MethodPost,
		"/v1/practice-plans",
		validPlanBody(),
		map[string]string{"Idempotency-Key": "plan-before-profile-key"},
	)
	assertError(t, status, body, http.StatusNotFound, "preparation_profile_not_found")

	client.must(
		t,
		http.MethodPost,
		"/v1/preparation-profiles",
		`{"background_summary":"Backend engineer"}`,
		map[string]string{"Idempotency-Key": "profile-for-plan-key"},
		http.StatusCreated,
	)
	client.must(
		t,
		http.MethodPost,
		"/v1/practice-plans",
		validPlanBody(),
		map[string]string{"Idempotency-Key": "plan-with-profile-key"},
		http.StatusCreated,
	)

	sessionPath := "/v1/practice-plans/" + demoPracticePlan + "/practice-sessions"
	status, body = client.request(
		t,
		http.MethodPost,
		sessionPath,
		validSessionBody(),
		map[string]string{"Idempotency-Key": "session-before-snapshot-key"},
	)
	assertError(t, status, body, http.StatusNotFound, "resource_not_found")

	client.must(
		t,
		http.MethodPost,
		"/v1/preparation-profiles/"+demoPreparationProfile+"/snapshots",
		`{"source_version":1}`,
		map[string]string{"Idempotency-Key": "snapshot-for-session-key"},
		http.StatusCreated,
	)
	client.must(
		t,
		http.MethodPost,
		sessionPath,
		validSessionBody(),
		map[string]string{"Idempotency-Key": "session-with-snapshot-key"},
		http.StatusCreated,
	)
}

func TestPendingAnalysisOmitsCompletedFields(t *testing.T) {
	client := newContractClient(t)
	seedThroughQuestion(t, client)
	turnBody := client.must(
		t,
		http.MethodPost,
		"/v1/questions/question_demo_001/turns",
		`{"interaction_mode":"PUSH_TO_TALK","answer_text":"A complete answer."}`,
		map[string]string{"Idempotency-Key": "pending-analysis-turn-key"},
		http.StatusAccepted,
	)
	var turn Turn
	if err := json.Unmarshal(turnBody, &turn); err != nil {
		t.Fatal(err)
	}
	pendingBody := client.must(
		t,
		http.MethodPost,
		"/v1/turns/"+turn.ID+"/turn-analyses",
		"",
		map[string]string{"Idempotency-Key": "pending-analysis-key"},
		http.StatusAccepted,
	)
	var pending map[string]json.RawMessage
	if err := json.Unmarshal(pendingBody, &pending); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"score", "summary", "analysis_transcript", "completed_at"} {
		if _, present := pending[field]; present {
			t.Fatalf("pending analysis exposed %q: %s", field, pendingBody)
		}
	}
	for _, field := range []string{
		"turn_analysis_id",
		"turn_id",
		"evaluator_version",
		"analysis_status",
		"created_at",
	} {
		if _, present := pending[field]; !present {
			t.Fatalf("pending analysis omitted %q: %s", field, pendingBody)
		}
	}
}

func TestRetryRequiresCompletedOriginalTurn(t *testing.T) {
	runtime := NewRuntime()
	runtime.mu.Lock()
	runtime.turns = append(runtime.turns, Turn{
		ID:        "turn_answering",
		SessionID: demoPracticeSession,
		Status:    "answering",
	})
	runtime.mu.Unlock()

	if _, err := runtime.createRetryTurn("retry_pending_original", "turn_answering"); !errors.Is(err, ErrRetryConflict) {
		t.Fatalf("createRetryTurn error=%v, want ErrRetryConflict", err)
	}
}

func TestFailureInjectionRunsAfterResourceValidation(t *testing.T) {
	client := newContractClient(t)
	seedThroughQuestion(t, client)

	futureAnswer := `{"interaction_mode":"PUSH_TO_TALK","answer_text":"future answer"}`
	status, body := client.request(
		t,
		http.MethodPost,
		"/v1/questions/question_demo_002/turns",
		futureAnswer,
		map[string]string{
			"Idempotency-Key":  "future-question-failure-key",
			"X-Mock-Fail-Once": "true",
		},
	)
	assertError(t, status, body, http.StatusNotFound, "question_not_found")

	client.must(
		t,
		http.MethodPost,
		"/v1/questions/question_demo_001/turns",
		`{"interaction_mode":"PUSH_TO_TALK","answer_text":"first answer"}`,
		map[string]string{"Idempotency-Key": "create-future-question-key"},
		http.StatusAccepted,
	)
	client.must(
		t,
		http.MethodPost,
		"/v1/questions/question_demo_002/turns",
		futureAnswer,
		map[string]string{"Idempotency-Key": "future-question-success-key"},
		http.StatusAccepted,
	)
}

func TestSlowSubscriberIsRemovedWithoutBlockingEventPublication(t *testing.T) {
	runtime := NewRuntime()
	_, live, unsubscribe := runtime.subscribe(0)

	published := make(chan struct{})
	go func() {
		for index := 0; index < 129; index++ {
			runtime.mu.Lock()
			runtime.appendEventLocked("question.created", map[string]any{
				"question_id": formatID("slow_subscriber_question", index+1),
			})
			runtime.mu.Unlock()
		}
		close(published)
	}()

	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("event publication blocked on a slow subscriber")
	}

	runtime.mu.Lock()
	subscriberCount := len(runtime.subscribers)
	runtime.mu.Unlock()
	if subscriberCount != 0 {
		t.Fatalf("slow subscriber was not removed: subscribers=%d", subscriberCount)
	}
	for range live {
	}
	unsubscribe()
}

func TestReplayabilityIsExplicitForEverySmokeEventType(t *testing.T) {
	tests := map[string]bool{
		"stream.ready":               false,
		"answer.processing_failed":   false,
		"question.created":           true,
		"turn.submitted":             true,
		"turn.processing":            true,
		"turn.completed":             true,
		"turn_analysis.completed":    true,
		"practice_session.started":   true,
		"practice_session.completed": true,
	}
	for eventType, expected := range tests {
		if actual := isReplayableEvent(eventType); actual != expected {
			t.Fatalf("isReplayableEvent(%q)=%t, want %t", eventType, actual, expected)
		}
	}
}

func TestResourceAndQueryErrors(t *testing.T) {
	client := newContractClient(t)
	tests := []struct {
		method  string
		path    string
		body    string
		headers map[string]string
		status  int
		code    string
	}{
		{http.MethodGet, "/v1/scenario-definitions/unknown", "", nil, 404, "scenario_definition_not_found"},
		{http.MethodGet, "/v1/scenario-definitions/unknown/role-definitions", "", nil, 404, "scenario_definition_not_found"},
		{http.MethodPost, "/v1/preparation-profiles/unknown/snapshots", `{"source_version":1}`, map[string]string{"Idempotency-Key": "unknown-profile-key"}, 404, "preparation_profile_not_found"},
		{http.MethodPost, "/v1/practice-plans/unknown/practice-sessions", validSessionBody(), map[string]string{"Idempotency-Key": "unknown-plan-key"}, 404, "practice_plan_not_found"},
		{http.MethodGet, "/v1/practice-sessions/unknown", "", nil, 404, "practice_session_not_found"},
		{http.MethodGet, "/v1/practice-sessions/unknown/snapshot", "", nil, 404, "practice_session_not_found"},
		{http.MethodGet, "/v1/practice-sessions/unknown/bootstrap", "", nil, 404, "practice_session_not_found"},
		{http.MethodPost, "/v1/practice-sessions/unknown/questions", "", map[string]string{"Idempotency-Key": "unknown-session-key"}, 404, "practice_session_not_found"},
		{http.MethodPost, "/v1/questions/unknown/turns", `{"interaction_mode":"PUSH_TO_TALK","answer_text":"valid"}`, map[string]string{"Idempotency-Key": "unknown-question-key"}, 404, "question_not_found"},
		{http.MethodGet, "/v1/turns/unknown", "", nil, 404, "turn_not_found"},
		{http.MethodPost, "/v1/turns/unknown/turn-analyses", "", map[string]string{"Idempotency-Key": "unknown-turn-key"}, 404, "turn_not_found"},
		{http.MethodGet, "/v1/turns/unknown/turn-analyses", "", nil, 404, "turn_not_found"},
		{http.MethodGet, "/v1/turn-analyses/unknown/feedback-items", "", nil, 404, "turn_analysis_not_found"},
		{http.MethodPost, "/v1/feedback-items/unknown/retry-requests", "", map[string]string{"Idempotency-Key": "unknown-feedback-key"}, 404, "feedback_item_not_found"},
		{http.MethodGet, "/v1/retry-requests/unknown", "", nil, 404, "retry_request_not_found"},
		{http.MethodGet, "/v1/history-records", "", nil, 400, "invalid_request"},
		{http.MethodGet, "/v1/history-records?practice_session_id=unknown", "", nil, 404, "practice_session_not_found"},
	}
	for _, test := range tests {
		status, body := client.request(t, test.method, test.path, test.body, test.headers)
		assertError(t, status, body, test.status, test.code)
	}

	seedThroughQuestion(t, client)
	for index := 1; index <= 4; index++ {
		client.must(
			t,
			http.MethodPost,
			"/v1/questions/question_demo_00"+string(rune('0'+index))+"/turns",
			`{"interaction_mode":"PUSH_TO_TALK","answer_text":"valid answer"}`,
			map[string]string{"Idempotency-Key": "complete-turn-key-" + string(rune('0'+index))},
			http.StatusAccepted,
		)
	}
	status, body := client.request(
		t,
		http.MethodPost,
		"/v1/questions/unknown-after-completion/turns",
		`{"interaction_mode":"PUSH_TO_TALK","answer_text":"valid"}`,
		map[string]string{"Idempotency-Key": "unknown-after-complete"},
	)
	assertError(t, status, body, http.StatusNotFound, "question_not_found")
}

func TestWebSocketHandshakeValidation(t *testing.T) {
	client := newContractClient(t)
	seedThroughQuestion(t, client)
	base := WebSocketURL(client.baseURL) + "/v1/practice-sessions/"
	assertWebSocketStatus(t, base+"unknown/events?after_sequence=0", []string{eventProtocol}, http.StatusNotFound)
	assertWebSocketStatus(t, base+demoPracticeSession+"/events?after_sequence=abc", []string{eventProtocol}, http.StatusBadRequest)
	assertWebSocketStatus(t, base+demoPracticeSession+"/events?after_sequence=-1", []string{eventProtocol}, http.StatusBadRequest)
	assertWebSocketStatus(t, base+demoPracticeSession+"/events?after_sequence=", []string{eventProtocol}, http.StatusBadRequest)
	assertWebSocketStatus(t, base+demoPracticeSession+"/events?after_sequence=0&after_sequence=1", []string{eventProtocol}, http.StatusBadRequest)
	assertWebSocketStatus(t, base+demoPracticeSession+"/events?after_sequence=0", nil, http.StatusBadRequest)
	assertWebSocketStatus(t, base+demoPracticeSession+"/events?after_sequence=0", []string{"wrong.events.v1"}, http.StatusBadRequest)
}

func concurrentRequests(
	client contractClient,
	path string,
	bodies []string,
	key string,
) []concurrentResponse {
	start := make(chan struct{})
	results := make(chan concurrentResponse, len(bodies))
	for _, body := range bodies {
		body := body
		go func() {
			<-start
			request, err := http.NewRequest(
				http.MethodPost,
				client.baseURL+path,
				strings.NewReader(body),
			)
			if err != nil {
				results <- concurrentResponse{err: err}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", key)
			response, err := client.client.Do(request)
			if err != nil {
				results <- concurrentResponse{err: err}
				return
			}
			payload, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil {
				results <- concurrentResponse{err: readErr}
				return
			}
			if closeErr != nil {
				results <- concurrentResponse{err: closeErr}
				return
			}
			results <- concurrentResponse{
				status: response.StatusCode,
				body:   payload,
			}
		}()
	}
	close(start)

	collected := make([]concurrentResponse, 0, len(bodies))
	for range bodies {
		collected = append(collected, <-results)
	}
	return collected
}

func seedThroughQuestion(t *testing.T, client contractClient) {
	t.Helper()
	client.must(
		t,
		http.MethodPost,
		"/v1/preparation-profiles",
		`{"background_summary":"Backend engineer"}`,
		map[string]string{"Idempotency-Key": "seed-profile-key"},
		http.StatusCreated,
	)
	client.must(
		t,
		http.MethodPost,
		"/v1/preparation-profiles/"+demoPreparationProfile+"/snapshots",
		`{"source_version":1}`,
		map[string]string{"Idempotency-Key": "seed-snapshot-key"},
		http.StatusCreated,
	)
	client.must(
		t,
		http.MethodPost,
		"/v1/practice-plans",
		`{
			"scenario_definition_id":"scenario_programmer_interview",
			"scenario_definition_version":1,
			"scenario_config_id":"scenario_config_backend",
			"scenario_config_version":1,
			"preparation_profile_id":"profile_demo_001",
			"selected_role_ids":["role_technical_interviewer"]
		}`,
		map[string]string{"Idempotency-Key": "seed-plan-key"},
		http.StatusCreated,
	)
	client.must(
		t,
		http.MethodPost,
		"/v1/practice-plans/"+demoPracticePlan+"/practice-sessions",
		validSessionBody(),
		map[string]string{"Idempotency-Key": "seed-session-key"},
		http.StatusCreated,
	)
	client.must(
		t,
		http.MethodPost,
		"/v1/practice-sessions/"+demoPracticeSession+"/questions",
		"",
		map[string]string{"Idempotency-Key": "seed-question-key"},
		http.StatusOK,
	)
}

func validSessionBody() string {
	return `{
		"expected_plan_revision":1,
		"preparation_snapshot_id":"preparation_snapshot_demo_001",
		"practice_option_id":"option_full_interview",
		"role_definition_ids":["role_technical_interviewer"]
	}`
}

func validPlanBody() string {
	return `{
		"scenario_definition_id":"scenario_programmer_interview",
		"scenario_definition_version":1,
		"scenario_config_id":"scenario_config_backend",
		"scenario_config_version":1,
		"preparation_profile_id":"profile_demo_001",
		"selected_role_ids":["role_technical_interviewer"]
	}`
}

func assertReplay(
	t *testing.T,
	client contractClient,
	path string,
	body string,
	key string,
	status int,
) {
	t.Helper()
	headers := map[string]string{"Idempotency-Key": key}
	first := client.must(t, http.MethodPost, path, body, headers, status)
	replayed := client.must(t, http.MethodPost, path, body, headers, status)
	if !bytes.Equal(first, replayed) {
		t.Fatalf("%s replay changed response:\n%s\n%s", path, first, replayed)
	}
}

func (c contractClient) request(
	t *testing.T,
	method string,
	path string,
	body string,
	headers map[string]string,
) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
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
	return response.StatusCode, payload
}

func (c contractClient) must(
	t *testing.T,
	method string,
	path string,
	body string,
	headers map[string]string,
	status int,
) []byte {
	t.Helper()
	actual, payload := c.request(t, method, path, body, headers)
	if actual != status {
		t.Fatalf("%s %s returned %d, want %d: %s", method, path, actual, status, payload)
	}
	return payload
}

func assertError(
	t *testing.T,
	status int,
	body []byte,
	expectedStatus int,
	expectedCode string,
) {
	t.Helper()
	if status != expectedStatus {
		t.Fatalf("status=%d, want %d: %s", status, expectedStatus, body)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error response: %v: %s", err, body)
	}
	if envelope.Error.Code != expectedCode {
		t.Fatalf("error code=%q, want %q: %s", envelope.Error.Code, expectedCode, body)
	}
}

func assertWebSocketStatus(
	t *testing.T,
	url string,
	protocols []string,
	expectedStatus int,
) {
	t.Helper()
	dialer := websocket.Dialer{Subprotocols: protocols}
	connection, response, err := dialer.Dial(url, nil)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatalf("WebSocket handshake unexpectedly succeeded for %s", url)
	}
	if response == nil {
		t.Fatalf("WebSocket handshake returned no HTTP response for %s: %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("WebSocket status=%d, want %d: %s", response.StatusCode, expectedStatus, body)
	}
}
