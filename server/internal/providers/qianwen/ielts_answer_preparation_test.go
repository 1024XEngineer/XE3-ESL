package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestIELTSAnswerPreparationGeneratorMapsOnlyOwnedMaterial(t *testing.T) {
	t.Parallel()

	request := ielts.AnswerGenerationRequest{
		Part:           ielts.PracticeModePart1,
		Question:       "Do you enjoy music?",
		PersonalPoints: []string{"I play piano with my sister."},
		TargetBand:     7.5,
	}
	var received chatCompletionRequest
	client := mustBusinessGenerator(t, doerFunc(func(providerRequest *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(providerRequest.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-ielts-1",
			"model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"Yes.\",\"outline\":[\"Preference\"],\"useful_expressions\":[\"a big part of my life\"],\"speech_text\":\"Yes.\"}"}}],
			"usage":{"prompt_tokens":20,"completion_tokens":12,"total_tokens":32}
		}`), nil
	}))

	result, err := (&IELTSAnswerPreparationGenerator{generator: client}).
		GenerateAnswerPreparation(context.Background(), request)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.RequestID != "chatcmpl-ielts-1" ||
		result.Provider != providerName || result.Model != "qwen3.5-flash" ||
		result.Answer != "Yes." || result.SpeechText != "Yes." {
		t.Fatalf("result = %#v", result)
	}
	if len(received.Messages) != 2 ||
		received.Messages[0].Content != ielts.GenerationSystemPrompt() ||
		received.Messages[1].Content != ielts.GenerationUserPrompt(request) {
		t.Fatalf("messages = %#v", received.Messages)
	}
	if received.ResponseFormat == nil ||
		received.ResponseFormat.Type != string(protocol.TextResponseFormatJSON) {
		t.Fatalf("response format = %#v", received.ResponseFormat)
	}
}

func TestIELTSAnswerPreparationGeneratorRejectsUnknownOutputFields(t *testing.T) {
	t.Parallel()

	client := mustBusinessGenerator(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"id":"chatcmpl-ielts-invalid",
			"model":"qwen3.5-flash",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"Yes.\",\"outline\":[\"Preference\"],\"useful_expressions\":[\"often listen to\"],\"speech_text\":\"Yes.\",\"invented_fact\":\"x\"}"}}],
			"usage":{"prompt_tokens":20,"completion_tokens":12,"total_tokens":32}
		}`), nil
	}))

	_, err := (&IELTSAnswerPreparationGenerator{generator: client}).
		GenerateAnswerPreparation(context.Background(), ielts.AnswerGenerationRequest{
			Part:           ielts.PracticeModePart1,
			Question:       "Do you enjoy music?",
			PersonalPoints: []string{"I play piano."},
			TargetBand:     7,
		})
	var failure *protocol.GenerationError
	if !errors.As(err, &failure) || failure.Kind != protocol.ErrorInvalidResponse {
		t.Fatalf("failure = %#v", err)
	}
}
