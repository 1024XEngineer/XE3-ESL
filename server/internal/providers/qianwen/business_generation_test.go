package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/fieldextractor"
)

func TestBusinessGeneratorsMapOwnedRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		systemPrompt   string
		userPrompt     string
		responseFormat protocol.TextResponseFormat
		generate       func(*textClient) (string, string, string, error)
	}{
		{
			name:           "Memory JSON",
			systemPrompt:   "extract durable memory",
			userPrompt:     "one user turn",
			responseFormat: protocol.TextResponseFormatJSON,
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&MemoryGenerator{generator: client}).GenerateJSON(
					context.Background(),
					memory.GenerationRequest{
						SystemPrompt: "extract durable memory",
						UserPrompt:   "one user turn",
					},
				)
				return result.Provider, result.Model, result.Content, err
			},
		},
		{
			name:           "Summary JSON",
			systemPrompt:   "summarize the thread",
			userPrompt:     "thread transcript",
			responseFormat: protocol.TextResponseFormatJSON,
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&SummaryGenerator{generator: client}).GenerateJSON(
					context.Background(),
					summary.GenerationRequest{
						SystemPrompt: "summarize the thread",
						UserPrompt:   "thread transcript",
					},
				)
				return result.Provider, result.Model, result.Content, err
			},
		},
		{
			name:           "Preparation text",
			systemPrompt:   "identify a job target",
			userPrompt:     "resume and role material",
			responseFormat: protocol.TextResponseFormatDefault,
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&PreparationJobTargetGenerator{generator: client}).GenerateJobTarget(
					context.Background(),
					preparation.JobTargetGenerationRequest{
						SystemInstruction: "identify a job target",
						UserMaterial:      "resume and role material",
					},
				)
				return "", "", result.Content, err
			},
		},
		{
			name:           "Resume JSON",
			systemPrompt:   "extract resume fields",
			userPrompt:     "resume document payload",
			responseFormat: protocol.TextResponseFormatJSON,
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&ResumeFieldGenerator{generator: client}).GenerateJSON(
					context.Background(),
					fieldextractor.GenerationRequest{
						SystemPrompt:        "extract resume fields",
						DocumentPayload:     "resume document payload",
						MinimumOutputTokens: fieldextractor.MinimumGenerationOutputTokens,
					},
				)
				return result.Provider, result.Model, result.Content, err
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var received chatCompletionRequest
			client := mustBusinessGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				return jsonResponse(http.StatusOK, `{
					"id":"chatcmpl-business-1",
					"model":"qwen3.5-flash",
					"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"mapped output"}}],
					"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}
				}`), nil
			}))

			provider, model, content, err := test.generate(client)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if content != "mapped output" {
				t.Fatalf("content = %q", content)
			}
			if test.name != "Preparation text" &&
				(provider != providerName || model != "qwen3.5-flash") {
				t.Fatalf("provider/model = %q/%q", provider, model)
			}
			if len(received.Messages) != 2 ||
				received.Messages[0].Role != string(protocol.TextRoleSystem) ||
				received.Messages[0].Content != test.systemPrompt ||
				received.Messages[1].Role != string(protocol.TextRoleUser) ||
				received.Messages[1].Content != test.userPrompt {
				t.Fatalf("messages = %#v", received.Messages)
			}
			if test.responseFormat == protocol.TextResponseFormatJSON {
				if received.ResponseFormat == nil ||
					received.ResponseFormat.Type != string(protocol.TextResponseFormatJSON) {
					t.Fatalf("response format = %#v", received.ResponseFormat)
				}
			} else if received.ResponseFormat != nil {
				t.Fatalf("response format = %#v, want omitted", received.ResponseFormat)
			}
			if len(received.Tools) != 0 || received.ToolChoice != nil {
				t.Fatalf("business request exposed tools: %#v", received)
			}
		})
	}
}

func TestBusinessGeneratorPreservesStableProviderFailure(t *testing.T) {
	t.Parallel()

	client := mustBusinessGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(
			http.StatusTooManyRequests,
			`{"code":"Throttling.RateQuota"}`,
		), nil
	}))
	_, err := (&MemoryGenerator{generator: client}).GenerateJSON(
		context.Background(),
		memory.GenerationRequest{
			SystemPrompt: "extract durable memory",
			UserPrompt:   "one user turn",
		},
	)
	var failure memory.ProviderFailure
	if !errors.As(err, &failure) ||
		failure.StableCategory() != string(protocol.ErrorRateLimited) ||
		!failure.Retryable() {
		t.Fatalf("provider failure = %#v", err)
	}
}

func TestResumeGeneratorRejectsNonAuthoritativeOutputBudget(t *testing.T) {
	t.Parallel()

	generator := &ResumeFieldGenerator{generator: mustBusinessGenerator(t, doerFunc(
		func(*http.Request) (*http.Response, error) {
			t.Fatal("provider must not be called")
			return nil, nil
		},
	))}
	_, err := generator.GenerateJSON(context.Background(), fieldextractor.GenerationRequest{
		SystemPrompt:        "extract resume fields",
		DocumentPayload:     "resume document payload",
		MinimumOutputTokens: fieldextractor.MinimumGenerationOutputTokens - 1,
	})
	var failure fieldextractor.GenerationFailure
	if !errors.As(err, &failure) ||
		failure.StableCategory() != string(protocol.ErrorInvalidRequest) {
		t.Fatalf("generation failure = %#v", err)
	}
}

func mustBusinessGenerator(t *testing.T, client httpDoer) *textClient {
	t.Helper()
	generator, err := newWithClient(TextConfig{
		BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:           "qwen3.5-flash",
		Timeout:         time.Second,
		MaxOutputTokens: fieldextractor.MinimumGenerationOutputTokens,
	}, "test-api-key", client)
	if err != nil {
		t.Fatalf("new business generator: %v", err)
	}
	return generator
}
