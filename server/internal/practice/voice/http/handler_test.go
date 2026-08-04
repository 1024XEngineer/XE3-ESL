package voicehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/voice"
	"github.com/gin-gonic/gin"
)

func testVoiceHTTPOptions() Options {
	return Options{AudioReadTimeout: defaultReadTimeout}
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
	router := newVoiceHTTPTestRouter(
		t, voice, testVoiceHTTPOptions(),
	)

	start := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/practice-sessions/session-1/voice-activation",
		nil,
		map[string]string{"Idempotency-Key": "session-start-1"},
	)
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", start.Code, start.Body)
	}
	started := decodeVoiceJSONObject(t, start)
	if started["scene_family"] != "INTERVIEW" ||
		started["scene_model"] != "PROJECT_EXPERIENCE_DEEP_DIVE" {
		t.Fatalf(
			"frozen scenario identity = %q/%q",
			started["scene_family"],
			started["scene_model"],
		)
	}
	requireVoiceKeys(t, started,
		"current_question",
		"effective_turns",
		"practice_plan_id",
		"practice_session_id",
		"scene_id",
		"scene_version",
		"scene_model",
		"scene_family",
		"session_completed",
		"session_version",
		"turn_limit",
	)
	requireVoiceKeys(
		t,
		started["current_question"].(map[string]any),
		"addressee_participant_ids",
		"content",
		"practice_session_id",
		"question_id",
		"question_type",
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
			"practice_plan_id",
			"practice_session_id",
			"scene_id",
			"scene_version",
			"scene_model",
			"scene_family",
			"session_completed",
			"session_version",
			"turn_history",
			"turn_limit",
		)
		requireVoiceKeys(
			t,
			state["current_turn"].(map[string]any),
			"answer_text",
			"candidate_id",
			"counts_toward_effective_turn_limit",
			"effective_turns",
			"evidence_version",
			"practice_session_id",
			"question_id",
			"respondent_participant_id",
			"session_completed",
			"turn_id",
		)
		if _, found := state["review"]; found {
			t.Fatalf("completed confirmation blocked on Review: %#v", state)
		}
	}
}

func TestVoiceSessionStateResponseProjectsFullMockScenarioIdentity(
	t *testing.T,
) {
	response := SessionStateResponse(practicevoice.SessionState{
		Session: practicevoice.Session{
			SceneFamily: "EXAM",
			SceneModel:  "IELTS_SPEAKING_FULL_MOCK",
		},
	})
	if response["scene_family"] != "EXAM" ||
		response["scene_model"] != "IELTS_SPEAKING_FULL_MOCK" {
		t.Fatalf("full mock identity = %#v", response)
	}
}

func TestVoiceHTTPTextAnswerAdvancesWithoutAudioCandidateEndpoint(t *testing.T) {
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
	router := newVoiceHTTPTestRouter(
		t, voice, testVoiceHTTPOptions(),
	)

	start := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/practice-sessions/session-1/voice-activation",
		nil,
		map[string]string{"Idempotency-Key": "session-start-text"},
	)
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", start.Code, start.Body)
	}
	answer := voiceHTTPRequest(
		t,
		router,
		http.MethodPost,
		"/v1/voice-practice-sessions/session-1/questions/question-1/text-answers",
		[]byte(`{"answer_text":"I led the rollout and communicated the risk."}`),
		map[string]string{
			"Content-Type":    "application/json",
			"Idempotency-Key": "text-answer-1",
		},
	)
	if answer.Code != http.StatusOK {
		t.Fatalf("text answer status = %d, body = %s", answer.Code, answer.Body)
	}
	state := decodeVoiceJSONObject(t, answer)
	turn := state["current_turn"].(map[string]any)
	if turn["answer_text"] !=
		"I led the rollout and communicated the risk." {
		t.Fatalf("text turn = %#v", turn)
	}
	if _, found := turn["audio_asset_id"]; found {
		t.Fatalf("text turn exposed audio asset: %#v", turn)
	}
	if conversations.textSubmitCalls != 1 ||
		conversations.transcribeCalls != 0 {
		t.Fatalf(
			"text calls = %d, ASR calls = %d",
			conversations.textSubmitCalls,
			conversations.transcribeCalls,
		)
	}
}

func TestVoiceHTTPCapacityErrorIsStableAndRetryable(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler := &Handler{errors: httpresponse.NewRenderer(
		func() string { return "corr_capacity" },
	)}
	handler.write(context, mapError(conversation.ErrVoiceRoundCapacity))
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
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler := &Handler{errors: httpresponse.NewRenderer(
		func() string { return "corr_processing" },
	)}
	handler.write(context, mapError(conversation.ErrVoiceRoundProcessing))
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
	orchestrator, err := practicevoice.NewRoundOrchestrator(
		reading,
		practice,
		reviews,
		agentVoiceCompletionEvaluation{},
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
	router := newVoiceHTTPTestRouter(
		t,
		voice,
		Options{AudioReadTimeout: 100 * time.Millisecond},
	)
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
	router := newVoiceHTTPTestRouter(
		t, voice, testVoiceHTTPOptions(),
	)

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
		"/v1/practice-sessions/session-1/voice-activation",
		nil,
		map[string]string{"Idempotency-Key": "session-start-1"},
	)
	state := decodeVoiceJSONObject(t, session)
	question, ok := state["current_question"].(map[string]any)
	if !ok || question["content"] != "What happened next?" {
		t.Fatalf("text Question after TTS failure = %#v", state)
	}
}

func TestVoiceHTTPResumeUsesExplicitSession(t *testing.T) {
	conversations := newAgentVoiceConversation(3)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)
	sessions := &voiceHTTPRecordingSessionPort{
		session: practicevoice.Session{
			ID:           "session-1",
			PlanID:       "plan-1",
			SceneID:      "scene-1",
			SceneVersion: 1,
			SceneFamily:  "INTERVIEW",
			SceneModel:   "PROJECT_EXPERIENCE_DEEP_DIVE",
			Prompt: scene.ScenePrompt{
				PublicSceneBrief: "Discuss one project.",
				PracticeGoal:     "Explain decisions clearly.",
				UserRole:         "Candidate",
				AIRole:           "Technical interviewer",
				PersonaSummary:   "Professional and concise",
				FocusAreas:       []string{"clarity"},
				TurnBlueprints:   []string{"Ask about the project"},
			},
			SessionVersion:           1,
			TurnLimit:                3,
			Status:                   "in_progress",
			FacilitatorParticipantID: "participant-facilitator",
			LearnerParticipantID:     "participant-a",
		},
	}
	voice, err := practicevoice.NewSessionApplication(
		sessions,
		voiceSessionTestQuestions{},
		voiceSessionTestCheckpoints{conversations: conversations},
		orchestrator,
		voiceSessionTestReviews{reviews: reviews},
	)
	if err != nil {
		t.Fatalf("new Voice Session application: %v", err)
	}
	router := newVoiceHTTPTestRouter(t, voice, testVoiceHTTPOptions())

	response := voiceHTTPRequest(
		t,
		router,
		http.MethodGet,
		"/v1/practice-sessions/session-1/voice-state",
		nil,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("resume status = %d, body = %s", response.Code, response.Body)
	}
	if sessions.resumeCalls != 1 || sessions.resumeSessionID != "session-1" {
		t.Fatalf(
			"Resume calls/Session = %d/%q",
			sessions.resumeCalls,
			sessions.resumeSessionID,
		)
	}
}

type voiceHTTPRecordingSessionPort struct {
	session         practicevoice.Session
	resumeCalls     int
	resumeSessionID string
}

func (port *voiceHTTPRecordingSessionPort) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (practicevoice.Session, error) {
	return port.session, nil
}

func (port *voiceHTTPRecordingSessionPort) GetByID(
	_ context.Context,
	_ requestcontext.Actor,
	sessionID string,
) (practicevoice.Session, error) {
	port.resumeCalls++
	port.resumeSessionID = sessionID
	return port.session, nil
}

func newVoiceHTTPTestRouter(
	t *testing.T,
	application *practicevoice.SessionApplication,
	options Options,
) *gin.Engine {
	t.Helper()
	handler, err := NewHandler(
		application,
		options,
		httpresponse.NewRenderer(func() string { return "corr_voice" }),
	)
	if err != nil {
		t.Fatalf("new voice HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(requestcontext.WithActor(
			c.Request.Context(), agentVoiceActor("a"),
		))
		c.Next()
	})
	handler.RegisterRoutes(router)
	return router
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
