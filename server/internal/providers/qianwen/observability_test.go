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
	retries      int
}

func (recorder *providerRecorder) Record(observation providerobservability.Observation) {
	recorder.observations = append(recorder.observations, observation)
}

func (recorder *providerRecorder) RecordRetry(
	providerobservability.Provider,
	providerobservability.Capability,
) {
	recorder.retries++
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

type assistantSpeechSessionStub struct{}

func (*assistantSpeechSessionStub) AppendText(string) error { return nil }
func (*assistantSpeechSessionStub) Finish() error           { return nil }
func (*assistantSpeechSessionStub) Close() error            { return nil }

var _ agentconversation.AssistantSpeechSession = (*assistantSpeechSessionStub)(nil)
