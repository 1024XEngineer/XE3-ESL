package title

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestServiceGeneratesSemanticTitleFromFirstExchange(t *testing.T) {
	generator := &recordingGenerator{result: GenerationResult{
		Provider: "fake",
		Model:    "fake-model",
		Content:  `{"title":"产品经理面试准备"}`,
	}}
	service, err := NewService(generator, testConfiguration())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	title, err := service.GenerateTitle(context.Background(), testClaim())
	if err != nil {
		t.Fatalf("GenerateTitle: %v", err)
	}
	if title != "产品经理面试准备" {
		t.Fatalf("title = %q", title)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("request count = %d", len(generator.requests))
	}
	var payload map[string]string
	if err := json.Unmarshal(
		[]byte(generator.requests[0].UserPrompt),
		&payload,
	); err != nil {
		t.Fatalf("decode generation payload: %v", err)
	}
	if payload["user_message"] != testClaim().UserMessage ||
		payload["assistant_message"] != testClaim().AssistantMessage {
		t.Fatalf("generation payload = %#v", payload)
	}
}

func TestServiceRejectsInvalidGenerationResponses(t *testing.T) {
	tests := map[string]GenerationResult{
		"wrong provider": {
			Provider: "other",
			Model:    "fake-model",
			Content:  `{"title":"面试准备"}`,
		},
		"extra field": {
			Provider: "fake",
			Model:    "fake-model",
			Content:  `{"title":"面试准备","other":true}`,
		},
		"trailing value": {
			Provider: "fake",
			Model:    "fake-model",
			Content:  `{"title":"面试准备"}{}`,
		},
		"oversized title": {
			Provider: "fake",
			Model:    "fake-model",
			Content: `{"title":"` +
				strings.Repeat("面", MaxTitleRunes+1) + `"}`,
		},
	}
	for name, result := range tests {
		t.Run(name, func(t *testing.T) {
			service, err := NewService(
				&recordingGenerator{result: result},
				testConfiguration(),
			)
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			if _, err := service.GenerateTitle(
				context.Background(),
				testClaim(),
			); err != ErrInvalidResponse {
				t.Fatalf("GenerateTitle error = %v", err)
			}
		})
	}
}

type recordingGenerator struct {
	result   GenerationResult
	err      error
	requests []GenerationRequest
}

func (generator *recordingGenerator) GenerateJSON(
	ctx context.Context,
	request GenerationRequest,
) (GenerationResult, error) {
	if err := ctx.Err(); err != nil {
		return GenerationResult{}, err
	}
	generator.requests = append(generator.requests, request)
	return generator.result, generator.err
}

func testConfiguration() Configuration {
	return Configuration{
		PromptVersion: "thread-title-prompt-v1",
		Provider:      "fake",
		Model:         "fake-model",
	}
}

func testClaim() JobClaim {
	return JobClaim{Job: Job{
		SourceRunID:      "10000000-0000-4000-8000-000000000001",
		OwnerID:          "20000000-0000-4000-8000-000000000001",
		ThreadID:         "30000000-0000-4000-8000-000000000001",
		UserMessage:      "你好，我想准备产品经理模拟面试。",
		AssistantMessage: "可以，我们先梳理岗位信息和面试重点。",
		Status:           JobRunning,
		AttemptCount:     1,
		LeaseToken:       "40000000-0000-4000-8000-000000000001",
		LeaseExpiresAt:   time.Now().Add(time.Minute),
		PromptVersion:    "thread-title-prompt-v1",
		Provider:         "fake",
		Model:            "fake-model",
	}}
}
