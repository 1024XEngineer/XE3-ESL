package qianwen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

func TestTranscribeUsesDocumentedFunASRFlashContract(t *testing.T) {
	t.Parallel()

	const apiKey = "test-api-key"
	audio := []byte("validated-wav-bytes")
	var received asrRequest
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost ||
			request.URL.String() != "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation" {
			t.Fatalf("unexpected ASR request: %s %s", request.Method, request.URL)
		}
		if request.Header.Get(authorizationHeaderName) != "Bearer "+apiKey ||
			request.Header.Get("X-DashScope-SSE") != "disable" {
			t.Fatal("ASR headers do not match the synchronous contract")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode ASR request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"output":{
				"sentence":{"text":"I am preparing for an interview."},
				"text":"I am preparing for an interview."
			},
			"usage":{"duration":4},
			"request_id":"fun-asr-safe-1"
		}`), nil
	})
	recognizer := mustRecognizer(t, doer, apiKey)

	result, err := recognizer.Transcribe(
		context.Background(),
		ai.TranscriptionRequest{Audio: &asrTestAudio{data: audio}},
	)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if received.Model != "fun-asr-flash-2026-06-15" ||
		received.Parameters.Format != "wav" ||
		received.Parameters.SampleRate != "16000" ||
		len(received.Input.Messages) != 1 ||
		len(received.Input.Messages[0].Content) != 1 {
		t.Fatalf("unexpected ASR payload: %#v", received)
	}
	expectedDataURL := "data:audio/wav;base64," +
		base64.StdEncoding.EncodeToString(audio)
	if received.Input.Messages[0].Content[0].InputAudio.Data != expectedDataURL {
		t.Fatal("ASR audio was not encoded as the documented Data URL")
	}
	expected := ai.TranscriptionResult{
		ID:         "fun-asr-safe-1",
		Provider:   providerName,
		Model:      "fun-asr-flash-2026-06-15",
		Transcript: "I am preparing for an interview.",
		Usage: ai.SpeechUsage{
			AudioSeconds: 4,
		},
	}
	if result != expected {
		t.Fatalf("result = %#v, want %#v", result, expected)
	}
}

func TestTranscribeAcceptsEachDocumentedTranscriptLocation(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"top-level output text": `{
			"output":{"text":"Hello from the top level."},
			"request_id":"fun-asr-top"
		}`,
		"documented sentence text": `{
			"output":{"sentence":{"text":"Hello from the sentence."}},
			"request_id":"fun-asr-sentence"
		}`,
		"legacy nested sentence text": `{
			"output":{"output":{"sentence":{"text":"Hello from the nested sentence."}}},
			"request_id":"fun-asr-nested"
		}`,
	}
	for name, body := range tests {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recognizer := mustRecognizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			}), "test-api-key")
			result, err := recognizer.Transcribe(
				context.Background(),
				ai.TranscriptionRequest{Audio: &asrTestAudio{data: []byte("wav")}},
			)
			if err != nil {
				t.Fatalf("transcribe: %v", err)
			}
			if result.Transcript == "" {
				t.Fatal("documented transcript location was not parsed")
			}
		})
	}
}

func TestTranscribePrefersCumulativeTextAndMapsDurationUsage(t *testing.T) {
	t.Parallel()

	recognizer := mustRecognizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"output":{
				"text":"This is the first sentence. This is the second sentence.",
				"sentence":{"text":"This is the second sentence."}
			},
			"usage":{"duration":7},
			"request_id":"fun-asr-cumulative"
		}`), nil
	}), "test-api-key")
	result, err := recognizer.Transcribe(
		context.Background(),
		ai.TranscriptionRequest{Audio: &asrTestAudio{data: []byte("wav")}},
	)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Transcript !=
		"This is the first sentence. This is the second sentence." ||
		result.Usage.AudioSeconds != 7 {
		t.Fatalf("unexpected cumulative result: %#v", result)
	}
}

func TestTranscribeRejectsInvalidAudioBeforeProviderCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	recognizer := mustRecognizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return jsonResponse(http.StatusOK, `{}`), nil
	}), "test-api-key")
	_, err := recognizer.Transcribe(context.Background(), ai.TranscriptionRequest{})
	assertSpeechError(t, err, ai.SpeechOperationTranscription, ai.ErrorInvalidRequest, false)
	if calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want zero", calls.Load())
	}
}

func TestTranscribeRejectsInvalidResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"malformed":          `{`,
		"missing request ID": `{"output":{"text":"hello"}}`,
		"blank transcript":   `{"output":{},"request_id":"fun-asr-1"}`,
		"negative duration usage": `{
			"output":{"text":"one"},
			"usage":{"duration":-1},
			"request_id":"fun-asr-1"
		}`,
	}
	for name, body := range tests {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recognizer := mustRecognizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			}), "test-api-key")
			_, err := recognizer.Transcribe(
				context.Background(),
				ai.TranscriptionRequest{Audio: &asrTestAudio{data: []byte("wav")}},
			)
			assertSpeechError(
				t,
				err,
				ai.SpeechOperationTranscription,
				ai.ErrorInvalidResponse,
				true,
			)
		})
	}
}

func TestTranscribeMapsFailuresWithoutLeakingAudioOrCredentials(t *testing.T) {
	t.Parallel()

	const sensitive = "private-audio-api-key"
	recognizer := mustRecognizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(
			http.StatusForbidden,
			`{"code":"AllocationQuota.FreeTierOnly","message":"private-audio-api-key"}`,
		), nil
	}), sensitive)
	_, err := recognizer.Transcribe(
		context.Background(),
		ai.TranscriptionRequest{Audio: &asrTestAudio{data: []byte(sensitive)}},
	)
	assertSpeechError(
		t,
		err,
		ai.SpeechOperationTranscription,
		ai.ErrorQuotaExhausted,
		false,
	)
	if strings.Contains(err.Error(), sensitive) {
		t.Fatalf("error leaked sensitive value: %q", err)
	}
}

func TestRecognizerTimeoutAndFormattingAreSafe(t *testing.T) {
	t.Parallel()

	const apiKey = "must-never-be-logged"
	blocking := doerFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	recognizer, err := newRecognizerWithClient(ASRConfig{
		BaseURL: "https://dashscope.aliyuncs.com/api/v1",
		Model:   "fun-asr-flash-2026-06-15",
		Timeout: 10 * time.Millisecond,
	}, apiKey, blocking)
	if err != nil {
		t.Fatalf("new recognizer: %v", err)
	}
	_, err = recognizer.Transcribe(
		context.Background(),
		ai.TranscriptionRequest{Audio: &asrTestAudio{data: []byte("wav")}},
	)
	assertSpeechError(
		t,
		err,
		ai.SpeechOperationTranscription,
		ai.ErrorTimeout,
		true,
	)
	for _, value := range []string{
		fmt.Sprint(recognizer),
		fmt.Sprintf("%+v", recognizer),
		fmt.Sprintf("%#v", recognizer),
		fmt.Sprint(*recognizer),
		fmt.Sprintf("%+v", *recognizer),
		fmt.Sprintf("%#v", *recognizer),
	} {
		if strings.Contains(value, apiKey) {
			t.Fatalf("recognizer formatting exposed API key: %q", value)
		}
	}
	var nilRecognizer *Recognizer
	for _, value := range []string{
		fmt.Sprint(nilRecognizer),
		fmt.Sprintf("%+v", nilRecognizer),
		fmt.Sprintf("%#v", nilRecognizer),
	} {
		if strings.Contains(value, apiKey) {
			t.Fatalf("nil recognizer formatting exposed API key: %q", value)
		}
	}
}

func TestNewRecognizerRejectsUnsupportedModelAndEndpoint(t *testing.T) {
	t.Parallel()

	valid := ASRConfig{
		BaseURL: "https://dashscope.aliyuncs.com/api/v1",
		Model:   "fun-asr-flash-2026-06-15",
		Timeout: time.Second,
	}
	tests := []struct {
		name   string
		mutate func(*ASRConfig)
	}{
		{name: "potentially paid model", mutate: func(config *ASRConfig) {
			config.Model = "qwen3-asr-flash"
		}},
		{name: "wrong endpoint", mutate: func(config *ASRConfig) {
			config.BaseURL = "https://example.com/api/v1"
		}},
		{name: "zero timeout", mutate: func(config *ASRConfig) {
			config.Timeout = 0
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			test.mutate(&config)
			if _, err := NewRecognizer(config, "test-api-key"); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func mustRecognizer(t *testing.T, client httpDoer, apiKey string) *Recognizer {
	t.Helper()
	recognizer, err := newRecognizerWithClient(ASRConfig{
		BaseURL: "https://dashscope.aliyuncs.com/api/v1",
		Model:   "fun-asr-flash-2026-06-15",
		Timeout: time.Second,
	}, apiKey, client)
	if err != nil {
		t.Fatalf("new recognizer: %v", err)
	}
	return recognizer
}

type asrTestAudio struct {
	data []byte
}

func (audio *asrTestAudio) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(audio.data))), nil
}

func (*asrTestAudio) MediaType() string {
	return platformmedia.ContentTypeWAV
}

func (audio *asrTestAudio) Size() int64 {
	return int64(len(audio.data))
}

func (*asrTestAudio) Duration() time.Duration {
	return time.Second
}

func (*asrTestAudio) SampleRate() int {
	return 16_000
}

func assertSpeechError(
	t *testing.T,
	err error,
	operation ai.SpeechOperation,
	kind ai.ErrorKind,
	retryable bool,
) {
	t.Helper()
	var speechError *ai.SpeechError
	if !errors.As(err, &speechError) ||
		speechError.Operation != operation ||
		speechError.Kind != kind ||
		speechError.Retryable() != retryable {
		t.Fatalf(
			"expected %s %s retryable=%v, got %#v",
			operation,
			kind,
			retryable,
			err,
		)
	}
}
