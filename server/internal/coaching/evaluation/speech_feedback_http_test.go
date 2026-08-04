package evaluation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSpeechFeedbackHTTPGetIsPrivateAndOmitsInapplicableStateFields(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "f475b521-a96f-44be-b447-8b85bed7e6e9",
		SessionID: "session-1",
	}
	reader := &speechFeedbackReaderStub{
		feedback: queuedSpeechFeedbackFixture(),
	}
	response := performSpeechFeedbackGet(t, reader, &actor)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{
		"scoreability_status",
		"gate_status",
		"reason_codes",
		"stable_failure",
		"completed_at",
	} {
		if _, exists := payload[absent]; exists {
			t.Fatalf("QUEUED response contains %q: %#v", absent, payload)
		}
	}
}

func TestSpeechFeedbackHTTPProtectsErrorsAndAuthentication(
	t *testing.T,
) {
	t.Parallel()
	response := performSpeechFeedbackGet(
		t,
		&speechFeedbackReaderStub{},
		nil,
	)
	if response.Code != http.StatusUnauthorized ||
		response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf(
			"unauthorized response = %d %#v",
			response.Code,
			response.Header(),
		)
	}
}

func TestSpeechFeedbackHTTPRejectsOversizedResponseBeforeWritingIt(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "f475b521-a96f-44be-b447-8b85bed7e6e9",
		SessionID: "session-1",
	}
	feedback := queuedSpeechFeedbackFixture()
	feedback.Items = make([]SpeechFeedbackItem, 300)
	for index := range feedback.Items {
		feedback.Items[index].Explanation = strings.Repeat("x", 2048)
	}
	response := performSpeechFeedbackGet(
		t,
		&speechFeedbackReaderStub{feedback: feedback},
		&actor,
	)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if response.Body.Len() >= maxSpeechFeedbackResponseBytes ||
		strings.Contains(response.Body.String(), strings.Repeat("x", 100)) {
		t.Fatal("oversized private resource was partially written")
	}
}

func performSpeechFeedbackGet(
	t *testing.T,
	reader SpeechFeedbackReader,
	actor *requestcontext.Actor,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewSpeechFeedbackHTTPHandler(reader)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.RegisterRoutes(router)
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/speech-feedback/729cdce7-4d33-418c-8497-d2932c651003",
		nil,
	)
	if actor != nil {
		request = request.WithContext(
			requestcontext.WithActor(request.Context(), *actor),
		)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

type speechFeedbackReaderStub struct {
	feedback SpeechFeedback
	err      error
}

func (reader *speechFeedbackReaderStub) Get(
	context.Context,
	requestcontext.Actor,
	string,
) (SpeechFeedback, error) {
	return reader.feedback, reader.err
}

func (*speechFeedbackReaderStub) StatusURLForConversationTurn(
	context.Context,
	requestcontext.Actor,
	string,
) (string, bool, error) {
	panic("not used")
}

func (*speechFeedbackReaderStub) StatusURLForAgentVoiceMessage(
	context.Context,
	requestcontext.Actor,
	string,
) (string, bool, error) {
	panic("not used")
}

func queuedSpeechFeedbackFixture() SpeechFeedback {
	now := time.Now().UTC()
	id := "729cdce7-4d33-418c-8497-d2932c651003"
	return SpeechFeedback{
		SpeechFeedbackID: id,
		Source: SpeechFeedbackSource{
			SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
			ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
			MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
			TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
			CandidateVersion:     1,
		},
		FeedbackStatus:     SpeechFeedbackQueued,
		SchemaVersion:      SpeechFeedbackSchemaVersion,
		StrategyRef:        SpeechFeedbackStrategyRef,
		PipelineVersion:    SpeechFeedbackPipelineVersion,
		Items:              []SpeechFeedbackItem{},
		AcousticAssessment: unavailableSpeechFeedbackAcoustics(),
		StatusURL:          SpeechFeedbackStatusURL(id),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}
