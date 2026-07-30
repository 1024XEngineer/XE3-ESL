package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

func TestGenerateUsesOpenAICompatibleChatContract(t *testing.T) {
	t.Parallel()

	const testAPIKey = "test-api-key"
	var received chatCompletionRequest
	var rawPayload map[string]json.RawMessage
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if got := request.URL.String(); got !=
			"https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions" {
			t.Errorf("request URL = %q", got)
		}
		if got := request.Header.Get(authorizationHeaderName); got != "Bearer "+testAPIKey {
			t.Errorf("authorization header was not set")
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := json.Unmarshal(requestBody, &received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if err := json.Unmarshal(requestBody, &rawPayload); err != nil {
			t.Fatalf("decode raw request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-safe-1",
			"model":"qwen3.5-flash",
			"choices":[{
				"finish_reason":"stop",
				"index":0,
				"message":{"role":"assistant","content":"  A useful answer.  "}
			}],
			"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}
		}`), nil
	})
	generator := mustGenerator(t, doer, testAPIKey)
	request := ai.TextRequest{Messages: []ai.TextMessage{
		{Role: ai.TextRoleSystem, Content: "You are an English coach."},
		{Role: ai.TextRoleAssistant, Content: "What are you preparing for?"},
		{Role: ai.TextRoleUser, Content: "A product interview."},
	}}

	result, err := generator.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if received.Model != "qwen3.5-flash" ||
		received.Stream ||
		received.MaxTokens != 512 ||
		len(received.Messages) != len(request.Messages) {
		t.Fatalf("unexpected request payload: %#v", received)
	}
	if _, exists := rawPayload["max_tokens"]; !exists {
		t.Fatal("request omitted the enforceable max_tokens output budget")
	}
	if _, exists := rawPayload["max_completion_tokens"]; exists {
		t.Fatal("request used max_completion_tokens, which the compatibility endpoint ignores")
	}
	if _, exists := rawPayload["tools"]; exists {
		t.Fatal("plain text request unexpectedly included tools")
	}
	if _, exists := rawPayload["response_format"]; exists {
		t.Fatal("default request unexpectedly selected a response format")
	}
	rawThinking, exists := rawPayload["enable_thinking"]
	if !exists || string(rawThinking) != "false" {
		t.Fatalf(
			"non-streaming request did not explicitly disable thinking: %s",
			rawThinking,
		)
	}
	for index, message := range received.Messages {
		if message.Role != string(request.Messages[index].Role) ||
			message.Content != request.Messages[index].Content {
			t.Fatalf("message %d changed: %#v", index, message)
		}
	}
	expected := ai.TextResult{
		ID:           "chatcmpl-safe-1",
		Provider:     providerName,
		Model:        "qwen3.5-flash",
		Content:      "A useful answer.",
		FinishReason: "stop",
		Usage: ai.TokenUsage{
			InputTokens:  12,
			OutputTokens: 4,
			TotalTokens:  16,
		},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("result = %#v, want %#v", result, expected)
	}
}

func TestGenerateRequestsJSONObjectResponse(t *testing.T) {
	t.Parallel()

	var received chatCompletionRequest
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-json-1",
			"model":"qwen3.5-flash",
			"choices":[{
				"finish_reason":"stop",
				"index":0,
				"message":{"role":"assistant","content":"{\"items\":[]}"}
			}],
			"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}
		}`), nil
	})
	generator := mustGenerator(t, doer, "test-api-key")
	request := validRequest()
	request.ResponseFormat = ai.TextResponseFormatJSON

	if _, err := generator.Generate(context.Background(), request); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if received.ResponseFormat == nil ||
		received.ResponseFormat.Type != "json_object" {
		t.Fatalf("response format = %#v", received.ResponseFormat)
	}
}

func TestGenerateMapsToolCallingContract(t *testing.T) {
	t.Parallel()

	var received chatCompletionRequest
	var rawPayload map[string]json.RawMessage
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if err := json.Unmarshal(body, &rawPayload); err != nil {
			t.Fatalf("decode raw request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-tools-1",
			"model":"qwen3.5-flash",
			"choices":[{
				"finish_reason":"tool_calls",
				"message":{
					"role":"assistant",
					"content":null,
					"tool_calls":[
						{
							"id":"call-1",
							"type":"function",
							"index":0,
							"function":{
								"name":"scenario_create_v1",
								"arguments":"{\"type\":\"interview\"}"
							}
						},
						{
							"id":"call-2",
							"type":"function",
							"index":1,
							"function":{
								"name":"material_search_v1",
								"arguments":"{\"kind\":\"resume\"}"
							}
						}
					]
				}
			}],
			"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}
		}`), nil
	})
	generator := mustGenerator(t, doer, "test-api-key")
	request := ai.TextRequest{
		Messages: []ai.TextMessage{{
			Role:    ai.TextRoleUser,
			Content: "Prepare for my interview using my resume.",
		}},
		Tools: []ai.ToolDefinition{
			{
				Name:        "scenario.create.v1",
				Description: "Create a confirmed preparation scenario.",
				InputSchema: map[string]any{"type": "object"},
			},
			{
				Name:        "material.search.v1",
				Description: "Search resume and job description material.",
				InputSchema: map[string]any{"type": "object"},
			},
		},
		ToolChoice: ai.ToolChoice{
			Mode: ai.ToolChoiceSpecific,
			Name: "material.search.v1",
		},
	}

	result, err := generator.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(received.Tools) != 2 ||
		received.Tools[0].Type != "function" ||
		received.Tools[0].Function.Name != "scenario_create_v1" ||
		received.Tools[1].Function.Name != "material_search_v1" {
		t.Fatalf("unexpected provider tools: %#v", received.Tools)
	}
	if got := string(rawPayload["tool_choice"]); got !=
		`{"type":"function","function":{"name":"material_search_v1"}}` {
		t.Fatalf("tool_choice = %s", got)
	}
	expectedCalls := []ai.ToolCall{
		{
			ID:        "call-1",
			Name:      "scenario.create.v1",
			Arguments: json.RawMessage(`{"type":"interview"}`),
		},
		{
			ID:        "call-2",
			Name:      "material.search.v1",
			Arguments: json.RawMessage(`{"kind":"resume"}`),
		},
	}
	if result.Content != "" ||
		result.FinishReason != "tool_calls" ||
		!reflect.DeepEqual(result.ToolCalls, expectedCalls) {
		t.Fatalf("unexpected tool result: %#v", result)
	}
}

func TestGenerateParsesSingleToolCall(t *testing.T) {
	t.Parallel()

	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-tools-single",
			"model":"qwen3.5-flash",
			"choices":[{
				"finish_reason":"tool_calls",
				"message":{
					"role":"assistant",
					"tool_calls":[{
						"id":"call-review-1",
						"type":"function",
						"function":{
							"name":"review_search_v1",
							"arguments":"{\"limit\":1}"
						}
					}]
				}
			}],
			"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}
		}`), nil
	})
	generator := mustGenerator(t, doer, "test-api-key")

	result, err := generator.Generate(context.Background(), toolRequest())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	expected := []ai.ToolCall{{
		ID:        "call-review-1",
		Name:      "review.search.v1",
		Arguments: json.RawMessage(`{"limit":1}`),
	}}
	if !reflect.DeepEqual(result.ToolCalls, expected) {
		t.Fatalf("tool calls = %#v, want %#v", result.ToolCalls, expected)
	}
}

func TestGenerateNormalizesStopFinishReasonWithToolCalls(t *testing.T) {
	t.Parallel()

	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-tools-stop",
			"model":"qwen3.5-flash",
			"choices":[{
				"finish_reason":"stop",
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call-review-stop",
						"type":"function",
						"function":{
							"name":"review_search_v1",
							"arguments":"{\"query\":\"PM interview\"}"
						}
					}]
				}
			}],
			"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}
		}`), nil
	})
	generator := mustGenerator(t, doer, "test-api-key")

	result, err := generator.Generate(context.Background(), toolRequest())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.FinishReason != "tool_calls" ||
		len(result.ToolCalls) != 1 ||
		result.ToolCalls[0].Name != "review.search.v1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestGenerateMapsAssistantCallsAndToolResultsBackToProvider(t *testing.T) {
	t.Parallel()

	var received chatCompletionRequest
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-tools-2",
			"model":"qwen3.5-flash",
			"choices":[{
				"finish_reason":"stop",
				"message":{"role":"assistant","content":"I found one review."}
			}],
			"usage":{"prompt_tokens":24,"completion_tokens":5,"total_tokens":29}
		}`), nil
	})
	generator := mustGenerator(t, doer, "test-api-key")
	request := ai.TextRequest{
		Messages: []ai.TextMessage{
			{Role: ai.TextRoleUser, Content: "Find my last review."},
			{
				Role: ai.TextRoleAssistant,
				ToolCalls: []ai.ToolCall{{
					ID:        "call-review-1",
					Name:      "review.search.v1",
					Arguments: json.RawMessage(`{"limit":1}`),
				}},
			},
			{
				Role:       ai.TextRoleTool,
				Content:    `{"reviews":[{"id":"review-1"}]}`,
				ToolCallID: "call-review-1",
			},
		},
		Tools: []ai.ToolDefinition{{
			Name:        "review.search.v1",
			Description: "Search review summaries.",
			InputSchema: map[string]any{"type": "object"},
		}},
	}

	if _, err := generator.Generate(context.Background(), request); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(received.Messages) != 3 {
		t.Fatalf("provider messages = %d, want 3", len(received.Messages))
	}
	assistantMessage := received.Messages[1]
	if assistantMessage.Content != nil ||
		len(assistantMessage.ToolCalls) != 1 ||
		assistantMessage.ToolCalls[0].ID != "call-review-1" ||
		assistantMessage.ToolCalls[0].Index != 0 ||
		assistantMessage.ToolCalls[0].Function.Name != "review_search_v1" {
		t.Fatalf("unexpected assistant tool message: %#v", assistantMessage)
	}
	toolMessage := received.Messages[2]
	if toolMessage.Role != "tool" ||
		toolMessage.ToolCallID != "call-review-1" ||
		toolMessage.Content != request.Messages[2].Content {
		t.Fatalf("unexpected tool result message: %#v", toolMessage)
	}
}

func TestGenerateMapsMultimodalUserContentWithTools(t *testing.T) {
	t.Parallel()

	var rawPayload struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools          []chatTool          `json:"tools"`
		ResponseFormat *chatResponseFormat `json:"response_format"`
	}
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&rawPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-vision-1",
			"model":"qwen3.5-flash",
			"choices":[{
				"finish_reason":"tool_calls",
				"message":{
					"role":"assistant",
					"tool_calls":[{
						"id":"call-review-image",
						"type":"function",
						"function":{
							"name":"review_search_v1",
							"arguments":"{\"limit\":1}"
						}
					}]
				}
			}],
			"usage":{"prompt_tokens":1024,"completion_tokens":8,"total_tokens":1032}
		}`), nil
	})
	generator := mustGenerator(t, doer, "test-api-key")
	request := toolRequest()
	request.ResponseFormat = ai.TextResponseFormatJSON
	request.Messages[0] = ai.TextMessage{
		Role: ai.TextRoleUser,
		ContentParts: []ai.ContentPart{
			{
				Kind: ai.ContentPartText,
				Text: "Review the English in this screenshot.",
			},
			{
				Kind: ai.ContentPartImageURL,
				ImageURL: "https://private.example.test/image.png" +
					"?Expires=60&Signature=redacted",
			},
			{
				Kind: ai.ContentPartImageURL,
				ImageURL: "https://private.example.test/second.png" +
					"?Expires=60&Signature=redacted-2",
			},
		},
	}

	result, err := generator.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(rawPayload.Messages) != 1 ||
		rawPayload.Messages[0].Role != "user" ||
		len(rawPayload.Tools) != 1 ||
		rawPayload.ResponseFormat == nil ||
		rawPayload.ResponseFormat.Type != "json_object" {
		t.Fatalf("unexpected multimodal request: %#v", rawPayload)
	}
	var parts []chatContentPart
	if err := json.Unmarshal(rawPayload.Messages[0].Content, &parts); err != nil {
		t.Fatalf("decode multimodal content: %v", err)
	}
	if len(parts) != 3 ||
		parts[0].Type != "text" ||
		parts[0].Text != "Review the English in this screenshot." ||
		parts[0].ImageURL != nil ||
		parts[1].Type != "image_url" ||
		parts[1].Text != "" ||
		parts[1].ImageURL == nil ||
		parts[1].ImageURL.URL !=
			"https://private.example.test/image.png?Expires=60&Signature=redacted" ||
		parts[2].Type != "image_url" ||
		parts[2].Text != "" ||
		parts[2].ImageURL == nil ||
		parts[2].ImageURL.URL !=
			"https://private.example.test/second.png?Expires=60&Signature=redacted-2" {
		t.Fatalf("provider content parts = %#v", parts)
	}
	if result.FinishReason != "tool_calls" ||
		len(result.ToolCalls) != 1 ||
		result.ToolCalls[0].Name != "review.search.v1" {
		t.Fatalf("multimodal tool result = %#v", result)
	}
}

func TestProviderContentPartsMapsSingleImage(t *testing.T) {
	t.Parallel()

	mapped := providerContentParts([]ai.ContentPart{
		{Kind: ai.ContentPartText, Text: "Describe this image."},
		{
			Kind:     ai.ContentPartImageURL,
			ImageURL: "https://private.example.test/image.png?Signature=redacted",
		},
	})
	body, err := json.Marshal(mapped)
	if err != nil {
		t.Fatalf("marshal content parts: %v", err)
	}
	const expected = `[{"type":"text","text":"Describe this image."},` +
		`{"type":"image_url","image_url":` +
		`{"url":"https://private.example.test/image.png?Signature=redacted"}}]`
	if string(body) != expected {
		t.Fatalf("content parts = %s, want %s", body, expected)
	}
}

func TestGenerateMultimodalTransportFailureDoesNotLeakImageURL(t *testing.T) {
	t.Parallel()

	const imageURL = "https://private.example.test/image.png" +
		"?AccessKeyId=secret&Signature=sensitive"
	generator := mustGenerator(t, doerFunc(
		func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed while fetching " + imageURL)
		},
	), "test-api-key")
	request := validRequest()
	request.Messages[0] = ai.TextMessage{
		Role: ai.TextRoleUser,
		ContentParts: []ai.ContentPart{
			{Kind: ai.ContentPartText, Text: "Describe this image."},
			{Kind: ai.ContentPartImageURL, ImageURL: imageURL},
		},
	}

	_, err := generator.Generate(context.Background(), request)
	if err == nil {
		t.Fatal("expected transport failure")
	}
	if strings.Contains(err.Error(), imageURL) ||
		strings.Contains(fmt.Sprintf("%+v", err), imageURL) {
		t.Fatalf("generation error exposed image URL: %v", err)
	}
}

func TestGenerateMapsProviderFailuresWithoutLeakingSensitiveValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		kind       ai.ErrorKind
		retryable  bool
		code       string
		requestID  string
	}{
		{
			name:       "invalid request",
			statusCode: http.StatusBadRequest,
			body: `{"error":{"code":"BadRequest","message":"private-prompt"},
				"request_id":"request-400"}`,
			kind:      ai.ErrorInvalidRequest,
			code:      "BadRequest",
			requestID: "request-400",
		},
		{
			name:       "invalid API key",
			statusCode: http.StatusUnauthorized,
			body:       `{"code":"InvalidApiKey","message":"test-api-key"}`,
			kind:       ai.ErrorAuthentication,
			code:       "InvalidApiKey",
		},
		{
			name:       "provider quota exhausted",
			statusCode: http.StatusForbidden,
			body:       `{"code":"AllocationQuota.FreeTierOnly"}`,
			kind:       ai.ErrorQuotaExhausted,
			code:       "AllocationQuota.FreeTierOnly",
		},
		{
			name:       "permission denied",
			statusCode: http.StatusForbidden,
			body:       `{"code":"Model.AccessDenied"}`,
			kind:       ai.ErrorAuthorization,
			code:       "Model.AccessDenied",
		},
		{
			name:       "model missing",
			statusCode: http.StatusNotFound,
			body:       `{"error":{"type":"model_not_found","code":null}}`,
			kind:       ai.ErrorConfiguration,
			code:       "model_not_found",
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"code":"Throttling.RateQuota"}`,
			kind:       ai.ErrorRateLimited,
			retryable:  true,
			code:       "Throttling.RateQuota",
		},
		{
			name:       "billing unavailable",
			statusCode: http.StatusTooManyRequests,
			body:       `{"code":"PostpaidBillOverdue"}`,
			kind:       ai.ErrorAuthorization,
			code:       "PostpaidBillOverdue",
		},
		{
			name:       "account in arrears",
			statusCode: http.StatusBadRequest,
			body:       `{"code":"Arrearage"}`,
			kind:       ai.ErrorAuthorization,
			code:       "Arrearage",
		},
		{
			name:       "model not purchased",
			statusCode: http.StatusBadRequest,
			body:       `{"code":"CommodityNotPurchased"}`,
			kind:       ai.ErrorAuthorization,
			code:       "CommodityNotPurchased",
		},
		{
			name:       "provider timeout",
			statusCode: http.StatusInternalServerError,
			body:       `{"code":"RequestTimeOut"}`,
			kind:       ai.ErrorTimeout,
			retryable:  true,
			code:       "RequestTimeOut",
		},
		{
			name:       "provider unavailable",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"code":"ModelUnavailable"}`,
			kind:       ai.ErrorProviderUnavailable,
			retryable:  true,
			code:       "ModelUnavailable",
		},
		{
			name:       "redirect is not followed",
			statusCode: http.StatusTemporaryRedirect,
			body:       `<html>redirect</html>`,
			kind:       ai.ErrorConfiguration,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			doer := doerFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				response := jsonResponse(test.statusCode, test.body)
				response.Header.Set("X-Request-Id", "header-request-id")
				return response, nil
			})
			generator := mustGenerator(t, doer, "test-api-key")
			_, err := generator.Generate(context.Background(), ai.TextRequest{
				Messages: []ai.TextMessage{{
					Role:    ai.TextRoleUser,
					Content: "private-prompt",
				}},
			})

			var generationError *ai.GenerationError
			if !errors.As(err, &generationError) {
				t.Fatalf("expected GenerationError, got %T: %v", err, err)
			}
			if generationError.Kind != test.kind ||
				generationError.Retryable() != test.retryable ||
				generationError.ProviderCode != test.code {
				t.Fatalf("unexpected error metadata: %#v", generationError)
			}
			expectedRequestID := test.requestID
			if expectedRequestID == "" {
				expectedRequestID = "header-request-id"
			}
			if generationError.RequestID != expectedRequestID {
				t.Fatalf(
					"request ID = %q, want %q",
					generationError.RequestID,
					expectedRequestID,
				)
			}
			if calls.Load() != 1 {
				t.Fatalf("provider calls = %d, want exactly one", calls.Load())
			}
			for _, sensitive := range []string{"private-prompt", "test-api-key"} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatalf("error leaked %q: %q", sensitive, err)
				}
			}
		})
	}
}

func TestGenerateRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"malformed JSON": `{`,
		"missing ID": `{
			"model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}]
		}`,
		"missing model": `{
			"id":"chatcmpl-1",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}]
		}`,
		"provider switched model": `{
			"id":"chatcmpl-1",
			"model":"qwen-plus",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`,
		"no choices": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash","choices":[]
		}`,
		"multiple choices": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[
				{"finish_reason":"stop","message":{"role":"assistant","content":"one"}},
				{"finish_reason":"stop","message":{"role":"assistant","content":"two"}}
			]
		}`,
		"wrong role": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"user","content":"answer"}}]
		}`,
		"blank content": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"  "}}]
		}`,
		"tool call": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls",
				"message":{"role":"assistant","content":"answer"}}]
		}`,
		"missing usage": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}]
		}`,
		"incomplete usage": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1}
		}`,
		"negative usage": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}],
			"usage":{"prompt_tokens":-1,"completion_tokens":1,"total_tokens":0}
		}`,
		"inconsistent usage": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":1}
		}`,
		"output budget exceeded": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop",
				"message":{"role":"assistant","content":"answer"}}],
			"usage":{"prompt_tokens":1,"completion_tokens":513,"total_tokens":514}
		}`,
	}
	for name, body := range tests {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			generator := mustGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			}), "test-api-key")

			_, err := generator.Generate(context.Background(), validRequest())
			var generationError *ai.GenerationError
			if !errors.As(err, &generationError) ||
				generationError.Kind != ai.ErrorInvalidResponse ||
				!generationError.Retryable() {
				t.Fatalf("expected retryable invalid response, got %#v", err)
			}
		})
	}
}

func TestGenerateRejectsInvalidToolResponses(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"unknown tool": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls","message":{
				"role":"assistant","tool_calls":[{
					"id":"call-1","type":"function",
					"function":{"name":"unknown_tool","arguments":"{}"}
				}]
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`,
		"missing call ID": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls","message":{
				"role":"assistant","tool_calls":[{
					"id":"","type":"function",
					"function":{"name":"review_search_v1","arguments":"{}"}
				}]
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`,
		"malformed arguments": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls","message":{
				"role":"assistant","tool_calls":[{
					"id":"call-1","type":"function",
					"function":{"name":"review_search_v1","arguments":"{"}
				}]
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`,
		"non-object arguments": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls","message":{
				"role":"assistant","tool_calls":[{
					"id":"call-1","type":"function",
					"function":{"name":"review_search_v1","arguments":"[]"}
				}]
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`,
		"duplicate call IDs": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls","message":{
				"role":"assistant","tool_calls":[
					{
						"id":"call-1","type":"function",
						"function":{"name":"review_search_v1","arguments":"{}"}
					},
					{
						"id":"call-1","type":"function",
						"function":{"name":"review_search_v1","arguments":"{}"}
					}
				]
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`,
		"unsupported tool type": `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls","message":{
				"role":"assistant","tool_calls":[{
					"id":"call-1","type":"web_search",
					"function":{"name":"review_search_v1","arguments":"{}"}
				}]
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`,
	}
	for name, body := range tests {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			generator := mustGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			}), "test-api-key")

			_, err := generator.Generate(context.Background(), toolRequest())
			assertGenerationErrorKind(t, err, ai.ErrorInvalidResponse, true)
		})
	}
}

func TestGenerateRejectsInvalidRequestBeforeProviderCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	generator := mustGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return jsonResponse(http.StatusOK, `{}`), nil
	}), "test-api-key")

	_, err := generator.Generate(context.Background(), ai.TextRequest{})
	assertGenerationErrorKind(t, err, ai.ErrorInvalidRequest, false)
	if calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want zero", calls.Load())
	}
}

func TestGenerateRejectsProviderToolNameCollisionsBeforeProviderCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	generator := mustGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return jsonResponse(http.StatusOK, `{}`), nil
	}), "test-api-key")
	request := validRequest()
	request.Tools = []ai.ToolDefinition{
		{
			Name:        "review.search.v1",
			Description: "Search reviews.",
			InputSchema: map[string]any{},
		},
		{
			Name:        "review_search_v1",
			Description: "Colliding provider name.",
			InputSchema: map[string]any{},
		},
	}

	_, err := generator.Generate(context.Background(), request)
	assertGenerationErrorKind(t, err, ai.ErrorInvalidRequest, false)
	if calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want zero", calls.Load())
	}
}

func TestGenerateRejectsHistoricalToolNotExposedThisTurn(t *testing.T) {
	t.Parallel()

	generator := mustGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-1","model":"qwen3.5-flash",
			"choices":[{"finish_reason":"tool_calls","message":{
				"role":"assistant","tool_calls":[{
					"id":"call-2","type":"function",
					"function":{"name":"material_search_v1","arguments":"{}"}
				}]
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`), nil
	}), "test-api-key")
	request := toolRequest()
	request.Messages = []ai.TextMessage{
		{Role: ai.TextRoleUser, Content: "Use my resume."},
		{
			Role: ai.TextRoleAssistant,
			ToolCalls: []ai.ToolCall{{
				ID:        "call-1",
				Name:      "material.search.v1",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{Role: ai.TextRoleTool, Content: `{}`, ToolCallID: "call-1"},
	}

	_, err := generator.Generate(context.Background(), request)
	assertGenerationErrorKind(t, err, ai.ErrorInvalidResponse, true)
}

func TestGenerateRejectsMissingHTTPResponseWithoutPanicking(t *testing.T) {
	t.Parallel()

	tests := map[string]doerFunc{
		"nil response": func(*http.Request) (*http.Response, error) {
			return nil, nil
		},
		"nil body": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
			}, nil
		},
	}
	for name, doer := range tests {
		doer := doer
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			generator := mustGenerator(t, doer, "test-api-key")
			_, err := generator.Generate(context.Background(), validRequest())
			assertGenerationErrorKind(t, err, ai.ErrorInvalidResponse, true)
		})
	}
}

func TestGenerateClassifiesCallerCancellationAndTimeout(t *testing.T) {
	t.Parallel()

	blockingDoer := doerFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	t.Run("caller cancellation", func(t *testing.T) {
		t.Parallel()
		generator := mustGenerator(t, blockingDoer, "test-api-key")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := generator.Generate(ctx, validRequest())
		assertGenerationErrorKind(t, err, ai.ErrorCancelled, true)
	})

	t.Run("configured timeout", func(t *testing.T) {
		t.Parallel()
		generator, err := newWithClient(Config{
			BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:           "qwen3.5-flash",
			Timeout:         10 * time.Millisecond,
			MaxOutputTokens: 512,
		}, "test-api-key", blockingDoer)
		if err != nil {
			t.Fatalf("new generator: %v", err)
		}
		_, err = generator.Generate(context.Background(), validRequest())
		assertGenerationErrorKind(t, err, ai.ErrorTimeout, true)
	})
}

func TestGenerateDoesNotExposeTransportErrorDetails(t *testing.T) {
	t.Parallel()

	const sensitive = "must-never-be-logged"
	generator := mustGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(sensitive)
	}), sensitive)

	_, err := generator.Generate(context.Background(), validRequest())
	assertGenerationErrorKind(t, err, ai.ErrorProviderUnavailable, true)
	if strings.Contains(err.Error(), sensitive) {
		t.Fatalf("error string leaked transport details: %q", err)
	}
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		t.Fatalf("provider-unavailable error retained unsafe transport details: %v", unwrapped)
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	valid := Config{
		BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:           "qwen3.5-flash",
		Timeout:         time.Second,
		MaxOutputTokens: 512,
	}
	tests := []struct {
		name   string
		mutate func(*Config) string
	}{
		{
			name: "plain HTTP",
			mutate: func(config *Config) string {
				config.BaseURL = "http://dashscope.aliyuncs.com/compatible-mode/v1"
				return "test-api-key"
			},
		},
		{
			name: "untrusted host",
			mutate: func(config *Config) string {
				config.BaseURL = "https://example.com/compatible-mode/v1"
				return "test-api-key"
			},
		},
		{
			name: "credentials in URL",
			mutate: func(config *Config) string {
				config.BaseURL =
					"https://user:password@dashscope.aliyuncs.com/compatible-mode/v1"
				return "test-api-key"
			},
		},
		{
			name: "query in URL",
			mutate: func(config *Config) string {
				config.BaseURL =
					"https://dashscope.aliyuncs.com/compatible-mode/v1?redirect=1"
				return "test-api-key"
			},
		},
		{
			name: "wrong path",
			mutate: func(config *Config) string {
				config.BaseURL = "https://dashscope.aliyuncs.com/api/v1"
				return "test-api-key"
			},
		},
		{
			name: "token plan endpoint",
			mutate: func(config *Config) string {
				config.BaseURL =
					"https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
				return "test-api-key"
			},
		},
		{
			name: "non Qwen model",
			mutate: func(config *Config) string {
				config.Model = "deepseek-v3"
				return "test-api-key"
			},
		},
		{
			name: "non ASCII model ID",
			mutate: func(config *Config) string {
				config.Model = "qwen-模型"
				return "test-api-key"
			},
		},
		{
			name: "empty API key",
			mutate: func(*Config) string {
				return ""
			},
		},
		{
			name: "API key newline",
			mutate: func(*Config) string {
				return "test-key\ninjected"
			},
		},
		{
			name: "zero timeout",
			mutate: func(config *Config) string {
				config.Timeout = 0
				return "test-api-key"
			},
		},
		{
			name: "zero output budget",
			mutate: func(config *Config) string {
				config.MaxOutputTokens = 0
				return "test-api-key"
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			apiKey := test.mutate(&config)
			if _, err := New(config, apiKey); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestNewAcceptsOfficialWorkspaceAndTrialHosts(t *testing.T) {
	t.Parallel()

	hosts := []string{
		"https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		"https://workspace.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1/",
		"https://trial.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		"https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		"https://dashscope-us.aliyuncs.com/compatible-mode/v1",
	}
	for _, baseURL := range hosts {
		baseURL := baseURL
		t.Run(baseURL, func(t *testing.T) {
			t.Parallel()
			if _, err := New(Config{
				BaseURL:         baseURL,
				Model:           "qwen3.5-flash",
				Timeout:         time.Second,
				MaxOutputTokens: 512,
			}, "test-api-key"); err != nil {
				t.Fatalf("official base URL rejected: %v", err)
			}
		})
	}
}

func TestNewDisablesHTTPRedirects(t *testing.T) {
	t.Parallel()

	generator, err := New(Config{
		BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:           "qwen3.5-flash",
		Timeout:         time.Second,
		MaxOutputTokens: 512,
	}, "test-api-key")
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	client, ok := generator.client.(*http.Client)
	if !ok {
		t.Fatalf("client has unexpected type %T", generator.client)
	}
	if err := client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy returned %v", err)
	}
}

func TestGeneratorFormattingRedactsAPIKey(t *testing.T) {
	t.Parallel()

	const apiKey = "must-never-be-logged"
	generator, err := New(Config{
		BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:           "qwen3.5-flash",
		Timeout:         time.Second,
		MaxOutputTokens: 512,
	}, apiKey)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	for _, formatted := range []string{
		fmt.Sprint(generator),
		fmt.Sprintf("%+v", generator),
		fmt.Sprintf("%#v", generator),
		fmt.Sprint(*generator),
		fmt.Sprintf("%+v", *generator),
		fmt.Sprintf("%#v", *generator),
	} {
		if strings.Contains(formatted, apiKey) {
			t.Fatalf("generator formatting exposed API key: %q", formatted)
		}
	}
	var nilGenerator *Generator
	for _, formatted := range []string{
		fmt.Sprint(nilGenerator),
		fmt.Sprintf("%+v", nilGenerator),
		fmt.Sprintf("%#v", nilGenerator),
	} {
		if strings.Contains(formatted, apiKey) {
			t.Fatalf("nil generator formatting exposed API key: %q", formatted)
		}
	}
}

func TestGenerateRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	body := bytes.Repeat([]byte("x"), maxResponseBytes+1)
	generator := mustGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	}), "test-api-key")

	_, err := generator.Generate(context.Background(), validRequest())
	assertGenerationErrorKind(t, err, ai.ErrorInvalidResponse, true)
}

func mustGenerator(t *testing.T, client httpDoer, apiKey string) *Generator {
	t.Helper()
	generator, err := newWithClient(Config{
		BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:           "qwen3.5-flash",
		Timeout:         time.Second,
		MaxOutputTokens: 512,
	}, apiKey, client)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	return generator
}

func validRequest() ai.TextRequest {
	return ai.TextRequest{Messages: []ai.TextMessage{{
		Role:    ai.TextRoleUser,
		Content: "question",
	}}}
}

func toolRequest() ai.TextRequest {
	request := validRequest()
	request.Tools = []ai.ToolDefinition{{
		Name:        "review.search.v1",
		Description: "Search review summaries.",
		InputSchema: map[string]any{"type": "object"},
	}}
	return request
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func assertGenerationErrorKind(
	t *testing.T,
	err error,
	kind ai.ErrorKind,
	retryable bool,
) {
	t.Helper()
	var generationError *ai.GenerationError
	if !errors.As(err, &generationError) ||
		generationError.Kind != kind ||
		generationError.Retryable() != retryable {
		t.Fatalf("expected %s retryable=%v, got %#v", kind, retryable, err)
	}
}
