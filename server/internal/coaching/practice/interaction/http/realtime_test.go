package voicehttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func TestPracticeInteractionHTTPUsesSeparateReadTimeouts(t *testing.T) {
	handler, err := NewHandler(
		&practiceRealtimeHTTPApplication{},
		Options{},
		nil,
	)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	if handler.realtimeReadTimeout != 15*time.Second ||
		handler.recordedReadTimeout != 60*time.Second {
		t.Fatalf(
			"read timeouts = realtime %s, recorded %s",
			handler.realtimeReadTimeout,
			handler.recordedReadTimeout,
		)
	}
}

func TestPracticeRealtimeVoiceInputStreamsBeforeFinishAndPersistsCandidate(
	t *testing.T,
) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	question := practice.Question{
		ID:        "question-1",
		SessionID: "session-1",
		Content:   "Tell me about yourself.",
	}
	application := &practiceRealtimeHTTPApplication{
		state: practiceinteraction.SessionState{
			Session:  practiceinteraction.Session{ID: "session-1"},
			Question: &question,
		},
		candidate: practiceinteraction.TranscriptionCandidate{
			ID:                      "candidate-1",
			SessionID:               "session-1",
			QuestionID:              "question-1",
			RespondentParticipantID: "learner-1",
			TranscriptID:            "transcript-1",
			EvidenceVersion:         1,
			Transcript:              "A complete practice answer.",
			CreatedAt:               now,
		},
	}
	server := httptest.NewServer(newPracticeRealtimeHTTPRouter(t, application))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/v1/practice-sessions/session-1/questions/question-1/" +
		"transcription-candidates/realtime"
	connection, response, err := (&websocket.Dialer{
		Subprotocols: []string{practiceVoiceInputWebSocketProtocol},
	}).Dial(
		endpoint,
		http.Header{"Authorization": []string{"Bearer voice-token-a"}},
	)
	if err != nil {
		if response != nil {
			t.Fatalf("dial status = %d: %v", response.StatusCode, err)
		}
		t.Fatalf("dial realtime Practice Interaction: %v", err)
	}
	defer connection.Close()
	if connection.Subprotocol() != practiceVoiceInputWebSocketProtocol {
		t.Fatalf("subprotocol = %q", connection.Subprotocol())
	}
	if err := connection.WriteJSON(map[string]any{
		"type":            "start",
		"idempotency_key": "practice-voice-realtime-1",
		"sample_rate":     16_000,
	}); err != nil {
		t.Fatalf("write start: %v", err)
	}
	type realtimeEvent struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	readEvent := func() realtimeEvent {
		t.Helper()
		_, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			t.Fatalf("read realtime event: %v", readErr)
		}
		var event realtimeEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		return event
	}
	events := []realtimeEvent{readEvent()}
	if err := connection.WriteMessage(
		websocket.BinaryMessage,
		make([]byte, 3_200),
	); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set live update deadline: %v", err)
	}
	events = append(events, readEvent())
	if events[1].Type != "transcription.updated" {
		t.Fatalf("event before finish = %#v", events[1])
	}
	if err := connection.WriteJSON(map[string]string{"type": "finish"}); err != nil {
		t.Fatalf("write finish: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set completion deadline: %v", err)
	}
	for len(events) < 4 {
		events = append(events, readEvent())
	}
	wantTypes := []string{
		"transcription.started",
		"transcription.updated",
		"transcription.updated",
		"candidate.ready",
	}
	for index, want := range wantTypes {
		if events[index].Type != want {
			t.Fatalf("events = %#v", events)
		}
	}
	var ready struct {
		Candidate map[string]any `json:"candidate"`
	}
	if err := json.Unmarshal(events[3].Data, &ready); err != nil {
		t.Fatalf("decode ready candidate: %v", err)
	}
	if ready.Candidate["candidate_id"] != "candidate-1" ||
		ready.Candidate["transcript"] != "A complete practice answer." ||
		application.streamSessionID != "session-1" ||
		application.streamQuestionID != "question-1" ||
		application.streamKey != "practice-voice-realtime-1" ||
		application.streamBytes != 3_200 ||
		application.streamSampleRate != 16_000 {
		t.Fatalf("ready = %#v, application = %#v", ready, application)
	}
}

func TestPracticeRealtimeVoiceInputRejectsUnauthorizedOrNonCurrentQuestion(
	t *testing.T,
) {
	question := practice.Question{ID: "question-1", SessionID: "session-1"}
	application := &practiceRealtimeHTTPApplication{
		state: practiceinteraction.SessionState{
			Session:  practiceinteraction.Session{ID: "session-1"},
			Question: &question,
		},
	}
	server := httptest.NewServer(newPracticeRealtimeHTTPRouter(t, application))
	defer server.Close()
	base := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/v1/practice-sessions/session-1/questions/"
	for _, test := range []struct {
		name       string
		questionID string
		token      string
		wantStatus int
	}{
		{name: "unauthorized", questionID: "question-1", wantStatus: http.StatusUnauthorized},
		{name: "non-current", questionID: "question-2", token: "voice-token-a", wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{}
			if test.token != "" {
				header.Set("Authorization", "Bearer "+test.token)
			}
			_, response, err := (&websocket.Dialer{
				Subprotocols: []string{practiceVoiceInputWebSocketProtocol},
			}).Dial(
				base+test.questionID+"/transcription-candidates/realtime",
				header,
			)
			if err == nil || response == nil || response.StatusCode != test.wantStatus {
				t.Fatalf("dial response = %#v, err = %v", response, err)
			}
			_ = response.Body.Close()
		})
	}
}

func TestPracticeQuestionSpeechStreamsTrustedQuestionPCM(t *testing.T) {
	application := &practiceRealtimeHTTPApplication{
		questionText: "Could you introduce yourself?",
	}
	synthesizer := &practiceQuestionSpeechSynthesizer{}
	server := httptest.NewServer(
		newPracticeRealtimeHTTPRouterWithOptions(
			t,
			application,
			Options{RealtimeSpeech: synthesizer},
		),
	)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/v1/practice-questions/question-1/speech/realtime"
	connection, response, err := (&websocket.Dialer{
		Subprotocols: []string{practiceQuestionSpeechWebSocketProtocol},
	}).Dial(
		endpoint,
		http.Header{"Authorization": []string{"Bearer voice-token-a"}},
	)
	if err != nil {
		if response != nil {
			t.Fatalf("dial status = %d: %v", response.StatusCode, err)
		}
		t.Fatalf("dial realtime question speech: %v", err)
	}
	defer connection.Close()
	_, ready, err := connection.ReadMessage()
	if err != nil || !strings.Contains(string(ready), `"type":"stream.ready"`) {
		t.Fatalf("ready = %q, err = %v", ready, err)
	}
	messageType, audio, err := connection.ReadMessage()
	if err != nil || messageType != websocket.BinaryMessage ||
		string(audio) != "\x01\x02\x03\x04" {
		t.Fatalf("audio = (%d, %v), err = %v", messageType, audio, err)
	}
	_, completed, err := connection.ReadMessage()
	if err != nil || !strings.Contains(string(completed), `"type":"stream.completed"`) {
		t.Fatalf("completed = %q, err = %v", completed, err)
	}
	if application.questionID != "question-1" ||
		application.questionActor != "user-a" ||
		synthesizer.text != application.questionText {
		t.Fatalf("application = %#v, speech text = %q", application, synthesizer.text)
	}
}

func TestPracticePromptSpeechStreamsBoundedClientText(t *testing.T) {
	application := &practiceRealtimeHTTPApplication{}
	synthesizer := &practiceQuestionSpeechSynthesizer{}
	server := httptest.NewServer(
		newPracticeRealtimeHTTPRouterWithOptions(
			t,
			application,
			Options{RealtimeSpeech: synthesizer},
		),
	)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/v1/practice-sessions/session-a/speech/realtime"
	connection, response, err := (&websocket.Dialer{
		Subprotocols: []string{practiceQuestionSpeechWebSocketProtocol},
	}).Dial(
		endpoint,
		http.Header{"Authorization": []string{"Bearer voice-token-a"}},
	)
	if err != nil {
		if response != nil {
			t.Fatalf("dial status = %d: %v", response.StatusCode, err)
		}
		t.Fatalf("dial realtime prompt speech: %v", err)
	}
	defer connection.Close()
	if _, _, err := connection.ReadMessage(); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if err := connection.WriteJSON(gin.H{
		"type": "speak", "text": "  Try this answer.  ",
	}); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	messageType, audio, err := connection.ReadMessage()
	if err != nil || messageType != websocket.BinaryMessage ||
		string(audio) != "\x01\x02\x03\x04" {
		t.Fatalf("audio = (%d, %v), err = %v", messageType, audio, err)
	}
	if _, completed, err := connection.ReadMessage(); err != nil ||
		!strings.Contains(string(completed), `"type":"stream.completed"`) {
		t.Fatalf("completed = %q, err = %v", completed, err)
	}
	if synthesizer.text != "Try this answer." || application.speechSessionID != "session-a" {
		t.Fatalf("speech text = %q, session = %q", synthesizer.text, application.speechSessionID)
	}
}

func TestPracticeRealtimeFailureUsesStableSafeCategory(t *testing.T) {
	providerFailure := practiceinteraction.NewProviderError(
		practiceinteraction.ProviderOperationTranscription,
		practiceinteraction.ProviderErrorAuthentication,
		"private-provider-request",
		errors.New("private provider payload"),
	)
	for _, test := range []struct {
		name          string
		err           error
		wantKind      string
		wantRetryable bool
	}{
		{
			name:     "provider authentication",
			err:      providerFailure,
			wantKind: "authentication",
		},
		{
			name:          "live reservation",
			err:           practiceinteraction.ErrVoiceRoundProcessing,
			wantKind:      "processing",
			wantRetryable: true,
		},
		{
			name:     "idempotency conflict",
			err:      practiceinteraction.ErrVoiceRoundConflict,
			wantKind: "idempotency_conflict",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			kind, retryable := practiceRealtimeFailure(test.err)
			if kind != test.wantKind || retryable != test.wantRetryable {
				t.Fatalf("failure = (%q, %t)", kind, retryable)
			}
		})
	}
}

func newPracticeRealtimeHTTPRouter(
	t *testing.T,
	application Application,
) http.Handler {
	return newPracticeRealtimeHTTPRouterWithOptions(t, application, Options{})
}

func newPracticeRealtimeHTTPRouterWithOptions(
	t *testing.T,
	application Application,
	options Options,
) http.Handler {
	t.Helper()
	handler, err := NewHandler(
		application,
		options,
		httpresponse.NewRenderer(
			func() string { return "corr_practice_interaction_realtime" },
		),
	)
	if err != nil {
		t.Fatalf("new Practice Interaction HTTP handler: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "Bearer voice-token-a" {
			actor := requestcontext.Actor{
				UserID: "user-a", SessionID: "auth-session-a",
			}
			c.Request = c.Request.WithContext(
				requestcontext.WithActor(c.Request.Context(), actor),
			)
		}
		c.Next()
	})
	handler.RegisterRoutes(router)
	return router
}

type practiceRealtimeHTTPApplication struct {
	state            practiceinteraction.SessionState
	candidate        practiceinteraction.TranscriptionCandidate
	streamSessionID  string
	streamQuestionID string
	streamKey        string
	streamBytes      int
	streamSampleRate int
	questionText     string
	questionID       string
	questionActor    string
	speechSessionID  string
}

func (application *practiceRealtimeHTTPApplication) SessionSynthesisProfile(
	_ context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (practiceinteraction.SynthesisProfile, error) {
	if actor.UserID != "user-a" || sessionID != "session-a" {
		return practiceinteraction.SynthesisProfile{}, practiceinteraction.ErrNotFound
	}
	application.speechSessionID = sessionID
	return practiceinteraction.SynthesisProfile{Provider: "qianwen", ProviderProfile: "qianwen_default", Model: "model", VoiceID: "voice", Locale: "en-US"}, nil
}

func (application *practiceRealtimeHTTPApplication) QuestionSynthesis(
	_ context.Context,
	actor requestcontext.Actor,
	questionID string,
) (practiceinteraction.QuestionSynthesisInput, error) {
	if actor.UserID != "user-a" || questionID != "question-1" {
		return practiceinteraction.QuestionSynthesisInput{}, practiceinteraction.ErrNotFound
	}
	application.questionID = questionID
	application.questionActor = actor.UserID
	return practiceinteraction.QuestionSynthesisInput{
		Text:    application.questionText,
		Profile: practiceinteraction.SynthesisProfile{Provider: "qianwen", ProviderProfile: "qianwen_default", Model: "model", VoiceID: "voice", Locale: "en-US"},
	}, nil
}

func (application *practiceRealtimeHTTPApplication) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (practiceinteraction.SessionState, error) {
	return application.state, nil
}

func (application *practiceRealtimeHTTPApplication) Resume(
	_ context.Context,
	actor requestcontext.Actor,
	_ string,
) (practiceinteraction.SessionState, error) {
	if actor.UserID != "user-a" {
		return practiceinteraction.SessionState{}, practiceinteraction.ErrNotFound
	}
	return application.state, nil
}

func (application *practiceRealtimeHTTPApplication) Transcribe(
	context.Context,
	requestcontext.Actor,
	practiceinteraction.TranscribeVoiceCommand,
) (practiceinteraction.TranscriptionCandidate, error) {
	return application.candidate, nil
}

func (application *practiceRealtimeHTTPApplication) TranscribeStream(
	ctx context.Context,
	_ requestcontext.Actor,
	command practiceinteraction.TranscribeVoiceStreamCommand,
	observer practiceinteraction.TranscriptionObserver,
) (practiceinteraction.TranscriptionCandidate, error) {
	firstFrame := make([]byte, 3_200)
	if _, err := io.ReadFull(command.PCM, firstFrame); err != nil {
		return practiceinteraction.TranscriptionCandidate{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		practiceinteraction.TranscriptionUpdate{Transcript: "A complete"},
	); err != nil {
		return practiceinteraction.TranscriptionCandidate{}, err
	}
	remainder, err := io.ReadAll(command.PCM)
	if err != nil {
		return practiceinteraction.TranscriptionCandidate{}, err
	}
	application.streamSessionID = command.SessionID
	application.streamQuestionID = command.QuestionID
	application.streamKey = command.IdempotencyKey
	application.streamBytes = len(firstFrame) + len(remainder)
	application.streamSampleRate = command.SampleRate
	if err := observer.OnTranscriptionUpdate(
		ctx,
		practiceinteraction.TranscriptionUpdate{
			Transcript: "A complete practice answer.",
			Final:      true,
		},
	); err != nil {
		return practiceinteraction.TranscriptionCandidate{}, err
	}
	return application.candidate, nil
}

func (application *practiceRealtimeHTTPApplication) SubmitText(
	context.Context,
	requestcontext.Actor,
	practiceinteraction.SubmitTextAnswerCommand,
) (practiceinteraction.SessionState, error) {
	return application.state, nil
}

func (application *practiceRealtimeHTTPApplication) Confirm(
	context.Context,
	requestcontext.Actor,
	practiceinteraction.ConfirmVoiceTurnCommand,
) (practiceinteraction.SessionState, error) {
	return application.state, nil
}

func (application *practiceRealtimeHTTPApplication) QuestionSpeech(
	context.Context,
	requestcontext.Actor,
	string,
) (practiceinteraction.QuestionSpeech, error) {
	return practiceinteraction.QuestionSpeech{}, nil
}

func (application *practiceRealtimeHTTPApplication) QuestionTranslation(
	context.Context,
	requestcontext.Actor,
	string,
) (practiceinteraction.QuestionTranslation, error) {
	return practiceinteraction.QuestionTranslation{}, nil
}

func (application *practiceRealtimeHTTPApplication) EnsureQuestionTip(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (practiceinteraction.QuestionTipResult, error) {
	return practiceinteraction.QuestionTipResult{}, nil
}

var _ Application = (*practiceRealtimeHTTPApplication)(nil)

type practiceQuestionSpeechSynthesizer struct {
	text string
}

func (synthesizer *practiceQuestionSpeechSynthesizer) OpenAssistantSpeech(
	_ context.Context,
	onAudio func([]byte) error,
) (agentconversation.AssistantSpeechSession, error) {
	return &practiceQuestionSpeechSession{
		synthesizer: synthesizer,
		onAudio:     onAudio,
	}, nil
}

func (synthesizer *practiceQuestionSpeechSynthesizer) OpenPracticeSpeech(
	_ context.Context,
	_ practiceinteraction.SynthesisProfile,
	onAudio func([]byte) error,
) (practiceinteraction.StreamingSpeechSession, error) {
	return &practiceQuestionSpeechSession{synthesizer: synthesizer, onAudio: onAudio}, nil
}

type practiceQuestionSpeechSession struct {
	synthesizer *practiceQuestionSpeechSynthesizer
	onAudio     func([]byte) error
}

func (session *practiceQuestionSpeechSession) AppendText(text string) error {
	session.synthesizer.text = text
	return session.onAudio([]byte{1, 2, 3, 4})
}

func (*practiceQuestionSpeechSession) Finish() error { return nil }

func (*practiceQuestionSpeechSession) Close() error { return nil }
