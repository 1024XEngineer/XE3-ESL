package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func testVoiceHTTPOptions() VoiceHTTPOptions {
	return VoiceHTTPOptions{
		AudioReadTimeout: defaultVoiceReadTimeout,
		ReviewHistoryCursorKey: []byte(
			"0123456789abcdef0123456789abcdef",
		),
	}
}

func TestVoiceHTTPUsesFrozenResponseDTOs(t *testing.T) {
	conversations := newAgentVoiceConversation(3)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)
	voice := newVoiceSessionTestApplication(
		t,
		conversations,
		practice,
		reviews,
		orchestrator,
	)
	handler, err := NewHTTPHandlerWithRunsAndVoice(
		voiceHTTPApplication{},
		nil,
		voice,
		voiceHTTPMatters{},
		voiceHTTPAuthenticator{},
		func() string { return "corr_voice" },
		testVoiceHTTPOptions(),
	)
	if err != nil {
		t.Fatalf("new voice HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)

	start := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/agent-threads/thread-1/voice-practice-sessions",
		nil,
		map[string]string{"Idempotency-Key": "session-start-1"},
	)
	if start.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body = %s", start.Code, start.Body)
	}
	started := decodeVoiceJSONObject(t, start)
	requireVoiceKeys(t, started,
		"current_question",
		"effective_turns",
		"matter",
		"practice_plan_id",
		"practice_session_id",
		"session_completed",
		"session_version",
		"thread_id",
		"turn_limit",
	)
	requireVoiceKeys(
		t,
		started["current_question"].(map[string]any),
		"addressee_participant_ids",
		"content",
		"practice_session_id",
		"question_id",
		"speaker_participant_id",
		"speech_path",
	)

	candidate := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/voice-practice-sessions/session-1/questions/question-1/transcription-candidates",
		[]byte("bounded test body"),
		map[string]string{
			"Content-Type":    "audio/wav",
			"Idempotency-Key": "transcribe-1",
		},
	)
	if candidate.Code != http.StatusCreated {
		t.Fatalf(
			"candidate status = %d, body = %s",
			candidate.Code,
			candidate.Body,
		)
	}
	requireVoiceKeys(
		t,
		decodeVoiceJSONObject(t, candidate),
		"candidate_id",
		"created_at",
		"evidence_version",
		"practice_session_id",
		"question_id",
		"respondent_participant_id",
		"transcript",
		"transcript_id",
	)

	for round := 1; round <= 3; round++ {
		confirmed := voiceHTTPRequest(
			t,
			router,
			http.MethodPost,
			"/v1/transcription-candidates/"+
				agentVoiceCandidateID(round)+
				"/confirmations",
			nil,
			map[string]string{
				"Idempotency-Key": "confirm-candidate-" +
					agentVoiceCandidateID(round),
			},
		)
		if confirmed.Code != http.StatusOK {
			t.Fatalf(
				"confirm %d status = %d, body = %s",
				round,
				confirmed.Code,
				confirmed.Body,
			)
		}
		state := decodeVoiceJSONObject(t, confirmed)
		if round < 3 {
			if _, found := state["review"]; found {
				t.Fatalf("round %d exposed Review: %#v", round, state)
			}
			if _, found := state["current_question"]; !found {
				t.Fatalf("round %d omitted next Question: %#v", round, state)
			}
			continue
		}
		requireVoiceKeys(t, state,
			"current_turn",
			"effective_turns",
			"matter",
			"practice_plan_id",
			"practice_session_id",
			"review",
			"session_completed",
			"session_version",
			"thread_id",
			"turn_limit",
		)
		requireVoiceKeys(
			t,
			state["current_turn"].(map[string]any),
			"answer_text",
			"candidate_id",
			"effective_turns",
			"evidence_version",
			"practice_session_id",
			"question_id",
			"respondent_participant_id",
			"review_id",
			"session_completed",
			"turn_id",
		)
		requireVoiceKeys(
			t,
			state["review"].(map[string]any),
			"completed_at",
			"created_at",
			"implementation_version",
			"practice_session_id",
			"result",
			"review_id",
			"source_turn_id",
			"source_turn_version",
			"status",
			"updated_at",
		)
	}
}

func TestVoiceHTTPListsAuthenticatedReviewHistoryWithOpaqueCursor(
	t *testing.T,
) {
	conversations := newAgentVoiceConversation(3)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)
	voice := newVoiceSessionTestApplication(
		t,
		conversations,
		practice,
		reviews,
		orchestrator,
	)
	newerCreatedAt := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	olderCreatedAt := newerCreatedAt.Add(-time.Hour)
	newer := completedVoiceHistoryReview(
		"20000000-0000-4000-8000-000000000002",
		newerCreatedAt,
		91,
	)
	older := completedVoiceHistoryReview(
		"20000000-0000-4000-8000-000000000001",
		olderCreatedAt,
		78,
	)
	voice.reviews = voiceSessionTestReviews{
		reviews: reviews,
		history: []VoiceSessionReview{newer, older},
	}
	handler, err := NewHTTPHandlerWithRunsAndVoice(
		voiceHTTPApplication{},
		nil,
		voice,
		voiceHTTPMatters{},
		voiceHTTPAuthenticator{},
		func() string { return "corr_review_history" },
		testVoiceHTTPOptions(),
	)
	if err != nil {
		t.Fatalf("new voice HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(
		unauthenticated,
		httptest.NewRequest(http.MethodGet, "/v1/formal-reviews", nil),
	)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated history status = %d", unauthenticated.Code)
	}

	firstResponse := voiceHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/formal-reviews?limit=1",
		nil,
		nil,
	)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf(
			"first history status = %d, body = %s",
			firstResponse.Code,
			firstResponse.Body,
		)
	}
	first := decodeVoiceJSONObject(t, firstResponse)
	requireVoiceKeys(t, first, "items", "next_cursor")
	firstItems := first["items"].([]any)
	if len(firstItems) != 1 {
		t.Fatalf("first history items = %#v", firstItems)
	}
	firstItem := firstItems[0].(map[string]any)
	if firstItem["review_id"] != newer.ID ||
		firstItem["practice_session_id"] != newer.SessionID ||
		firstItem["status"] != "completed" {
		t.Fatalf("first history item = %#v", firstItem)
	}
	if _, leaked := firstItem["owner_user_id"]; leaked {
		t.Fatalf("history DTO leaked owner: %#v", firstItem)
	}
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" || strings.Contains(cursor, newer.ID) {
		t.Fatalf("history cursor is not opaque: %#v", first["next_cursor"])
	}

	secondResponse := voiceHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/formal-reviews?limit=1&cursor="+cursor,
		nil,
		nil,
	)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf(
			"second history status = %d, body = %s",
			secondResponse.Code,
			secondResponse.Body,
		)
	}
	second := decodeVoiceJSONObject(t, secondResponse)
	requireVoiceKeys(t, second, "items")
	secondItems := second["items"].([]any)
	if len(secondItems) != 1 ||
		secondItems[0].(map[string]any)["review_id"] != older.ID {
		t.Fatalf("second history items = %#v", secondItems)
	}

	for _, path := range []string{
		"/v1/formal-reviews?limit=51",
		"/v1/formal-reviews?limit=1&limit=2",
		"/v1/formal-reviews?cursor=not-base64",
		"/v1/formal-reviews?offset=1",
		"/v1/formal-reviews?limit=1;offset=1",
	} {
		response := voiceHTTPRequest(
			t,
			router,
			http.MethodGet,
			path,
			nil,
			nil,
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid history %q status = %d", path, response.Code)
		}
	}
}

func TestReviewHistoryCursorIsSignedCanonicalAndActorBound(t *testing.T) {
	key := testVoiceHTTPOptions().ReviewHistoryCursorKey
	handler := &HTTPHandler{reviewCursorKey: key}
	cursor := VoiceReviewHistoryCursor{
		CreatedAt: time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC),
		ReviewID:  "20000000-0000-4000-8000-000000000002",
	}
	encoded, ok := handler.encodeReviewHistoryCursor("actor-a", cursor)
	if !ok || strings.Count(encoded, ".") != 1 {
		t.Fatalf("encodeReviewHistoryCursor() = %q, %t", encoded, ok)
	}
	decoded, ok := handler.decodeReviewHistoryCursor("actor-a", encoded)
	if !ok || decoded != cursor {
		t.Fatalf("decodeReviewHistoryCursor() = %+v, %t", decoded, ok)
	}
	if _, ok := handler.decodeReviewHistoryCursor("actor-b", encoded); ok {
		t.Fatal("cursor was accepted for another Actor")
	}
	otherHandler := &HTTPHandler{
		reviewCursorKey: []byte("fedcba9876543210fedcba9876543210"),
	}
	if _, ok := otherHandler.decodeReviewHistoryCursor(
		"actor-a",
		encoded,
	); ok {
		t.Fatal("cursor was accepted with another signing key")
	}
	tampered := "A" + encoded[1:]
	if _, ok := handler.decodeReviewHistoryCursor(
		"actor-a",
		tampered,
	); ok {
		t.Fatal("tampered cursor was accepted")
	}
	if _, ok := handler.decodeReviewHistoryCursor(
		"actor-a",
		encoded+"=",
	); ok {
		t.Fatal("non-canonical padded cursor was accepted")
	}
}

func TestVoiceHTTPReviewHistoryResponseBudget(t *testing.T) {
	t.Run("maximum legal 50 item page stays below 768 KiB", func(t *testing.T) {
		router := voiceHistoryTestRouter(
			t,
			maximumVoiceHistoryPage(t),
		)
		response := voiceHTTPRequest(
			t,
			router,
			http.MethodGet,
			"/v1/formal-reviews?limit=50",
			nil,
			nil,
		)
		if response.Code != http.StatusOK {
			t.Fatalf(
				"maximum history status = %d, body = %s",
				response.Code,
				response.Body,
			)
		}
		if response.Body.Len() >= maxReviewHistoryBody {
			t.Fatalf(
				"maximum history bytes = %d, limit = %d",
				response.Body.Len(),
				maxReviewHistoryBody,
			)
		}
		items := decodeVoiceJSONObject(t, response)["items"].([]any)
		if len(items) != 50 {
			t.Fatalf("maximum history item count = %d", len(items))
		}
		root := decodeVoiceJSONObject(t, response)
		cursor, ok := root["next_cursor"].(string)
		if !ok || cursor == "" {
			t.Fatalf("maximum history omitted next_cursor: %#v", root)
		}
		continuation := voiceHTTPRequest(
			t,
			router,
			http.MethodGet,
			"/v1/formal-reviews?limit=50&cursor="+cursor,
			nil,
			nil,
		)
		if continuation.Code != http.StatusOK {
			t.Fatalf(
				"maximum history continuation status = %d, body = %s",
				continuation.Code,
				continuation.Body,
			)
		}
		continuationRoot := decodeVoiceJSONObject(t, continuation)
		continuationItems := continuationRoot["items"].([]any)
		if len(continuationItems) != 1 {
			t.Fatalf(
				"maximum history continuation items = %d",
				len(continuationItems),
			)
		}
		if _, present := continuationRoot["next_cursor"]; present {
			t.Fatalf(
				"maximum history continuation exposed cursor: %#v",
				continuationRoot,
			)
		}
	})

	t.Run("oversized fake adapter page returns only safe 500", func(t *testing.T) {
		history := maximumVoiceHistoryPage(t)
		router := voiceHistoryTestRouterWithReader(
			t,
			oversizedVoiceHistoryReader{items: history},
		)
		response := voiceHTTPRequest(
			t,
			router,
			http.MethodGet,
			"/v1/formal-reviews?limit=50",
			nil,
			nil,
		)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf(
				"oversized history status = %d, body bytes = %d",
				response.Code,
				response.Body.Len(),
			)
		}
		root := decodeVoiceJSONObject(t, response)
		requireVoiceKeys(t, root, "error")
		failure := root["error"].(map[string]any)
		if failure["code"] != "internal_error" ||
			failure["retryable"] != true ||
			strings.Contains(response.Body.String(), `"items"`) ||
			strings.Contains(response.Body.String(), `"review_id"`) {
			t.Fatalf(
				"unsafe oversized history response bytes = %d",
				response.Body.Len(),
			)
		}
	})

	t.Run("hard cap rejects a fully encoded response before any product bytes", func(t *testing.T) {
		gin.SetMode(gin.ReleaseMode)
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		handler := &HTTPHandler{
			correlationID: func() string { return "corr_review_cap" },
		}
		handler.writeBoundedReviewJSON(context, gin.H{
			"items": []string{strings.Repeat("x", maxReviewHistoryBody)},
		})
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf(
				"hard-cap status = %d, body bytes = %d",
				recorder.Code,
				recorder.Body.Len(),
			)
		}
		root := decodeVoiceJSONObject(t, recorder)
		requireVoiceKeys(t, root, "error")
		if strings.Contains(recorder.Body.String(), strings.Repeat("x", 64)) ||
			strings.Contains(recorder.Body.String(), `"items"`) {
			t.Fatalf(
				"hard-cap response leaked product bytes; response bytes = %d",
				recorder.Body.Len(),
			)
		}
	})
}

func TestVoiceHTTPRejectsOverBudgetReviewResultAdapterAsInternalError(
	t *testing.T,
) {
	item := completedVoiceHistoryReview(
		"20000000-0000-4000-8000-000000000001",
		time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC),
		80,
	)
	item.Result = maximumVoiceReviewResult(t)
	item.Result.Summary = "<" + item.Result.Summary[1:]
	encoded, err := json.Marshal(item.Result)
	if err != nil {
		t.Fatalf("marshal over-budget fake Result: %v", err)
	}
	if len(encoded) <= maxVoiceReviewResultJSONBytes {
		t.Fatalf(
			"fake Result bytes = %d, want over %d",
			len(encoded),
			maxVoiceReviewResultJSONBytes,
		)
	}
	response := voiceHTTPRequest(
		t,
		voiceHistoryTestRouter(t, []VoiceSessionReview{item}),
		http.MethodGet,
		"/v1/formal-reviews?limit=1",
		nil,
		nil,
	)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf(
			"invalid adapter history status = %d, body = %s",
			response.Code,
			response.Body,
		)
	}
	root := decodeVoiceJSONObject(t, response)
	requireVoiceKeys(t, root, "error")
	if strings.Contains(response.Body.String(), `"items"`) ||
		strings.Contains(response.Body.String(), strings.Repeat("s", 64)) {
		t.Fatalf("invalid adapter data leaked: %s", response.Body)
	}
}

func TestVoiceHTTPCapacityErrorIsStableAndRetryable(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	handler := &HTTPHandler{
		correlationID: func() string { return "corr_capacity" },
	}
	handler.writeVoiceError(context, conversation.ErrVoiceRoundCapacity)
	if recorder.Code != http.StatusServiceUnavailable ||
		recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf(
			"capacity response = %d %#v",
			recorder.Code,
			recorder.Header(),
		)
	}
	failure := decodeVoiceJSONObject(t, recorder)["error"].(map[string]any)
	if failure["code"] != "voice_capacity_exhausted" ||
		failure["retryable"] != true {
		t.Fatalf("capacity failure = %#v", failure)
	}
}

func TestVoiceHTTPProcessingErrorIsStableAndRetryable(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	handler := &HTTPHandler{
		correlationID: func() string { return "corr_processing" },
	}
	handler.writeVoiceError(context, conversation.ErrVoiceRoundProcessing)
	if recorder.Code != http.StatusConflict ||
		recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf(
			"processing response = %d %#v",
			recorder.Code,
			recorder.Header(),
		)
	}
	failure := decodeVoiceJSONObject(t, recorder)["error"].(map[string]any)
	if failure["code"] != "resource_processing" ||
		failure["retryable"] != true {
		t.Fatalf("processing failure = %#v", failure)
	}
}

func TestVoiceHTTPReadDeadlineInterruptsStalledUpload(t *testing.T) {
	conversations := newAgentVoiceConversation(3)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	reading := &readingVoiceConversation{
		agentVoiceConversation: conversations,
	}
	orchestrator, err := NewVoiceRoundOrchestrator(
		reading,
		practice,
		reviews,
	)
	if err != nil {
		t.Fatalf("new voice orchestrator: %v", err)
	}
	voice := newVoiceSessionTestApplication(
		t,
		conversations,
		practice,
		reviews,
		orchestrator,
	)
	handler, err := NewHTTPHandlerWithRunsAndVoice(
		voiceHTTPApplication{},
		nil,
		voice,
		voiceHTTPMatters{},
		voiceHTTPAuthenticator{},
		func() string { return "corr_voice_timeout" },
		VoiceHTTPOptions{
			AudioReadTimeout: 100 * time.Millisecond,
			ReviewHistoryCursorKey: testVoiceHTTPOptions().
				ReviewHistoryCursorKey,
		},
	)
	if err != nil {
		t.Fatalf("new voice HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()

	reader, writer := io.Pipe()
	request, err := http.NewRequest(
		http.MethodPost,
		server.URL+
			"/v1/voice-practice-sessions/session-1/questions/question-1/transcription-candidates",
		reader,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer voice-token-a")
	request.Header.Set("Content-Type", "audio/wav")
	request.Header.Set("Idempotency-Key", "stalled-upload-1")
	result := make(chan *http.Response, 1)
	failures := make(chan error, 1)
	go func() {
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			failures <- requestErr
			return
		}
		result <- response
	}()

	select {
	case response := <-result:
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("stalled upload status = %d", response.StatusCode)
		}
	case err := <-failures:
		// A read deadline may close the client connection before a JSON error
		// can be written. Either outcome proves the stalled body was bounded.
		if !errors.Is(err, io.ErrUnexpectedEOF) &&
			!strings.Contains(err.Error(), "timeout") {
			t.Fatalf("stalled upload client error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled voice upload was not interrupted by read deadline")
	}
	_ = writer.Close()
}

func TestVoiceHTTPTTSFailureKeepsTextQuestionAvailable(t *testing.T) {
	conversations := newAgentVoiceConversation(1)
	conversations.speech = conversation.QuestionSpeech{
		Text: "What happened next?",
		Failure: &conversation.SafeProcessingAttempt{
			Operation: ai.SpeechOperationSynthesis,
			Kind:      ai.ErrorQuotaExhausted,
			Retryable: false,
		},
	}
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)
	voice := newVoiceSessionTestApplication(
		t,
		conversations,
		practice,
		reviews,
		orchestrator,
	)
	handler, err := NewHTTPHandlerWithRunsAndVoice(
		voiceHTTPApplication{},
		nil,
		voice,
		voiceHTTPMatters{},
		voiceHTTPAuthenticator{},
		func() string { return "corr_voice" },
		testVoiceHTTPOptions(),
	)
	if err != nil {
		t.Fatalf("new voice HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)

	speech := voiceHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/voice-questions/question-next/speech",
		nil,
		nil,
	)
	if speech.Code != http.StatusServiceUnavailable ||
		speech.Header().Get("Retry-After") != "" {
		t.Fatalf("speech response = %d %#v", speech.Code, speech.Header())
	}
	failure := decodeVoiceJSONObject(t, speech)["error"].(map[string]any)
	if failure["code"] != "quota_exhausted" ||
		failure["message"] != "The configured provider quota is exhausted." ||
		failure["retryable"] != false {
		t.Fatalf("quota failure = %#v", failure)
	}
	session := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/agent-threads/thread-1/voice-practice-sessions",
		nil,
		map[string]string{"Idempotency-Key": "session-start-1"},
	)
	state := decodeVoiceJSONObject(t, session)
	question, ok := state["current_question"].(map[string]any)
	if !ok || question["content"] != "What happened next?" {
		t.Fatalf("text Question after TTS failure = %#v", state)
	}
}

type voiceHTTPApplication struct {
	Application
}

func (voiceHTTPApplication) GetThread(
	_ context.Context,
	actor requestcontext.Actor,
	threadID string,
) (Thread, error) {
	if actor.UserID != "user-a" || threadID != "thread-1" {
		return Thread{}, ErrNotFound
	}
	return Thread{
		ID:             threadID,
		OwnerID:        actor.UserID,
		ActiveMatterID: "matter-1",
	}, nil
}

type voiceHTTPMatters struct {
	matter.Application
}

func (voiceHTTPMatters) ReadOwned(
	ctx context.Context,
	actor requestcontext.Actor,
	matterID string,
) (matter.Matter, error) {
	return voiceSessionTestMatters{}.ReadOwned(ctx, actor, matterID)
}

type voiceHTTPAuthenticator struct{}

func (voiceHTTPAuthenticator) AuthenticateSession(
	_ context.Context,
	token string,
) (requestcontext.Actor, error) {
	if token != "voice-token-a" {
		return requestcontext.Actor{}, identity.ErrAuthenticationRequired
	}
	return agentVoiceActor("a"), nil
}

func completedVoiceHistoryReview(
	id string,
	createdAt time.Time,
	score int,
) VoiceSessionReview {
	completedAt := createdAt.Add(time.Minute)
	return VoiceSessionReview{
		ID:                    id,
		SessionID:             "session-" + id,
		Status:                "completed",
		ImplementationVersion: "review-v1",
		SourceTurnID:          "turn-" + id,
		SourceTurnVersion:     "conversation-turn:evidence-v1",
		Result: &VoiceReviewResult{
			OverallScore: score,
			Summary:      "Server-owned review history.",
			Conclusions: []VoiceReviewConclusion{{
				Key:        "clarity",
				Category:   "clarity",
				Message:    "Clear response.",
				Suggestion: "Add one concrete outcome.",
			}},
		},
		CreatedAt:   createdAt,
		UpdatedAt:   completedAt,
		CompletedAt: &completedAt,
	}
}

func voiceHistoryTestRouter(
	t *testing.T,
	history []VoiceSessionReview,
) http.Handler {
	t.Helper()
	reviews := newAgentVoiceReview()
	return voiceHistoryTestRouterWithReader(
		t,
		voiceSessionTestReviews{
			reviews: reviews,
			history: history,
		},
	)
}

func voiceHistoryTestRouterWithReader(
	t *testing.T,
	reader VoiceReviewReader,
) http.Handler {
	t.Helper()
	conversations := newAgentVoiceConversation(3)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)
	voice := newVoiceSessionTestApplication(
		t,
		conversations,
		practice,
		reviews,
		orchestrator,
	)
	voice.reviews = reader
	handler, err := NewHTTPHandlerWithRunsAndVoice(
		voiceHTTPApplication{},
		nil,
		voice,
		voiceHTTPMatters{},
		voiceHTTPAuthenticator{},
		func() string { return "corr_review_budget" },
		testVoiceHTTPOptions(),
	)
	if err != nil {
		t.Fatalf("new voice HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}

func maximumVoiceHistoryPage(
	t *testing.T,
) []VoiceSessionReview {
	t.Helper()
	result := maximumVoiceReviewResult(t)
	history := make([]VoiceSessionReview, 51)
	escapedMetadata := strings.Repeat(
		"<",
		maxVoiceReviewMetadataUTF8Bytes,
	)
	baseCreatedAt := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	for index := range history {
		createdAt := baseCreatedAt.Add(-time.Duration(index) * time.Second)
		completedAt := createdAt.Add(time.Millisecond)
		id := fmt.Sprintf(
			"20000000-0000-4000-8000-%012d",
			50-index,
		)
		history[index] = VoiceSessionReview{
			ID:                    id,
			SessionID:             escapedMetadata,
			Status:                "completed",
			ImplementationVersion: escapedMetadata,
			SourceTurnID:          escapedMetadata,
			SourceTurnVersion:     "conversation-turn:evidence-v1",
			Result:                result,
			CreatedAt:             createdAt,
			UpdatedAt:             completedAt,
			CompletedAt:           &completedAt,
		}
	}
	return history
}

type oversizedVoiceHistoryReader struct {
	items []VoiceSessionReview
}

func (reader oversizedVoiceHistoryReader) GetReview(
	context.Context,
	requestcontext.Actor,
	string,
) (VoiceSessionReview, error) {
	return VoiceSessionReview{}, ErrNotFound
}

func (reader oversizedVoiceHistoryReader) ListReviews(
	context.Context,
	requestcontext.Actor,
	VoiceReviewHistoryQuery,
) (VoiceReviewHistoryPage, error) {
	return VoiceReviewHistoryPage{Items: reader.items}, nil
}

func voiceHTTPRequest(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
	body []byte,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer voice-token-a")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeVoiceJSONObject(
	t *testing.T,
	response *httptest.ResponseRecorder,
) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	return result
}

func requireVoiceKeys(
	t *testing.T,
	value map[string]any,
	expected ...string,
) {
	t.Helper()
	actual := make([]string, 0, len(value))
	for key := range value {
		actual = append(actual, key)
	}
	slices.Sort(actual)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		t.Fatalf("keys = %v, want %v; value = %#v", actual, expected, value)
	}
}

type readingVoiceConversation struct {
	*agentVoiceConversation
}

func (conversationPort *readingVoiceConversation) Transcribe(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	command conversation.TranscribeVoiceCommand,
) (conversation.TranscriptionCandidate, error) {
	if _, err := io.ReadAll(command.Audio); err != nil {
		return conversation.TranscriptionCandidate{},
			conversation.ErrVoiceRoundInvalid
	}
	return conversation.TranscriptionCandidate{
		ID:                      "candidate-read",
		SessionID:               command.SessionID,
		QuestionID:              command.QuestionID,
		AddresseeParticipantIDs: []string{"candidate-a"},
		RespondentParticipantID: "candidate-a",
		TranscriptID:            "transcript-read",
		EvidenceVersion:         1,
		Transcript:              "read",
		CreatedAt:               time.Now().UTC(),
	}, nil
}
