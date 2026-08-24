package qianwen

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type providerRecorder struct {
	observations []providerobservability.Observation
}

func (recorder *providerRecorder) Record(observation providerobservability.Observation) {
	recorder.observations = append(recorder.observations, observation)
}

func TestTextClientRecordsSuccessUsageAndSanitizedTransportFailure(t *testing.T) {
	const sensitiveError = "private-user@example.com secret provider payload"

	t.Run("success", func(t *testing.T) {
		recorder := &providerRecorder{}
		generator := observedTextGenerator(t, recorder, doerFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{
				"id":"chatcmpl-observed","model":"qwen3.5-flash",
				"choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant","content":"answer"}}],
				"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}
			}`), nil
		}))
		if _, err := generator.Generate(context.Background(), validRequest()); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		observation := onlyObservation(t, recorder)
		if observation.Provider != providerobservability.ProviderQianwen ||
			observation.Capability != providerobservability.CapabilityTextGeneration ||
			observation.ErrorKind != providerobservability.ErrorNone ||
			observation.Usage.Tokens != 16 || observation.Duration < 0 {
			t.Fatalf("observation = %#v", observation)
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		recorder := &providerRecorder{}
		generator := observedTextGenerator(t, recorder, doerFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New(sensitiveError)
		}))
		if _, err := generator.Generate(context.Background(), validRequest()); err == nil {
			t.Fatal("Generate error = nil")
		}
		observation := onlyObservation(t, recorder)
		if observation.ErrorKind != providerobservability.ErrorProviderUnavailable ||
			observation.Usage != (providerobservability.Usage{}) {
			t.Fatalf("observation = %#v", observation)
		}
		if strings.Contains(fmt.Sprintf("%#v", observation), sensitiveError) {
			t.Fatal("observation retained provider error text")
		}
	})

	t.Run("reported usage survives local response validation", func(t *testing.T) {
		recorder := &providerRecorder{}
		generator := observedTextGenerator(t, recorder, doerFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{
				"id":"chatcmpl-observed","model":"qwen3.5-flash",
				"choices":[{"finish_reason":"stop","index":0,"message":{"role":"tool","content":"invalid"}}],
				"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}
			}`), nil
		}))
		if _, err := generator.Generate(context.Background(), validRequest()); err == nil {
			t.Fatal("Generate error = nil")
		}
		observation := onlyObservation(t, recorder)
		if observation.ErrorKind != providerobservability.ErrorInvalidResponse ||
			observation.Usage.Tokens != 16 {
			t.Fatalf("observation = %#v", observation)
		}
	})
}

func TestSpeechCallMapsTimeoutAndFixedUsage(t *testing.T) {
	recorder := &providerRecorder{}
	recordSpeechCall(
		recorder,
		providerobservability.CapabilitySpeechRecognition,
		time.Now(),
		protocol.SpeechUsage{TotalTokens: 9, AudioSeconds: 4},
		protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorTimeout,
			0,
			"",
			"",
			context.DeadlineExceeded,
		),
		0,
	)
	observation := onlyObservation(t, recorder)
	if observation.ErrorKind != providerobservability.ErrorTimeout ||
		observation.Usage.Tokens != 9 || observation.Usage.AudioSeconds != 4 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestObservedAssistantSpeechSessionRecordsCharactersOnce(t *testing.T) {
	recorder := &providerRecorder{}
	session := &observedAssistantSpeechSession{
		delegate: &assistantSpeechSessionStub{}, recorder: recorder,
		startedAt: time.Now(),
	}
	if err := session.AppendText("你好"); err != nil {
		t.Fatalf("AppendText: %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	observation := onlyObservation(t, recorder)
	if observation.ErrorKind != providerobservability.ErrorNone ||
		observation.Usage.Characters != 2 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestObservedAssistantSpeechSessionCloseBeforeFinishIsCancelled(t *testing.T) {
	recorder := &providerRecorder{}
	session := &observedAssistantSpeechSession{
		delegate: &assistantSpeechSessionStub{closeErr: errors.New("private provider close detail")},
		recorder: recorder, startedAt: time.Now(),
	}
	if err := session.Close(); err == nil {
		t.Fatal("Close error = nil")
	}
	observation := onlyObservation(t, recorder)
	if observation.ErrorKind != providerobservability.ErrorCancelled {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestObservedAssistantSpeechSessionRecordsConsumerFailureAsCancelled(t *testing.T) {
	cause := errors.New("client audio write failed")
	consume := downstreamSpeechConsumer(func([]byte) error { return cause })
	callbackErr := consume([]byte{1, 2, 3})
	if !errors.Is(callbackErr, cause) ||
		observedErrorKind(callbackErr) != providerobservability.ErrorCancelled {
		t.Fatalf("consumer error = %#v", callbackErr)
	}

	recorder := &providerRecorder{}
	session := &observedAssistantSpeechSession{
		delegate: &assistantSpeechSessionStub{finishErr: callbackErr},
		recorder: recorder, startedAt: time.Now(),
	}
	if err := session.AppendText("你好"); err != nil {
		t.Fatalf("AppendText: %v", err)
	}
	if err := session.Finish(); !errors.Is(err, cause) {
		t.Fatalf("Finish error = %v", err)
	}
	observation := onlyObservation(t, recorder)
	if observation.ErrorKind != providerobservability.ErrorCancelled ||
		observation.Usage.Characters != 0 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestObservedAssistantSpeechSessionKeepsCharactersAfterLocalValidationFailure(t *testing.T) {
	recorder := &providerRecorder{}
	finishErr := protocol.NewSpeechError(
		protocol.SpeechOperationSynthesis,
		protocol.ErrorInvalidResponse,
		200,
		"",
		"",
		errors.New("local final response validation"),
	)
	session := &observedAssistantSpeechSession{
		delegate: &assistantSpeechSessionStub{finishErr: finishErr},
		recorder: recorder, startedAt: time.Now(),
	}
	if err := session.AppendText("你好"); err != nil {
		t.Fatalf("AppendText: %v", err)
	}
	if err := session.Finish(); !errors.Is(err, finishErr) {
		t.Fatalf("Finish error = %v", err)
	}
	observation := onlyObservation(t, recorder)
	if observation.ErrorKind != providerobservability.ErrorInvalidResponse ||
		observation.Usage.Characters != 2 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestObservedSynthesisCharactersSurviveLocalResponseValidation(t *testing.T) {
	invalidResponse := protocol.NewSpeechError(
		protocol.SpeechOperationSynthesis,
		protocol.ErrorInvalidResponse,
		200,
		"",
		"",
		errors.New("local response validation"),
	)
	if got := observedSynthesisCharacters("你好", invalidResponse); got != 2 {
		t.Fatalf("observedSynthesisCharacters() = %d, want 2", got)
	}
	invalidRequest := protocol.NewSpeechError(
		protocol.SpeechOperationSynthesis,
		protocol.ErrorInvalidRequest,
		0,
		"",
		"",
		errors.New("local request validation"),
	)
	if got := observedSynthesisCharacters("你好", invalidRequest); got != 0 {
		t.Fatalf("invalid request characters = %d, want 0", got)
	}
}

func observedTextGenerator(
	t *testing.T,
	recorder providerobservability.Recorder,
	doer httpDoer,
) *textClient {
	t.Helper()
	generator, err := newWithClient(TextConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "qwen3.5-flash", Timeout: time.Second, MaxOutputTokens: 512,
		Observer: recorder,
	}, "test-api-key", doer)
	if err != nil {
		t.Fatalf("newWithClient: %v", err)
	}
	return generator
}

func onlyObservation(
	t *testing.T,
	recorder *providerRecorder,
) providerobservability.Observation {
	t.Helper()
	if len(recorder.observations) != 1 {
		t.Fatalf("observations = %#v", recorder.observations)
	}
	return recorder.observations[0]
}

type assistantSpeechSessionStub struct {
	finishErr error
	closeErr  error
}

func (*assistantSpeechSessionStub) AppendText(string) error { return nil }
func (stub *assistantSpeechSessionStub) Finish() error      { return stub.finishErr }
func (stub *assistantSpeechSessionStub) Close() error       { return stub.closeErr }

var _ agentconversation.AssistantSpeechSession = (*assistantSpeechSessionStub)(nil)
