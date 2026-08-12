package ielts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type ieltsSpeechSynthesizerStub struct {
	text  string
	err   error
	audio *ieltsSpeechAudio
}

func (stub *ieltsSpeechSynthesizerStub) Synthesize(_ context.Context, text string) (platformmedia.ManagedAudioSource, error) {
	stub.text = text
	if stub.err != nil {
		return nil, stub.err
	}
	stub.audio = &ieltsSpeechAudio{data: []byte("RIFF-test-wave")}
	return stub.audio, nil
}

type ieltsSpeechAudio struct {
	data   []byte
	closed bool
}

func (audio *ieltsSpeechAudio) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(audio.data)), nil
}
func (audio *ieltsSpeechAudio) MediaType() string       { return platformmedia.ContentTypeWAV }
func (audio *ieltsSpeechAudio) Size() int64             { return int64(len(audio.data)) }
func (audio *ieltsSpeechAudio) Duration() time.Duration { return time.Second }
func (audio *ieltsSpeechAudio) SampleRate() int         { return 24_000 }
func (audio *ieltsSpeechAudio) Close() error {
	audio.closed = true
	return nil
}

func TestSpeechServiceUsesResolvedQuestionAndOwnedAnswerText(t *testing.T) {
	repository := &answerRepositoryStub{value: AnswerPreparation{
		ID:         "ielts_answer_0123456789abcdef0123456789abcdef",
		Status:     AnswerPreparationReady,
		SpeechText: "My prepared answer.",
	}}
	answers, err := NewAnswerPreparationService(repository, answerQuestionStub{}, &answerGeneratorStub{}, answerIDStub{})
	if err != nil {
		t.Fatal(err)
	}
	synthesizer := &ieltsSpeechSynthesizerStub{}
	service, err := NewSpeechService(answerQuestionStub{}, answers, synthesizer)
	if err != nil {
		t.Fatal(err)
	}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	reference := QuestionReference{BankID: "bank-1", Part: PracticeModePart1, SourceID: "teachers", QuestionPosition: 1}

	questionAudio, err := service.Question(context.Background(), actor, reference)
	if err != nil {
		t.Fatalf("Question: %v", err)
	}
	if synthesizer.text != "Do you enjoy music?" {
		t.Fatalf("question synthesis text = %q", synthesizer.text)
	}
	_ = questionAudio.Close()

	answerAudio, err := service.Answer(context.Background(), actor, repository.value.ID)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if synthesizer.text != "My prepared answer." {
		t.Fatalf("answer synthesis text = %q", synthesizer.text)
	}
	_ = answerAudio.Close()
}

func TestSpeechServiceRejectsNonReadyAnswerAndProviderFailure(t *testing.T) {
	repository := &answerRepositoryStub{value: AnswerPreparation{
		ID:     "ielts_answer_0123456789abcdef0123456789abcdef",
		Status: AnswerPreparationDraft,
	}}
	answers, err := NewAnswerPreparationService(repository, answerQuestionStub{}, &answerGeneratorStub{}, answerIDStub{})
	if err != nil {
		t.Fatal(err)
	}
	synthesizer := &ieltsSpeechSynthesizerStub{err: errors.New("provider failed")}
	service, err := NewSpeechService(answerQuestionStub{}, answers, synthesizer)
	if err != nil {
		t.Fatal(err)
	}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}

	if _, err := service.Answer(context.Background(), actor, repository.value.ID); !errors.Is(err, ErrSpeechNotFound) {
		t.Fatalf("non-ready answer error = %v", err)
	}
	if _, err := service.Question(context.Background(), actor, QuestionReference{BankID: "bank-1", Part: PracticeModePart1, SourceID: "teachers", QuestionPosition: 1}); !errors.Is(err, ErrSpeechUnavailable) {
		t.Fatalf("provider error = %v", err)
	}
}

func TestSpeechHTTPRequiresAuthenticationAndServesWAV(t *testing.T) {
	router, synthesizer := speechTestRouter(t, false)
	path := "/v1/ielts-speaking/question-banks/bank-1/PART_1/teachers/questions/1/speech"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.Code)
	}

	router, synthesizer = speechTestRouter(t, true)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != platformmedia.ContentTypeWAV {
		t.Fatalf("speech response = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	if response.Body.String() != "RIFF-test-wave" || synthesizer.audio == nil || !synthesizer.audio.closed {
		t.Fatalf("speech body or cleanup invalid: body=%q audio=%#v", response.Body.String(), synthesizer.audio)
	}
}

func speechTestRouter(t *testing.T, authenticated bool) (*gin.Engine, *ieltsSpeechSynthesizerStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
			c.Request = c.Request.WithContext(requestcontext.WithActor(c.Request.Context(), actor))
			c.Next()
		})
	}
	repository := &answerRepositoryStub{}
	answers, err := NewAnswerPreparationService(repository, answerQuestionStub{}, &answerGeneratorStub{}, answerIDStub{})
	if err != nil {
		t.Fatal(err)
	}
	synthesizer := &ieltsSpeechSynthesizerStub{}
	service, err := NewSpeechService(answerQuestionStub{}, answers, synthesizer)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewSpeechHTTPHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	handler.RegisterRoutes(router)
	return router, synthesizer
}
