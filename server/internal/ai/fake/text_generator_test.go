package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

func TestTextGeneratorReturnsDeterministicResult(t *testing.T) {
	t.Parallel()

	expected := ai.TextResult{
		ID:           "fake-1",
		Provider:     "fake",
		Model:        "deterministic",
		Content:      "A stable answer.",
		FinishReason: "stop",
	}
	generator := NewTextGenerator(expected)
	request := ai.TextRequest{Messages: []ai.TextMessage{{
		Role:    ai.TextRoleUser,
		Content: "question",
	}}}

	first, err := generator.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("first generation failed: %v", err)
	}
	second, err := generator.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("second generation failed: %v", err)
	}
	if first != expected || second != expected {
		t.Fatalf("fake result changed: first=%#v second=%#v", first, second)
	}
}

func TestTextGeneratorRespectsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewTextGenerator(ai.TextResult{}).Generate(ctx, ai.TextRequest{})
	var generationError *ai.GenerationError
	if !errors.As(err, &generationError) ||
		generationError.Kind != ai.ErrorCancelled ||
		!generationError.Retryable() {
		t.Fatalf("expected cancelled generation error, got %v", err)
	}
}

func TestTextGeneratorCanFailDeterministically(t *testing.T) {
	t.Parallel()

	expected := errors.New("controlled failure")
	generator := NewFailingTextGenerator(expected)
	_, err := generator.Generate(context.Background(), ai.TextRequest{
		Messages: []ai.TextMessage{{Role: ai.TextRoleUser, Content: "question"}},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected controlled error, got %v", err)
	}
}
