package summary

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

func TestGeneratorServiceReturnsStrictContent(t *testing.T) {
	t.Parallel()
	generator := &generatorStub{result: GenerationResult{
		Provider: "qianwen", Model: "qwen-plus",
		Content: `{"current_intents":["Prepare"],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`,
	}}
	service, err := NewGeneratorService(generator, Configuration{
		Provider: "qianwen", Model: "qwen-plus",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := service.Generate(context.Background(), GenerateCommand{
		Messages:           []conversation.Message{summaryMessage(1, "Prepare")},
		MaxInputCharacters: 12000,
	})
	if err != nil || !content.Valid() || len(content.CurrentIntents) != 1 {
		t.Fatalf("content=%#v error=%v", content, err)
	}
	if generator.request.SystemPrompt == "" || generator.request.UserPrompt == "" {
		t.Fatal("provider request was not populated")
	}
}

func TestGeneratorServiceRejectsProviderDrift(t *testing.T) {
	t.Parallel()
	service, err := NewGeneratorService(&generatorStub{result: GenerationResult{
		Provider: "qiniu", Model: "qwen-plus",
		Content: `{"current_intents":["Prepare"],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`,
	}}, Configuration{Provider: "qianwen", Model: "qwen-plus"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Generate(context.Background(), GenerateCommand{
		Messages:           []conversation.Message{summaryMessage(1, "Prepare")},
		MaxInputCharacters: 12000,
	})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("error=%v", err)
	}
}

type generatorStub struct {
	request GenerationRequest
	result  GenerationResult
	err     error
}

func (generator *generatorStub) GenerateJSON(
	_ context.Context,
	request GenerationRequest,
) (GenerationResult, error) {
	generator.request = request
	return generator.result, generator.err
}

func summaryMessage(sequence int64, content string) conversation.Message {
	return conversation.Message{
		ID:        "30000000-0000-4000-8000-000000000001",
		OwnerID:   "10000000-0000-4000-8000-000000000001",
		ThreadID:  "20000000-0000-4000-8000-000000000001",
		Sequence:  sequence,
		Role:      conversation.MessageRoleUser,
		Modality:  conversation.MessageModalityText,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}
