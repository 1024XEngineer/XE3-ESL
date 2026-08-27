package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/fieldextractor"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestBusinessGeneratorsMapOwnedRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		systemPrompt   string
		userPrompt     string
		responseFormat protocol.TextResponseFormat
		schemaName     string
		generate       func(*textClient) (string, string, string, error)
	}{
		{
			name:           "Evaluation JSON",
			systemPrompt:   "evaluate frozen evidence",
			userPrompt:     "sanitized evidence payload",
			responseFormat: protocol.TextResponseFormatJSONSchema,
			schemaName:     "evaluation_report",
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&EvaluationScoringGenerator{
					generator: client,
				}).Generate(
					context.Background(),
					textgeneration.Request{
						SystemPrompt: "evaluate frozen evidence",
						UserPrompt:   "sanitized evidence payload",
						Report: textgeneration.ReportContract{
							DimensionKeys: []string{"TASK_ACHIEVEMENT"},
							ScoreMaximum:  100,
						},
					},
				)
				return result.Provider, result.Model, result.Content, err
			},
		},
		{
			name:           "IELTS profile JSON",
			systemPrompt:   "update provisional IELTS profile",
			userPrompt:     "frozen Part evidence",
			responseFormat: protocol.TextResponseFormatJSONSchema,
			schemaName:     "ielts_cumulative_profile",
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&EvaluationProfileGenerator{
					generator: client,
				}).Generate(
					context.Background(),
					textgeneration.Request{
						SystemPrompt: "update provisional IELTS profile",
						UserPrompt:   "frozen Part evidence",
					},
				)
				return result.Provider, result.Model, result.Content, err
			},
		},
		{
			name:           "Summary JSON",
			systemPrompt:   "summarize the thread",
			userPrompt:     "thread transcript",
			responseFormat: protocol.TextResponseFormatJSONSchema,
			schemaName:     "conversation_summary",
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
			name:           "Practice turn intent JSON",
			systemPrompt:   "classify current behavior",
			userPrompt:     "current user message",
			responseFormat: protocol.TextResponseFormatJSONSchema,
			schemaName:     "practice_turn_intent",
			generate: func(client *textClient) (string, string, string, error) {
				result, err := (&PracticeTurnIntentGenerator{generator: client}).
					GeneratePracticeTurnIntent(
						context.Background(),
						preparationagentcapability.PracticeTurnIntentGenerationRequest{
							SystemInstruction: "classify current behavior",
							UserMaterial:      "current user message",
						},
					)
				return "", "", result.Content, err
			},
		},
		{
			name:           "Resume JSON",
			systemPrompt:   "extract resume fields",
			userPrompt:     "resume document payload",
			responseFormat: protocol.TextResponseFormatJSONSchema,
			schemaName:     "resume_fields",
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
				test.name != "Practice turn intent JSON" &&
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
			switch test.responseFormat {
			case protocol.TextResponseFormatJSONSchema:
				assertStrictResponseSchema(t, received, test.schemaName)
				if test.schemaName == "evaluation_report" {
					assertEvaluationFindingSchema(t, received)
				}
			default:
				if received.ResponseFormat != nil {
					t.Fatalf("response format = %#v, want omitted", received.ResponseFormat)
				}
			}
			if len(received.Tools) != 0 || received.ToolChoice != nil {
				t.Fatalf("business request exposed tools: %#v", received)
			}
		})
	}
}

func assertStrictResponseSchema(
	t *testing.T,
	received chatCompletionRequest,
	name string,
) {
	t.Helper()
	format := received.ResponseFormat
	if format == nil ||
		format.Type != string(protocol.TextResponseFormatJSONSchema) ||
		format.JSONSchema == nil || format.JSONSchema.Name != name ||
		!format.JSONSchema.Strict || received.MaxTokens != nil {
		t.Fatalf("strict response format = %#v", format)
	}
}

func assertEvaluationFindingSchema(
	t *testing.T,
	received chatCompletionRequest,
) {
	t.Helper()
	format := received.ResponseFormat
	properties, ok := format.JSONSchema.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("evaluation schema properties = %#v", format.JSONSchema.Schema)
	}
	dimensions, ok := properties["dimensions"].(map[string]any)
	if !ok {
		t.Fatalf("evaluation dimensions schema = %#v", properties["dimensions"])
	}
	dimension, ok := dimensions["items"].(map[string]any)
	if !ok {
		t.Fatalf("evaluation dimension item = %#v", dimensions["items"])
	}
	dimensionProperties, ok := dimension["properties"].(map[string]any)
	if !ok {
		t.Fatalf("evaluation dimension properties = %#v", dimension)
	}
	strengths, ok := dimensionProperties["strengths"].(map[string]any)
	if !ok {
		t.Fatalf("evaluation strengths schema = %#v", dimensionProperties["strengths"])
	}
	finding, ok := strengths["items"].(map[string]any)
	if !ok || finding["type"] != "object" || finding["additionalProperties"] != false {
		t.Fatalf("evaluation finding schema = %#v", strengths["items"])
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
