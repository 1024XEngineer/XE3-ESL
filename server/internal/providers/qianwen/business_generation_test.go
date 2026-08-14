package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
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
			name:           "Evaluation JSON",
			systemPrompt:   "evaluate frozen evidence",
			userPrompt:     "sanitized evidence payload",
			responseFormat: protocol.TextResponseFormatJSON,
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&EvaluationScoringGenerator{
					generator: client,
				}).Generate(
					context.Background(),
					scoring.TextGenerationRequest{
						SystemPrompt: "evaluate frozen evidence",
						UserPrompt:   "sanitized evidence payload",
					},
				)
				return result.Provider, result.Model, result.Content, err
			},
		},
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

func TestQianwenIELTSCriterionUsesStrictJSONSchema(t *testing.T) {
	t.Parallel()
	const content = `{"schema_version":"ielts-speaking-full-mock-shadow-provider/v3","criteria":[{"criterion_id":"IELTS_LR","rubric_descriptor":"LR_PRACTICE_BAND_6","strengths":[{"template_id":"ielts.lr.strength.v1","evidence":[{"evidence_ref_id":"evidence-1","quote":"I answer clearly.","occurrence":1}]}],"improvements":[],"upgrade_examples":[]}]}`
	var received chatCompletionRequest
	client := mustBusinessGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-qianwen-ielts-criterion",
			"model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":`+strconv.Quote(content)+`}}],
			"usage":{"prompt_tokens":30,"completion_tokens":12,"total_tokens":42}
		}`), nil
	}))
	result, err := (&EvaluationScoringGenerator{generator: client}).Generate(
		context.Background(),
		scoring.TextGenerationRequest{
			SystemPrompt:         scoring.IELTSSpeakingShadowSystemContract,
			UserPrompt:           `{"input":{"assessable_criteria":["IELTS_LR"]}}`,
			OutputContract:       scoring.TextGenerationOutputIELTSSpeakingCriterionV3,
			OutputCriterion:      scoring.IELTSCriterionLR,
			OutputRubricRequired: true,
		},
	)
	if err != nil || result.Content != content {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if received.ResponseFormat == nil ||
		received.ResponseFormat.Type != "json_schema" ||
		received.ResponseFormat.JSONSchema == nil ||
		!received.ResponseFormat.JSONSchema.Strict ||
		received.ResponseFormat.JSONSchema.Name !=
			"ielts_speaking_criterion_v3" ||
		received.MaxTokens != nil || len(received.Tools) != 0 ||
		received.ToolChoice != nil {
		t.Fatalf("request=%#v", received)
	}
	criteria := received.ResponseFormat.JSONSchema.Schema["properties"].(map[string]any)["criteria"].(map[string]any)
	if criteria["minItems"] != float64(1) ||
		criteria["maxItems"] != float64(1) {
		t.Fatalf("criteria schema=%#v", criteria)
	}
	criterion := criteria["items"].(map[string]any)
	required := criterion["required"].([]any)
	if !slices.Contains(required, any("rubric_descriptor")) {
		t.Fatalf("criterion required=%#v", required)
	}
	strengths := criterion["properties"].(map[string]any)["strengths"].(map[string]any)
	if strengths["minItems"] != float64(1) {
		t.Fatalf("strengths schema=%#v", strengths)
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
