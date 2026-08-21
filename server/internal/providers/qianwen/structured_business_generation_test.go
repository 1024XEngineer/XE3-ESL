package qianwen

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
)

func TestEvaluationSpeechFeedbackUsesStrictSchema(t *testing.T) {
	t.Parallel()

	var received chatCompletionRequest
	client := mustBusinessGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-feedback-1",
			"model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"items\":[{\"kind\":\"STRENGTH\",\"explanation\":\"表达清晰。\",\"source_text\":null,\"source_occurrence\":null,\"suggested_text\":null}]}"}}],
			"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}
		}`), nil
	}))

	result, err := (&EvaluationSpeechFeedbackGenerator{generator: client}).Generate(
		context.Background(),
		speechfeedback.TextGenerationRequest{
			SystemPrompt: "evaluate transcript",
			UserPrompt:   `{"english_text":"I enjoy music."}`,
		},
	)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Content == "" {
		t.Fatal("content is empty")
	}
	assertStrictResponseSchema(t, received, "speech_feedback")
	properties := received.ResponseFormat.JSONSchema.Schema["properties"].(map[string]any)
	items := properties["items"].(map[string]any)
	item := items["items"].(map[string]any)
	itemProperties := item["properties"].(map[string]any)
	source := itemProperties["source_text"].(map[string]any)
	if _, ok := source["anyOf"].([]any); !ok {
		t.Fatalf("source_text schema = %#v", source)
	}
	occurrence := itemProperties["source_occurrence"].(map[string]any)
	if _, ok := occurrence["anyOf"].([]any); !ok {
		t.Fatalf("source_occurrence schema = %#v", occurrence)
	}
	suggested := itemProperties["suggested_text"].(map[string]any)
	if _, ok := suggested["anyOf"].([]any); !ok {
		t.Fatalf("suggested_text schema = %#v", suggested)
	}
}

func TestIELTSAnswerUsesStrictSchema(t *testing.T) {
	t.Parallel()

	var received chatCompletionRequest
	client := mustBusinessGenerator(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-ielts-1",
			"model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"I enjoy upbeat music.\",\"outline\":[\"preference\",\"reason\"],\"useful_expressions\":[\"upbeat music\"],\"speech_text\":\"I enjoy upbeat music.\"}"}}],
			"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}
		}`), nil
	}))

	result, err := (&IELTSAnswerGenerator{generator: client}).GenerateIELTSAnswer(
		context.Background(),
		ielts.AnswerGenerationInput{
			Part:       ielts.PracticeModePart1,
			Question:   "What music do you like?",
			TargetBand: 6.5,
		},
	)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Answer != "I enjoy upbeat music." {
		t.Fatalf("answer = %q", result.Answer)
	}
	assertStrictResponseSchema(t, received, "ielts_answer")
}
