package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

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
