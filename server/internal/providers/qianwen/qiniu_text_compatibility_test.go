package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestQiniuGenerateUsesOfficialMultimodalJSONContract(t *testing.T) {
	const apiKey = "qiniu-test-key"
	var payload map[string]json.RawMessage
	generator := mustQiniuGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://api.qnaigc.com/v1/chat/completions" {
			t.Fatalf("Qiniu request URL = %q", request.URL.String())
		}
		if request.Header.Get(authorizationHeaderName) != "Bearer "+apiKey {
			t.Fatal("Qiniu authorization header was not set")
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode Qiniu request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-qiniu-1",
			"model":"moonshotai/kimi-k2.6",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"summary\":\"safe\"}"}}],
			"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}
		}`), nil
	}), apiKey)

	result, err := generator.Generate(context.Background(), protocol.TextRequest{
		Messages: []protocol.TextMessage{{
			Role: protocol.TextRoleUser,
			ContentParts: []protocol.ContentPart{
				{Kind: protocol.ContentPartText, Text: "Read the image."},
				{Kind: protocol.ContentPartImageURL, ImageURL: "https://private.example.com/image.jpg?token=ephemeral"},
			},
		}},
		ResponseFormat: protocol.TextResponseFormatJSON,
	})
	if err != nil {
		t.Fatalf("Qiniu Generate() error = %v", err)
	}
	if result.Provider != qiniuProviderName || result.Model != "moonshotai/kimi-k2.6" ||
		result.Content != `{"summary":"safe"}` || result.Usage.TotalTokens != 18 {
		t.Fatalf("Qiniu result = %#v", result)
	}
	if _, exists := payload["enable_thinking"]; exists {
		t.Fatal("Qiniu request included the Qianwen-only enable_thinking field")
	}
	if string(payload["thinking"]) != `{"type":"disabled"}` {
		t.Fatalf("Qiniu thinking = %s", payload["thinking"])
	}
	if string(payload["response_format"]) != `{"type":"json_object"}` {
		t.Fatalf("Qiniu response_format = %s", payload["response_format"])
	}
	var messages []struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload["messages"], &messages); err != nil ||
		len(messages) != 1 || len(messages[0].Content) != 2 ||
		messages[0].Content[0].Text != "Read the image." ||
		messages[0].Content[1].ImageURL == nil ||
		messages[0].Content[1].ImageURL.URL == "" {
		t.Fatalf("Qiniu multimodal messages = %#v, %v", messages, err)
	}
}

func TestQiniuGenerateMapsToolCallsAndLineage(t *testing.T) {
	generator := mustQiniuGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		var payload chatCompletionRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "review_search_v1" {
			t.Fatalf("Qiniu tools = %#v", payload.Tools)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-qiniu-tool",
			"model":"moonshotai/kimi-k2.6",
			"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{
				"id":"call_qiniu_1","type":"function","function":{"name":"review_search_v1","arguments":"{\"query\":\"IELTS\"}"}
			}]}}],
			"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}
		}`), nil
	}), "qiniu-test-key")

	result, err := generator.Generate(context.Background(), toolRequest())
	if err != nil {
		t.Fatalf("Qiniu tool Generate() error = %v", err)
	}
	if result.Provider != qiniuProviderName || result.FinishReason != "tool_calls" ||
		len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "review.search.v1" {
		t.Fatalf("Qiniu tool result = %#v", result)
	}
}

func TestQiniuIELTSCriterionUsesForcedSingleCriterionTool(t *testing.T) {
	const arguments = `{
		"schema_version":"ielts-speaking-full-mock-shadow-provider/v3",
		"criteria":[{
			"criterion_id":"IELTS_LR",
			"rubric_descriptor":"LR_PRACTICE_BAND_6",
			"strengths":[{"template_id":"ielts.lr.strength.v1","evidence":[{"evidence_ref_id":"evidence-1","quote":"I answer clearly.","occurrence":1}]}],
			"improvements":[],
			"upgrade_examples":[]
		}]
	}`
	var payload chatCompletionRequest
	client := mustQiniuGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-qiniu-ielts-criterion",
			"model":"moonshotai/kimi-k2.6",
			"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{
				"id":"call_ielts_1","type":"function","function":{"name":"ielts_speaking_criterion_v3","arguments":`+strconv.Quote(arguments)+`}
			}]}}],
			"usage":{"prompt_tokens":30,"completion_tokens":12,"total_tokens":42}
		}`), nil
	}), "qiniu-test-key")

	result, err := (&EvaluationScoringGenerator{generator: client}).Generate(
		context.Background(),
		scoring.TextGenerationRequest{
			SystemPrompt:    scoring.IELTSSpeakingShadowSystemContract,
			UserPrompt:      `{"input":{"assessable_criteria":["IELTS_LR"]}}`,
			OutputContract:  scoring.TextGenerationOutputIELTSSpeakingCriterionV3,
			OutputCriterion: scoring.IELTSCriterionLR,
		},
	)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Content != arguments || payload.ResponseFormat != nil ||
		len(payload.Tools) != 1 ||
		payload.Tools[0].Function.Name != "ielts_speaking_criterion_v3" {
		t.Fatalf("result = %#v; request = %#v", result, payload)
	}
	choice, ok := payload.ToolChoice.(map[string]any)
	if !ok || choice["type"] != "function" {
		t.Fatalf("tool choice = %#v", payload.ToolChoice)
	}
	function, ok := choice["function"].(map[string]any)
	if !ok || function["name"] != "ielts_speaking_criterion_v3" {
		t.Fatalf("tool choice function = %#v", choice["function"])
	}

	schema := payload.Tools[0].Function.Parameters
	assertBasicQiniuJSONSchema(t, schema)
	properties := schema["properties"].(map[string]any)
	criteria := properties["criteria"].(map[string]any)
	if criteria["minItems"] != float64(1) ||
		criteria["maxItems"] != float64(1) {
		t.Fatalf("criteria schema = %#v", criteria)
	}
	criterion := criteria["items"].(map[string]any)
	criterionProperties := criterion["properties"].(map[string]any)
	assertSchemaStringEnum(
		t,
		criterionProperties["criterion_id"].(map[string]any),
		[]string{"IELTS_LR"},
	)
	rubricSchema := criterionProperties["rubric_descriptor"].(map[string]any)
	for _, value := range rubricSchema["enum"].([]any) {
		if !strings.HasPrefix(value.(string), "LR_PRACTICE_BAND_") {
			t.Fatalf("rubric descriptor enum = %#v", rubricSchema["enum"])
		}
	}
	for collection, kind := range map[string]string{
		"strengths":        "strength",
		"improvements":     "improvement",
		"upgrade_examples": "upgrade",
	} {
		value := criterionProperties[collection].(map[string]any)
		if _, hasMinimum := value["minItems"]; hasMinimum ||
			value["maxItems"] != float64(3) {
			t.Fatalf("%s schema = %#v", collection, value)
		}
		finding := value["items"].(map[string]any)
		template := finding["properties"].(map[string]any)["template_id"].(map[string]any)
		assertSchemaStringEnum(
			t,
			template,
			[]string{"ielts.lr." + kind + ".v1"},
		)
	}
	evidence := criterionProperties["improvements"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["evidence"].(map[string]any)
	if evidence["minItems"] != float64(1) ||
		evidence["maxItems"] != float64(4) {
		t.Fatalf("evidence schema = %#v", evidence)
	}
}

func assertSchemaStringEnum(
	t *testing.T,
	schema map[string]any,
	want []string,
) {
	t.Helper()
	values, ok := schema["enum"].([]any)
	if !ok || len(values) != len(want) {
		t.Fatalf("schema enum = %#v, want %#v", schema["enum"], want)
	}
	for index, value := range values {
		if value != want[index] {
			t.Fatalf("schema enum = %#v, want %#v", values, want)
		}
	}
}

func TestQiniuIELTSCriterionRejectsMissingForcedToolCall(t *testing.T) {
	client := mustQiniuGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-qiniu-ielts-invalid",
			"model":"moonshotai/kimi-k2.6",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}],
			"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}
		}`), nil
	}), "qiniu-test-key")
	_, err := (&EvaluationScoringGenerator{generator: client}).Generate(
		context.Background(),
		scoring.TextGenerationRequest{
			SystemPrompt:    scoring.IELTSSpeakingShadowSystemContract,
			UserPrompt:      `{"input":{"assessable_criteria":["IELTS_LR"]}}`,
			OutputContract:  scoring.TextGenerationOutputIELTSSpeakingCriterionV3,
			OutputCriterion: scoring.IELTSCriterionLR,
		},
	)
	if err == nil {
		t.Fatal("Generate succeeded without the required criterion tool call")
	}
}

func assertBasicQiniuJSONSchema(t *testing.T, schema map[string]any) {
	t.Helper()
	allowed := map[string]bool{
		"type": true, "properties": true, "required": true,
		"additionalProperties": true, "items": true, "enum": true,
		"minimum": true, "maximum": true, "minItems": true,
		"maxItems": true, "minLength": true, "maxLength": true,
	}
	for keyword, value := range schema {
		if !allowed[keyword] {
			t.Fatalf("unsupported Qiniu JSON Schema keyword %q", keyword)
		}
		switch keyword {
		case "properties":
			for name, property := range value.(map[string]any) {
				child, ok := property.(map[string]any)
				if !ok {
					t.Fatalf("schema property %q = %#v", name, property)
				}
				assertBasicQiniuJSONSchema(t, child)
			}
		case "items":
			child, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("schema items = %#v", value)
			}
			assertBasicQiniuJSONSchema(t, child)
		}
	}
}

func TestQiniuGeminiRequestOmitsKimiThinkingControl(t *testing.T) {
	var payload map[string]json.RawMessage
	generator := mustQiniuGeneratorForModel(
		t,
		"gemini-2.5-flash",
		doerFunc(func(request *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			return jsonResponse(http.StatusOK, `{
				"id":"chatcmpl-qiniu-gemini",
				"model":"gemini-2.5-flash",
				"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Hello"}}],
				"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}
			}`), nil
		}),
		"qiniu-test-key",
	)
	if _, err := generator.Generate(context.Background(), validRequest()); err != nil {
		t.Fatalf("Qiniu Gemini Generate() error = %v", err)
	}
	if _, exists := payload["enable_thinking"]; exists {
		t.Fatal("Qiniu Gemini request included the Qianwen thinking field")
	}
	if _, exists := payload["thinking"]; exists {
		t.Fatal("Qiniu Gemini request included the Kimi thinking field")
	}
}

func TestQiniuGenerateStreamRequiresUsageAndReportsLineage(t *testing.T) {
	var payload map[string]json.RawMessage
	generator := mustQiniuGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return streamResponse(
			`data: {"id":"chatcmpl-qiniu-stream","model":"moonshotai/kimi-k2.6","choices":[{"delta":{"role":"assistant","content":"Hello"}}]}` + "\n\n" +
				`data: {"id":"chatcmpl-qiniu-stream","model":"moonshotai/kimi-k2.6","choices":[{"delta":{"content":" there"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}` + "\n\n" +
				"data: [DONE]\n\n",
		), nil
	}), "qiniu-test-key")
	var deltas []string
	result, err := generator.GenerateStream(
		context.Background(),
		validRequest(),
		protocol.TextDeltaObserverFunc(func(_ context.Context, delta string) error {
			deltas = append(deltas, delta)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Qiniu GenerateStream() error = %v", err)
	}
	if result.Provider != qiniuProviderName || result.Content != "Hello there" ||
		result.Usage.TotalTokens != 7 || strings.Join(deltas, "") != "Hello there" {
		t.Fatalf("Qiniu stream result = %#v, deltas=%#v", result, deltas)
	}
	if _, exists := payload["enable_thinking"]; exists {
		t.Fatal("Qiniu stream included the Qianwen-only enable_thinking field")
	}
	if string(payload["thinking"]) != `{"type":"disabled"}` {
		t.Fatalf("Qiniu stream thinking = %s", payload["thinking"])
	}
	if string(payload["stream_options"]) != `{"include_usage":true}` {
		t.Fatalf("Qiniu stream_options = %s", payload["stream_options"])
	}
}

func TestQiniuErrorsAreBoundedAndClassified(t *testing.T) {
	const sensitive = "prompt and key must not leak"
	generator := mustQiniuGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		response := jsonResponse(http.StatusPaymentRequired, fmt.Sprintf(
			`{"error":{"type":"quota_exceeded_error","message":%q}}`,
			sensitive,
		))
		response.Header.Set("X-Request-Id", "request-qiniu-1")
		return response, nil
	}), "qiniu-test-key")
	_, err := generator.Generate(context.Background(), validRequest())
	var generationError *protocol.GenerationError
	if !errors.As(err, &generationError) ||
		generationError.Kind != protocol.ErrorQuotaExhausted ||
		generationError.RequestID != "request-qiniu-1" ||
		strings.Contains(fmt.Sprint(err), sensitive) {
		t.Fatalf("Qiniu status error = %#v, %v", generationError, err)
	}
}

func TestQiniuTextConfigRejectsNonOfficialEndpoints(t *testing.T) {
	invalid := []TextConfig{
		{Provider: qiniuProviderName, BaseURL: "https://example.com/v1", Model: "moonshotai/kimi-k2.6"},
		{Provider: qiniuProviderName, BaseURL: "http://api.qnaigc.com/v1", Model: "moonshotai/kimi-k2.6"},
		{Provider: qiniuProviderName, BaseURL: "https://api.qnaigc.com/v1/chat/completions", Model: "moonshotai/kimi-k2.6"},
		{Provider: qiniuProviderName, BaseURL: "https://api.qnaigc.com/v1", Model: "../unsafe"},
		{Provider: qiniuProviderName, BaseURL: "https://api.qnaigc.com/v1", Model: " moonshotai/kimi-k2.6"},
		{Provider: qiniuProviderName, BaseURL: "https://api.qnaigc.com/v1", Model: "moonshotai//kimi-k2.6"},
	}
	for _, configuration := range invalid {
		configuration.Timeout = time.Second
		configuration.MaxOutputTokens = 512
		if client, err := newWithClient(configuration, "qiniu-test-key", doerFunc(
			func(*http.Request) (*http.Response, error) {
				return &http.Response{Body: io.NopCloser(strings.NewReader(""))}, nil
			},
		)); err == nil || client != nil {
			t.Fatalf("unsafe Qiniu config created client=%#v error=%v", client, err)
		}
	}
}

func mustQiniuGenerator(t *testing.T, client httpDoer, apiKey string) *textClient {
	return mustQiniuGeneratorForModel(t, qiniuKimiK26Model, client, apiKey)
}

func mustQiniuGeneratorForModel(
	t *testing.T,
	model string,
	client httpDoer,
	apiKey string,
) *textClient {
	t.Helper()
	generator, err := newWithClient(TextConfig{
		Provider: qiniuProviderName, BaseURL: "https://api.qnaigc.com/v1",
		Model: model, Timeout: time.Second, MaxOutputTokens: 512,
	}, apiKey, client)
	if err != nil {
		t.Fatalf("new Qiniu generator: %v", err)
	}
	return generator
}
